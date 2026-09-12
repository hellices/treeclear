package discovery

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/git"
	"github.com/hellices/treeclear/internal/pathutil"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestFindDeduplicatesPrimaryAndLinkedGitfile(test *testing.T) {
	repository := testutil.NewRepository(test)
	linked := repository.AddWorktree(test, "feature", "feature/one")
	for _, roots := range [][]string{{linked}, {repository.Root, linked, repository.Root}} {
		found, failures := Find(context.Background(), roots)
		want := []Repository{{Root: repository.Root, CommonGitDir: filepath.Join(repository.Root, ".git")}}
		if len(failures) != 0 || !reflect.DeepEqual(found, want) {
			test.Fatalf("Find(%q) = %#v, %v; want %#v", roots, found, failures, want)
		}
	}
}

func TestFindDiscoversNestedRepositories(test *testing.T) {
	repository := testutil.NewRepository(test)
	nested := moveRepository(test, filepath.Join(repository.Root, "nested"))
	found, failures := Find(context.Background(), []string{repository.Root})
	if len(failures) != 0 || len(found) != 2 || found[0].Root != repository.Root || found[1].Root != nested {
		test.Fatalf("Find() = %#v, %v", found, failures)
	}
}

func TestFinderResolvesDescendantRootWithoutScanningItsParents(test *testing.T) {
	repository := testutil.NewRepository(test)
	linked := repository.AddWorktree(test, "feature", "feature/one")
	root := filepath.Join(linked, "subdirectory")
	nested := moveRepository(test, filepath.Join(root, "nested"))
	moveRepository(test, filepath.Join(linked, "unrequested"))
	finder := Finder{ReadDir: func(path string) ([]os.DirEntry, error) {
		if !pathutil.Contains(root, path) {
			test.Errorf("discovery scanned outside the requested subtree: %q", path)
			return nil, fs.ErrPermission
		}
		return os.ReadDir(path)
	}}
	found, failures := finder.Find(context.Background(), []string{root})
	paths := make([]string, 0, len(found))
	for _, foundRepository := range found {
		paths = append(paths, foundRepository.Root)
	}
	want := []string{repository.Root, nested}
	sort.Strings(want)
	if len(failures) != 0 || !reflect.DeepEqual(paths, want) {
		test.Fatalf("descendant discovery = %#v, %v; want %q", found, failures, want)
	}
}

func TestFinderRejectsMalformedDataDirectory(test *testing.T) {
	for _, kind := range []string{"file", "dangling-link"} {
		test.Run(kind, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			dataDirectory := filepath.Join(repository.Root, "treeclear-data")
			if kind == "file" {
				if err := os.WriteFile(dataDirectory, nil, 0o600); err != nil {
					test.Fatal(err)
				}
			} else {
				makeSymlink(test, filepath.Join(repository.Root, "missing"), dataDirectory)
			}
			found, failures := (Finder{DataDir: dataDirectory}).Find(context.Background(), []string{repository.Root})
			if len(found) != 0 || len(failures) != 1 {
				test.Fatalf("malformed exclusion was accepted: %#v, %v", found, failures)
			}
		})
	}
}

func TestFinderSkipsMetadataCachesAndConfiguredDataDirectory(test *testing.T) {
	repository := testutil.NewRepository(test)
	dataDirectory := filepath.Join(repository.Root, "state", "treeclear")
	ignored := []string{filepath.Join(repository.Root, ".git"), dataDirectory}
	for _, name := range []string{"node_modules", ".cache", "target"} {
		ignored = append(ignored, filepath.Join(repository.Root, name))
	}
	for _, directory := range ignored {
		moveRepository(test, filepath.Join(directory, "nested"))
	}
	visible := moveRepository(test, filepath.Join(repository.Root, "state", "treeclear-other"))
	finder := Finder{
		DataDir: dataDirectory,
		ReadDir: func(path string) ([]os.DirEntry, error) {
			for _, directory := range ignored {
				if pathutil.Contains(directory, path) {
					test.Errorf("discovery entered excluded directory %q", path)
					return nil, fs.ErrPermission
				}
			}
			return os.ReadDir(path)
		},
	}
	found, failures := finder.Find(context.Background(), append([]string{repository.Root}, ignored...))
	if len(failures) != 0 || len(found) != 2 || found[0].Root != repository.Root || found[1].Root != visible {
		test.Fatalf("Find() = %#v, %v", found, failures)
	}
}

func TestFinderSkipsGitDirectoryCaseAlias(test *testing.T) {
	repository := testutil.NewRepository(test)
	marker := filepath.Join(repository.Root, ".git")
	alias := filepath.Join(repository.Root, ".GIT")
	if err := os.Rename(marker, alias); err != nil {
		test.Fatal(err)
	}
	if _, err := os.Stat(marker); errors.Is(err, fs.ErrNotExist) {
		test.Skip("filesystem has case-sensitive directory names")
	} else if err != nil {
		test.Fatal(err)
	}
	finder := Finder{ReadDir: func(path string) ([]os.DirEntry, error) {
		if pathutil.Contains(alias, path) {
			test.Errorf("discovery traversed case-aliased Git metadata: %q", path)
			return nil, fs.ErrPermission
		}
		return os.ReadDir(path)
	}}
	found, failures := finder.Find(context.Background(), []string{repository.Root})
	if len(found) != 1 || len(failures) != 0 {
		test.Fatalf("case-aliased Git metadata was not excluded: %#v, %v", found, failures)
	}
}

func TestFinderRejectsUnverifiedSeparateGitDirectoryAnchor(test *testing.T) {
	repository := testutil.NewRepository(test)
	repository.Git(test, "config", "core.worktree", repository.Root)
	metadata := filepath.Join(filepath.Dir(repository.Root), "metadata")
	if err := os.Rename(filepath.Join(repository.Root, ".git"), metadata); err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository.Root, ".git"), []byte("gitdir: ../metadata\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	found, failures := Find(context.Background(), []string{repository.Root})
	if len(found) != 0 || len(failures) != 1 {
		test.Fatalf("accepted unverified primary identity: %#v, %v", found, failures)
	}
	var pathError *fs.PathError
	if !errors.As(failures[0], &pathError) || pathError.Path != repository.Root {
		test.Fatalf("missing per-path primary identity error: %v", failures)
	}
}

func TestFinderPreservesUnicodePathComponents(test *testing.T) {
	root := canonicalDirectory(test, test.TempDir())
	repository := moveRepository(test, filepath.Join(root, "nameįtarget"))
	found, failures := Find(context.Background(), []string{root})
	if len(failures) != 0 || len(found) != 1 || found[0].Root != repository {
		test.Fatalf("Unicode directory component was misinterpreted: %#v, %v", found, failures)
	}
}

func TestFinderReportsCancellationAfterSuccessfulRead(test *testing.T) {
	repository := testutil.NewRepository(test)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finder := Finder{ReadDir: func(string) ([]os.DirEntry, error) {
		cancel()
		return nil, nil
	}}
	_, failures := finder.Find(ctx, []string{repository.Root})
	if !errors.Is(errors.Join(failures...), context.Canceled) {
		test.Fatalf("cancellation was discarded: %v", failures)
	}
}

func TestFinderRetainsTypedPerPathErrors(test *testing.T) {
	repository := testutil.NewRepository(test)
	blocked := filepath.Join(repository.Root, "blocked")
	malformed := filepath.Join(repository.Root, "malformed")
	for _, directory := range []string{blocked, malformed} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			test.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(malformed, ".git"), []byte("not a gitfile\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	missing := filepath.Join(repository.Root, "missing")
	finder := Finder{ReadDir: func(path string) ([]os.DirEntry, error) {
		if path == blocked {
			return nil, fs.ErrPermission
		}
		return os.ReadDir(path)
	}}
	found, failures := finder.Find(context.Background(), []string{missing, repository.Root})
	if len(found) != 1 || len(failures) != 3 {
		test.Fatalf("Find() = %#v, %v", found, failures)
	}
	paths := make([]string, 0, len(failures))
	for _, failure := range failures {
		var pathError *fs.PathError
		if !errors.As(failure, &pathError) {
			test.Fatalf("untyped error: %T: %v", failure, failure)
		}
		paths = append(paths, pathError.Path)
		if pathError.Path == blocked && !errors.Is(failure, fs.ErrPermission) {
			test.Fatalf("permission error was lost: %v", failure)
		}
	}
	wantPaths := []string{blocked, malformed, missing}
	sort.Strings(wantPaths)
	if !reflect.DeepEqual(paths, wantPaths) {
		test.Fatalf("error paths = %q, want sorted %q", paths, wantPaths)
	}
}

func TestFinderDoesNotFollowSymlinks(test *testing.T) {
	repository := testutil.NewRepository(test)
	root := canonicalDirectory(test, test.TempDir())
	alias := filepath.Join(root, "alias")
	makeSymlink(test, repository.Root, alias)
	found, failures := Find(context.Background(), []string{root})
	if len(found) != 0 || len(failures) != 0 {
		test.Fatalf("followed directory symlink: %#v, %v", found, failures)
	}
	found, failures = Find(context.Background(), []string{alias})
	if len(found) != 0 || len(failures) != 1 {
		test.Fatalf("explicit symlink root was not rejected: %#v, %v", found, failures)
	}
}

func TestFinderRejectsSymlinkGitMarker(test *testing.T) {
	repository := testutil.NewRepository(test)
	metadata := filepath.Join(filepath.Dir(repository.Root), "metadata")
	if err := os.Rename(filepath.Join(repository.Root, ".git"), metadata); err != nil {
		test.Fatal(err)
	}
	makeSymlink(test, metadata, filepath.Join(repository.Root, ".git"))
	found, failures := Find(context.Background(), []string{repository.Root})
	if len(found) != 0 || len(failures) != 1 {
		test.Fatalf("symlink metadata was not rejected: %#v, %v", found, failures)
	}
}

func TestFinderRejectsConflictingRepositoryIdentity(test *testing.T) {
	repository := testutil.NewRepository(test)
	client := git.NewClient(nil)
	finder := Finder{Git: discoveryGit{
		GitClient: client,
		list: func(ctx context.Context, root string) ([]domain.Worktree, error) {
			worktrees, err := client.ListWorktrees(ctx, root)
			worktrees[0].CommonGitDir = repository.Root
			return worktrees, err
		},
	}}
	found, failures := finder.Find(context.Background(), []string{repository.Root})
	if len(found) != 0 || len(failures) != 1 {
		test.Fatalf("conflicting common directory was accepted: %#v, %v", found, failures)
	}
}

func TestFinderHandlesMissingDataDirectoryWithoutCreatingIt(test *testing.T) {
	repository := testutil.NewRepository(test)
	dataDirectory := filepath.Join(repository.Root, "missing", "treeclear")
	found, failures := (Finder{DataDir: dataDirectory}).Find(context.Background(), []string{repository.Root})
	if len(found) != 1 || len(failures) != 0 {
		test.Fatalf("Find() = %#v, %v", found, failures)
	}
	if _, err := os.Stat(dataDirectory); !errors.Is(err, fs.ErrNotExist) {
		test.Fatalf("discovery created data directory: %v", err)
	}
}

func TestFindEmptyRootsAndCancellation(test *testing.T) {
	if found, failures := Find(context.Background(), nil); len(found) != 0 || len(failures) != 0 {
		test.Fatalf("empty roots = %#v, %v", found, failures)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	found, failures := Find(ctx, []string{test.TempDir()})
	if len(found) != 0 || len(failures) != 1 || !errors.Is(failures[0], context.Canceled) {
		test.Fatalf("cancelled discovery = %#v, %v", found, failures)
	}
}

type discoveryGit struct {
	GitClient
	list func(context.Context, string) ([]domain.Worktree, error)
}

func (client discoveryGit) ListWorktrees(ctx context.Context, root string) ([]domain.Worktree, error) {
	return client.list(ctx, root)
}

func moveRepository(test *testing.T, destination string) string {
	test.Helper()
	repository := testutil.NewRepository(test)
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		test.Fatal(err)
	}
	if err := os.Rename(repository.Root, destination); err != nil {
		test.Fatal(err)
	}
	return destination
}

func canonicalDirectory(test *testing.T, path string) string {
	test.Helper()
	canonical, err := pathutil.Canonical(path)
	if err != nil {
		test.Fatal(err)
	}
	return canonical
}

func makeSymlink(test *testing.T, target, path string) {
	test.Helper()
	if err := os.Symlink(target, path); err != nil {
		test.Skipf("symlink creation unavailable: %v", err)
	}
}
