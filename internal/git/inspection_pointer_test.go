package git

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadInspectionPointerBoundsAndCancellation(test *testing.T) {
	for _, size := range []int{0, 1, maxInspectionPointerBytes, maxInspectionPointerBytes + 1} {
		directory := readonlyIndexCanonicalTemporaryDirectory(test)
		contents := bytes.Repeat([]byte("x"), size)
		if err := os.WriteFile(filepath.Join(directory, "gitdir"), contents, 0o600); err != nil {
			test.Fatal(err)
		}
		actual, err := readInspectionPointer(test.Context(), directory, "gitdir")
		if size > maxInspectionPointerBytes {
			if !errors.Is(err, ErrReadLimit) || actual != nil {
				test.Fatalf("oversized pointer returned %d bytes, error %v", len(actual), err)
			}
		} else if err != nil || !bytes.Equal(actual, contents) {
			test.Fatalf("size %d: pointer returned %d bytes, error %v", size, len(actual), err)
		}
		ctx, cancel := context.WithCancel(test.Context())
		cancel()
		actual, err = readInspectionPointer(ctx, directory, "gitdir")
		if !errors.Is(err, context.Canceled) || actual != nil {
			test.Fatalf("canceled pointer returned %d bytes, error %v", len(actual), err)
		}
	}
}

func TestReadInspectionPointerRefusesUnknownEvidence(test *testing.T) {
	directory := readonlyIndexCanonicalTemporaryDirectory(test)
	for _, name := range []string{"missing", "directory"} {
		if name == "directory" {
			if err := os.Mkdir(filepath.Join(directory, name), 0o700); err != nil {
				test.Fatal(err)
			}
		}
		actual, err := readInspectionPointer(test.Context(), directory, name)
		if err == nil || errors.Is(err, ErrWorktreeChanged) || actual != nil {
			test.Fatalf("unknown %s pointer returned %d bytes, error %v", name, len(actual), err)
		}
		if name == "missing" && !errors.Is(err, fs.ErrNotExist) {
			test.Fatalf("missing pointer lost its filesystem cause: %v", err)
		}
	}
}

func TestInspectionPointerPaths(test *testing.T) {
	directory := readonlyIndexCanonicalTemporaryDirectory(test)
	for _, value := range []string{"", "invalid\x00path", strings.Repeat("x", maxInspectionPointerBytes+1)} {
		actual, err := inspectionPointerPath(directory, value)
		if err == nil || actual != "" || errors.Is(err, ErrWorktreeChanged) {
			test.Fatalf("invalid pointer %q returned %q, error %v", value, actual, err)
		}
	}
	for _, value := range []string{"../target/.git", filepath.Join(directory, "space target", ".git")} {
		actual, err := inspectionPointerPath(directory, filepath.ToSlash(value))
		want := value
		if !filepath.IsAbs(value) {
			want = filepath.Join(directory, value)
		}
		if err != nil || actual != want {
			test.Fatalf("pointer %q resolved to %q, want %q, error %v", value, actual, want, err)
		}
	}
}
