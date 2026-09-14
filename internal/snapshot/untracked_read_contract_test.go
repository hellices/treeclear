//go:build darwin || linux || windows

package snapshot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUntrackedReadRawAndArchiveBudgetsAreDistinct(test *testing.T) {
	directory, err := filepath.EvalSymlinks(test.TempDir())
	if err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "file"), []byte("x"), 0o600); err != nil {
		test.Fatal(err)
	}
	entries, err := ReadUntracked(test.Context(), directory, []string{"file"}, 1)
	if err != nil || len(entries) != 1 || string(entries[0].Data) != "x" {
		test.Fatalf("exact raw byte budget rejected: %#v, %v", entries, err)
	}
	if archive, err := EncodeUntracked(entries, 1); !errors.Is(err, ErrUntrackedLimit) || archive != nil {
		test.Fatalf("serialized overhead was confused with the raw-byte allowance: %v", err)
	}
	if archive, err := EncodeUntracked(entries, 1<<20); err != nil || archive == nil {
		test.Fatalf("independent archive allowance rejected: %v", err)
	}
}

func TestUntrackedReadPreservesCancelledContext(test *testing.T) {
	ctx, cancel := context.WithCancel(test.Context())
	cancel()
	entries, err := ReadUntracked(ctx, test.TempDir(), []string{"file"}, 1)
	if !errors.Is(err, context.Canceled) || entries != nil {
		test.Fatalf("cancelled request returned data or lost its cause: %#v, %v", entries, err)
	}
}

func TestUntrackedReadEmptySelectionAndLeaves(test *testing.T) {
	directory, err := filepath.EvalSymlinks(test.TempDir())
	if err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "empty"), nil, 0o600); err != nil {
		test.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "folder"), 0o700); err != nil {
		test.Fatal(err)
	}
	for _, scenario := range []struct {
		name  string
		paths []string
		cause error
	}{
		{name: "empty-selection"},
		{name: "empty-file", paths: []string{"empty"}},
		{name: "requested-directory", paths: []string{"empty", "folder"}, cause: ErrUntrackedInvalid},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			entries, err := ReadUntracked(test.Context(), directory, scenario.paths, 1)
			if scenario.cause != nil {
				if !errors.Is(err, scenario.cause) || entries != nil {
					test.Fatalf("invalid leaf returned entries or lost its cause: %#v, %v", entries, err)
				}
				return
			}
			if err != nil || len(entries) != len(scenario.paths) {
				test.Fatalf("empty selection or file rejected: %#v, %v", entries, err)
			}
			if len(entries) == 1 && (entries[0].Path != "empty" || entries[0].Kind != "file" || len(entries[0].Data) != 0) {
				test.Fatalf("empty file was not preserved: %#v", entries)
			}
			archive, err := EncodeUntracked(entries, 1<<20)
			if err != nil {
				test.Fatalf("empty source result is not codec-compatible: %v", err)
			}
			decoded, err := DecodeUntracked(archive, 1<<20)
			if err != nil || len(decoded) != len(entries) {
				test.Fatalf("empty source result did not round trip: %#v, %v", decoded, err)
			}
		})
	}
}

func TestUntrackedReadRejectsBadRequestsBeforeIO(test *testing.T) {
	directory := test.TempDir()
	for _, scenario := range []struct {
		name      string
		directory string
		paths     []string
		budget    int64
		cause     error
	}{
		{name: "zero-budget", directory: directory, paths: []string{"file"}, cause: ErrUntrackedInvalid},
		{name: "unsafe-budget", directory: directory, budget: int64(int(^uint(0) >> 1)), cause: ErrUntrackedInvalid},
		{name: "relative-root", directory: "relative", budget: 1, cause: ErrUntrackedInvalid},
		{name: "nul-root", directory: directory + "\x00", budget: 1, cause: ErrUntrackedInvalid},
		{name: "unclean-root", directory: directory + string(os.PathSeparator) + "..", budget: 1, cause: ErrUntrackedInvalid},
		{name: "root-text-limit", directory: directory + strings.Repeat("a", 32<<10), budget: 1, cause: ErrUntrackedLimit},
		{name: "absolute-leaf", directory: directory, paths: []string{"valid", "/escape"}, budget: 1, cause: ErrUntrackedInvalid},
		{name: "traversal-leaf", directory: directory, paths: []string{"valid", "../escape"}, budget: 1, cause: ErrUntrackedInvalid},
		{name: "git-component", directory: directory, paths: []string{"valid", "nested/.GIT/config"}, budget: 1, cause: ErrUntrackedInvalid},
		{name: "duplicate", directory: directory, paths: []string{"valid", "valid"}, budget: 1, cause: ErrUntrackedInvalid},
		{name: "folded-alias", directory: directory, paths: []string{"Parent/left", "parent/right"}, budget: 1, cause: ErrUntrackedInvalid},
		{name: "leaf-ancestor", directory: directory, paths: []string{"parent/child", "parent"}, budget: 1, cause: ErrUntrackedInvalid},
		{name: "text-limit", directory: directory, paths: []string{strings.Repeat("a", 4097)}, budget: 1, cause: ErrUntrackedLimit},
		{name: "leaf-count-limit", directory: directory, paths: make([]string, 4097), budget: 1, cause: ErrUntrackedLimit},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			operations := defaultUntrackedReadOperations()
			operations.openRootMetadata = func(string) (*os.File, error) {
				test.Fatal("source was opened before the complete request was validated")
				return nil, nil
			}
			entries, err := readUntracked(test.Context(), scenario.directory, scenario.paths, scenario.budget, operations)
			if !errors.Is(err, scenario.cause) || entries != nil {
				test.Fatalf("bad request lost its cause or returned data: %#v, %v", entries, err)
			}
		})
	}
}

func TestUntrackedReadReservesCompleteSelectionBeforeOpen(test *testing.T) {
	deepPrefix := strings.Repeat("a/", 2043)
	for _, scenario := range []struct {
		name   string
		count  int
		prefix string
		limit  bool
	}{
		{name: "flat-boundary", count: 4096},
		{name: "shared-parent-boundary", count: 4095, prefix: "nested/"},
		{name: "shared-parent-over", count: 4096, prefix: "nested/", limit: true},
		{name: "deep-shared-boundary", count: 2053, prefix: deepPrefix},
		{name: "deep-shared-over", count: 2054, prefix: deepPrefix, limit: true},
		{name: "empty-still-observes-root"},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			paths := make([]string, scenario.count)
			for index := range paths {
				paths[index] = fmt.Sprintf("%sfile-%04d", scenario.prefix, index)
			}
			rootFailure := errors.New("reserved selection reached root metadata")
			operations := defaultUntrackedReadOperations()
			opened := 0
			operations.openRootMetadata = func(string) (*os.File, error) {
				opened++
				return nil, rootFailure
			}
			entries, err := readUntracked(test.Context(), test.TempDir(), paths, 1, operations)
			if entries != nil {
				test.Fatal("failed selection/root observation returned partial entries")
			}
			if scenario.limit {
				if !errors.Is(err, ErrUntrackedLimit) || opened != 0 {
					test.Fatalf("entry capacity was not reserved before open: opens %d, %v", opened, err)
				}
			} else if !errors.Is(err, rootFailure) || opened != 1 {
				test.Fatalf("valid selection did not reach exactly one root observation: opens %d, %v", opened, err)
			}
		})
	}
}
