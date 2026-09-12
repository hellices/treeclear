package inventory

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/hellices/treeclear/internal/discovery"
	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/git"
	"github.com/hellices/treeclear/internal/pathutil"
)

type GitClient interface {
	CommonGitDir(context.Context, string) (string, error)
	ListWorktrees(context.Context, string) ([]domain.Worktree, error)
	InspectWorktree(context.Context, string, domain.Worktree) (domain.Worktree, error)
}

type RepositoryFinder interface {
	Find(context.Context, []string) ([]discovery.Repository, []error)
}

type Loader struct {
	Git     GitClient
	Finder  RepositoryFinder
	CWD     func() (string, error)
	DataDir string
	ReadDir func(string) ([]os.DirEntry, error)
}

type repositoryResult struct {
	worktrees  []domain.Worktree
	failures   []error
	incomplete []error
}

func (loader Loader) Load(ctx context.Context, roots []string) ([]domain.Worktree, []error) {
	if len(roots) == 0 {
		return nil, nil
	}
	dataDirectory, err := exclusionPath(loader.DataDir)
	if err != nil {
		return nil, []error{pathError("data directory", loader.DataDir, err)}
	}
	loader.DataDir = dataDirectory
	if loader.Git == nil {
		loader.Git = git.NewClient(nil)
	}
	if loader.CWD == nil {
		loader.CWD = os.Getwd
	}
	if loader.ReadDir == nil {
		loader.ReadDir = os.ReadDir
	}
	if loader.Finder == nil {
		loader.Finder = discovery.Finder{Git: loader.Git, DataDir: loader.DataDir, ReadDir: loader.ReadDir}
	}
	repositories, discoveryErrors := loader.Finder.Find(ctx, roots)
	failures := append([]error(nil), discoveryErrors...)
	incomplete := append([]error(nil), discoveryErrors...)
	var unique []discovery.Repository
	for _, repository := range repositories {
		root, rootError := safeDirectory(repository.Root)
		common, commonError := safeDirectory(repository.CommonGitDir)
		if err := errors.Join(rootError, commonError); err != nil {
			path := repository.Root
			if commonError == nil {
				path = common
			}
			failure := pathError("repository identity", path, err)
			failures, incomplete = append(failures, failure), append(incomplete, failure)
			continue
		}
		duplicate := false
		for _, existing := range unique {
			if sameDirectory(existing.CommonGitDir, common) {
				duplicate = true
				if !sameDirectory(existing.Root, root) {
					failure := pathError("repository identity", common, fmt.Errorf("common Git directory has conflicting primary anchors %q and %q", existing.Root, root))
					failures, incomplete = append(failures, failure), append(incomplete, failure)
				}
				break
			}
		}
		if !duplicate {
			unique = append(unique, discovery.Repository{Root: root, CommonGitDir: common})
		}
	}
	if len(unique) == 0 {
		return nil, sortedErrors(failures)
	}
	sort.Slice(unique, func(first, second int) bool { return unique[first].Root < unique[second].Root })
	cwd, cwdError := loader.CWD()
	originalCWD := cwd
	if cwdError == nil {
		cwd, cwdError = pathutil.Canonical(cwd)
		if cwdError == nil {
			metadata, err := os.Stat(cwd)
			cwdError = err
			if err == nil && !metadata.IsDir() {
				cwdError = errors.New("current working directory is not a directory")
			}
		}
	}
	if cwdError != nil {
		cwdError = pathError("current working directory", originalCWD, cwdError)
		failures = append(failures, cwdError)
	}
	results := make([]repositoryResult, len(unique))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), 8, len(unique)) {
		workers.Go(func() {
			for index := range jobs {
				results[index] = loader.loadRepository(ctx, unique[index], cwd)
			}
		})
	}
	for index := range unique {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	var worktrees []domain.Worktree
	for _, result := range results {
		worktrees = append(worktrees, result.worktrees...)
		failures = append(failures, result.failures...)
		incomplete = append(incomplete, result.incomplete...)
	}
	var cancellation error
	if err := ctx.Err(); err != nil {
		cancellation = pathError("inventory", roots[0], err)
		failures = append(failures, cancellation)
	}
	for index := range worktrees {
		worktree := &worktrees[index]
		if cancellation != nil {
			protect(worktree, cancellation)
		}
		if cwdError != nil {
			protect(worktree, cwdError)
		}
		for _, failure := range incomplete {
			var location *fs.PathError
			if !errors.As(failure, &location) || location.Path == "" || overlap(worktree.Path, location.Path) || overlap(worktree.CommonGitDir, location.Path) {
				protect(worktree, failure)
			}
		}
		for _, repository := range unique {
			if worktree.Primary && sameDirectory(worktree.CommonGitDir, repository.CommonGitDir) {
				continue
			}
			if overlap(worktree.Path, repository.CommonGitDir) {
				failure := pathError("path safety", worktree.Path, fmt.Errorf("worktree overlaps Git metadata %q", repository.CommonGitDir))
				protect(worktree, failure)
				failures = append(failures, failure)
			}
		}
		for other := 0; other < index; other++ {
			if overlap(worktree.Path, worktrees[other].Path) {
				for _, pair := range [][2]*domain.Worktree{{worktree, &worktrees[other]}, {&worktrees[other], worktree}} {
					failure := pathError("path safety", pair[0].Path, fmt.Errorf("overlapping worktree %q", pair[1].Path))
					protect(pair[0], failure)
					failures = append(failures, failure)
				}
			}
		}
	}
	for index := range worktrees {
		sort.Strings(worktrees[index].CollectionErrors)
	}
	sort.SliceStable(worktrees, func(first, second int) bool {
		if worktrees[first].Path == worktrees[second].Path {
			return worktrees[first].CommonGitDir < worktrees[second].CommonGitDir
		}
		return worktrees[first].Path < worktrees[second].Path
	})
	return worktrees, sortedErrors(failures)
}

func (loader Loader) loadRepository(ctx context.Context, repository discovery.Repository, cwd string) repositoryResult {
	var result repositoryResult
	if err := ctx.Err(); err != nil {
		failure := pathError("list worktrees", repository.Root, err)
		result.failures, result.incomplete = []error{failure}, []error{failure}
		return result
	}
	listed, err := loader.Git.ListWorktrees(ctx, repository.Root)
	if err != nil || len(listed) == 0 {
		if err == nil {
			err = errors.New("Git returned no worktrees")
		}
		failure := pathError("list worktrees", repository.Root, err)
		result.failures, result.incomplete = []error{failure}, []error{failure}
		for _, worktree := range listed {
			worktree.CollectionErrors = append([]string(nil), worktree.CollectionErrors...)
			protect(&worktree, failure)
			result.worktrees = append(result.worktrees, worktree)
		}
		return result
	}
	for _, worktree := range listed {
		worktree, failures := loader.enrich(ctx, repository, worktree, cwd)
		result.worktrees = append(result.worktrees, worktree)
		result.failures = append(result.failures, failures...)
	}
	return result
}

func (loader Loader) enrich(ctx context.Context, repository discovery.Repository, worktree domain.Worktree, cwd string) (domain.Worktree, []error) {
	worktree.Path = filepath.FromSlash(worktree.Path)
	worktree.GitStateKnown, worktree.PathSafe, worktree.Current, worktree.EstimatedBytes = false, false, false, 0
	worktree.CollectionErrors = append([]string(nil), worktree.CollectionErrors...)
	var failures []error
	record := func(operation, path string, err error) {
		failure := pathError(operation, path, err)
		protect(&worktree, failure)
		failures = append(failures, failure)
	}
	canonical, err := pathutil.Canonical(worktree.Path)
	if err != nil {
		record("worktree path", worktree.Path, err)
		return worktree, failures
	}
	worktree.Primary = sameDirectory(canonical, repository.Root)
	worktree.Current = cwd != "" && pathutil.Contains(canonical, cwd)
	if _, err := safeDirectory(worktree.Path); err != nil {
		record("worktree path", worktree.Path, err)
		return worktree, failures
	}
	worktree.Path = canonical
	if !sameDirectory(worktree.RepositoryRoot, repository.Root) || !sameDirectory(worktree.CommonGitDir, repository.CommonGitDir) {
		record("worktree identity", canonical, errors.New("worktree repository identity conflicts with discovery"))
		return worktree, failures
	}
	worktree.RepositoryRoot, worktree.CommonGitDir = repository.Root, repository.CommonGitDir
	if loader.DataDir != "" && overlap(canonical, loader.DataDir) {
		record("path safety", canonical, errors.New("worktree overlaps the Treeclear data directory"))
		return worktree, failures
	}
	marker := filepath.Join(canonical, ".git")
	metadata, err := os.Lstat(marker)
	if err == nil && (metadata.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || (!metadata.IsDir() && !metadata.Mode().IsRegular())) {
		err = errors.New("Git marker is a link or non-regular file")
	}
	if err != nil {
		record("worktree marker", marker, err)
		return worktree, failures
	}
	if err := ctx.Err(); err != nil {
		record("inspect worktree", canonical, err)
		return worktree, failures
	}
	primary, current := worktree.Primary, worktree.Current
	worktree.PathSafe = true
	worktree, err = loader.Git.InspectWorktree(ctx, repository.Root, worktree)
	worktree.Primary, worktree.Current = primary, current
	if worktree.Path != canonical || !sameDirectory(worktree.RepositoryRoot, repository.Root) || !sameDirectory(worktree.CommonGitDir, repository.CommonGitDir) {
		record("worktree identity", canonical, errors.New("worktree identity changed during inspection"))
		worktree.Path, worktree.RepositoryRoot = canonical, repository.Root
	}
	if err != nil {
		failure := pathError("inspect worktree", canonical, err)
		worktree.GitStateKnown = false
		worktree.CollectionErrors = append(worktree.CollectionErrors, failure.Error())
		failures = append(failures, failure)
	} else if !worktree.GitStateKnown || len(worktree.CollectionErrors) != 0 {
		failure := pathError("inspect worktree", canonical, errors.New("Git collection is incomplete"))
		worktree.GitStateKnown = false
		worktree.CollectionErrors = append(worktree.CollectionErrors, failure.Error())
		failures = append(failures, failure)
	}
	if worktree.PathSafe {
		var sizeErrors []error
		worktree.EstimatedBytes, sizeErrors = loader.estimateBytes(ctx, canonical)
		for _, failure := range sizeErrors {
			protect(&worktree, failure)
			failures = append(failures, failure)
		}
	}
	return worktree, failures
}

func (loader Loader) estimateBytes(ctx context.Context, root string) (int64, []error) {
	var total int64
	var failures []error
	var walk func(string)
	walk = func(directory string) {
		if err := ctx.Err(); err != nil {
			failures = append(failures, pathError("estimate bytes", directory, err))
			return
		}
		if _, err := safeDirectory(directory); err != nil {
			failures = append(failures, pathError("estimate bytes", directory, err))
			return
		}
		if directory != root {
			marker := filepath.Join(directory, ".git")
			if _, err := os.Lstat(marker); err == nil {
				failures = append(failures, pathError("path safety", directory, errors.New("nested repository metadata overlaps worktree")))
				return
			} else if !errors.Is(err, fs.ErrNotExist) {
				failures = append(failures, pathError("path safety", marker, err))
				return
			}
		}
		entries, err := loader.ReadDir(directory)
		if err != nil {
			failures = append(failures, pathError("estimate bytes", directory, err))
			return
		}
		if err := ctx.Err(); err != nil {
			failures = append(failures, pathError("estimate bytes", directory, err))
			return
		}
		entries = append([]os.DirEntry(nil), entries...)
		sort.Slice(entries, func(first, second int) bool { return entries[first].Name() < entries[second].Name() })
		for _, entry := range entries {
			path := filepath.Join(directory, entry.Name())
			metadata, err := os.Lstat(path)
			if err != nil {
				failures = append(failures, pathError("estimate bytes", path, err))
				continue
			}
			switch {
			case metadata.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0:
				continue
			case metadata.IsDir():
				walk(path)
			case metadata.Mode().IsRegular():
				if metadata.Size() < 0 || metadata.Size() > math.MaxInt64-total {
					failures = append(failures, pathError("estimate bytes", path, errors.New("file size overflows byte estimate")))
				} else {
					total += metadata.Size()
				}
			default:
				failures = append(failures, pathError("estimate bytes", path, errors.New("cannot estimate a non-regular file")))
			}
			if ctx.Err() != nil {
				return
			}
		}
	}
	walk(root)
	if err := ctx.Err(); err != nil && !errors.Is(errors.Join(failures...), err) {
		failures = append(failures, pathError("estimate bytes", root, err))
	}
	if len(failures) != 0 {
		return 0, failures
	}
	return total, nil
}

func safeDirectory(path string) (string, error) {
	canonical, err := pathutil.Canonical(path)
	if err != nil {
		return "", err
	}
	metadata, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !metadata.IsDir() {
		return "", errors.New("path is not a directory")
	}
	path = filepath.FromSlash(path)
	volumeLength := len(filepath.VolumeName(path))
	for component := strings.TrimRight(path, string(filepath.Separator)); len(component) > volumeLength; {
		metadata, err := os.Lstat(component)
		if err != nil {
			return "", err
		}
		if metadata.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return "", fmt.Errorf("symlink, junction, or irregular path component %q", component)
		}
		separator := strings.LastIndexByte(component, filepath.Separator)
		if separator <= volumeLength {
			break
		}
		component = strings.TrimRight(component[:separator], string(filepath.Separator))
	}
	return canonical, nil
}

func sameDirectory(first, second string) bool {
	if first == "" || second == "" {
		return false
	}
	firstInfo, firstError := os.Stat(first)
	secondInfo, secondError := os.Stat(second)
	return firstError == nil && secondError == nil && firstInfo.IsDir() && secondInfo.IsDir() && os.SameFile(firstInfo, secondInfo)
}

func exclusionPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	canonical, err := pathutil.Canonical(path)
	if err == nil {
		metadata, statError := os.Stat(canonical)
		if statError != nil {
			return "", statError
		}
		if !metadata.IsDir() {
			return "", errors.New("Treeclear data path is not a directory")
		}
		return canonical, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if _, statError := os.Lstat(path); statError == nil {
		return "", err
	} else if !errors.Is(statError, fs.ErrNotExist) {
		return "", statError
	}
	for _, component := range strings.FieldsFunc(filepath.FromSlash(path), func(character rune) bool { return character == rune(filepath.Separator) }) {
		if component == ".." {
			return "", err
		}
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	canonicalParent, parentError := exclusionPath(parent)
	if parentError != nil {
		return "", parentError
	}
	return filepath.Join(canonicalParent, filepath.Base(path)), nil
}

func overlap(first, second string) bool {
	if first == "" || second == "" {
		return false
	}
	if pathutil.Contains(first, second) || pathutil.Contains(second, first) {
		return true
	}
	first, firstError := filepath.Abs(first)
	second, secondError := filepath.Abs(second)
	if firstError != nil || secondError != nil {
		return true
	}
	for _, pair := range [][2]string{{first, second}, {second, first}} {
		relative, err := filepath.Rel(pair[0], pair[1])
		if err == nil && filepath.IsLocal(relative) {
			return true
		}
	}
	return false
}

func protect(worktree *domain.Worktree, failure error) {
	worktree.PathSafe, worktree.GitStateKnown = false, false
	worktree.CollectionErrors = append(worktree.CollectionErrors, failure.Error())
}

func pathError(operation, path string, err error) error {
	return &fs.PathError{Op: operation, Path: path, Err: err}
}

func sortedErrors(failures []error) []error {
	sort.SliceStable(failures, func(first, second int) bool {
		var left, right *fs.PathError
		var leftPath, rightPath string
		if errors.As(failures[first], &left) {
			leftPath = left.Path
		}
		if errors.As(failures[second], &right) {
			rightPath = right.Path
		}
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return failures[first].Error() < failures[second].Error()
	})
	return failures
}
