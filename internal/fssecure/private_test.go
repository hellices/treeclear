//go:build darwin || linux || windows

package fssecure

import (
	"bytes"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestEnsurePrivateDirectoryCreatesMissingComponents(test *testing.T) {
	ancestor := test.TempDir()
	first := filepath.Join(ancestor, "state")
	second := filepath.Join(first, "plans")
	if err := EnsurePrivateDirectory(second); err != nil {
		test.Fatal(err)
	}
	assertPrivateObject(test, first, true)
	assertPrivateObject(test, second, true)
	if err := EnsurePrivateDirectory(second); err != nil {
		test.Fatalf("repeat EnsurePrivateDirectory: %v", err)
	}
}

func TestPrivateOperationsResolveAncestorBeforeParentTraversal(test *testing.T) {
	for _, operation := range []string{"read", "write", "directory", "export"} {
		test.Run(operation, func(test *testing.T) {
			root := test.TempDir()
			outer := filepath.Join(root, "outer")
			target := filepath.Join(outer, "target")
			if err := EnsurePrivateDirectory(target); err != nil {
				test.Fatal(err)
			}
			alias := filepath.Join(root, "alias")
			makeSymlinkFixture(test, target, alias)
			reference := alias + string(filepath.Separator) + ".." + string(filepath.Separator) + "report"
			wantPath := filepath.Join(outer, "report")
			wrongPath := filepath.Join(root, "report")
			contents := []byte("correct private object")
			switch operation {
			case "read":
				writePrivateFixture(test, wantPath, contents)
				writePrivateFixture(test, wrongPath, []byte("wrong private object"))
				actual, err := ReadPrivateFile(reference, 1024)
				if err != nil || !bytes.Equal(actual, contents) {
					test.Fatalf("read selected wrong object: %q, %v", actual, err)
				}
				return
			case "write":
				if err := WritePrivateFile(reference, contents); err != nil {
					test.Fatal(err)
				}
			case "directory":
				if err := EnsurePrivateDirectory(reference + string(filepath.Separator) + "nested"); err != nil {
					test.Fatal(err)
				}
				assertPrivateObject(test, filepath.Join(wantPath, "nested"), true)
			case "export":
				staging := alias + string(filepath.Separator) + ".." + string(filepath.Separator) + "state"
				if err := WritePrivateExport(staging, reference, contents); err != nil {
					test.Fatal(err)
				}
				assertPrivateObject(test, filepath.Join(outer, "state"), true)
			}
			if _, err := os.Lstat(wrongPath); !errors.Is(err, fs.ErrNotExist) {
				test.Fatalf("operation touched the lexically cleaned path: %v", err)
			}
			if operation != "directory" {
				assertContents(test, wantPath, contents)
				assertPrivateObject(test, wantPath, false)
			}
		})
	}
}

func TestEnsurePrivateDirectoryNarrowsOnlyRequestedDirectory(test *testing.T) {
	ancestor := test.TempDir()
	makeBroadFixture(test, ancestor, true)
	before := securitySnapshot(test, ancestor)
	requested := filepath.Join(ancestor, "state")
	if err := os.Mkdir(requested, 0o755); err != nil {
		test.Fatal(err)
	}
	makeBroadFixture(test, requested, true)
	if err := EnsurePrivateDirectory(requested); err != nil {
		test.Fatal(err)
	}
	assertPrivateObject(test, requested, true)
	if after := securitySnapshot(test, ancestor); after != before {
		test.Fatalf("existing ancestor security changed: before %q, after %q", before, after)
	}
	if err := EnsurePrivateDirectory(filepath.Join(ancestor, "missing", "nested")); err != nil {
		test.Fatal(err)
	}
	if after := securitySnapshot(test, ancestor); after != before {
		test.Fatalf("creating missing components changed ancestor: before %q, after %q", before, after)
	}
}

func TestEnsurePrivateDirectoryRejectsNonDirectory(test *testing.T) {
	path := filepath.Join(test.TempDir(), "file")
	writePrivateFixture(test, path, []byte("unchanged"))
	before := securitySnapshot(test, path)
	if err := EnsurePrivateDirectory(path); err == nil {
		test.Fatal("accepted a regular file as a private directory")
	}
	if after := securitySnapshot(test, path); after != before {
		test.Fatal("changed the existing file's security")
	}
	assertContents(test, path, []byte("unchanged"))
}

func TestEnsurePrivateDirectoryRejectsFinalSymlinks(test *testing.T) {
	for _, suffix := range []string{"", string(filepath.Separator), string(filepath.Separator) + "."} {
		test.Run("suffix="+suffix, func(test *testing.T) {
			parent := test.TempDir()
			target := filepath.Join(parent, "target")
			if err := os.Mkdir(target, 0o755); err != nil {
				test.Fatal(err)
			}
			makeBroadFixture(test, target, true)
			before := securitySnapshot(test, target)
			link := filepath.Join(parent, "link")
			makeSymlinkFixture(test, target, link)
			if err := EnsurePrivateDirectory(link + suffix); err == nil {
				test.Fatal("accepted a final directory symlink")
			}
			if after := securitySnapshot(test, target); after != before {
				test.Fatal("changed the symlink target's security")
			}
		})
	}
}

func TestPrivateOperationsAllowAncestorAliases(test *testing.T) {
	parent := test.TempDir()
	realParent := filepath.Join(parent, "real")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		test.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	makeSymlinkFixture(test, realParent, alias)
	directory := filepath.Join(alias, "state", "plans")
	if err := EnsurePrivateDirectory(directory); err != nil {
		test.Fatal(err)
	}
	path := filepath.Join(directory, "plan.json")
	contents := []byte("private through an ancestor alias")
	if err := WritePrivateFile(path, contents); err != nil {
		test.Fatal(err)
	}
	actual, err := ReadPrivateFile(path, int64(len(contents)))
	if err != nil || !bytes.Equal(actual, contents) {
		test.Fatalf("ReadPrivateFile = %q, %v", actual, err)
	}
	assertPrivateObject(test, filepath.Join(realParent, "state"), true)
	assertPrivateObject(test, filepath.Join(realParent, "state", "plans"), true)
}

func TestWritePrivateFileAllowsImmediateParentAlias(test *testing.T) {
	ancestor := privateDirectoryFixture(test)
	makeBroadFixture(test, ancestor, true)
	before := securitySnapshot(test, ancestor)
	realParent := filepath.Join(ancestor, "real")
	if err := os.Mkdir(realParent, 0o755); err != nil {
		test.Fatal(err)
	}
	makeBroadFixture(test, realParent, true)
	alias := filepath.Join(ancestor, "alias")
	makeSymlinkFixture(test, realParent, alias)
	path := filepath.Join(alias, "plan.json")
	contents := []byte("private through the immediate parent alias")
	if err := WritePrivateFile(path, contents); err != nil {
		test.Fatalf("WritePrivateFile through parent alias: %v", err)
	}
	actual, err := ReadPrivateFile(path, int64(len(contents)))
	if err != nil || !bytes.Equal(actual, contents) {
		test.Fatalf("ReadPrivateFile through parent alias = %q, %v", actual, err)
	}
	assertPrivateObject(test, realParent, true)
	assertPrivateObject(test, filepath.Join(realParent, "plan.json"), false)
	if after := securitySnapshot(test, ancestor); after != before {
		test.Fatal("changed the existing ancestor's security")
	}
	if err := WritePrivateFile(path, []byte("replacement")); !errors.Is(err, fs.ErrExist) {
		test.Fatalf("overwrote a file through a parent alias: %v", err)
	}
	assertContents(test, path, contents)
}

func TestEnsurePrivateDirectoryConcurrentCreators(test *testing.T) {
	path := filepath.Join(test.TempDir(), "state", "plans")
	const creators = 16
	start := make(chan struct{})
	results := make(chan error, creators)
	for range creators {
		go func() {
			<-start
			results <- EnsurePrivateDirectory(path)
		}()
	}
	close(start)
	for range creators {
		if err := <-results; err != nil {
			test.Errorf("concurrent EnsurePrivateDirectory: %v", err)
		}
	}
	assertPrivateObject(test, filepath.Dir(path), true)
	assertPrivateObject(test, path, true)
}

func TestWritePrivateFilePublishesExactBytes(test *testing.T) {
	for _, contents := range [][]byte{nil, bytes.Repeat([]byte{0x42}, 32), bytes.Repeat([]byte{0, 1, 0xff, '\n'}, 65536)} {
		test.Run("size="+sizeName(len(contents)), func(test *testing.T) {
			parent := privateDirectoryFixture(test)
			path := filepath.Join(parent, "plan with spaces 日本語.json")
			if err := WritePrivateFile(path, contents); err != nil {
				test.Fatal(err)
			}
			assertContents(test, path, contents)
			assertPrivateObject(test, path, false)
			assertOnlyNames(test, parent, filepath.Base(path))
		})
	}
}

func TestWritePrivateFileNeverChangesExistingObjects(test *testing.T) {
	for _, directory := range []bool{false, true} {
		test.Run(map[bool]string{false: "file", true: "directory"}[directory], func(test *testing.T) {
			parent := privateDirectoryFixture(test)
			path := filepath.Join(parent, "existing")
			if directory {
				if err := os.Mkdir(path, 0o700); err != nil {
					test.Fatal(err)
				}
			} else {
				writePrivateFixture(test, path, []byte("original bytes"))
			}
			makeBroadFixture(test, path, directory)
			before := securitySnapshot(test, path)
			if err := WritePrivateFile(path, []byte("replacement")); !errors.Is(err, fs.ErrExist) {
				test.Fatalf("WritePrivateFile error = %v, want fs.ErrExist", err)
			}
			if after := securitySnapshot(test, path); after != before {
				test.Fatal("changed existing object security")
			}
			if !directory {
				assertContents(test, path, []byte("original bytes"))
			}
			assertOnlyNames(test, parent, "existing")
		})
	}
}

func TestWritePrivateFileRejectsExistingLinks(test *testing.T) {
	for _, dangling := range []bool{false, true} {
		test.Run(map[bool]string{false: "live", true: "dangling"}[dangling], func(test *testing.T) {
			parent := privateDirectoryFixture(test)
			target := filepath.Join(test.TempDir(), "target")
			if !dangling {
				writePrivateFixture(test, target, []byte("target is unchanged"))
			}
			path := filepath.Join(parent, "link")
			makeSymlinkFixture(test, target, path)
			if err := WritePrivateFile(path, []byte("replacement")); !errors.Is(err, fs.ErrExist) {
				test.Fatalf("WritePrivateFile error = %v, want fs.ErrExist", err)
			}
			if actual, err := os.Readlink(path); err != nil || actual != target {
				test.Fatalf("Readlink = %q, %v; want %q", actual, err, target)
			}
			if !dangling {
				assertContents(test, target, []byte("target is unchanged"))
			} else if _, err := os.Lstat(target); !errors.Is(err, fs.ErrNotExist) {
				test.Fatalf("created dangling link target: %v", err)
			}
			assertOnlyNames(test, parent, "link")
		})
	}
}

func TestWritePrivateFileCreatesAndSecuresParent(test *testing.T) {
	for _, existing := range []bool{false, true} {
		test.Run(map[bool]string{false: "missing", true: "existing"}[existing], func(test *testing.T) {
			ancestor := test.TempDir()
			makeBroadFixture(test, ancestor, true)
			before := securitySnapshot(test, ancestor)
			parent := filepath.Join(ancestor, "state", "plans")
			if existing {
				if err := os.MkdirAll(parent, 0o755); err != nil {
					test.Fatal(err)
				}
				makeBroadFixture(test, parent, true)
			}
			path := filepath.Join(parent, "plan.json")
			if err := WritePrivateFile(path, []byte("sensitive")); err != nil {
				test.Fatal(err)
			}
			assertPrivateObject(test, parent, true)
			assertPrivateObject(test, path, false)
			assertContents(test, path, []byte("sensitive"))
			assertOnlyNames(test, parent, "plan.json")
			if !existing {
				assertPrivateObject(test, filepath.Dir(parent), true)
			}
			if after := securitySnapshot(test, ancestor); after != before {
				test.Fatal("WritePrivateFile changed an existing ancestor's security")
			}
		})
	}
}

func TestWritePrivateFileConcurrentPublication(test *testing.T) {
	parent := privateDirectoryFixture(test)
	path := filepath.Join(parent, "integrity.key")
	const creators = 24
	contents := make([][]byte, creators)
	for index := range contents {
		contents[index] = bytes.Repeat([]byte{byte(index + 1)}, 65536)
	}
	type result struct {
		index    int
		writeErr error
		readErr  error
		read     []byte
	}
	start := make(chan struct{})
	results := make(chan result, creators)
	var ready sync.WaitGroup
	ready.Add(creators)
	for index := range creators {
		go func() {
			ready.Done()
			<-start
			writeErr := WritePrivateFile(path, contents[index])
			read, readErr := ReadPrivateFile(path, int64(len(contents[index])))
			results <- result{index: index, writeErr: writeErr, readErr: readErr, read: read}
		}()
	}
	ready.Wait()
	close(start)
	winner := -1
	var observed []result
	for range creators {
		actual := <-results
		observed = append(observed, actual)
		if actual.writeErr == nil {
			if winner != -1 {
				test.Errorf("multiple publication winners: %d and %d", winner, actual.index)
			}
			winner = actual.index
		} else if !errors.Is(actual.writeErr, fs.ErrExist) {
			test.Errorf("creator %d error = %v, want fs.ErrExist", actual.index, actual.writeErr)
		}
	}
	if winner == -1 {
		test.Fatal("no successful publication")
	}
	for _, actual := range observed {
		if actual.readErr != nil || !bytes.Equal(actual.read, contents[winner]) {
			test.Errorf("creator %d did not immediately read the complete winner: len=%d, error=%v", actual.index, len(actual.read), actual.readErr)
		}
	}
	assertContents(test, path, contents[winner])
	assertPrivateObject(test, path, false)
	assertOnlyNames(test, parent, "integrity.key")
}

func TestReadPrivateFileBounds(test *testing.T) {
	for _, entry := range []struct {
		name     string
		contents []byte
		limit    int64
		wantErr  bool
	}{
		{name: "empty", limit: 1},
		{name: "key", contents: bytes.Repeat([]byte{0x42}, 32), limit: 32},
		{name: "oversized-key", contents: bytes.Repeat([]byte{0x42}, 33), limit: 32, wantErr: true},
		{name: "plan-bound", contents: bytes.Repeat([]byte{0x42}, 16<<20), limit: 16 << 20},
		{name: "oversized-plan", contents: bytes.Repeat([]byte{0x42}, (16<<20)+1), limit: 16 << 20, wantErr: true},
		{name: "zero-limit", limit: 0, wantErr: true},
		{name: "negative-limit", limit: -1, wantErr: true},
		{name: "overflow-limit", limit: math.MaxInt64, wantErr: true},
	} {
		test.Run(entry.name, func(test *testing.T) {
			path := filepath.Join(privateDirectoryFixture(test), "data")
			writePrivateFixture(test, path, entry.contents)
			before := securitySnapshot(test, path)
			actual, err := ReadPrivateFile(path, entry.limit)
			if entry.wantErr {
				if err == nil || actual != nil {
					test.Fatalf("ReadPrivateFile = %d bytes, %v; want nil bytes and error", len(actual), err)
				}
			} else if err != nil || !bytes.Equal(actual, entry.contents) {
				test.Fatalf("ReadPrivateFile = %d bytes, %v; want %d bytes", len(actual), err, len(entry.contents))
			}
			if after := securitySnapshot(test, path); after != before {
				test.Fatal("read changed file security")
			}
		})
	}
}

func TestReadPrivateFileRejectsBroadPermissionsWithoutRepair(test *testing.T) {
	path := filepath.Join(privateDirectoryFixture(test), "data")
	writePrivateFixture(test, path, []byte("private"))
	makeBroadFixture(test, path, false)
	before := securitySnapshot(test, path)
	if contents, err := ReadPrivateFile(path, 32); err == nil || contents != nil {
		test.Fatalf("ReadPrivateFile = %q, %v; want nil and error", contents, err)
	}
	if after := securitySnapshot(test, path); after != before {
		test.Fatal("read repaired broad permissions")
	}
	assertContents(test, path, []byte("private"))
}

func TestReadPrivateFileDoesNotRequirePrivateParent(test *testing.T) {
	parent := test.TempDir()
	makeBroadFixture(test, parent, true)
	path := filepath.Join(parent, "user-supplied.json")
	writePrivateFixture(test, path, []byte("private file, public parent"))
	before := securitySnapshot(test, parent)
	contents, err := ReadPrivateFile(path, 32)
	if err != nil || string(contents) != "private file, public parent" {
		test.Fatalf("ReadPrivateFile = %q, %v", contents, err)
	}
	if after := securitySnapshot(test, parent); after != before {
		test.Fatal("read changed user-supplied parent")
	}
}

func TestReadPrivateFileRejectsMissingAndDirectory(test *testing.T) {
	parent := privateDirectoryFixture(test)
	if contents, err := ReadPrivateFile(filepath.Join(parent, "missing"), 32); !errors.Is(err, fs.ErrNotExist) || contents != nil {
		test.Fatalf("missing ReadPrivateFile = %q, %v; want fs.ErrNotExist", contents, err)
	}
	if contents, err := ReadPrivateFile(parent, 32); err == nil || contents != nil {
		test.Fatalf("directory ReadPrivateFile = %q, %v; want error", contents, err)
	}
}

func TestReadPrivateFileRejectsFinalSymlink(test *testing.T) {
	parent := privateDirectoryFixture(test)
	target := filepath.Join(parent, "target")
	writePrivateFixture(test, target, []byte("do not follow"))
	link := filepath.Join(parent, "link")
	makeSymlinkFixture(test, target, link)
	if contents, err := ReadPrivateFile(link, 32); err == nil || contents != nil {
		test.Fatalf("symlink ReadPrivateFile = %q, %v; want error", contents, err)
	}
	assertContents(test, target, []byte("do not follow"))
}

func TestPrivateOperationsRejectEmptyPaths(test *testing.T) {
	if err := EnsurePrivateDirectory(""); err == nil {
		test.Error("EnsurePrivateDirectory accepted empty path")
	}
	if err := WritePrivateFile("", nil); err == nil {
		test.Error("WritePrivateFile accepted empty path")
	}
	if contents, err := ReadPrivateFile("", 1); err == nil || contents != nil {
		test.Error("ReadPrivateFile accepted empty path")
	}
}

func TestPublishPrivateFileCleansStagingOnFailure(test *testing.T) {
	for _, failure := range []string{"closed-file", "read-only-handle", "destination-exists", "missing-destination-parent"} {
		test.Run(failure, func(test *testing.T) {
			parent := privateDirectoryFixture(test)
			stagingPath := filepath.Join(parent, ".staged")
			staged, err := createPrivateFile(stagingPath)
			if err != nil {
				test.Fatal(err)
			}
			path := filepath.Join(parent, "final")
			switch failure {
			case "closed-file":
				if err := staged.Close(); err != nil {
					test.Fatal(err)
				}
			case "read-only-handle":
				if err := staged.Close(); err != nil {
					test.Fatal(err)
				}
				staged, err = os.Open(stagingPath)
				if err != nil {
					test.Fatal(err)
				}
			case "destination-exists":
				writePrivateFixture(test, path, []byte("complete winner"))
			case "missing-destination-parent":
				path = filepath.Join(parent, "missing", "final")
			}
			err = publishPrivateFile(staged, path, []byte("must not overwrite or leak"))
			if err == nil {
				test.Fatal("expected publication failure")
			}
			if failure == "destination-exists" {
				if !errors.Is(err, fs.ErrExist) {
					test.Fatalf("publication error = %v, want fs.ErrExist", err)
				}
				assertContents(test, path, []byte("complete winner"))
				assertOnlyNames(test, parent, "final")
			} else {
				if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
					test.Fatalf("visible final file after failed publication: %v", err)
				}
				assertOnlyNames(test, parent)
			}
		})
	}
}

func TestReadPrivateFileRejectsSubstitutedSymlinks(test *testing.T) {
	parent := privateDirectoryFixture(test)
	original := filepath.Join(parent, "original")
	target := filepath.Join(parent, "target")
	path := filepath.Join(parent, "entry")
	next := filepath.Join(parent, "next")
	writePrivateFixture(test, original, []byte("allowed"))
	writePrivateFixture(test, target, []byte("forbidden symlink target"))
	makeSymlinkFixture(test, target, path)
	if contents, err := ReadPrivateFile(target, 32); err != nil || string(contents) != "forbidden symlink target" {
		test.Fatalf("symlink target is not a readable private fixture: %q, %v", contents, err)
	}
	var reader sync.Mutex
	replaceEntry := func() error {
		firstErr := os.Rename(next, path)
		if firstErr == nil {
			return nil
		}
		reader.Lock()
		defer reader.Unlock()
		if err := os.Rename(next, path); err != nil {
			return errors.Join(firstErr, err)
		}
		test.Logf("fixture rename succeeded after reader closed: %v", firstErr)
		return nil
	}
	finished := make(chan error, 1)
	go func() {
		for range 300 {
			if err := os.Link(original, next); err != nil {
				finished <- err
				return
			}
			if err := replaceEntry(); err != nil {
				finished <- err
				return
			}
			if err := os.Symlink(target, next); err != nil {
				finished <- err
				return
			}
			if err := replaceEntry(); err != nil {
				finished <- err
				return
			}
		}
		finished <- nil
	}()
	for range 300 {
		reader.Lock()
		contents, err := ReadPrivateFile(path, 32)
		reader.Unlock()
		if err == nil && string(contents) != "allowed" || err != nil && contents != nil {
			test.Errorf("substituted ReadPrivateFile = %q, %v", contents, err)
			break
		}
	}
	if err := <-finished; err != nil {
		test.Fatal(err)
	}
	if contents, err := ReadPrivateFile(path, 32); err == nil || contents != nil {
		test.Fatalf("final symlink ReadPrivateFile = %q, %v; want nil and error", contents, err)
	}
}

func TestReadPrivateFileRejectsOversizedSparseFile(test *testing.T) {
	path := filepath.Join(privateDirectoryFixture(test), "large")
	writePrivateFixture(test, path, nil)
	if err := os.Truncate(path, 1<<30); err != nil {
		test.Fatal(err)
	}
	if contents, err := ReadPrivateFile(path, 32); err == nil || contents != nil {
		test.Fatalf("oversized sparse file = %d bytes, %v; want nil and error", len(contents), err)
	}
}

func privateDirectoryFixture(test *testing.T) string {
	test.Helper()
	path := test.TempDir()
	makePrivateFixture(test, path, true)
	return path
}

func writePrivateFixture(test *testing.T, path string, contents []byte) {
	test.Helper()
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		test.Fatal(err)
	}
	makePrivateFixture(test, path, false)
}

func makeSymlinkFixture(test *testing.T, target, path string) {
	test.Helper()
	if err := os.Symlink(target, path); err != nil {
		if symlinkCapabilityUnavailable(err) {
			test.Skipf("native symlink capability unavailable: %v", err)
		}
		test.Fatal(err)
	}
}

func assertContents(test *testing.T, path string, want []byte) {
	test.Helper()
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, want) {
		test.Fatalf("contents of %q = %d bytes, %v; want exact %d bytes", path, len(actual), err, len(want))
	}
}

func assertOnlyNames(test *testing.T, directory string, names ...string) {
	test.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		test.Fatal(err)
	}
	actual := make([]string, 0, len(entries))
	for _, entry := range entries {
		actual = append(actual, entry.Name())
	}
	if len(actual) != len(names) {
		test.Fatalf("directory entries = %q; want %q (no leaked staging files)", actual, names)
	}
	for index := range names {
		if actual[index] != names[index] {
			test.Fatalf("directory entries = %q; want %q", actual, names)
		}
	}
}

func sizeName(size int) string {
	if size == 0 {
		return "empty"
	}
	if size == 32 {
		return "key"
	}
	return "large"
}
