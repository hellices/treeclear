//go:build darwin || linux

package snapshot

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hellices/treeclear/internal/testutil"
)

func TestUntrackedReadGitNativeModesAndDanglingLink(test *testing.T) {
	repository := testutil.NewRepository(test)
	worktree := repository.AddWorktree(test, "untracked-native", "topic")
	filename := filepath.Join(worktree, "binary.bin")
	directory := filepath.Join(worktree, "nested")
	if err := os.WriteFile(filename, []byte{0, 0xff, '\n', 1}, 0o600); err != nil {
		test.Fatal(err)
	}
	if err := os.Chmod(filename, 0o640|fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky); err != nil {
		test.Fatal(err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		test.Fatal(err)
	}
	if err := os.Chmod(directory, 0o750|fs.ModeSetgid|fs.ModeSticky); err != nil {
		test.Fatal(err)
	}
	if err := os.Symlink("../missing.bin", filepath.Join(directory, "link")); err != nil {
		test.Fatal(err)
	}
	names := []string{"binary.bin", "nested", "nested/link"}
	wanted := untrackedReadIntegrationEntries(test, worktree, names)
	if wanted[0].Mode&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky || wanted[1].Mode&(fs.ModeSetgid|fs.ModeSticky) != fs.ModeSetgid|fs.ModeSticky {
		test.Fatal("native fixture lacks the required original special modes")
	}
	actual, err := ReadUntracked(test.Context(), worktree, []string{"nested/link", "binary.bin"}, 4)
	if err != nil || !reflect.DeepEqual(actual, wanted) {
		test.Fatalf("native source modes or non-dereferenced link text changed: got %#v; %v", actual, err)
	}
	archive, err := EncodeUntracked(actual, 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	assertUntrackedStandardArchive(test, archive, wanted, 1<<20)
	decoded, err := DecodeUntracked(archive, 1<<20)
	if err != nil || !reflect.DeepEqual(decoded, wanted) {
		test.Fatalf("native collection did not round trip through the codec: %v", err)
	}
	if after := untrackedReadIntegrationEntries(test, worktree, names); !reflect.DeepEqual(after, wanted) {
		test.Fatal("native collection changed its source fixture")
	}
}
