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
		actual, err := inspectionPointerPath(test.Context(), directory, value)
		if err == nil || actual != "" || errors.Is(err, ErrWorktreeChanged) {
			test.Fatalf("invalid pointer %q returned %q, error %v", value, actual, err)
		}
	}
	base := filepath.Join(directory, "nested")
	for _, name := range []string{"nested", "target", "space target"} {
		if err := os.Mkdir(filepath.Join(directory, name), 0o700); err != nil {
			test.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name, ".git"), []byte("owned marker"), 0o600); err != nil {
			test.Fatal(err)
		}
	}
	for _, scenario := range []struct {
		value string
		want  string
	}{
		{"../target/.git", filepath.Join(directory, "target", ".git")},
		{filepath.Join(directory, "space target", ".git"), filepath.Join(directory, "space target", ".git")},
	} {
		actual, err := inspectionPointerPath(test.Context(), base, filepath.ToSlash(scenario.value))
		if err != nil || actual != scenario.want {
			test.Fatalf("pointer %q resolved to %q, want %q, error %v", scenario.value, actual, scenario.want, err)
		}
		ctx, cancel := context.WithCancel(test.Context())
		cancel()
		actual, err = inspectionPointerPath(ctx, base, scenario.value)
		if !errors.Is(err, context.Canceled) || actual != "" {
			test.Fatalf("canceled pointer resolved to %q, error %v", actual, err)
		}
	}
	actual, err := inspectionPointerPath(test.Context(), base, "missing")
	if !errors.Is(err, fs.ErrNotExist) || actual != "" {
		test.Fatalf("missing pointer target resolved to %q, error %v", actual, err)
	}
}

func TestInspectionPointerPathsRejectInvalidEncoding(test *testing.T) {
	directory := readonlyIndexCanonicalTemporaryDirectory(test)
	if err := os.Mkdir(filepath.Join(directory, "target\ufffd"), 0o700); err != nil {
		test.Fatal(err)
	}
	actual, err := inspectionPointerPath(test.Context(), directory, "target\xff")
	if !errors.Is(err, fs.ErrInvalid) || actual != "" {
		test.Fatalf("invalid encoded pointer was resolved or not rejected as invalid: %q, %v", actual, err)
	}
}
