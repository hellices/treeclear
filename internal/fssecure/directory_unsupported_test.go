//go:build !darwin

package fssecure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishPrivateDirectoryUnsupportedWithoutWrites(test *testing.T) {
	parent := test.TempDir()
	files := []PrivateFile{{Name: "payload", Contents: []byte("private")}}
	for _, target := range []string{filepath.Join(parent, "snapshot"), filepath.Join(parent, "missing", "snapshot")} {
		path, err := PublishPrivateDirectory(context.Background(), target, files)
		if path != "" || !errors.Is(err, errors.ErrUnsupported) {
			test.Fatalf("unsupported publication = %q, %v", path, err)
		}
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		test.Fatalf("unsupported publication wrote data: %v, %v", entries, err)
	}
}
