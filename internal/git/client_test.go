package git

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/testutil"
)

type runnerFunc func(context.Context, execx.Request) (execx.Result, error)

func (runner runnerFunc) Run(ctx context.Context, request execx.Request) (execx.Result, error) {
	return runner(ctx, request)
}

func TestClientRejectsOldGit(test *testing.T) {
	for _, version := range []struct {
		value string
		valid bool
	}{{"git version 2.35.9", false}, {"git version 2.36.0", true}, {"git version 2.50.1.windows.1", true}, {"not Git", false}} {
		client := NewClient(runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
			return execx.Result{Stdout: []byte(version.value)}, nil
		}))
		if err := client.CheckVersion(context.Background()); (err == nil) != version.valid {
			test.Fatalf("version %q: %v", version.value, err)
		}
	}
}

func TestClientRejectsEmptyWorkingDirectory(test *testing.T) {
	for _, arguments := range [][]string{
		{"rev-parse", "--git-common-dir"},
		{"worktree", "list", "--porcelain", "-z"},
		{"status", "--porcelain=v2", "-z"},
		{"diff", "--binary"},
		{"worktree", "remove", "--", "target"},
	} {
		test.Run(strings.Join(arguments, " "), func(test *testing.T) {
			calls := 0
			client := NewClient(runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
				calls++
				return execx.Result{}, nil
			}))
			if _, err := client.run(context.Background(), "", arguments...); err == nil || calls != 0 {
				test.Fatalf("empty directory invoked Git %d times, error = %v", calls, err)
			}
		})
	}
}

func TestClientInspectionRejectsMissingLocationsBeforeRunningGit(test *testing.T) {
	root := test.TempDir()
	for _, locations := range []struct {
		repository string
		worktree   string
	}{{"", root}, {root, ""}, {"", ""}} {
		calls := 0
		client := NewClient(runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
			calls++
			return execx.Result{}, nil
		}))
		actual, err := client.InspectWorktree(context.Background(), locations.repository, domain.Worktree{
			Path: locations.worktree, GitStateKnown: true,
		})
		if err == nil || calls != 0 || actual.GitStateKnown || len(actual.CollectionErrors) == 0 {
			test.Fatalf("locations = %#v: %d Git calls, inspection = %#v, error = %v", locations, calls, actual, err)
		}
	}
}

func TestClientRemovalNeverForcesOrDeletesBranches(test *testing.T) {
	var requests []execx.Request
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		requests = append(requests, request)
		return execx.Result{}, nil
	}))
	repository := test.TempDir()
	path := filepath.Join(test.TempDir(), "--force; not a command")
	if err := client.RemoveWorktree(context.Background(), repository, path); err != nil {
		test.Fatal(err)
	}
	if len(requests) != 1 {
		test.Fatalf("removal issued %d Git requests, want exactly one: %#v", len(requests), requests)
	}
	actual := requests[0]
	if actual.Directory != repository || actual.Name != "git" || !reflect.DeepEqual(actual.Args, []string{"worktree", "remove", "--", path}) {
		test.Fatalf("removal request = %#v", actual)
	}
}

func TestClientRemovalRejectsEmptyTarget(test *testing.T) {
	calls := 0
	client := NewClient(runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
		calls++
		return execx.Result{}, nil
	}))
	if err := client.RemoveWorktree(context.Background(), test.TempDir(), ""); err == nil || calls != 0 {
		test.Fatalf("empty target invoked Git %d times, error = %v", calls, err)
	}
}

func TestClientDiffDisablesExternalPrograms(test *testing.T) {
	directory := test.TempDir()
	var actual execx.Request
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		actual = request
		if request.Args[0] == "rev-parse" {
			return readonlyIndexPreflightResult(test, request, directory), nil
		}
		return execx.Result{}, nil
	}))
	if _, err := client.Diff(context.Background(), directory, true); err != nil {
		test.Fatal(err)
	}
	arguments := strings.Join(actual.Args, " ")
	for _, required := range []string{"--no-ext-diff", "--no-textconv", "--cached"} {
		if !strings.Contains(arguments, required) {
			test.Fatalf("missing %s in %q", required, actual.Args)
		}
	}
}

func TestClientInventoriesRealLinkedWorktree(test *testing.T) {
	repository := testutil.NewRepository(test)
	linked := repository.AddWorktree(test, "feature with spaces", "topic")
	client := NewClient(execx.OSRunner{})
	worktrees, err := client.ListWorktrees(context.Background(), repository.Root)
	if err != nil || len(worktrees) != 2 {
		test.Fatalf("worktrees = %#v, error = %v", worktrees, err)
	}
	var candidate domain.Worktree
	for _, worktree := range worktrees {
		if worktree.Path != filepath.FromSlash(worktree.Path) || worktree.RepositoryRoot != repository.Root {
			test.Fatalf("worktree locations are not native paths: %#v", worktree)
		}
		if worktree.Path == linked {
			candidate = worktree
		}
	}
	if candidate.Path == "" {
		test.Fatalf("linked worktree %q was not found in %#v", linked, worktrees)
	}
	actual, err := client.InspectWorktree(context.Background(), repository.Root, candidate)
	if err != nil || !actual.GitStateKnown || !actual.Status.Clean() || !actual.Recoverable {
		test.Fatalf("inspection = %#v, error = %v", actual, err)
	}
	if len(actual.IndexHash) != 64 || len(actual.AdminHash) != 64 || actual.LastCommitAt.IsZero() || actual.MetadataModifiedAt.IsZero() {
		test.Fatalf("missing preconditions: %#v", actual)
	}
	if err := os.WriteFile(filepath.Join(linked, "seed.txt"), []byte("dirty\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	actual, err = client.InspectWorktree(context.Background(), repository.Root, candidate)
	if err != nil || !actual.GitStateKnown || actual.Status.Unstaged != 1 {
		test.Fatalf("dirty inspection = %#v, error = %v", actual, err)
	}
}

func TestClientRejectsAdministrativeDirectoryOutsideRepository(test *testing.T) {
	repository := testutil.NewRepository(test)
	other := testutil.NewRepository(test)
	actual, err := NewClient(nil).InspectWorktree(context.Background(), repository.Root, domain.Worktree{
		Path: other.Root, RepositoryRoot: repository.Root, PathSafe: true, GitStateKnown: true,
	})
	if !errors.Is(err, ErrWorktreeChanged) {
		test.Fatalf("administrative directory escape was not reported: %v", err)
	}
	if actual.PathSafe || actual.GitStateKnown || len(actual.CollectionErrors) == 0 || actual.IndexHash != "" || actual.AdminHash != "" {
		test.Fatalf("escaped administrative identity remained trusted: %#v", actual)
	}
}

func TestClientUnverifiedAdministrativePathsAreUnsafe(test *testing.T) {
	for _, flag := range []string{"--git-common-dir", "--absolute-git-dir"} {
		test.Run(flag, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			denied := errors.New("metadata path denied")
			client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
				if len(request.Args) != 0 && request.Args[len(request.Args)-1] == flag {
					return execx.Result{ExitCode: 128}, denied
				}
				return (execx.OSRunner{}).Run(ctx, request)
			}))
			actual, err := client.InspectWorktree(context.Background(), repository.Root, domain.Worktree{
				Path: repository.Root, Primary: true, PathSafe: true, GitStateKnown: true,
			})
			if !errors.Is(err, denied) || actual.PathSafe || actual.GitStateKnown || len(actual.CollectionErrors) == 0 || actual.IndexHash != "" || actual.AdminHash != "" {
				test.Fatalf("unverified administrative identity remained trusted: %#v, %v", actual, err)
			}
		})
	}
}

func TestClientMetadataHashHonorsCancellation(test *testing.T) {
	repository := testutil.NewRepository(test)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	canceled := false
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		result, err := (execx.OSRunner{}).Run(ctx, request)
		if err == nil && len(request.Args) != 0 && request.Args[len(request.Args)-1] == "--absolute-git-dir" {
			canceled = true
			cancel()
		}
		return result, err
	}))
	actual, err := client.InspectWorktree(ctx, repository.Root, domain.Worktree{Path: repository.Root, Primary: true, PathSafe: true})
	if !canceled || !errors.Is(err, context.Canceled) || actual.GitStateKnown || actual.IndexHash != "" || actual.AdminHash != "" || !actual.MetadataModifiedAt.IsZero() {
		test.Fatalf("canceled metadata hashing produced a result: %#v, %v", actual, err)
	}
}

func TestClientMetadataHashBounds(test *testing.T) {
	for _, limit := range []string{"entries", "bytes"} {
		test.Run(limit, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			info := filepath.Join(repository.Root, ".git", "info")
			expected := "entry limit"
			if limit == "entries" {
				for index := range 4096 {
					if err := os.WriteFile(filepath.Join(info, fmt.Sprintf("empty-%04d", index)), nil, 0o600); err != nil {
						test.Fatal(err)
					}
				}
			} else {
				expected = "collection limit"
				path := filepath.Join(info, "large")
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					test.Fatal(err)
				}
				if err := os.Truncate(path, maxGitBytes+1); err != nil {
					test.Fatal(err)
				}
			}
			actual, err := NewClient(nil).InspectWorktree(context.Background(), repository.Root, domain.Worktree{
				Path: repository.Root, Primary: true, PathSafe: true,
			})
			if err == nil || !strings.Contains(err.Error(), expected) || actual.GitStateKnown || actual.AdminHash != "" || !actual.MetadataModifiedAt.IsZero() {
				test.Fatalf("metadata %s limit was not enforced: %#v, %v", limit, actual, err)
			}
		})
	}
}

func TestHashAdminPreservesMetadataSelection(test *testing.T) {
	directory := test.TempDir()
	contents := map[string]string{
		"HEAD": "ref: refs/heads/main\n", "index": "synthetic index",
		"info/exclude": "ignored\n", "info/nested/hidden": "nested metadata", "objects/blob": "object data",
	}
	for relative, content := range contents {
		path := filepath.Join(directory, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			test.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			test.Fatal(err)
		}
	}
	for _, primary := range []bool{true, false} {
		expected := sha256.New()
		var expectedTime time.Time
		paths := []string{".", "HEAD", "index", "info", "info/exclude"}
		if !primary {
			paths = append(paths, "info/nested", "info/nested/hidden", "objects", "objects/blob")
		}
		for _, relative := range paths {
			metadata, err := os.Lstat(filepath.Join(directory, filepath.FromSlash(relative)))
			if err != nil {
				test.Fatal(err)
			}
			if metadata.ModTime().After(expectedTime) {
				expectedTime = metadata.ModTime().UTC()
			}
			fmt.Fprintf(expected, "%q\x00%o\x00", relative, metadata.Mode())
			if !metadata.IsDir() {
				fmt.Fprintf(expected, "%x\n", sha256.Sum256([]byte(contents[relative])))
			}
		}
		actual, latest, err := hashAdmin(context.Background(), directory, primary)
		if err != nil || actual != fmt.Sprintf("%x", expected.Sum(nil)) || !latest.Equal(expectedTime) {
			test.Fatalf("primary=%v: hash = %q, latest = %v, error = %v", primary, actual, latest, err)
		}
	}
}

func TestClientFailedCollectionCannotLookClean(test *testing.T) {
	client := NewClient(runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
		return execx.Result{ExitCode: 128}, errors.New("access denied")
	}))
	actual, err := client.InspectWorktree(context.Background(), test.TempDir(), domain.Worktree{Path: test.TempDir(), GitStateKnown: true})
	if err == nil || actual.GitStateKnown || len(actual.CollectionErrors) == 0 {
		test.Fatalf("inspection = %#v, error = %v", actual, err)
	}
}

func TestClientRefusesExecutableFilters(test *testing.T) {
	repository := testutil.NewRepository(test)
	repository.Git(test, "config", "filter.tripwire.clean", "invalid-command-must-not-run")
	client := NewClient(execx.OSRunner{})
	if _, err := client.Status(context.Background(), repository.Root); err == nil || !strings.Contains(err.Error(), "filter") {
		test.Fatalf("executable filter was not rejected: %v", err)
	}
}

func TestClientRejectsUnsafeIndexEntries(test *testing.T) {
	object := strings.Repeat("a", 40)
	for _, entry := range []struct {
		name    string
		output  string
		allowed bool
	}{
		{"empty", "", true},
		{"ordinary", "H 100644 " + object + " 0\tspace\nname\twith-tab\x00", true},
		{"unmerged", "M 100644 " + object + " 1\tfile\x00", true},
		{"assume-unchanged", "h 100644 " + object + " 0\tfile\x00", false},
		{"skip-worktree", "S 100644 " + object + " 0\tfile\x00", false},
		{"both-flags", "s 100644 " + object + " 0\tfile\x00", false},
		{"submodule", "H 160000 " + object + " 0\tmodule\x00", false},
		{"missing-terminator", "H 100644 " + object + " 0\tfile", false},
		{"missing-path", "H 100644 " + object + " 0\t\x00", false},
		{"unknown-tag", "X 100644 " + object + " 0\tfile\x00", false},
		{"malformed-record", "not an index entry\x00", false},
	} {
		test.Run(entry.name, func(test *testing.T) {
			directory := test.TempDir()
			client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
				if request.Args[0] == "rev-parse" {
					return readonlyIndexPreflightResult(test, request, directory), nil
				}
				if request.Args[0] == "ls-files" {
					return execx.Result{Stdout: []byte(entry.output)}, nil
				}
				return execx.Result{}, nil
			}))
			if _, err := client.Status(context.Background(), directory); (err == nil) != entry.allowed {
				test.Fatalf("allowed = %t, status error = %v", entry.allowed, err)
			}
		})
	}
}

func TestClientSanitizesGitEnvironment(test *testing.T) {
	test.Setenv("GIT_DIR", filepath.Join(test.TempDir(), "outside"))
	var environment []string
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		environment = request.Env
		return execx.Result{Stdout: []byte("git version 2.50.1")}, nil
	}))
	if err := client.CheckVersion(context.Background()); err != nil {
		test.Fatal(err)
	}
	for _, entry := range environment {
		if strings.HasPrefix(entry, "GIT_DIR=") {
			test.Fatalf("inherited dangerous Git routing: %q", entry)
		}
	}
	if !strings.Contains(strings.Join(environment, "\n"), "GIT_OPTIONAL_LOCKS=0") {
		test.Fatal("read-only collection must disable optional index writes")
	}
}

func TestClientIgnoresHostAttributes(test *testing.T) {
	repository := testutil.NewRepository(test)
	home := test.TempDir()
	attributes := filepath.Join(home, ".config", "git", "attributes")
	if err := os.MkdirAll(filepath.Dir(attributes), 0o700); err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(attributes, []byte("seed.txt working-tree-encoding=UTF-16LE\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	test.Setenv("HOME", home)
	if err := os.Chtimes(filepath.Join(repository.Root, "seed.txt"), time.Unix(100, 0), time.Unix(100, 0)); err != nil {
		test.Fatal(err)
	}
	client := NewClient(execx.OSRunner{})
	attributesResult, attributesErr := client.run(context.Background(), repository.Root, "check-attr", "working-tree-encoding", "--", "seed.txt")
	if attributesErr != nil || !strings.Contains(string(attributesResult.Stdout), "unspecified") {
		test.Fatalf("host attribute was read: %q, %v", attributesResult.Stdout, attributesErr)
	}
	actual, err := client.Status(context.Background(), repository.Root)
	if err != nil || !actual.Clean() {
		test.Fatalf("host attributes affected fixture status: %#v, %v", actual, err)
	}
}

func TestClientDiffDoesNotRunConfiguredPrograms(test *testing.T) {
	for _, driver := range []string{"external", "textconv"} {
		test.Run(driver, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			marker := filepath.Join(test.TempDir(), "executed")
			program := filepath.Join(test.TempDir(), "diff-program")
			quote := func(path string) string { return "'" + strings.ReplaceAll(filepath.ToSlash(path), "'", "'\\''") + "'" }
			script := "#!/bin/sh\nprintf executed > " + quote(marker) + "\nprintf transformed\n"
			if err := os.WriteFile(program, []byte(script), 0o700); err != nil {
				test.Fatal(err)
			}
			if driver == "external" {
				repository.Git(test, "config", "diff.external", quote(program))
			} else {
				repository.Git(test, "config", "diff.tripwire.textconv", quote(program))
				if err := os.WriteFile(filepath.Join(repository.Root, ".gitattributes"), []byte("seed.txt diff=tripwire\n"), 0o600); err != nil {
					test.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(repository.Root, "seed.txt"), []byte("changed\n"), 0o600); err != nil {
				test.Fatal(err)
			}
			repository.Git(test, "diff", "--", "seed.txt")
			if err := os.Remove(marker); err != nil {
				test.Fatalf("control diff did not exercise configured %s: %v", driver, err)
			}
			patch, err := NewClient(execx.OSRunner{}).Diff(context.Background(), repository.Root, false)
			if err != nil || !strings.Contains(string(patch), "diff --git") {
				test.Fatalf("safe diff = %q, error = %v", patch, err)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				test.Fatalf("configured %s ran: %v", driver, err)
			}
		})
	}
}

func TestClientRequiresExactBaseOrRemoteRecovery(test *testing.T) {
	repository := testutil.NewRepository(test)
	linked := repository.AddWorktree(test, "feature", "topic")
	if err := os.WriteFile(filepath.Join(linked, "seed.txt"), []byte("unique commit\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	repository.Git(test, "-C", linked, "add", "--", "seed.txt")
	repository.Git(test, "-C", linked, "commit", "-m", "Unique feature commit")
	head := repository.Git(test, "-C", linked, "rev-parse", "HEAD")
	repository.Git(test, "branch", "base/other", head)
	client := NewClient(execx.OSRunner{})
	client.BaseBranches = []string{"base"}
	actual, err := client.recoverable(context.Background(), linked, head)
	if err != nil || actual {
		test.Fatalf("base/other must not match base: recoverable = %t, error = %v", actual, err)
	}
	repository.Git(test, "update-ref", "refs/remotes/fixture/topic", head)
	actual, err = client.recoverable(context.Background(), linked, head)
	if err != nil || !actual {
		test.Fatalf("remote recovery = %t, error = %v", actual, err)
	}
}
