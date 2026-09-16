//go:build darwin || linux

package git

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadInspectionPointerRejectsSpecialLeaves(test *testing.T) {
	directory := readonlyIndexCanonicalTemporaryDirectory(test)
	outside := filepath.Join(readonlyIndexCanonicalTemporaryDirectory(test), "owned-target")
	if err := os.WriteFile(outside, []byte("owned out-of-root evidence"), 0o600); err != nil {
		test.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "symlink")); err != nil {
		test.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(directory, "fifo"), 0o600); err != nil {
		test.Fatal(err)
	}
	for _, name := range []string{"symlink", "fifo"} {
		actual, err := readInspectionPointer(test.Context(), directory, name)
		if !errors.Is(err, fs.ErrInvalid) || actual != nil {
			test.Fatalf("special %s pointer returned %d bytes, error %v", name, len(actual), err)
		}
	}
}
