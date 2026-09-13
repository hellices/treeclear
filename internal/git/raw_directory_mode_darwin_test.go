package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestClientStatusRawRetainsAccessibleDirectoryMode(test *testing.T) {
	repository := testutil.NewRepository(test)
	blob := repository.Git(test, "rev-parse", "HEAD:seed.txt")
	wantIndex := []byte("H 100644 " + blob + " 0\tseed.txt\x00")
	wantRaw := []byte("1 .T N... 100644 100644 040000 " + blob + " " + blob + " seed.txt\x00")
	assertRawDirectoryMode(test, repository, wantIndex, wantRaw, domain.GitStatus{Unstaged: 1})
}

func TestClientStatusRawRetainsUnmergedAccessibleDirectoryMode(test *testing.T) {
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
	wantIndex := []byte("M 100644 " + baseBlob + " 1\tseed.txt\x00" +
		"M 100644 " + oursBlob + " 2\tseed.txt\x00" +
		"M 100644 " + theirsBlob + " 3\tseed.txt\x00")
	wantRaw := []byte("u UU N... 100644 100644 100644 040000 " + baseBlob + " " + oursBlob + " " + theirsBlob + " seed.txt\x00")
	assertRawDirectoryMode(test, repository, wantIndex, wantRaw, domain.GitStatus{Unmerged: 1})
}

func assertRawDirectoryMode(test *testing.T, repository *testutil.Repository, wantIndex, wantRaw []byte, wantStatus domain.GitStatus) {
	test.Helper()
	if err := os.Chmod(filepath.Dir(repository.Root), 0o700); err != nil {
		test.Fatal(err)
	}
	for _, ancestor := range []string{filepath.Dir(repository.Root), repository.Root} {
		info, err := os.Stat(ancestor)
		if err != nil {
			test.Fatal(err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			test.Fatalf("fixture ancestor is not private: %q, mode %v", ancestor, info.Mode())
		}
	}
	nested := testutil.NewRepository(test)
	target := filepath.Join(repository.Root, "seed.txt")
	if err := os.Remove(target); err != nil {
		test.Fatal(err)
	}
	if err := os.Rename(nested.Root, target); err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() {
		if err := os.Chmod(target, 0o700); err != nil {
			test.Errorf("restore temporary directory access: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(test.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/chmod", "+a", "everyone allow list,search,readattr,readextattr,readsecurity", target)
	command.Dir = repository.Root
	command.Env = []string{"LC_ALL=C"}
	if output, err := command.CombinedOutput(); err != nil {
		test.Fatalf("add ACL to temporary embedded repository: %v\n%s", err, output)
	}
	if err := os.Chmod(target, 0); err != nil {
		test.Fatal(err)
	}
	var stat syscall.Stat_t
	if err := syscall.Lstat(target, &stat); err != nil {
		test.Fatal(err)
	}
	if stat.Mode != syscall.S_IFDIR {
		test.Fatalf("directory stat mode = %06o, want 040000", stat.Mode)
	}
	if entries, err := os.ReadDir(target); err != nil || len(entries) == 0 {
		test.Fatalf("ACL directory is inaccessible: entries=%d, error=%v", len(entries), err)
	}
	if head, err := os.ReadFile(filepath.Join(target, ".git", "HEAD")); err != nil || len(head) == 0 {
		test.Fatalf("embedded repository HEAD is inaccessible: %q, %v", head, err)
	}
	indexPath := filepath.Join(repository.Root, ".git", "index")
	indexBefore, err := os.ReadFile(indexPath)
	if err != nil {
		test.Fatal(err)
	}
	client, captured := captureRawFixtureReads(test, repository.Root)
	runner := client.Runner
	private := test.TempDir()
	if err := os.Chmod(private, 0o700); err != nil {
		test.Fatal(err)
	}
	client.Runner = runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		request.Env = append(request.Env, "HOME="+private, "USERPROFILE="+private,
			"XDG_CONFIG_HOME="+private, "XDG_DATA_HOME="+private, "XDG_CACHE_HOME="+private,
			"TMPDIR="+private, "TEMP="+private, "TMP="+private,
			"GIT_CEILING_DIRECTORIES="+filepath.Dir(repository.Root))
		return runner.Run(ctx, request)
	})
	status, raw, statusError := client.StatusRaw(test.Context(), repository.Root)
	indexAfter, err := os.ReadFile(indexPath)
	if err != nil || !bytes.Equal(indexBefore, indexAfter) {
		test.Fatalf("StatusRaw changed the fixture index: %v", err)
	}
	for _, name := range []string{"config", "ls-files", "status"} {
		if len(captured[name]) != 1 {
			test.Fatalf("%s calls = %d, want exactly one", name, len(captured[name]))
		}
	}
	assertCapturedRawBytes(test, captured, "ls-files", 0, wantIndex)
	assertCapturedRawBytes(test, captured, "status", 0, wantRaw)
	test.Logf("captured actual-Git directory-mode record: %q", captured["status"][0])
	if statusError != nil || status != wantStatus {
		test.Fatalf("directory-mode status = %#v, bytes %q, error %v; want %#v", status, raw, statusError, wantStatus)
	}
	assertCapturedRawBytes(test, captured, "status", 0, raw)
}
