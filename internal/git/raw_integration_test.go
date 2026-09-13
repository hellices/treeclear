package git

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestClientRawReadsCaptureRealGitBytesWithoutIndexWrites(test *testing.T) {
	repository := testutil.NewRepository(test)
	writeRawFixtureFile(test, filepath.Join(repository.Root, "binary.bin"), []byte{0, 1, 2, 0xff})
	repository.Git(test, "add", "--", "binary.bin")
	repository.Git(test, "commit", "-m", "Add binary fixture")
	linked := repository.AddWorktree(test, "raw feature", "topic/raw")
	writeRawFixtureFile(test, filepath.Join(linked, "seed.txt"), []byte("staged seed\n"))
	writeRawFixtureFile(test, filepath.Join(linked, "binary.bin"), []byte{0, 5, 6, 0xfe})
	repository.Git(test, "-C", linked, "add", "--", "seed.txt", "binary.bin")
	writeRawFixtureFile(test, filepath.Join(linked, "seed.txt"), []byte("unstaged seed\n"))
	writeRawFixtureFile(test, filepath.Join(linked, "binary.bin"), []byte{0, 8, 9, 0xfd})
	writeRawFixtureFile(test, filepath.Join(linked, "untracked with spaces.txt"), []byte("untracked fixture\n"))
	indexPath := repository.Git(test, "-C", linked, "rev-parse", "--path-format=absolute", "--git-path", "index")
	before, err := os.ReadFile(indexPath)
	if err != nil {
		test.Fatal(err)
	}
	client, captured := captureRawFixtureReads(test, linked)
	worktrees, rawList, err := client.ListWorktreesRaw(context.Background(), linked)
	if err != nil || len(worktrees) != 2 || !bytes.HasSuffix(rawList, []byte{0, 0}) {
		test.Fatalf("inventory = %#v, bytes %q, error %v", worktrees, rawList, err)
	}
	assertCapturedRawBytes(test, captured, "worktree", 0, rawList)
	status, rawStatus, err := client.StatusRaw(context.Background(), linked)
	if err != nil || status != (domain.GitStatus{Staged: 2, Unstaged: 2, Untracked: 1}) {
		test.Fatalf("status = %#v, bytes %q, error %v", status, rawStatus, err)
	}
	assertCapturedRawBytes(test, captured, "status", 0, rawStatus)
	if !bytes.Contains(rawStatus, []byte("? untracked with spaces.txt\x00")) {
		test.Fatalf("untracked NUL record missing from %q", rawStatus)
	}
	for position, staged := range []bool{true, false} {
		patch, err := client.Diff(context.Background(), linked, staged)
		if err != nil || !bytes.Contains(patch, []byte("GIT binary patch")) {
			test.Fatalf("staged %t: patch %q, error %v", staged, patch, err)
		}
		assertCapturedRawBytes(test, captured, "diff", position, patch)
		text := []byte("+staged seed\n")
		if !staged {
			text = []byte("-staged seed\n+unstaged seed\n")
		}
		if !bytes.Contains(patch, text) {
			test.Fatalf("staged %t: text modification missing from %q", staged, patch)
		}
	}
	if len(captured["worktree"]) != 1 || len(captured["status"]) != 1 || len(captured["diff"]) != 2 {
		test.Fatalf("duplicate payload commands: list %d, status %d, diff %d", len(captured["worktree"]), len(captured["status"]), len(captured["diff"]))
	}
	after, err := os.ReadFile(indexPath)
	if err != nil || !bytes.Equal(before, after) {
		test.Fatalf("raw reads rewrote the Git index: %v", err)
	}
}

func TestClientStatusRawRetainsRealRenameSource(test *testing.T) {
	repository := testutil.NewRepository(test)
	repository.Git(test, "config", "status.renames", "true")
	repository.Git(test, "mv", "--", "seed.txt", "renamed with spaces.txt")
	client, captured := captureRawFixtureReads(test, repository.Root)
	status, raw, err := client.StatusRaw(context.Background(), repository.Root)
	if err != nil || status != (domain.GitStatus{Staged: 1}) {
		test.Fatalf("rename status = %#v, bytes %q, error %v", status, raw, err)
	}
	assertCapturedRawBytes(test, captured, "status", 0, raw)
	if !bytes.HasPrefix(raw, []byte("2 R. N... ")) || !bytes.HasSuffix(raw, []byte(" R100 renamed with spaces.txt\x00seed.txt\x00")) {
		test.Fatalf("rename source or score lost from %q", raw)
	}
}

func TestClientStatusRawRetainsRealUnmergedStageMetadata(test *testing.T) {
	repository := testutil.NewRepository(test)
	base := repository.Git(test, "rev-parse", "HEAD")
	baseBlob := repository.Git(test, "rev-parse", "HEAD:seed.txt")
	writeRawFixtureFile(test, filepath.Join(repository.Root, "seed.txt"), []byte("ours\n"))
	repository.Git(test, "add", "--", "seed.txt")
	repository.Git(test, "commit", "-m", "Ours")
	ours := repository.Git(test, "rev-parse", "HEAD")
	oursBlob := repository.Git(test, "rev-parse", "HEAD:seed.txt")
	repository.Git(test, "switch", "--create", "incoming", base)
	writeRawFixtureFile(test, filepath.Join(repository.Root, "seed.txt"), []byte("theirs\n"))
	repository.Git(test, "add", "--", "seed.txt")
	repository.Git(test, "commit", "-m", "Theirs")
	theirs := repository.Git(test, "rev-parse", "HEAD")
	theirsBlob := repository.Git(test, "rev-parse", "HEAD:seed.txt")
	repository.Git(test, "switch", "main")
	repository.Git(test, "read-tree", "-m", base, ours, theirs)
	client, captured := captureRawFixtureReads(test, repository.Root)
	status, raw, err := client.StatusRaw(context.Background(), repository.Root)
	if err != nil || status != (domain.GitStatus{Unmerged: 1}) {
		test.Fatalf("unmerged status = %#v, bytes %q, error %v", status, raw, err)
	}
	assertCapturedRawBytes(test, captured, "status", 0, raw)
	want := []byte("u UU N... 100644 100644 100644 100644 " + baseBlob + " " + oursBlob + " " + theirsBlob + " seed.txt\x00")
	if !bytes.Equal(raw, want) {
		test.Fatalf("unmerged raw metadata = %q, want %q", raw, want)
	}
}

func TestClientRawReadsPreserveUnbornAndIntentToAddState(test *testing.T) {
	repository := testutil.NewRepository(test)
	repository.Git(test, "switch", "--orphan", "unborn")
	writeRawFixtureFile(test, filepath.Join(repository.Root, "intent.txt"), []byte("intent-to-add fixture\n"))
	repository.Git(test, "add", "--intent-to-add", "--", "intent.txt")
	client, captured := captureRawFixtureReads(test, repository.Root)
	worktrees, rawList, err := client.ListWorktreesRaw(context.Background(), repository.Root)
	if err != nil || len(worktrees) != 1 || worktrees[0].Head != strings.Repeat("0", 40) || worktrees[0].Branch != "unborn" {
		test.Fatalf("unborn inventory = %#v, bytes %q, error %v", worktrees, rawList, err)
	}
	assertCapturedRawBytes(test, captured, "worktree", 0, rawList)
	status, rawStatus, err := client.StatusRaw(context.Background(), repository.Root)
	if err != nil || status.Clean() || status.Untracked != 0 || status.Unmerged != 0 {
		test.Fatalf("intent-to-add status = %#v, bytes %q, error %v", status, rawStatus, err)
	}
	assertCapturedRawBytes(test, captured, "status", 0, rawStatus)
	if !bytes.Contains(rawStatus, []byte(strings.Repeat("0", 40))) || !bytes.HasSuffix(rawStatus, []byte(" intent.txt\x00")) {
		test.Fatalf("absent-stage metadata lost from %q", rawStatus)
	}
}

func TestClientStatusRawPreservesCleanEmptyOutput(test *testing.T) {
	repository := testutil.NewRepository(test)
	client, captured := captureRawFixtureReads(test, repository.Root)
	status, raw, err := client.StatusRaw(context.Background(), repository.Root)
	if err != nil || !status.Clean() || len(raw) != 0 {
		test.Fatalf("clean status = %#v, bytes %q, error %v", status, raw, err)
	}
	assertCapturedRawBytes(test, captured, "status", 0, raw)
}

func TestClientRawReadsSupportRealSHA256Repository(test *testing.T) {
	repository := testutil.NewRepository(test)
	if err := os.RemoveAll(filepath.Join(repository.Root, ".git")); err != nil {
		test.Fatal(err)
	}
	repository.Git(test, "init", "--initial-branch=main", "--object-format=sha256")
	repository.Git(test, "add", "--", "seed.txt")
	repository.Git(test, "commit", "-m", "Initial SHA256 fixture commit")
	linked := repository.AddWorktree(test, "sha256 linked", "topic/sha256")
	writeRawFixtureFile(test, filepath.Join(linked, "seed.txt"), []byte("SHA256 working change\n"))
	client, captured := captureRawFixtureReads(test, linked)
	worktrees, rawList, err := client.ListWorktreesRaw(context.Background(), linked)
	if err != nil || len(worktrees) != 2 {
		test.Fatalf("SHA256 inventory = %#v, error %v", worktrees, err)
	}
	for _, worktree := range worktrees {
		if len(worktree.Head) != 64 {
			test.Fatalf("SHA256 HEAD = %q", worktree.Head)
		}
	}
	assertCapturedRawBytes(test, captured, "worktree", 0, rawList)
	status, rawStatus, err := client.StatusRaw(context.Background(), linked)
	if err != nil || status != (domain.GitStatus{Unstaged: 1}) {
		test.Fatalf("SHA256 status = %#v, bytes %q, error %v", status, rawStatus, err)
	}
	assertCapturedRawBytes(test, captured, "status", 0, rawStatus)
	fields := strings.SplitN(strings.TrimSuffix(string(rawStatus), "\x00"), " ", 9)
	if len(fields) != 9 || len(fields[6]) != 64 || len(fields[7]) != 64 {
		test.Fatalf("SHA256 status fields = %q", fields)
	}
}

func captureRawFixtureReads(test *testing.T, directory string) (*Client, map[string][][]byte) {
	test.Helper()
	captured := make(map[string][][]byte)
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		assertRawReadRequest(test, request, directory)
		result, err := (execx.OSRunner{}).Run(ctx, request)
		captured[request.Args[0]] = append(captured[request.Args[0]], bytes.Clone(result.Stdout))
		return result, err
	}))
	return client, captured
}

func assertCapturedRawBytes(test *testing.T, captured map[string][][]byte, command string, position int, actual []byte) {
	test.Helper()
	outputs := captured[command]
	if len(outputs) <= position || !bytes.Equal(actual, outputs[position]) {
		test.Fatalf("%s result %d was not the exact captured output: %q", command, position, actual)
	}
}

func writeRawFixtureFile(test *testing.T, path string, contents []byte) {
	test.Helper()
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		test.Fatal(err)
	}
}
