//go:build darwin || linux || windows

package snapshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestReadAdministrativeFocusedExactDiagnostics(test *testing.T) {
	directory := newAdministrativeReadFixture(test)
	if err := os.Mkdir(filepath.Join(directory, "logs"), 0o750); err != nil {
		test.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"index":     {0, 0xff, 'D', 'I', 'R', 'C', '\n'},
		"logs/HEAD": []byte("diagnostic log\x00\r\n"),
		"empty":     {},
	} {
		if err := os.WriteFile(filepath.Join(directory, filepath.FromSlash(name)), data, 0o640); err != nil {
			test.Fatal(err)
		}
	}
	expected := administrativeReadFixtureEntries(test, directory, []string{".", "HEAD", "commondir", "empty", "gitdir", "index", "logs", "logs/HEAD"})
	actual, err := ReadAdministrative(test.Context(), directory)
	if err != nil || !reflect.DeepEqual(actual, expected) {
		test.Fatalf("exact diagnostics: got %#v, %v; want %#v", actual, err, expected)
	}
	if err := validateAdministrativeEntries(actual); err != nil {
		test.Fatal(err)
	}
	for entryIndex := range actual {
		actual[entryIndex].Path = "changed"
		if len(actual[entryIndex].Data) != 0 {
			actual[entryIndex].Data[0] ^= 0xff
		}
	}
	repeated, err := ReadAdministrative(test.Context(), directory)
	if err != nil || !reflect.DeepEqual(repeated, expected) {
		test.Fatalf("caller-owned diagnostics: got %#v, %v; want %#v", repeated, err, expected)
	}
	if after := administrativeReadFixtureEntries(test, directory, administrativeReadEntryPaths(expected)); !reflect.DeepEqual(after, expected) {
		test.Fatal("reading changed fixture bytes or modes")
	}
}

func TestReadAdministrativeFocusedRequiredEntries(test *testing.T) {
	for _, name := range []string{"HEAD", "commondir", "gitdir"} {
		for _, change := range []string{"missing", "empty", "directory"} {
			test.Run(name+"/"+change, func(test *testing.T) {
				directory := newAdministrativeReadFixture(test)
				filename := filepath.Join(directory, name)
				if err := os.Remove(filename); err != nil {
					test.Fatal(err)
				}
				switch change {
				case "empty":
					if err := os.WriteFile(filename, nil, 0o600); err != nil {
						test.Fatal(err)
					}
				case "directory":
					if err := os.Mkdir(filename, 0o700); err != nil {
						test.Fatal(err)
					}
				}
				entries, err := ReadAdministrative(test.Context(), directory)
				assertAdministrativeReadFailure(test, entries, err, nil)
			})
		}
	}
	for _, kind := range []string{"missing", "empty", "directory"} {
		test.Run("index/"+kind, func(test *testing.T) {
			directory := newAdministrativeReadFixture(test)
			switch kind {
			case "empty":
				if err := os.WriteFile(filepath.Join(directory, "index"), nil, 0o600); err != nil {
					test.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(filepath.Join(directory, "index"), 0o700); err != nil {
					test.Fatal(err)
				}
			}
			entries, err := ReadAdministrative(test.Context(), directory)
			if kind == "directory" {
				assertAdministrativeReadFailure(test, entries, err, nil)
				return
			}
			if err != nil {
				test.Fatal(err)
			}
			found := false
			for _, entry := range entries {
				if entry.Path == "index" {
					found = true
					if entry.Kind != "file" || len(entry.Data) != 0 {
						test.Fatalf("invalid empty index: %#v", entry)
					}
				}
			}
			if found != (kind == "empty") {
				test.Fatalf("index presence: %v", found)
			}
		})
	}
}

func TestReadAdministrativeFocusedRootForms(test *testing.T) {
	directory := newAdministrativeReadFixture(test)
	separator := string(filepath.Separator)
	for _, value := range []string{
		"", ".", "relative", directory + separator, directory + separator + ".",
		directory + separator + ".." + separator + filepath.Base(directory),
		directory + separator + separator + "child", directory + "\x00", directory + "\xff",
		separator + strings.Repeat("a", 4096), filepath.Join(directory, "missing"), filepath.Join(directory, "HEAD"),
	} {
		test.Run(fmt.Sprintf("%q", value[:min(len(value), 100)]), func(test *testing.T) {
			entries, err := ReadAdministrative(test.Context(), value)
			assertAdministrativeReadFailure(test, entries, err, nil)
		})
	}
}

func TestReadAdministrativeFocusedRootPathLimit(test *testing.T) {
	for _, pathBytes := range []int{4096, 4097, 32 << 10, (32 << 10) + 1} {
		test.Run(fmt.Sprintf("bytes_%d", pathBytes), func(test *testing.T) {
			directory := newAdministrativeReadFixture(test)
			logicalDirectory := directory
			for len(logicalDirectory) < pathBytes {
				remaining := pathBytes - len(logicalDirectory)
				if remaining == 1 {
					logicalDirectory += "a"
				} else {
					logicalDirectory += string(filepath.Separator) + strings.Repeat("a", min(120, remaining-1))
				}
			}
			operations := defaultAdministrativeReadOperations()
			openMetadata, openRoot := operations.openRootMetadata, operations.openRoot
			metadataOpens, rootOpens := 0, 0
			operations.openRootMetadata = func(name string) (*os.File, error) {
				metadataOpens++
				if name != logicalDirectory {
					test.Fatalf("metadata path changed: got %q, want %q", name, logicalDirectory)
				}
				return openMetadata(directory)
			}
			operations.openRoot = func(name string) (*os.Root, error) {
				rootOpens++
				if name != logicalDirectory {
					test.Fatalf("root path changed: got %q, want %q", name, logicalDirectory)
				}
				return openRoot(directory)
			}
			entries, err := readAdministrative(test.Context(), logicalDirectory, operations)
			if pathBytes > 32<<10 {
				assertAdministrativeReadFailure(test, entries, err, ErrManifestLimit)
				if metadataOpens != 0 || rootOpens != 0 {
					test.Fatalf("opened an over-limit root: metadata=%d, root=%d", metadataOpens, rootOpens)
				}
				return
			}
			expected := administrativeReadFixtureEntries(test, directory, []string{".", "HEAD", "commondir", "gitdir"})
			if err != nil || !reflect.DeepEqual(entries, expected) {
				test.Fatalf("root path of %d bytes: got %#v, %v; want %#v", pathBytes, entries, err, expected)
			}
			if metadataOpens < 2 || rootOpens != 1 {
				test.Fatalf("root observations: metadata=%d, root=%d", metadataOpens, rootOpens)
			}
		})
	}
}

func TestReadAdministrativeFocusedEntryLimit(test *testing.T) {
	directory := newAdministrativeReadFixture(test)
	for entryIndex := 4; entryIndex < maximumAdministrativeEntries; entryIndex++ {
		if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("entry-%04d", entryIndex)), nil, 0o600); err != nil {
			test.Fatal(err)
		}
	}
	entries, err := ReadAdministrative(test.Context(), directory)
	if err != nil || len(entries) != maximumAdministrativeEntries {
		test.Fatalf("at entry limit: %d entries, %v", len(entries), err)
	}
	if err := os.WriteFile(filepath.Join(directory, "over-limit"), nil, 0o600); err != nil {
		test.Fatal(err)
	}
	entries, err = ReadAdministrative(test.Context(), directory)
	assertAdministrativeReadFailure(test, entries, err, ErrManifestLimit)
}

func TestReadAdministrativeFocusedByteLimit(test *testing.T) {
	directory := newAdministrativeReadFixture(test)
	requiredBytes := 0
	for _, entry := range administrativeReadFixtureEntries(test, directory, []string{"HEAD", "commondir", "gitdir"}) {
		requiredBytes += len(entry.Data)
	}
	contents := bytes.Repeat([]byte{0xa5}, maximumAdministrativeBytes-requiredBytes)
	filename := filepath.Join(directory, "payload")
	if err := os.WriteFile(filename, contents, 0o600); err != nil {
		test.Fatal(err)
	}
	entries, err := ReadAdministrative(test.Context(), directory)
	if err != nil {
		test.Fatalf("at byte limit: %v", err)
	}
	collectedBytes := 0
	for _, entry := range entries {
		collectedBytes += len(entry.Data)
		if entry.Path == "payload" && !bytes.Equal(entry.Data, contents) {
			test.Fatal("payload differs at the byte limit")
		}
	}
	if collectedBytes != maximumAdministrativeBytes {
		test.Fatalf("got %d bytes at limit", collectedBytes)
	}
	if err := os.WriteFile(filename, append(contents, 0xff), 0o600); err != nil {
		test.Fatal(err)
	}
	entries, err = ReadAdministrative(test.Context(), directory)
	assertAdministrativeReadFailure(test, entries, err, ErrManifestLimit)
}

func TestReadAdministrativeFocusedCanceled(test *testing.T) {
	ctx, cancel := context.WithCancel(test.Context())
	cancel()
	entries, err := ReadAdministrative(ctx, newAdministrativeReadFixture(test))
	assertAdministrativeReadFailure(test, entries, err, context.Canceled)
}

func newAdministrativeReadFixture(test *testing.T) string {
	test.Helper()
	directory := filepath.Join(test.TempDir(), "admin")
	if err := os.Mkdir(directory, 0o700); err != nil {
		test.Fatal(err)
	}
	for name, contents := range map[string]string{"HEAD": "ref: refs/heads/topic\n", "commondir": "../..\n", "gitdir": "/diagnostic/worktree/.git\n"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o600); err != nil {
			test.Fatal(err)
		}
	}
	return directory
}

func administrativeReadFixtureEntries(test *testing.T, directory string, names []string) []AdminEntry {
	test.Helper()
	entries := make([]AdminEntry, 0, len(names))
	for _, name := range names {
		filename := filepath.Join(directory, filepath.FromSlash(name))
		information, err := os.Lstat(filename)
		if err != nil {
			test.Fatal(err)
		}
		entry := AdminEntry{Path: name, Mode: information.Mode(), Kind: "directory"}
		if !information.IsDir() {
			entry.Kind = "file"
			entry.Data, err = os.ReadFile(filename)
			if err != nil {
				test.Fatal(err)
			}
		}
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(left, right AdminEntry) int { return strings.Compare(left.Path, right.Path) })
	return entries
}

func administrativeReadEntryPaths(entries []AdminEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Path)
	}
	return names
}

func assertAdministrativeReadFailure(test *testing.T, entries []AdminEntry, err, cause error) {
	test.Helper()
	if entries != nil || err == nil || cause != nil && !errors.Is(err, cause) {
		test.Fatalf("want nil entries and error containing %v; got %d entries (nil=%v), %v", cause, len(entries), entries == nil, err)
	}
}
