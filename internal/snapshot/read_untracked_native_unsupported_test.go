//go:build !darwin && !linux && !windows

package snapshot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadUntrackedNativeUnsupportedPlatform(test *testing.T) {
	directory := test.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "leaf.bin"), []byte("local fixture"), 0o600); err != nil {
		test.Fatal(err)
	}
	entries, err := ReadUntracked(test.Context(), directory, []string{"leaf.bin"}, 64)
	if entries != nil || !errors.Is(err, errors.ErrUnsupported) {
		test.Fatalf("unsupported source reader did not fail closed: %#v, %v", entries, err)
	}
}
