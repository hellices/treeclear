//go:build darwin || linux || windows

package snapshot

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/testutil"
)

func TestUntrackedReadGitCodecAndManifestComposition(test *testing.T) {
	const archiveBudget = 1 << 20
	repository := testutil.NewRepository(test)
	worktree := repository.AddWorktree(test, "untracked-read", "topic")
	if err := os.MkdirAll(filepath.Join(worktree, "assets", "nested"), 0o750); err != nil {
		test.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0, 1, 0xff, '\n'}, 16385)
	environmentFixture := []byte("SYNTHETIC_FIXTURE=not-a-secret\n")
	notes := []byte("selected fixture notes\r\n")
	for name, contents := range map[string][]byte{
		"assets/nested/원본 payload.bin": payload,
		".env":                         environmentFixture,
		"notes.txt":                    notes,
		".gitignore":                   []byte("ignored.tmp\n"),
		"ignored.tmp":                  []byte("unrequested ignored fixture\n"),
		"assets/unrequested.bin":       []byte("unrequested sibling\n"),
	} {
		if err := os.WriteFile(filepath.Join(worktree, filepath.FromSlash(name)), contents, 0o600); err != nil {
			test.Fatal(err)
		}
	}
	if ignored := repository.Git(test, "-C", worktree, "check-ignore", "--", "ignored.tmp"); ignored != "ignored.tmp" {
		test.Fatalf("ignored control is not actually ignored: %q", ignored)
	}
	wantedNames := []string{".env", "assets", "assets/nested", "assets/nested/원본 payload.bin", "notes.txt"}
	wanted := untrackedReadIntegrationEntries(test, worktree, wantedNames)
	sourceNames := append(slices.Clone(wantedNames), ".git", ".gitignore", "ignored.tmp", "assets/unrequested.bin", "seed.txt")
	before := untrackedReadIntegrationEntries(test, worktree, sourceNames)
	manifest := gitManifestFixture(test, repository, worktree)
	paths := []string{"notes.txt", "assets/nested/원본 payload.bin", ".env"}
	originalPaths := slices.Clone(paths)
	rawBudget := int64(len(payload) + len(environmentFixture) + len(notes))
	actual, err := ReadUntracked(test.Context(), worktree, paths, rawBudget)
	if err != nil {
		test.Fatalf("read explicitly selected fixture leaves: %v", err)
	}
	if !reflect.DeepEqual(actual, wanted) {
		test.Fatalf("source bytes, modes, parents or selection changed: got %#v; want %#v", actual, wanted)
	}
	if !slices.Equal(paths, originalPaths) {
		test.Fatal("reader changed caller-owned path order")
	}
	archive, err := EncodeUntracked(actual, archiveBudget)
	if err != nil {
		test.Fatalf("collected entries are not codec-compatible: %v", err)
	}
	assertUntrackedStandardArchive(test, archive, wanted, archiveBudget)
	decoded, err := DecodeUntracked(archive, archiveBudget)
	if err != nil || !reflect.DeepEqual(decoded, wanted) {
		test.Fatalf("collected source entries did not round trip: %v", err)
	}
	manifest.UntrackedFiles = len(paths)
	manifest.UntrackedBytes = rawBudget
	payloads := payloadFixture()
	payloads["untracked.tar.gz"] = archive
	manifest.Files["untracked.tar.gz"] = fmt.Sprintf("sha256:%x", sha256.Sum256(archive))
	encodedManifest, err := EncodeManifest(manifest)
	if err != nil {
		test.Fatal(err)
	}
	loadedManifest, err := DecodeManifest(encodedManifest)
	if err != nil {
		test.Fatal(err)
	}
	if err := VerifyPayloads(loadedManifest, payloads); err != nil {
		test.Fatalf("collected archive did not compose with manifest integrity: %v", err)
	}
	slices.Reverse(paths)
	reversedPaths := slices.Clone(paths)
	repeated, err := ReadUntracked(test.Context(), worktree, paths, rawBudget)
	if err != nil || !reflect.DeepEqual(repeated, wanted) || !slices.Equal(paths, reversedPaths) {
		test.Fatalf("collection depends on request order or changes caller paths: %v", err)
	}
	repeatedArchive, err := EncodeUntracked(repeated, archiveBudget)
	if err != nil || !bytes.Equal(repeatedArchive, archive) {
		test.Fatalf("collected archive is not deterministic: %v", err)
	}
	actual[0].Data[0] ^= 0xff
	actual[0].Mode = 0
	loadedAgain, err := ReadUntracked(test.Context(), worktree, paths, rawBudget)
	if err != nil || !reflect.DeepEqual(loadedAgain, wanted) {
		test.Fatalf("returned data aliases source or future reads: %v", err)
	}
	if after := untrackedReadIntegrationEntries(test, worktree, sourceNames); !reflect.DeepEqual(after, before) {
		test.Fatal("collection changed selected, unrequested, ignored or Git-pointer source entries")
	}
	if after := gitManifestFixture(test, repository, worktree); !reflect.DeepEqual(after.AdministrativeEntries, manifest.AdministrativeEntries) {
		test.Fatal("collection changed source Git administrative metadata")
	}
}

func untrackedReadIntegrationEntries(test *testing.T, directory string, names []string) []UntrackedEntry {
	test.Helper()
	entries := make([]UntrackedEntry, 0, len(names))
	for _, name := range names {
		filename := filepath.Join(directory, filepath.FromSlash(name))
		information, err := os.Lstat(filename)
		if err != nil {
			test.Fatal(err)
		}
		entry := UntrackedEntry{Path: name, Mode: information.Mode()}
		switch {
		case information.IsDir():
			entry.Kind = "directory"
		case information.Mode()&fs.ModeSymlink != 0:
			entry.Kind = "symlink"
			entry.LinkTarget, err = os.Readlink(filename)
		case information.Mode().IsRegular():
			entry.Kind = "file"
			entry.Data, err = os.ReadFile(filename)
		default:
			test.Fatalf("unsupported integration fixture mode for %q: %s", name, information.Mode())
		}
		if err != nil {
			test.Fatal(err)
		}
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(left, right UntrackedEntry) int { return strings.Compare(left.Path, right.Path) })
	return entries
}
