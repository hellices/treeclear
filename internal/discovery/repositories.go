package discovery

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/git"
	"github.com/hellices/treeclear/internal/pathutil"
)

type Repository struct {
	Root         string
	CommonGitDir string
}

type GitClient interface {
	CommonGitDir(context.Context, string) (string, error)
	ListWorktrees(context.Context, string) ([]domain.Worktree, error)
}

type Finder struct {
	Git     GitClient
	DataDir string
	ReadDir func(string) ([]os.DirEntry, error)
}

func Find(ctx context.Context, roots []string) ([]Repository, []error) {
	return (Finder{}).Find(ctx, roots)
}

func (finder Finder) Find(ctx context.Context, roots []string) ([]Repository, []error) {
	if len(roots) == 0 {
		return nil, nil
	}
	if finder.Git == nil {
		finder.Git = git.NewClient(nil)
	}
	if finder.ReadDir == nil {
		finder.ReadDir = os.ReadDir
	}
	var repositories []Repository
	var failures []error
	record := func(operation, path string, err error) {
		failures = append(failures, &fs.PathError{Op: operation, Path: path, Err: err})
	}
	dataDirectory, err := exclusionPath(finder.DataDir)
	if err != nil {
		record("data directory", finder.DataDir, err)
		return nil, failures
	}
	excluded := func(path string) bool {
		for _, component := range strings.FieldsFunc(path, func(character rune) bool { return character == rune(filepath.Separator) }) {
			if ignoredName(component) {
				return true
			}
		}
		return dataDirectory != "" && (containsPath(dataDirectory, path) || pathutil.Contains(dataDirectory, path))
	}
	consider := func(path string, metadata fs.FileInfo) {
		if metadata.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || (!metadata.IsDir() && !metadata.Mode().IsRegular()) {
			record("repository marker", filepath.Join(path, ".git"), errors.New("Git metadata must be a directory or regular gitfile, not a link"))
			return
		}
		common, err := finder.Git.CommonGitDir(ctx, path)
		if err == nil {
			common, err = safeDirectory(common)
		}
		if err != nil {
			record("repository", path, err)
			return
		}
		for _, repository := range repositories {
			if sameDirectory(repository.CommonGitDir, common) {
				return
			}
		}
		repository, err := finder.primaryAnchor(ctx, path, common)
		if err != nil {
			record("repository", path, err)
		} else if !excluded(repository.Root) && (dataDirectory == "" || !pathutil.Contains(dataDirectory, common)) {
			repositories = append(repositories, repository)
		}
	}
	visited := make(map[string]bool)
	lastPath := roots[0]
	var walk func(string)
	walk = func(path string) {
		lastPath = path
		if err := ctx.Err(); err != nil {
			record("discover", path, err)
			return
		}
		if excluded(path) || visited[path] {
			return
		}
		visited[path] = true
		if _, err := safeDirectory(path); err != nil {
			record("directory", path, err)
			return
		}
		marker := filepath.Join(path, ".git")
		metadata, err := os.Lstat(marker)
		switch {
		case err == nil:
			consider(path, metadata)
		case !errors.Is(err, fs.ErrNotExist):
			record("repository marker", marker, err)
		}
		if ctx.Err() != nil {
			return
		}
		entries, err := finder.ReadDir(path)
		if err != nil {
			record("read directory", path, err)
			return
		}
		if err := ctx.Err(); err != nil {
			record("discover", path, err)
			return
		}
		entries = append([]os.DirEntry(nil), entries...)
		sort.Slice(entries, func(first, second int) bool { return entries[first].Name() < entries[second].Name() })
		for _, entry := range entries {
			if ignoredName(entry.Name()) || entry.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 || !entry.IsDir() {
				continue
			}
			if metadata != nil && metadata.IsDir() {
				entryInfo, err := entry.Info()
				if err != nil {
					record("directory entry", filepath.Join(path, entry.Name()), err)
					continue
				}
				if os.SameFile(metadata, entryInfo) {
					continue
				}
			}
			walk(filepath.Join(path, entry.Name()))
			if ctx.Err() != nil {
				return
			}
		}
	}
	roots = append([]string(nil), roots...)
	sort.Strings(roots)
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			record("discover", root, err)
			break
		}
		canonical, err := safeDirectory(root)
		if err != nil {
			record("root", root, err)
			continue
		}
		if excluded(canonical) {
			continue
		}
		for ancestor := canonical; ; ancestor = filepath.Dir(ancestor) {
			marker := filepath.Join(ancestor, ".git")
			metadata, err := os.Lstat(marker)
			if err == nil {
				if ancestor != canonical {
					consider(ancestor, metadata)
				}
				break
			}
			if !errors.Is(err, fs.ErrNotExist) {
				if ancestor != canonical {
					record("repository marker", marker, err)
				}
				break
			}
			if ctx.Err() != nil || filepath.Dir(ancestor) == ancestor {
				break
			}
		}
		walk(canonical)
		if ctx.Err() != nil {
			break
		}
	}
	if err := ctx.Err(); err != nil && !errors.Is(errors.Join(failures...), err) {
		record("discover", lastPath, err)
	}
	sort.Slice(repositories, func(first, second int) bool {
		if repositories[first].Root == repositories[second].Root {
			return repositories[first].CommonGitDir < repositories[second].CommonGitDir
		}
		return repositories[first].Root < repositories[second].Root
	})
	sort.SliceStable(failures, func(first, second int) bool {
		left, right := failures[first].(*fs.PathError), failures[second].(*fs.PathError)
		if left.Path == right.Path {
			return left.Error() < right.Error()
		}
		return left.Path < right.Path
	})
	return repositories, failures
}

func (finder Finder) primaryAnchor(ctx context.Context, path, common string) (Repository, error) {
	worktrees, err := finder.Git.ListWorktrees(ctx, path)
	if err != nil {
		return Repository{}, err
	}
	var primary string
	member := false
	for _, worktree := range worktrees {
		if !sameDirectory(worktree.CommonGitDir, common) {
			return Repository{}, errors.New("conflicting common Git directory identity")
		}
		member = member || sameDirectory(worktree.Path, path)
		if worktree.Primary {
			if primary != "" {
				return Repository{}, errors.New("multiple primary worktrees")
			}
			primary, err = safeDirectory(worktree.Path)
			if err != nil {
				return Repository{}, fmt.Errorf("primary worktree: %w", err)
			}
		}
	}
	if primary == "" || !member {
		return Repository{}, errors.New("repository has no verified primary anchor or registered worktree")
	}
	for _, worktree := range worktrees {
		if !sameDirectory(worktree.RepositoryRoot, primary) {
			return Repository{}, errors.New("conflicting primary repository identity")
		}
	}
	return Repository{Root: primary, CommonGitDir: common}, nil
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

func containsPath(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && filepath.IsLocal(relative)
}

func ignoredName(name string) bool {
	if runtime.GOOS == "windows" {
		name = strings.ToLower(name)
	}
	return name == ".git" || name == "node_modules" || name == ".cache" || name == "target"
}
