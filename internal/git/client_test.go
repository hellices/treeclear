package git

import (
	"context"
	"errors"
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

func TestClientRemovalNeverForcesOrDeletesBranches(test *testing.T) {
	var actual execx.Request
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		actual = request
		return execx.Result{}, nil
	}))
	path := filepath.Join(test.TempDir(), "--force; not a command")
	if err := client.RemoveWorktree(context.Background(), test.TempDir(), path); err != nil {
		test.Fatal(err)
	}
	if actual.Name != "git" || !reflect.DeepEqual(actual.Args, []string{"worktree", "remove", "--", path}) {
		test.Fatalf("removal request = %#v", actual)
	}
}

func TestClientDiffDisablesExternalPrograms(test *testing.T) {
	var actual execx.Request
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		actual = request
		return execx.Result{}, nil
	}))
	if _, err := client.Diff(context.Background(), test.TempDir(), true); err != nil {
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
		if worktree.Path == linked {
			candidate = worktree
		}
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
