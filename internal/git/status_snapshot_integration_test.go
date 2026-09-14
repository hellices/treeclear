package git

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestClientStatusSnapshotNativeSelectionPreservesSources(test *testing.T) {
	for _, kind := range []string{"primary", "linked"} {
		test.Run(kind, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			renameSource := "rename source.txt"
			if runtime.GOOS != "windows" {
				renameSource = "? rename source.txt"
			}
			renameContents := []byte("native rename fixture\n")
			ignoreContents := []byte("*.ignored\nignored-dir/\n")
			writeRawFixtureFile(test, filepath.Join(repository.Root, renameSource), renameContents)
			writeRawFixtureFile(test, filepath.Join(repository.Root, ".gitignore"), ignoreContents)
			repository.Git(test, "add", "--", renameSource, ".gitignore")
			repository.Git(test, "commit", "-m", "Add native status fixtures")
			repository.Git(test, "config", "status.renames", "true")
			worktree := repository.Root
			if kind == "linked" {
				worktree = repository.AddWorktree(test, "status snapshot", "topic/status-snapshot")
			}
			writeRawFixtureFile(test, filepath.Join(worktree, "seed.txt"), []byte("staged fixture\n"))
			repository.Git(test, "-C", worktree, "add", "--", "seed.txt")
			repository.Git(test, "-C", worktree, "mv", "--", renameSource, "renamed with spaces.txt")
			for _, directory := range []string{"nested/deeper", "ignored-dir"} {
				if err := os.MkdirAll(filepath.Join(worktree, filepath.FromSlash(directory)), 0o750); err != nil {
					test.Fatal(err)
				}
			}
			sources := map[string][]byte{
				".gitignore":                       ignoreContents,
				"seed.txt":                         []byte("unstaged fixture\r\n"),
				"renamed with spaces.txt":          renameContents,
				"alpha with spaces.txt":            []byte("untracked text\r\n"),
				"nested/deeper/original bytes.bin": {0, 1, 0xff, '\n'},
				"z-last.txt":                       []byte("last untracked fixture\n"),
				"root.ignored":                     []byte("ignored root fixture\n"),
				"nested/child.ignored":             []byte("ignored nested fixture\n"),
				"ignored-dir/cache.bin":            {0, 2, 0xfe},
			}
			for name, contents := range sources {
				writeRawFixtureFile(test, filepath.Join(worktree, filepath.FromSlash(name)), contents)
			}
			for _, name := range []string{"root.ignored", "nested/child.ignored", "ignored-dir/cache.bin"} {
				if actual := repository.Git(test, "-C", worktree, "check-ignore", "--", name); actual != name {
					test.Fatalf("ignored control %q is not actually ignored: %q", name, actual)
				}
			}
			indexPath := repository.Git(test, "-C", worktree, "rev-parse", "--path-format=absolute", "--git-path", "index")
			indexBefore, err := os.ReadFile(indexPath)
			if err != nil {
				test.Fatal(err)
			}
			client, captured := captureRawFixtureReads(test, worktree)
			actual, err := client.StatusSnapshot(test.Context(), worktree)
			wantStatus := domain.GitStatus{Staged: 2, Unstaged: 1, Untracked: 3}
			wantPaths := []string{"alpha with spaces.txt", "nested/deeper/original bytes.bin", "z-last.txt"}
			if err != nil || actual.Status != wantStatus || !slices.Equal(actual.UntrackedPaths, wantPaths) {
				test.Fatalf("native status snapshot = %#v, error %v; want status %#v and paths %q (rename source %q)", actual, err, wantStatus, wantPaths, renameSource)
			}
			assertCapturedRawBytes(test, captured, "status", 0, actual.Raw)
			if len(captured["status"]) != 1 {
				test.Fatalf("status observations = %d, want one", len(captured["status"]))
			}
			if !bytes.Contains(actual.Raw, []byte("2 R. N... ")) || !bytes.Contains(actual.Raw, []byte(" R100 renamed with spaces.txt\x00"+renameSource+"\x00")) {
				test.Fatalf("native rename/source records missing from %q", actual.Raw)
			}
			for name, wanted := range sources {
				remaining, err := os.ReadFile(filepath.Join(worktree, filepath.FromSlash(name)))
				if err != nil || !bytes.Equal(remaining, wanted) {
					test.Errorf("status snapshot changed source %q: %v", name, err)
				}
			}
			if _, err := os.Lstat(filepath.Join(worktree, renameSource)); !errors.Is(err, os.ErrNotExist) {
				test.Errorf("status snapshot recreated rename source %q: %v", renameSource, err)
			}
			if indexAfter, err := os.ReadFile(indexPath); err != nil || !bytes.Equal(indexAfter, indexBefore) {
				test.Errorf("status snapshot rewrote the Git index: %v", err)
			}
		})
	}
}

func TestClientStatusSnapshotNativeRootRejectionPreservesLegacySummary(test *testing.T) {
	for _, scenario := range []string{"primary-subdirectory", "linked-subdirectory", "configured-root"} {
		test.Run(scenario, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			effectiveRoot := repository.Root
			if scenario == "linked-subdirectory" {
				effectiveRoot = repository.AddWorktree(test, "nested status", "topic/nested-status")
			}
			requestedRoot := filepath.Join(effectiveRoot, "nested")
			if scenario == "configured-root" {
				requestedRoot = repository.Root
				effectiveRoot = repository.AddWorktree(test, "alternate root", "topic/alternate-root")
				repository.Git(test, "config", "core.worktree", effectiveRoot)
			} else if err := os.Mkdir(requestedRoot, 0o750); err != nil {
				test.Fatal(err)
			}
			writeRawFixtureFile(test, filepath.Join(effectiveRoot, "untracked.txt"), []byte("fixture-owned effective root\n"))
			client, captured := captureRawFixtureReads(test, requestedRoot)
			wanted := domain.GitStatus{Untracked: 1}
			legacy, err := client.Status(test.Context(), requestedRoot)
			if err != nil || legacy != wanted {
				test.Fatalf("legacy Status rejected the native fixture: %#v, %v", legacy, err)
			}
			legacy, raw, err := client.StatusRaw(test.Context(), requestedRoot)
			if err != nil || legacy != wanted || !bytes.Contains(raw, []byte("? untracked.txt\x00")) {
				test.Fatalf("legacy StatusRaw rejected the native fixture: %#v, %q, %v", legacy, raw, err)
			}
			assertCapturedRawBytes(test, captured, "status", 1, raw)
			clear(captured)
			actual, err := client.StatusSnapshot(test.Context(), requestedRoot)
			if err == nil || actual.Status != (domain.GitStatus{}) || actual.Raw != nil || actual.UntrackedPaths != nil {
				test.Fatalf("native root mismatch must return an error and the full zero result: %#v, %v", actual, err)
			}
			if len(captured) != 1 || len(captured["rev-parse"]) != 1 {
				test.Fatalf("root mismatch ran commands beyond one root check: %#v", captured)
			}
		})
	}
}
