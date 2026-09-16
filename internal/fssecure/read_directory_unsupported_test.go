//go:build !darwin

package fssecure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadPrivateDirectoryUnsupportedWithoutFilesystemAccess(test *testing.T) {
	parent := test.TempDir()
	path := filepath.Join(parent, "snapshot")
	if err := os.WriteFile(path, []byte("not a directory; preserve me"), 0o600); err != nil {
		test.Fatal(err)
	}
	before, err := os.Lstat(path)
	if err != nil {
		test.Fatal(err)
	}
	for _, target := range []string{path, filepath.Join(parent, "absent", "snapshot")} {
		files, err := ReadPrivateDirectory(context.Background(), target, []PrivateFileLimit{{Name: "manifest.json", MaximumBytes: 16}}, 16)
		if files != nil || !errors.Is(err, errors.ErrUnsupported) {
			test.Fatalf("unsupported read = %v, %v; want nil, ErrUnsupported", files, err)
		}
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		test.Fatalf("unsupported read changed fixture: %v", err)
	}
	assertContents(test, path, []byte("not a directory; preserve me"))
	assertOnlyNames(test, parent, "snapshot")
}
