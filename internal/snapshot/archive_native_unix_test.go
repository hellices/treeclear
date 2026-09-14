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

func TestUntrackedArchiveGitNativeModesAndLink(test *testing.T) {
	const byteBudget = 1 << 20
	repository := testutil.NewRepository(test)
	worktree := repository.AddWorktree(test, "archive-native", "topic")
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
	if err := os.Symlink("../binary.bin", filepath.Join(directory, "link")); err != nil {
		test.Fatal(err)
	}
	expected := untrackedArchiveNativeFixture(test, worktree)
	if expected[0].Mode&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky || expected[1].Mode&(fs.ModeSetgid|fs.ModeSticky) != fs.ModeSetgid|fs.ModeSticky {
		test.Fatal("native fixture did not preserve the required special modes")
	}
	archive, err := EncodeUntracked([]UntrackedEntry{expected[2], expected[0], expected[1]}, byteBudget)
	if err != nil {
		test.Fatalf("encode native archive fixture: %v", err)
	}
	assertUntrackedStandardArchive(test, archive, expected, byteBudget)
	actual, err := DecodeUntracked(archive, byteBudget)
	if err != nil || !reflect.DeepEqual(actual, expected) {
		test.Fatalf("native modes/link target did not round trip: %#v, %v", actual, err)
	}
	if after := untrackedArchiveNativeFixture(test, worktree); !reflect.DeepEqual(after, expected) {
		test.Fatal("codec changed its native source fixture")
	}
}

func untrackedArchiveNativeFixture(test *testing.T, worktree string) []UntrackedEntry {
	test.Helper()
	var entries []UntrackedEntry
	for _, name := range []string{"binary.bin", "nested", "nested/link"} {
		filename := filepath.Join(worktree, filepath.FromSlash(name))
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
			test.Fatalf("unsupported fixture mode: %s", information.Mode())
		}
		if err != nil {
			test.Fatal(err)
		}
		entries = append(entries, entry)
	}
	return entries
}
