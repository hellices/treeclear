package snapshot

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/pathutil"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestManifestPreservesGitAtSignBranch(test *testing.T) {
	repository := testutil.NewRepository(test)
	if actual := repository.Git(test, "check-ref-format", "--branch", "@"); actual != "@" {
		test.Fatalf("Git branch validation = %q", actual)
	}
	worktree := repository.AddWorktree(test, "at-sign", "@")
	value := gitManifestFixture(test, repository, worktree)
	qualified := repository.Git(test, "rev-parse", "refs/heads/@")
	repository.Git(test, "commit", "--allow-empty", "-m", "Advance primary fixture")
	if repository.Git(test, "rev-parse", "@") == qualified {
		test.Fatal("fixture does not distinguish a branch from revision shorthand")
	}
	contents, err := EncodeManifest(value)
	if err != nil {
		test.Fatal(err)
	}
	loaded, err := DecodeManifest(contents)
	if err != nil || loaded.Branch != "@" || loaded.Head != qualified || loaded.RecoveryRef != "" {
		test.Fatalf("attached @ branch did not round trip: %#v, %v", loaded, err)
	}
}

func TestManifestPreservesSharedGitAdministrativeModes(test *testing.T) {
	if runtime.GOOS == "windows" {
		test.Skip("Git setgid inheritance is a Unix permission feature")
	}
	repository := testutil.NewRepository(test)
	repository.Git(test, "init", "--shared=group")
	worktree := repository.AddWorktree(test, "shared-mode", "topic")
	value := gitManifestFixture(test, repository, worktree)
	foundSetgid := false
	for _, entry := range value.AdministrativeEntries {
		foundSetgid = foundSetgid || entry.Kind == "directory" && entry.Mode&fs.ModeSetgid != 0
	}
	if !foundSetgid {
		test.Fatal("shared Git fixture did not generate a setgid administrative directory")
	}
	contents, err := EncodeManifest(value)
	if err != nil {
		test.Fatal(err)
	}
	loaded, err := DecodeManifest(contents)
	if err != nil {
		test.Fatal(err)
	}
	expected := slices.Clone(value.AdministrativeEntries)
	slices.SortFunc(expected, func(left, right AdminEntry) int { return strings.Compare(left.Path, right.Path) })
	if !reflect.DeepEqual(loaded.AdministrativeEntries, expected) {
		test.Fatal("original Git administrative modes or bytes were lost")
	}
}

func gitManifestFixture(test *testing.T, repository *testutil.Repository, worktree string) Manifest {
	test.Helper()
	value := manifestFixture()
	value.RepositoryRoot = repository.Root
	value.WorktreePath = worktree
	var err error
	value.AdminDir, err = pathutil.Canonical(repository.Git(test, "-C", worktree, "rev-parse", "--absolute-git-dir"))
	if err != nil {
		test.Fatal(err)
	}
	value.CommonGitDir, err = pathutil.Canonical(repository.Git(test, "-C", worktree, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	if err != nil {
		test.Fatal(err)
	}
	value.Head = repository.Git(test, "-C", worktree, "rev-parse", "--verify", "HEAD")
	value.Branch = strings.TrimPrefix(repository.Git(test, "-C", worktree, "symbolic-ref", "HEAD"), "refs/heads/")
	value.AdministrativeEntries = nil
	err = filepath.WalkDir(value.AdminDir, func(filename string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		metadata, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(value.AdminDir, filename)
		if err != nil {
			return err
		}
		diagnostic := AdminEntry{Path: filepath.ToSlash(relative), Mode: metadata.Mode()}
		if metadata.IsDir() {
			diagnostic.Kind = "directory"
		} else if metadata.Mode().IsRegular() {
			diagnostic.Kind = "file"
			diagnostic.Data, err = os.ReadFile(filename)
			if err != nil {
				return err
			}
		} else {
			test.Fatalf("unexpected fixture metadata kind: %s", metadata.Mode())
		}
		value.AdministrativeEntries = append(value.AdministrativeEntries, diagnostic)
		return nil
	})
	if err != nil {
		test.Fatal(err)
	}
	return value
}
