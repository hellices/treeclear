package inventory

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/discovery"
	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/git"
	"github.com/hellices/treeclear/internal/pathutil"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestLoaderDeduplicatesCommonGitDirectories(test *testing.T) {
	repository := testutil.NewRepository(test)
	linked := repository.AddWorktree(test, "feature", "feature/one")
	loader := newTestLoader(repository.Root)
	worktrees, failures := loader.Load(context.Background(), []string{repository.Root, linked})
	if len(failures) != 0 || len(worktrees) != 2 {
		test.Fatalf("Load() = %#v, %v", worktrees, failures)
	}
	for _, worktree := range worktrees {
		if worktree.Primary != (worktree.Path == repository.Root) || !worktree.PathSafe || !worktree.GitStateKnown {
			test.Errorf("incorrect identity or unknown state: %#v", worktree)
		}
		if !worktree.Status.Clean() || !worktree.Recoverable || len(worktree.Head) != 40 || len(worktree.IndexHash) != 64 || len(worktree.AdminHash) != 64 || worktree.LastCommitAt.IsZero() || worktree.MetadataModifiedAt.IsZero() || worktree.EstimatedBytes <= 0 {
			test.Errorf("missing Git or size enrichment: %#v", worktree)
		}
	}
}

func TestLoaderRecognizesDescendantCWD(test *testing.T) {
	repository := testutil.NewRepository(test)
	linked := repository.AddWorktree(test, "feature", "feature/one")
	cwd := filepath.Join(linked, "subdirectory")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		test.Fatal(err)
	}
	worktrees, failures := newTestLoader(cwd).Load(context.Background(), []string{repository.Root})
	if len(failures) != 0 || len(worktrees) != 2 {
		test.Fatalf("Load() = %#v, %v", worktrees, failures)
	}
	for _, worktree := range worktrees {
		if worktree.Current != (worktree.Path == linked) {
			test.Errorf("incorrect descendant cwd association: %#v", worktree)
		}
	}
}

func TestLoaderRecognizesCurrentWorktreeEvenWhenPathIsUnsafe(test *testing.T) {
	repository := testutil.NewRepository(test)
	linked := repository.AddWorktree(test, "feature", "feature/one")
	cwd := filepath.Join(linked, "subdirectory")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		test.Fatal(err)
	}
	alias := filepath.Join(filepath.Dir(repository.Root), "alias")
	makeSymlink(test, linked, alias)
	loader := newTestLoader(cwd)
	client := loader.Git
	loader.Finder = staticFinder{{Root: repository.Root, CommonGitDir: filepath.Join(repository.Root, ".git")}}
	loader.Git = inventoryGit{GitClient: client, list: func(ctx context.Context, root string) ([]domain.Worktree, error) {
		worktrees, err := client.ListWorktrees(ctx, root)
		worktrees[1].Path = alias
		return worktrees, err
	}}
	worktrees, failures := loader.Load(context.Background(), []string{repository.Root})
	if len(failures) == 0 {
		test.Fatal("unsafe alias was not reported")
	}
	worktree := findWorktree(test, worktrees, alias)
	assertUnknownUnsafe(test, worktree)
	if !worktree.Current {
		test.Errorf("unsafe current worktree lost its known CWD association: %#v", worktree)
	}
}

func TestLoaderDeduplicatesInjectedFinderAndRejectsConflictingAnchors(test *testing.T) {
	for _, conflicting := range []bool{false, true} {
		test.Run(fmt.Sprintf("conflict=%v", conflicting), func(test *testing.T) {
			repository := testutil.NewRepository(test)
			anchor := discovery.Repository{Root: repository.Root, CommonGitDir: filepath.Join(repository.Root, ".git")}
			other := anchor
			if conflicting {
				other.Root = filepath.Join(filepath.Dir(repository.Root), "other")
				if err := os.Mkdir(other.Root, 0o700); err != nil {
					test.Fatal(err)
				}
			}
			loader := newTestLoader(repository.Root)
			loader.Finder = staticFinder{anchor, other}
			client := loader.Git
			var lists atomic.Int32
			loader.Git = inventoryGit{GitClient: client, list: func(ctx context.Context, root string) ([]domain.Worktree, error) {
				lists.Add(1)
				return client.ListWorktrees(ctx, root)
			}}
			worktrees, failures := loader.Load(context.Background(), []string{repository.Root})
			if len(worktrees) != 1 || lists.Load() != 1 {
				test.Fatalf("common directory was not deduplicated: %#v, %v, list calls = %d", worktrees, failures, lists.Load())
			}
			if conflicting {
				if len(failures) == 0 {
					test.Fatal("conflicting anchors were not reported")
				}
				assertUnknownUnsafe(test, worktrees[0])
			} else if len(failures) != 0 || !worktrees[0].GitStateKnown {
				test.Fatalf("matching anchors were rejected: %#v, %v", worktrees, failures)
			}
		})
	}
}

func TestLoaderPreservesLockedDetachedAndDirtyState(test *testing.T) {
	repository := testutil.NewRepository(test)
	locked := repository.AddWorktree(test, "locked", "locked")
	detached := repository.AddWorktree(test, "detached", "detached")
	dirty := repository.AddWorktree(test, "dirty", "dirty")
	repository.Git(test, "worktree", "lock", "--reason", "busy", "--", locked)
	repository.Git(test, "-C", detached, "checkout", "--detach", "HEAD")
	if err := os.WriteFile(filepath.Join(dirty, "seed.txt"), []byte("changed\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	worktrees, failures := newTestLoader(repository.Root).Load(context.Background(), []string{repository.Root})
	if len(failures) != 0 || len(worktrees) != 4 {
		test.Fatalf("Load() = %#v, %v", worktrees, failures)
	}
	if worktree := findWorktree(test, worktrees, locked); !worktree.Locked || worktree.LockReason != "busy" {
		test.Errorf("lost lock state: %#v", worktree)
	}
	if worktree := findWorktree(test, worktrees, detached); !worktree.Detached || worktree.Branch != "" {
		test.Errorf("lost detached state: %#v", worktree)
	}
	if worktree := findWorktree(test, worktrees, dirty); worktree.Status.Unstaged != 1 || !worktree.GitStateKnown {
		test.Errorf("lost dirty state: %#v", worktree)
	}
}

func TestLoaderProtectsNestedRepositoryOverlap(test *testing.T) {
	repository := testutil.NewRepository(test)
	linked := repository.AddWorktree(test, "feature", "feature/one")
	nested := testutil.NewRepository(test)
	nestedRoot := filepath.Join(linked, "nested")
	if err := os.Rename(nested.Root, nestedRoot); err != nil {
		test.Fatal(err)
	}
	for _, roots := range [][]string{{repository.Root}, {repository.Root, linked}} {
		worktrees, failures := newTestLoader(repository.Root).Load(context.Background(), roots)
		if len(failures) == 0 {
			test.Fatal("overlap was not reported")
		}
		assertUnknownUnsafe(test, findWorktree(test, worktrees, linked))
		if len(roots) == 2 {
			assertUnknownUnsafe(test, findWorktree(test, worktrees, nestedRoot))
		}
	}
}

func TestLoaderProtectsNestedRepositoryWithCaseAliasedGitMarker(test *testing.T) {
	repository := testutil.NewRepository(test)
	linked := repository.AddWorktree(test, "feature", "feature/one")
	nested := testutil.NewRepository(test)
	marker := filepath.Join(nested.Root, ".git")
	if err := os.Rename(marker, filepath.Join(nested.Root, ".GIT")); err != nil {
		test.Fatal(err)
	}
	if _, err := os.Stat(marker); errors.Is(err, fs.ErrNotExist) {
		test.Skip("filesystem has case-sensitive directory names")
	} else if err != nil {
		test.Fatal(err)
	}
	if err := os.Rename(nested.Root, filepath.Join(linked, "nested")); err != nil {
		test.Fatal(err)
	}
	worktrees, failures := newTestLoader(repository.Root).Load(context.Background(), []string{repository.Root})
	if len(failures) == 0 {
		test.Fatal("case-aliased nested repository was not detected")
	}
	assertUnknownUnsafe(test, findWorktree(test, worktrees, linked))
}

func TestLoaderRejectsSymlinkWorktreeAndAncestor(test *testing.T) {
	for _, throughAncestor := range []bool{false, true} {
		test.Run(fmt.Sprintf("ancestor=%v", throughAncestor), func(test *testing.T) {
			repository := testutil.NewRepository(test)
			linked := repository.AddWorktree(test, "feature", "feature/one")
			moved := filepath.Join(filepath.Dir(repository.Root), "moved")
			if err := os.Rename(linked, moved); err != nil {
				test.Fatal(err)
			}
			alias := linked
			if throughAncestor {
				alias = filepath.Join(filepath.Dir(repository.Root), "alias")
				makeSymlink(test, filepath.Dir(repository.Root), alias)
				linked = filepath.Join(alias, "moved")
			} else {
				makeSymlink(test, moved, alias)
			}
			client := git.NewClient(nil)
			loader := newTestLoader(repository.Root)
			loader.Finder = staticFinder{{Root: repository.Root, CommonGitDir: filepath.Join(repository.Root, ".git")}}
			loader.Git = inventoryGit{
				GitClient: client,
				list: func(ctx context.Context, root string) ([]domain.Worktree, error) {
					worktrees, err := client.ListWorktrees(ctx, root)
					worktrees[1].Path = linked
					return worktrees, err
				},
				inspect: func(ctx context.Context, root string, worktree domain.Worktree) (domain.Worktree, error) {
					if worktree.Path != repository.Root {
						test.Error("inspected an unsafe symlink path")
					}
					return client.InspectWorktree(ctx, root, worktree)
				},
			}
			worktrees, failures := loader.Load(context.Background(), []string{repository.Root})
			if len(worktrees) != 2 || len(failures) == 0 {
				test.Fatalf("Load() = %#v, %v", worktrees, failures)
			}
			for _, worktree := range worktrees {
				if !worktree.Primary {
					assertUnknownUnsafe(test, worktree)
				}
			}
		})
	}
}

func TestLoaderEstimatesBytesWithoutFollowingLinks(test *testing.T) {
	repository := testutil.NewRepository(test)
	linked := repository.AddWorktree(test, "feature", "feature/one")
	loader := newTestLoader(repository.Root)
	before, failures := loader.Load(context.Background(), []string{repository.Root})
	if len(failures) != 0 {
		test.Fatal(failures)
	}
	var additional int64
	for _, name := range []string{"node_modules", ".cache", "target"} {
		directory := filepath.Join(linked, name)
		if err := os.Mkdir(directory, 0o700); err != nil {
			test.Fatal(err)
		}
		contents := []byte("count these regular bytes")
		if err := os.WriteFile(filepath.Join(directory, "payload"), contents, 0o600); err != nil {
			test.Fatal(err)
		}
		additional += int64(len(contents))
	}
	external := test.TempDir()
	if err := os.WriteFile(filepath.Join(external, "large"), make([]byte, 1<<20), 0o600); err != nil {
		test.Fatal(err)
	}
	makeSymlink(test, external, filepath.Join(linked, "external"))
	makeSymlink(test, linked, filepath.Join(linked, "loop"))
	after, failures := loader.Load(context.Background(), []string{repository.Root})
	if len(failures) != 0 {
		test.Fatal(failures)
	}
	worktree := findWorktree(test, after, linked)
	want := findWorktree(test, before, linked).EstimatedBytes + additional
	if worktree.EstimatedBytes != want || !worktree.PathSafe {
		test.Fatalf("size = %d, want %d; worktree = %#v", worktree.EstimatedBytes, want, worktree)
	}
}

func TestLoaderRetainsInspectionErrorsAndUnknownState(test *testing.T) {
	repository := testutil.NewRepository(test)
	client := git.NewClient(nil)
	injected := errors.New("injected Git failure")
	loader := newTestLoader(repository.Root)
	loader.Git = inventoryGit{GitClient: client, inspect: func(_ context.Context, _ string, worktree domain.Worktree) (domain.Worktree, error) {
		if worktree.GitStateKnown {
			test.Error("GitStateKnown did not start false")
		}
		worktree.GitStateKnown = true
		worktree.CollectionErrors = append(worktree.CollectionErrors, "status: original failure")
		return worktree, injected
	}}
	worktrees, failures := loader.Load(context.Background(), []string{repository.Root})
	if len(worktrees) != 1 || worktrees[0].GitStateKnown || len(failures) == 0 || !strings.Contains(strings.Join(worktrees[0].CollectionErrors, "\n"), "status: original failure") {
		test.Fatalf("Load() = %#v, %v", worktrees, failures)
	}
	assertPathError(test, failures, repository.Root, injected)
}

func TestLoaderUnknownCWDProtectsEveryWorktree(test *testing.T) {
	repository := testutil.NewRepository(test)
	repository.AddWorktree(test, "feature", "feature/one")
	loader := newTestLoader(repository.Root)
	loader.CWD = func() (string, error) { return "", fs.ErrPermission }
	worktrees, failures := loader.Load(context.Background(), []string{repository.Root})
	if len(worktrees) != 2 || len(failures) == 0 {
		test.Fatalf("Load() = %#v, %v", worktrees, failures)
	}
	for _, worktree := range worktrees {
		assertUnknownUnsafe(test, worktree)
	}
}

func TestLoaderCWDCanonicalizationErrorRetainsOriginalPath(test *testing.T) {
	repository := testutil.NewRepository(test)
	cwd := filepath.Join(repository.Root, "missing-cwd")
	worktrees, failures := newTestLoader(cwd).Load(context.Background(), []string{repository.Root})
	if len(worktrees) != 1 {
		test.Fatalf("Load() = %#v, %v", worktrees, failures)
	}
	assertUnknownUnsafe(test, worktrees[0])
	assertPathError(test, failures, cwd, fs.ErrNotExist)
}

func TestLoaderUnknownByteEstimateIsNotPartialSuccess(test *testing.T) {
	repository := testutil.NewRepository(test)
	linked := repository.AddWorktree(test, "feature", "feature/one")
	blocked := filepath.Join(linked, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		test.Fatal(err)
	}
	loader := newTestLoader(repository.Root)
	loader.Finder = staticFinder{{Root: repository.Root, CommonGitDir: filepath.Join(repository.Root, ".git")}}
	loader.ReadDir = func(path string) ([]os.DirEntry, error) {
		if path == blocked {
			return nil, fs.ErrPermission
		}
		return os.ReadDir(path)
	}
	worktrees, failures := loader.Load(context.Background(), []string{repository.Root})
	worktree := findWorktree(test, worktrees, linked)
	assertUnknownUnsafe(test, worktree)
	if worktree.EstimatedBytes != 0 {
		test.Errorf("partial estimate reported as complete: %d", worktree.EstimatedBytes)
	}
	assertPathError(test, failures, blocked, fs.ErrPermission)
}

func TestLoaderProtectsWorktreeContainingTreeclearData(test *testing.T) {
	repository := testutil.NewRepository(test)
	linked := repository.AddWorktree(test, "feature", "feature/one")
	dataDirectory := filepath.Join(linked, "treeclear-data")
	if err := os.Mkdir(dataDirectory, 0o700); err != nil {
		test.Fatal(err)
	}
	loader := newTestLoader(repository.Root)
	loader.DataDir = dataDirectory
	client := loader.Git
	loader.Git = inventoryGit{GitClient: client, inspect: func(ctx context.Context, root string, worktree domain.Worktree) (domain.Worktree, error) {
		if worktree.Path == linked {
			test.Error("Git inspection entered a worktree containing excluded Treeclear data")
		}
		return client.InspectWorktree(ctx, root, worktree)
	}}
	worktrees, failures := loader.Load(context.Background(), []string{repository.Root})
	if len(failures) == 0 {
		test.Fatal("Treeclear data overlap was not reported")
	}
	assertUnknownUnsafe(test, findWorktree(test, worktrees, linked))
}

func TestLoaderRejectsInvalidDataDirectoryWithInjectedFinder(test *testing.T) {
	repository := testutil.NewRepository(test)
	loader := newTestLoader(repository.Root)
	loader.Finder = staticFinder{{Root: repository.Root, CommonGitDir: filepath.Join(repository.Root, ".git")}}
	loader.DataDir = "invalid\x00path"
	worktrees, failures := loader.Load(context.Background(), []string{repository.Root})
	if len(failures) == 0 || len(worktrees) != 0 {
		test.Fatalf("invalid exclusion was not reported: %#v, %v", worktrees, failures)
	}
}

func TestLoaderReportsCancellationAfterSuccessfulRead(test *testing.T) {
	repository := testutil.NewRepository(test)
	loader := newTestLoader(repository.Root)
	loader.Finder = staticFinder{{Root: repository.Root, CommonGitDir: filepath.Join(repository.Root, ".git")}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	loader.ReadDir = func(string) ([]os.DirEntry, error) {
		cancel()
		return nil, nil
	}
	worktrees, failures := loader.Load(ctx, []string{repository.Root})
	if !errors.Is(errors.Join(failures...), context.Canceled) || len(worktrees) != 1 {
		test.Fatalf("cancellation was discarded: %#v, %v", worktrees, failures)
	}
	assertUnknownUnsafe(test, worktrees[0])
}

func TestLoaderRetainsMissingWorktreeAsUnknown(test *testing.T) {
	repository := testutil.NewRepository(test)
	linked := repository.AddWorktree(test, "feature", "feature/one")
	if err := os.RemoveAll(linked); err != nil {
		test.Fatal(err)
	}
	worktrees, failures := newTestLoader(repository.Root).Load(context.Background(), []string{repository.Root})
	if len(worktrees) != 2 || len(failures) == 0 {
		test.Fatalf("Load() = %#v, %v", worktrees, failures)
	}
	assertUnknownUnsafe(test, findWorktree(test, worktrees, linked))
}

func TestLoaderBoundsRepositoryConcurrencyAndSortsOutput(test *testing.T) {
	limit := min(runtime.GOMAXPROCS(0), 8)
	var repositories staticFinder
	root, err := pathutil.Canonical(test.TempDir())
	if err != nil {
		test.Fatal(err)
	}
	for index := 2*limit + 2; index >= 0; index-- {
		path := filepath.Join(root, fmt.Sprintf("repo-%02d", index))
		common := filepath.Join(path, ".git")
		if err := os.MkdirAll(common, 0o700); err != nil {
			test.Fatal(err)
		}
		repositories = append(repositories, discovery.Repository{Root: path, CommonGitDir: common})
	}
	var active, maximum atomic.Int32
	started := make(chan struct{}, len(repositories))
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	client := inventoryGit{
		list: func(_ context.Context, root string) ([]domain.Worktree, error) {
			return []domain.Worktree{{Path: root, RepositoryRoot: root, CommonGitDir: filepath.Join(root, ".git")}}, nil
		},
		inspect: func(ctx context.Context, _ string, worktree domain.Worktree) (domain.Worktree, error) {
			current := active.Add(1)
			defer active.Add(-1)
			for prior := maximum.Load(); current > prior; prior = maximum.Load() {
				if maximum.CompareAndSwap(prior, current) {
					break
				}
			}
			started <- struct{}{}
			select {
			case <-release:
				worktree.GitStateKnown = true
				return worktree, nil
			case <-ctx.Done():
				return worktree, ctx.Err()
			}
		},
	}
	loader := newTestLoader(root)
	loader.Git, loader.Finder = client, repositories
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type result struct {
		worktrees []domain.Worktree
		failures  []error
	}
	finished := make(chan result, 1)
	go func() {
		worktrees, failures := loader.Load(ctx, []string{root})
		finished <- result{worktrees, failures}
	}()
	for range limit {
		select {
		case <-started:
		case <-ctx.Done():
			test.Fatal("repository workers did not reach the configured concurrency")
		}
	}
	select {
	case <-started:
		test.Error("more repositories than the limit were enriched concurrently")
	case <-time.After(20 * time.Millisecond):
	}
	unblock()
	var actual result
	select {
	case actual = <-finished:
	case <-ctx.Done():
		test.Fatal("Load did not finish")
	}
	if len(actual.failures) != 0 || len(actual.worktrees) != len(repositories) || maximum.Load() != int32(limit) {
		test.Fatalf("worktrees = %d, errors = %v, maximum concurrency = %d, want %d", len(actual.worktrees), actual.failures, maximum.Load(), limit)
	}
	paths := make([]string, 0, len(actual.worktrees))
	for _, worktree := range actual.worktrees {
		paths = append(paths, worktree.Path)
	}
	if !sort.StringsAreSorted(paths) {
		test.Errorf("inventory is not deterministic: %q", paths)
	}
}

func TestLoaderEmptyRootsDoNotReadCWD(test *testing.T) {
	loader := Loader{CWD: func() (string, error) {
		test.Error("empty roots consulted the process cwd")
		return "", fs.ErrPermission
	}}
	worktrees, failures := loader.Load(context.Background(), nil)
	if len(worktrees) != 0 || len(failures) != 0 {
		test.Fatalf("Load(nil) = %#v, %v", worktrees, failures)
	}
}

func TestLoaderPreservesGitOwnedFields(test *testing.T) {
	repository := testutil.NewRepository(test)
	client := git.NewClient(nil)
	listed, err := client.ListWorktrees(context.Background(), repository.Root)
	if err != nil {
		test.Fatal(err)
	}
	want, err := client.InspectWorktree(context.Background(), repository.Root, listed[0])
	if err != nil {
		test.Fatal(err)
	}
	actual, failures := newTestLoader(repository.Root).Load(context.Background(), []string{repository.Root})
	if len(failures) != 0 || len(actual) != 1 {
		test.Fatalf("Load() = %#v, %v", actual, failures)
	}
	want.Current, want.PathSafe, want.EstimatedBytes = actual[0].Current, actual[0].PathSafe, actual[0].EstimatedBytes
	want.Path, want.RepositoryRoot, want.Primary = actual[0].Path, actual[0].RepositoryRoot, actual[0].Primary
	if !reflect.DeepEqual(actual[0], want) {
		test.Errorf("loader changed Git-owned state:\n got %#v\nwant %#v", actual[0], want)
	}
}

type staticFinder []discovery.Repository

func (finder staticFinder) Find(context.Context, []string) ([]discovery.Repository, []error) {
	return append([]discovery.Repository(nil), finder...), nil
}

type inventoryGit struct {
	GitClient
	list    func(context.Context, string) ([]domain.Worktree, error)
	inspect func(context.Context, string, domain.Worktree) (domain.Worktree, error)
}

func (client inventoryGit) ListWorktrees(ctx context.Context, root string) ([]domain.Worktree, error) {
	if client.list != nil {
		return client.list(ctx, root)
	}
	return client.GitClient.ListWorktrees(ctx, root)
}

func (client inventoryGit) InspectWorktree(ctx context.Context, root string, worktree domain.Worktree) (domain.Worktree, error) {
	if client.inspect != nil {
		return client.inspect(ctx, root, worktree)
	}
	return client.GitClient.InspectWorktree(ctx, root, worktree)
}

func newTestLoader(cwd string) Loader {
	return Loader{Git: git.NewClient(nil), CWD: func() (string, error) { return cwd, nil }}
}

func findWorktree(test *testing.T, worktrees []domain.Worktree, path string) domain.Worktree {
	test.Helper()
	for _, worktree := range worktrees {
		if worktree.Path == path {
			return worktree
		}
	}
	test.Fatalf("worktree %q not found in %#v", path, worktrees)
	return domain.Worktree{}
}

func assertUnknownUnsafe(test *testing.T, worktree domain.Worktree) {
	test.Helper()
	if worktree.PathSafe || worktree.GitStateKnown || len(worktree.CollectionErrors) == 0 {
		test.Errorf("failed enrichment looked safe or known: %#v", worktree)
	}
}

func assertPathError(test *testing.T, failures []error, path string, cause error) {
	test.Helper()
	for _, failure := range failures {
		var pathError *fs.PathError
		if errors.As(failure, &pathError) && pathError.Path == path && errors.Is(failure, cause) {
			return
		}
	}
	test.Errorf("missing typed %q error for %q in %v", cause, path, failures)
}

func makeSymlink(test *testing.T, target, path string) {
	test.Helper()
	if err := os.Symlink(target, path); err != nil {
		test.Skipf("symlink creation unavailable: %v", err)
	}
}
