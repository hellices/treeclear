//go:build darwin || linux || windows

package snapshot

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/git"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestStatusSelectionNativeGitReadUntrackedCodecComposition(test *testing.T) {
	const archiveBudget = 1 << 20
	repository := testutil.NewRepository(test)
	for name, contents := range map[string][]byte{
		".gitignore":        []byte("ignored.tmp\nignored-tree/\n"),
		"rename source.txt": []byte("tracked rename fixture\n"),
	} {
		if err := os.WriteFile(filepath.Join(repository.Root, name), contents, 0o600); err != nil {
			test.Fatal(err)
		}
	}
	repository.Git(test, "add", "--", ".gitignore", "rename source.txt")
	repository.Git(test, "commit", "-m", "Add status selection fixtures")
	repository.Git(test, "config", "status.renames", "true")
	worktree := repository.AddWorktree(test, "status selection", "topic/status-selection")
	if err := os.WriteFile(filepath.Join(worktree, "seed.txt"), []byte("staged fixture\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	repository.Git(test, "-C", worktree, "add", "--", "seed.txt")
	repository.Git(test, "-C", worktree, "mv", "--", "rename source.txt", "renamed tracked.txt")
	for _, directory := range []string{"assets/nested", "ignored-tree"} {
		if err := os.MkdirAll(filepath.Join(worktree, filepath.FromSlash(directory)), 0o750); err != nil {
			test.Fatal(err)
		}
	}
	payload := bytes.Repeat([]byte{0, 1, 0xff, '\r', '\n'}, 257)
	fakeEnvironment := []byte("FAKE_TASK8F_FIXTURE=not-a-secret\n")
	notes := []byte("selected fixture notes\r\n")
	for name, source := range map[string]struct {
		contents []byte
		mode     fs.FileMode
	}{
		".env":                               {fakeEnvironment, 0o600},
		"assets/nested/original payload.bin": {payload, 0o640},
		"notes.txt":                          {notes, 0o750},
		"seed.txt":                           {[]byte("unstaged fixture\r\n"), 0o600},
		"ignored.tmp":                        {[]byte("ignored fixture\n"), 0o600},
		"assets/ignored.tmp":                 {[]byte("ignored nested fixture\n"), 0o600},
		"ignored-tree/cache.bin":             {[]byte{0, 2, 0xfe}, 0o600},
	} {
		if err := os.WriteFile(filepath.Join(worktree, filepath.FromSlash(name)), source.contents, source.mode); err != nil {
			test.Fatal(err)
		}
	}
	for _, name := range []string{"ignored.tmp", "assets/ignored.tmp", "ignored-tree/cache.bin"} {
		if actual := repository.Git(test, "-C", worktree, "check-ignore", "--", name); actual != name {
			test.Fatalf("ignored control %q is not actually ignored: %q", name, actual)
		}
	}
	wantedPaths := []string{".env", "assets/nested/original payload.bin", "notes.txt"}
	wantedNames := []string{".env", "assets", "assets/nested", "assets/nested/original payload.bin", "notes.txt"}
	wanted := untrackedReadIntegrationEntries(test, worktree, wantedNames)
	sourceNames := append(slices.Clone(wantedNames), ".git", ".gitignore", "seed.txt", "renamed tracked.txt", "ignored.tmp", "assets/ignored.tmp", "ignored-tree", "ignored-tree/cache.bin")
	sourcesBefore := untrackedReadIntegrationEntries(test, worktree, sourceNames)
	indexPath := repository.Git(test, "-C", worktree, "rev-parse", "--path-format=absolute", "--git-path", "index")
	indexBefore, err := os.ReadFile(indexPath)
	if err != nil {
		test.Fatal(err)
	}
	runner := &statusSelectionNativeRunner{}
	client := git.NewClient(runner)
	selection, err := client.StatusSnapshot(test.Context(), worktree)
	wantedStatus := domain.GitStatus{Staged: 2, Unstaged: 1, Untracked: len(wantedPaths)}
	if err != nil || selection.Status != wantedStatus || !slices.Equal(selection.UntrackedPaths, wantedPaths) {
		test.Fatalf("native Git selection = %#v, error %v; want status %#v and paths %q", selection, err, wantedStatus, wantedPaths)
	}
	if len(runner.statusOutputs) != 1 || !bytes.Equal(selection.Raw, runner.statusOutputs[0]) {
		test.Fatalf("selection does not retain the single native status payload: %q", selection.Raw)
	}
	originalRaw := bytes.Clone(selection.Raw)
	originalPaths := slices.Clone(selection.UntrackedPaths)
	rawBudget := int64(len(payload) + len(fakeEnvironment) + len(notes))
	entries, err := ReadUntracked(test.Context(), worktree, selection.UntrackedPaths, rawBudget)
	if err != nil || !reflect.DeepEqual(entries, wanted) {
		test.Fatalf("Git-selected source bytes, native modes or required parents changed: got %#v; want %#v; error %v", entries, wanted, err)
	}
	archive, err := EncodeUntracked(entries, archiveBudget)
	if err != nil {
		test.Fatalf("Git-selected entries are not codec-compatible: %v", err)
	}
	assertUntrackedStandardArchive(test, archive, wanted, archiveBudget)
	decoded, err := DecodeUntracked(archive, archiveBudget)
	if err != nil || !reflect.DeepEqual(decoded, wanted) || !reflect.DeepEqual(entries, wanted) {
		test.Fatalf("Git-selected entries failed to round trip or codec changed its input: %v", err)
	}
	if selection.Status != wantedStatus || !bytes.Equal(selection.Raw, originalRaw) || !slices.Equal(selection.UntrackedPaths, originalPaths) {
		test.Fatal("reader/codec composition changed the Git observation input")
	}
	if !slices.Equal(runner.commands, []string{"rev-parse", "config", "ls-files", "status"}) || len(runner.statusOutputs) != 1 {
		test.Fatalf("composition did not use exactly one guarded Git status observation: %q", runner.commands)
	}
	if sourcesAfter := untrackedReadIntegrationEntries(test, worktree, sourceNames); !reflect.DeepEqual(sourcesAfter, sourcesBefore) {
		test.Fatal("composition changed selected, tracked, ignored, parent or Git-pointer source entries")
	}
	if indexAfter, err := os.ReadFile(indexPath); err != nil || !bytes.Equal(indexAfter, indexBefore) {
		test.Fatalf("composition rewrote the Git index: %v", err)
	}
}

type statusSelectionNativeRunner struct {
	commands      []string
	statusOutputs [][]byte
}

func (runner *statusSelectionNativeRunner) Run(ctx context.Context, request execx.Request) (execx.Result, error) {
	result, err := (execx.OSRunner{}).Run(ctx, request)
	if len(request.Args) != 0 {
		runner.commands = append(runner.commands, request.Args[0])
		if request.Args[0] == "status" {
			runner.statusOutputs = append(runner.statusOutputs, bytes.Clone(result.Stdout))
		}
	}
	return result, err
}
