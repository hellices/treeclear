package snapshot

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/testutil"
)

func TestReadAdministrativeGitPreservesDiagnostics(test *testing.T) {
	repository := testutil.NewRepository(test)
	worktree := repository.AddWorktree(test, "diagnostics", "topic")
	if err := os.WriteFile(filepath.Join(worktree, "binary.dat"), []byte{0, 0xff, 1, '\n'}, 0o600); err != nil {
		test.Fatal(err)
	}
	repository.Git(test, "-C", worktree, "add", "--", "binary.dat")
	assertReadAdministrativeGit(test, repository, worktree)
}

func TestReadAdministrativeGitPreservesUnmergedIndex(test *testing.T) {
	repository := testutil.NewRepository(test)
	worktree := repository.AddWorktree(test, "unmerged", "ours")
	base := repository.Git(test, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(worktree, "seed.txt"), []byte("ours\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	repository.Git(test, "-C", worktree, "add", "--", "seed.txt")
	repository.Git(test, "-C", worktree, "commit", "-m", "Ours fixture")
	ours := repository.Git(test, "-C", worktree, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(repository.Root, "seed.txt"), []byte("theirs\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	repository.Git(test, "add", "--", "seed.txt")
	repository.Git(test, "commit", "-m", "Theirs fixture")
	theirs := repository.Git(test, "rev-parse", "HEAD")
	repository.Git(test, "-C", worktree, "read-tree", "-m", base, ours, theirs)
	stages := repository.Git(test, "-C", worktree, "ls-files", "--stage", "--", "seed.txt")
	for _, stage := range []string{"1", "2", "3"} {
		if !strings.Contains(stages, " "+stage+"\tseed.txt") {
			test.Fatalf("fixture lacks index stage %s: %q", stage, stages)
		}
	}
	assertReadAdministrativeGit(test, repository, worktree)
}

func TestReadAdministrativeGitPreservesSharedModes(test *testing.T) {
	if runtime.GOOS == "windows" {
		test.Skip("Git setgid inheritance is a Unix permission feature")
	}
	repository := testutil.NewRepository(test)
	repository.Git(test, "init", "--shared=group")
	worktree := repository.AddWorktree(test, "shared-diagnostics", "shared")
	entries := assertReadAdministrativeGit(test, repository, worktree)
	foundSetgid := false
	for _, entry := range entries {
		if entry.Kind == "directory" && entry.Mode&fs.ModeSetgid != 0 {
			foundSetgid = true
		}
	}
	if !foundSetgid {
		test.Fatal("shared Git fixture did not produce a setgid administrative directory")
	}
}

func assertReadAdministrativeGit(test *testing.T, repository *testutil.Repository, worktree string) []AdminEntry {
	test.Helper()
	value := gitManifestFixture(test, repository, worktree)
	expected := slices.Clone(value.AdministrativeEntries)
	slices.SortFunc(expected, func(left, right AdminEntry) int { return strings.Compare(left.Path, right.Path) })
	foundIndex := false
	for _, entry := range expected {
		if entry.Path == "index" {
			foundIndex = true
			if !bytes.HasPrefix(entry.Data, []byte("DIRC")) || !bytes.Contains(entry.Data, []byte{0}) {
				test.Fatal("fixture lacks a native binary Git index")
			}
		}
	}
	if !foundIndex {
		test.Fatal("fixture index is missing")
	}
	actual, err := ReadAdministrative(test.Context(), value.AdminDir)
	if err != nil {
		test.Fatalf("read actual Git administrative directory: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		test.Fatalf("administrative bytes/modes/order changed: got %#v; want %#v", actual, expected)
	}
	value.AdministrativeEntries = actual
	encoded, err := EncodeManifest(value)
	if err != nil {
		test.Fatalf("collected diagnostics do not compose with the manifest: %v", err)
	}
	decoded, err := DecodeManifest(encoded)
	if err != nil || !reflect.DeepEqual(decoded.AdministrativeEntries, expected) {
		test.Fatalf("manifest round trip changed diagnostic entries: %v", err)
	}
	after := gitManifestFixture(test, repository, worktree).AdministrativeEntries
	slices.SortFunc(after, func(left, right AdminEntry) int { return strings.Compare(left.Path, right.Path) })
	if !reflect.DeepEqual(after, expected) {
		test.Fatal("reader changed fixture administrative bytes or modes")
	}
	for _, entry := range actual {
		if len(entry.Data) > 0 {
			entry.Data[0] ^= 0xff
		}
	}
	repeated, err := ReadAdministrative(test.Context(), value.AdminDir)
	if err != nil || !reflect.DeepEqual(repeated, expected) {
		test.Fatalf("returned buffers are not isolated from source or later reads: %v", err)
	}
	return repeated
}
