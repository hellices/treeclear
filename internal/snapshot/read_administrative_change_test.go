//go:build darwin || linux || windows

package snapshot

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadAdministrativeFocusedMetadataChanges(test *testing.T) {
	for _, field := range []string{"mode", "mtime"} {
		for _, boundary := range []string{"before-read", "after-read", "later-file"} {
			test.Run(field+"/"+boundary, func(test *testing.T) {
				directory := newAdministrativeReadFixture(test)
				filename := filepath.Join(directory, "HEAD")
				initial, err := os.Stat(filename)
				if err != nil {
					test.Fatal(err)
				}
				test.Cleanup(func() {
					if err := os.Chmod(filename, initial.Mode()); err != nil {
						test.Error(err)
					}
				})
				changed := false
				change := func() {
					if field == "mode" {
						err = os.Chmod(filename, 0o400)
					} else {
						err = os.Chtimes(filename, initial.ModTime(), initial.ModTime().Add(2*time.Second))
					}
					if err != nil {
						test.Fatal(err)
					}
					changed = true
				}
				operations := defaultAdministrativeReadOperations()
				trackAdministrativeReadHandles(test, &operations)
				if boundary == "before-read" {
					original := operations.openFile
					operations.openFile = func(parent *os.Root, name string) (*os.File, error) {
						file, err := original(parent, name)
						if name == "HEAD" {
							change()
						}
						return file, err
					}
					operations.read = func(file *os.File, buffer []byte) (int, error) {
						test.Fatal("consumed bytes after an observed metadata change")
						return 0, nil
					}
				} else {
					original := operations.read
					operations.read = func(file *os.File, buffer []byte) (int, error) {
						count, err := original(file, buffer)
						name := administrativeReadFileName(file)
						if !changed && count > 0 && (boundary == "after-read" && name == "HEAD" || boundary == "later-file" && name == "commondir") {
							change()
						}
						return count, err
					}
				}
				entries, err := readAdministrative(test.Context(), directory, operations)
				assertAdministrativeReadFailure(test, entries, err, nil)
				if !changed {
					test.Fatal("metadata change boundary was not exercised")
				}
			})
		}
	}
}

func TestReadAdministrativeFocusedRootReplacement(test *testing.T) {
	for _, boundary := range []string{"before-open", "after-read"} {
		test.Run(boundary, func(test *testing.T) {
			directory := newAdministrativeReadFixture(test)
			replace := prepareAdministrativeReadReplacement(test, directory)
			operations := defaultAdministrativeReadOperations()
			trackAdministrativeReadHandles(test, &operations)
			changed := false
			if boundary == "before-open" {
				original := operations.openRoot
				operations.openRoot = func(name string) (*os.Root, error) {
					replace()
					changed = true
					return original(name)
				}
				operations.read = func(file *os.File, buffer []byte) (int, error) {
					test.Fatal("consumed bytes from a substituted root")
					return 0, nil
				}
			} else {
				original := operations.read
				operations.read = func(file *os.File, buffer []byte) (int, error) {
					count, err := original(file, buffer)
					if !changed && count > 0 {
						replace()
						changed = true
					}
					return count, err
				}
			}
			entries, err := readAdministrative(test.Context(), directory, operations)
			assertAdministrativeReadFailure(test, entries, err, nil)
			if !changed {
				test.Fatal("root replacement boundary was not exercised")
			}
		})
	}
}

func TestReadAdministrativeFocusedDirectoryReplacement(test *testing.T) {
	for _, boundary := range []string{"before-open", "after-open", "after-read"} {
		test.Run(boundary, func(test *testing.T) {
			directory := newAdministrativeReadFixture(test)
			addAdministrativeReadNestedFixture(test, directory)
			replace := prepareAdministrativeReadReplacement(test, filepath.Join(directory, "logs"))
			operations := defaultAdministrativeReadOperations()
			trackAdministrativeReadHandles(test, &operations)
			changed := false
			if boundary != "after-read" {
				original := operations.openDirectory
				operations.openDirectory = func(parent *os.Root, name string) (*os.Root, error) {
					if boundary == "before-open" {
						replace()
						changed = true
					}
					root, err := original(parent, name)
					if boundary == "after-open" {
						replace()
						changed = true
					}
					return root, err
				}
				originalRead := operations.read
				operations.read = func(file *os.File, buffer []byte) (int, error) {
					if administrativeReadFileName(file) == "entry" {
						test.Fatal("consumed bytes through a substituted directory")
					}
					return originalRead(file, buffer)
				}
			} else {
				original := operations.closeFile
				operations.closeFile = func(file *os.File) error {
					err := original(file)
					if administrativeReadFileName(file) == "entry" {
						replace()
						changed = true
					}
					return err
				}
			}
			entries, err := readAdministrative(test.Context(), directory, operations)
			assertAdministrativeReadFailure(test, entries, err, nil)
			if !changed {
				test.Fatal("directory replacement boundary was not exercised")
			}
		})
	}
}

func TestReadAdministrativeFocusedFileReplacement(test *testing.T) {
	for _, boundary := range []string{"before-open", "after-open", "after-read", "later-file"} {
		test.Run(boundary, func(test *testing.T) {
			directory := newAdministrativeReadFixture(test)
			replace := prepareAdministrativeReadReplacement(test, filepath.Join(directory, "HEAD"))
			operations := defaultAdministrativeReadOperations()
			trackAdministrativeReadHandles(test, &operations)
			changed := false
			if boundary == "before-open" || boundary == "after-open" {
				original := operations.openFile
				operations.openFile = func(parent *os.Root, name string) (*os.File, error) {
					if name == "HEAD" && boundary == "before-open" {
						replace()
						changed = true
					}
					file, err := original(parent, name)
					if name == "HEAD" && boundary == "after-open" {
						replace()
						changed = true
					}
					return file, err
				}
				operations.read = func(file *os.File, buffer []byte) (int, error) {
					test.Fatal("consumed bytes before checking the opened file and its path")
					return 0, nil
				}
			} else {
				original := operations.read
				operations.read = func(file *os.File, buffer []byte) (int, error) {
					count, err := original(file, buffer)
					name := administrativeReadFileName(file)
					if !changed && count > 0 && (boundary == "after-read" && name == "HEAD" || boundary == "later-file" && name == "commondir") {
						replace()
						changed = true
					}
					return count, err
				}
			}
			entries, err := readAdministrative(test.Context(), directory, operations)
			assertAdministrativeReadFailure(test, entries, err, nil)
			if !changed {
				test.Fatal("file replacement boundary was not exercised")
			}
		})
	}
}

func TestReadAdministrativeFocusedFileSizeChanges(test *testing.T) {
	for _, change := range []string{"growth", "growth-at-budget", "shrink"} {
		test.Run(change, func(test *testing.T) {
			directory := newAdministrativeReadFixture(test)
			size := 128
			if change == "growth-at-budget" {
				size = maximumAdministrativeBytes
				for _, entry := range administrativeReadFixtureEntries(test, directory, []string{"HEAD", "commondir", "gitdir"}) {
					size -= len(entry.Data)
				}
			}
			filename := filepath.Join(directory, "z-payload")
			if err := os.WriteFile(filename, bytes.Repeat([]byte{0xfe}, size), 0o600); err != nil {
				test.Fatal(err)
			}
			operations := defaultAdministrativeReadOperations()
			trackAdministrativeReadHandles(test, &operations)
			original, changed, totalRead := operations.read, false, 0
			operations.read = func(file *os.File, buffer []byte) (int, error) {
				if len(buffer) == 0 || len(buffer) > 32<<10 {
					test.Fatalf("unbounded read: %d bytes", len(buffer))
				}
				if administrativeReadFileName(file) == "z-payload" && !changed {
					changed = true
					if change == "shrink" {
						if err := os.Truncate(filename, 1); err != nil {
							test.Fatal(err)
						}
					} else {
						writer, err := os.OpenFile(filename, os.O_WRONLY|os.O_APPEND, 0)
						if err != nil {
							test.Fatal(err)
						}
						_, writeErr := writer.Write([]byte{0xff})
						closeErr := writer.Close()
						if writeErr != nil || closeErr != nil {
							test.Fatalf("grow fixture: %v, %v", writeErr, closeErr)
						}
					}
				}
				count, err := original(file, buffer)
				totalRead += count
				if totalRead > maximumAdministrativeBytes+1 {
					test.Fatalf("read past the aggregate budget: %d", totalRead)
				}
				return count, err
			}
			entries, err := readAdministrative(test.Context(), directory, operations)
			assertAdministrativeReadFailure(test, entries, err, nil)
			if !changed {
				test.Fatal("file size change boundary was not exercised")
			}
		})
	}
}

func prepareAdministrativeReadReplacement(test *testing.T, target string) func() {
	test.Helper()
	information, err := os.Stat(target)
	if err != nil {
		test.Fatal(err)
	}
	parentInformation, err := os.Stat(filepath.Dir(target))
	if err != nil {
		test.Fatal(err)
	}
	replacement := filepath.Join(test.TempDir(), "replacement")
	backup := filepath.Join(test.TempDir(), "previous")
	var directories []string
	err = filepath.WalkDir(target, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(target, filename)
		if err != nil {
			return err
		}
		destination := filepath.Join(replacement, relative)
		if entry.IsDir() {
			directories = append(directories, relative)
			return os.Mkdir(destination, 0o700)
		}
		contents, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		if len(contents) > 0 {
			contents[0] ^= 0xff
		}
		if err := os.WriteFile(destination, contents, 0o600); err != nil {
			return err
		}
		matchAdministrativeReadMetadata(test, filename, destination)
		return nil
	})
	if err != nil {
		test.Fatal(err)
	}
	for directoryIndex := len(directories) - 1; directoryIndex >= 0; directoryIndex-- {
		relative := directories[directoryIndex]
		matchAdministrativeReadMetadata(test, filepath.Join(target, relative), filepath.Join(replacement, relative))
	}
	return func() {
		if err := os.Rename(target, backup); err != nil {
			test.Fatal(err)
		}
		if err := os.Rename(replacement, target); err != nil {
			test.Fatal(err)
		}
		if err := os.Chtimes(filepath.Dir(target), parentInformation.ModTime(), parentInformation.ModTime()); err != nil {
			test.Fatal(err)
		}
		replaced, err := os.Stat(target)
		if err != nil {
			test.Fatal(err)
		}
		if replaced.Size() != information.Size() || replaced.Mode() != information.Mode() || !replaced.ModTime().Equal(information.ModTime()) {
			test.Fatal("replacement must match size, mode, and modification time")
		}
	}
}

func matchAdministrativeReadMetadata(test *testing.T, source, destination string) {
	test.Helper()
	information, err := os.Stat(source)
	if err != nil {
		test.Fatal(err)
	}
	if err := os.Chmod(destination, information.Mode()); err != nil {
		test.Fatal(err)
	}
	if err := os.Chtimes(destination, information.ModTime(), information.ModTime()); err != nil {
		test.Fatal(err)
	}
}
