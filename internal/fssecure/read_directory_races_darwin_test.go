package fssecure

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateDirectoryReaderRetainsEagerDirectoryIdentities(test *testing.T) {
	for _, location := range []string{"parent", "bundle"} {
		test.Run(location, func(test *testing.T) {
			path := readDirectoryFixture(test, []PrivateFile{{Name: "payload", Contents: []byte("original")}})
			operations := nativePrivateDirectoryReadOperations()
			openat := operations.openat
			changed := false
			target := path
			if location == "parent" {
				target = filepath.Dir(path)
			}
			retained := target + "-retained"
			operations.openat = func(descriptor int, name string, flags int, mode uint32) (*os.File, error) {
				if !changed && (location == "parent" && name == target || location == "bundle" && name == filepath.Base(target)) {
					changed = true
					if err := os.Rename(target, retained); err != nil {
						test.Fatal(err)
					}
					if err := os.MkdirAll(path, 0o700); err != nil {
						test.Fatal(err)
					}
					writePrivateFixture(test, filepath.Join(path, "payload"), []byte("replaced"))
				}
				return openat(descriptor, name, flags, mode)
			}
			reader := privateDirectoryReader{operations: operations}
			files, err := reader.read(context.Background(), path, []PrivateFileLimit{{Name: "payload", MaximumBytes: 8}}, 8)
			if files != nil || !errors.Is(err, fs.ErrInvalid) || !changed {
				test.Fatalf("eager %s identity = %v, %v; replacement ran %t", location, files, err, changed)
			}
			assertContents(test, filepath.Join(path, "payload"), []byte("replaced"))
			if location == "parent" {
				retained = filepath.Join(retained, "snapshot")
			}
			assertContents(test, filepath.Join(retained, "payload"), []byte("original"))
		})
	}
}

func TestPrivateDirectoryReaderRejectsObservedChanges(test *testing.T) {
	for _, kind := range []string{"current contents", "later contents", "earlier contents", "earlier permissions", "earlier ACL", "earlier hardlink", "earlier replacement", "extra entry", "missing entry", "bundle replacement", "parent replacement"} {
		test.Run(kind, func(test *testing.T) {
			path := readDirectoryFixture(test, []PrivateFile{{Name: "first", Contents: []byte("one")}, {Name: "second", Contents: []byte("two")}})
			limits := []PrivateFileLimit{{Name: "first", MaximumBytes: 3}, {Name: "second", MaximumBytes: 3}}
			operations := nativePrivateDirectoryReadOperations()
			read := operations.read
			closeFile := operations.close
			firstClosed := false
			operations.close = func(file *os.File) error {
				err := closeFile(file)
				if file.Name() == "first" {
					firstClosed = true
				}
				return err
			}
			changed := false
			var after map[string]fileReadFixtureSnapshot
			operations.read = func(file *os.File, buffer []byte) (int, error) {
				count, readErr := read(file, buffer)
				trigger := "second"
				if kind == "current contents" || kind == "later contents" {
					trigger = "first"
				}
				if changed || file.Name() != trigger {
					return count, readErr
				}
				changed = true
				if trigger == "second" && !firstClosed {
					test.Fatal("earlier child was not closed before later read")
				}
				child := filepath.Join(path, "first")
				switch kind {
				case "current contents", "earlier contents", "later contents":
					if kind == "later contents" {
						child = filepath.Join(path, "second")
					}
					if err := os.WriteFile(child, []byte("new"), 0o600); err != nil {
						test.Fatal(err)
					}
				case "earlier permissions":
					if err := os.Chmod(child, 0o644); err != nil {
						test.Fatal(err)
					}
				case "earlier ACL":
					addDirectoryFixtureACL(test, child)
				case "earlier hardlink":
					if err := os.Link(child, filepath.Join(filepath.Dir(path), "retained")); err != nil {
						test.Fatal(err)
					}
				case "earlier replacement":
					if err := os.Rename(child, filepath.Join(filepath.Dir(path), "retained")); err != nil {
						test.Fatal(err)
					}
					writePrivateFixture(test, child, []byte("new"))
				case "extra entry":
					writePrivateFixture(test, filepath.Join(path, "extra"), []byte("keep"))
				case "missing entry":
					if err := os.Rename(child, filepath.Join(filepath.Dir(path), "retained")); err != nil {
						test.Fatal(err)
					}
				case "bundle replacement", "parent replacement":
					target := path
					if kind == "parent replacement" {
						target = filepath.Dir(path)
					}
					if err := os.Rename(target, target+"-retained"); err != nil {
						test.Fatal(err)
					}
					if err := os.MkdirAll(path, 0o700); err != nil {
						test.Fatal(err)
					}
					writePrivateFixture(test, filepath.Join(path, "first"), []byte("new"))
					writePrivateFixture(test, filepath.Join(path, "second"), []byte("new"))
				}
				after = readDirectoryTreeState(test, filepath.Dir(filepath.Dir(path)))
				return count, readErr
			}
			reader := privateDirectoryReader{operations: operations}
			files, err := reader.read(context.Background(), path, limits, 6)
			if files != nil || err == nil || !changed {
				test.Fatalf("observed change = %v, %v; change ran %t", files, err, changed)
			}
			assertReadDirectoryTreeUnchanged(test, filepath.Dir(filepath.Dir(path)), after)
		})
	}
}

func TestPrivateDirectoryReaderRevalidatesAfterChildClose(test *testing.T) {
	path := readDirectoryFixture(test, []PrivateFile{{Name: "first", Contents: []byte("one")}, {Name: "second", Contents: []byte("two")}})
	operations := nativePrivateDirectoryReadOperations()
	closeFile := operations.close
	changed := false
	operations.close = func(file *os.File) error {
		err := closeFile(file)
		if !changed && file.Name() == "first" {
			changed = true
			if err := os.WriteFile(filepath.Join(path, "first"), []byte("new"), 0o600); err != nil {
				test.Fatal(err)
			}
		}
		return err
	}
	reader := privateDirectoryReader{operations: operations}
	files, err := reader.read(context.Background(), path, []PrivateFileLimit{{Name: "first", MaximumBytes: 3}, {Name: "second", MaximumBytes: 3}}, 6)
	if files != nil || !errors.Is(err, fs.ErrInvalid) || !changed {
		test.Fatalf("post-close change = %v, %v; change ran %t", files, err, changed)
	}
	assertContents(test, filepath.Join(path, "first"), []byte("new"))
	assertContents(test, filepath.Join(path, "second"), []byte("two"))
}

func TestPrivateDirectoryReaderRejectsChildReplacedBeforeOpen(test *testing.T) {
	path := readDirectoryFixture(test, []PrivateFile{{Name: "payload", Contents: []byte("old")}})
	operations := nativePrivateDirectoryReadOperations()
	openat := operations.openat
	changed := false
	operations.openat = func(descriptor int, name string, flags int, mode uint32) (*os.File, error) {
		if !changed && name == "payload" {
			changed = true
			if err := os.Rename(filepath.Join(path, name), filepath.Join(filepath.Dir(path), "retained")); err != nil {
				test.Fatal(err)
			}
			writePrivateFixture(test, filepath.Join(path, name), []byte("new"))
		}
		return openat(descriptor, name, flags, mode)
	}
	operations.read = func(file *os.File, buffer []byte) (int, error) {
		test.Error("read bytes before checking the retained child identity")
		return file.Read(buffer)
	}
	reader := privateDirectoryReader{operations: operations}
	files, err := reader.read(context.Background(), path, []PrivateFileLimit{{Name: "payload", MaximumBytes: 3}}, 3)
	if files != nil || !errors.Is(err, fs.ErrInvalid) || !changed {
		test.Fatalf("child replacement = %v, %v; change ran %t", files, err, changed)
	}
	assertContents(test, filepath.Join(path, "payload"), []byte("new"))
	assertContents(test, filepath.Join(filepath.Dir(path), "retained"), []byte("old"))
}

func TestPrivateDirectoryReaderRejectsParentAliasInsertedAfterResolution(test *testing.T) {
	path := readDirectoryFixture(test, []PrivateFile{{Name: "payload", Contents: []byte("keep")}})
	parent := filepath.Dir(path)
	retained := parent + "-retained"
	operations := nativePrivateDirectoryReadOperations()
	openat := operations.openat
	changed := false
	operations.openat = func(descriptor int, name string, flags int, mode uint32) (*os.File, error) {
		if !changed && name == parent {
			changed = true
			if err := os.Rename(parent, retained); err != nil {
				test.Fatal(err)
			}
			makeSymlinkFixture(test, retained, parent)
		}
		return openat(descriptor, name, flags, mode)
	}
	reader := privateDirectoryReader{operations: operations}
	files, err := reader.read(context.Background(), path, []PrivateFileLimit{{Name: "payload", MaximumBytes: 4}}, 4)
	if files != nil || err == nil || !changed {
		test.Fatalf("inserted parent alias = %v, %v; change ran %t", files, err, changed)
	}
	if target, err := os.Readlink(parent); err != nil || target != retained {
		test.Fatalf("reader changed inserted alias: %q, %v", target, err)
	}
	assertContents(test, filepath.Join(retained, "snapshot", "payload"), []byte("keep"))
}

func TestPrivateDirectoryReaderRevalidatesAfterFinalEnumeration(test *testing.T) {
	path := readDirectoryFixture(test, []PrivateFile{{Name: "payload", Contents: []byte("keep")}})
	operations := nativePrivateDirectoryReadOperations()
	readDir := operations.readDir
	listings := 0
	operations.readDir = func(file *os.File, count int) ([]os.DirEntry, error) {
		entries, err := readDir(file, count)
		listings++
		if listings == 2 {
			writePrivateFixture(test, filepath.Join(path, "extra"), []byte("retain"))
		}
		return entries, err
	}
	reader := privateDirectoryReader{operations: operations}
	files, err := reader.read(context.Background(), path, []PrivateFileLimit{{Name: "payload", MaximumBytes: 4}}, 4)
	if files != nil || !errors.Is(err, fs.ErrInvalid) || listings < 2 {
		test.Fatalf("late entry change = %v, %v after %d listings", files, err, listings)
	}
	assertContents(test, filepath.Join(path, "payload"), []byte("keep"))
	assertContents(test, filepath.Join(path, "extra"), []byte("retain"))
}

type fileReadFixtureSnapshot struct {
	info     fs.FileInfo
	contents string
}

func readDirectoryTreeState(test *testing.T, root string) map[string]fileReadFixtureSnapshot {
	test.Helper()
	state := make(map[string]fileReadFixtureSnapshot)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var contents []byte
		if info.Mode().IsRegular() {
			contents, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		state[path] = fileReadFixtureSnapshot{info: info, contents: string(contents)}
		return nil
	})
	if err != nil {
		test.Fatal(err)
	}
	return state
}

func assertReadDirectoryTreeUnchanged(test *testing.T, root string, expected map[string]fileReadFixtureSnapshot) {
	test.Helper()
	actual := readDirectoryTreeState(test, root)
	if len(actual) != len(expected) {
		test.Fatalf("reader mutated %d stored entries into %d", len(expected), len(actual))
	}
	for path, before := range expected {
		after, exists := actual[path]
		if !exists || !os.SameFile(before.info, after.info) || before.info.Mode() != after.info.Mode() || !before.info.ModTime().Equal(after.info.ModTime()) || before.contents != after.contents {
			test.Fatalf("reader mutated stored object %q", path)
		}
	}
}
