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

func TestReadUntrackedChangeIdentityReplacement(test *testing.T) {
	for _, target := range []string{"root", "parent", "leaf"} {
		for _, boundary := range []string{"pre-open", "post-open", "post-read", "later-file"} {
			test.Run(target+"/"+boundary, func(test *testing.T) {
				directory := newUntrackedFaultFixture(test)
				filename := directory
				if target == "parent" {
					filename = filepath.Join(directory, "nested")
				} else if target == "leaf" {
					filename = filepath.Join(directory, "nested", "first.bin")
				}
				initial := untrackedChangeFixtureInfo(test, filename)
				replace := prepareAdministrativeReadReplacement(test, filename)
				changed, firstBytes, firstClosed := false, 0, false
				mutate := func() {
					if changed {
						test.Fatal("replacement unexpectedly repeated")
					}
					replace()
					replacement := untrackedChangeFixtureInfo(test, filename)
					if os.SameFile(initial, replacement) {
						test.Fatal("replacement did not change native identity")
					}
					changed = true
				}
				operations := trackedUntrackedFaultOperations(test)
				originalRead, originalClose := operations.read, operations.closeFile
				operations.read = func(file *os.File, buffer []byte) (int, error) {
					if changed && (boundary == "pre-open" || boundary == "post-open") {
						test.Fatal("consumed bytes before detecting an opened-object replacement")
					}
					count, err := originalRead(file, buffer)
					if administrativeReadFileName(file) == "first.bin" {
						firstBytes += count
					}
					return count, err
				}
				operations.closeFile = func(file *os.File) error {
					err := originalClose(file)
					if administrativeReadFileName(file) == "first.bin" && err == nil {
						firstClosed = true
						if boundary == "post-read" {
							mutate()
						}
					}
					return err
				}
				if boundary == "later-file" {
					original := operations.openFile
					operations.openFile = func(parent *os.Root, name string) (*os.File, error) {
						if name == "later.bin" {
							if firstBytes != 6 || !firstClosed {
								test.Fatal("later-file boundary did not follow a completed earlier leaf")
							}
							mutate()
						}
						return original(parent, name)
					}
				} else if boundary != "post-read" {
					switch target {
					case "root":
						original := operations.openRoot
						operations.openRoot = func(name string) (*os.Root, error) {
							if boundary == "pre-open" {
								mutate()
							}
							root, err := original(name)
							if boundary == "post-open" {
								mutate()
							}
							return root, err
						}
					case "parent":
						original := operations.openDirectory
						operations.openDirectory = func(parent *os.Root, name string) (*os.Root, error) {
							if boundary == "pre-open" {
								mutate()
							}
							root, err := original(parent, name)
							if boundary == "post-open" {
								mutate()
							}
							return root, err
						}
					case "leaf":
						original := operations.openFile
						operations.openFile = func(parent *os.Root, name string) (*os.File, error) {
							if name == "first.bin" && boundary == "pre-open" {
								mutate()
							}
							file, err := original(parent, name)
							if name == "first.bin" && boundary == "post-open" {
								mutate()
							}
							return file, err
						}
					}
				}
				entries, err := readUntracked(test.Context(), directory, []string{"nested/first.bin", "nested/later.bin"}, 64, operations)
				if !changed {
					test.Errorf("%s replacement at %s was not exercised", target, boundary)
				}
				if (boundary == "post-read" || boundary == "later-file") && firstBytes != 6 {
					test.Errorf("replacement did not follow a complete earlier read: %d", firstBytes)
				}
				assertUntrackedFaultFailure(test, entries, err, ErrUntrackedInvalid)
			})
		}
	}
}

func TestReadUntrackedChangeLeafMetadata(test *testing.T) {
	for _, field := range []string{"mode", "mtime", "size"} {
		for _, boundary := range []string{"post-open", "post-read", "later-file"} {
			test.Run(field+"/"+boundary, func(test *testing.T) {
				directory := newUntrackedFaultFixture(test)
				filename := filepath.Join(directory, "nested", "first.bin")
				initial := untrackedChangeFixtureInfo(test, filename)
				test.Cleanup(func() {
					if err := os.Chmod(filename, initial.Mode()); err != nil {
						test.Error(err)
					}
				})
				changed := false
				mutate := func() {
					var err error
					switch field {
					case "mode":
						err = os.Chmod(filename, 0o400)
					case "mtime":
						err = os.Chtimes(filename, initial.ModTime(), initial.ModTime().Add(2*time.Second))
					case "size":
						err = os.Truncate(filename, initial.Size()-1)
						if err == nil {
							err = os.Chtimes(filename, initial.ModTime(), initial.ModTime())
						}
					}
					if err != nil {
						test.Fatal(err)
					}
					current := untrackedChangeFixtureInfo(test, filename)
					if !os.SameFile(initial, current) || (current.Mode() != initial.Mode()) != (field == "mode") || (!current.ModTime().Equal(initial.ModTime())) != (field == "mtime") || (current.Size() != initial.Size()) != (field == "size") {
						test.Fatalf("fixture did not isolate %s change", field)
					}
					changed = true
				}
				operations := trackedUntrackedFaultOperations(test)
				if boundary == "post-open" {
					original := operations.openFile
					operations.openFile = func(parent *os.Root, name string) (*os.File, error) {
						file, err := original(parent, name)
						if name == "first.bin" {
							mutate()
						}
						return file, err
					}
					operations.read = func(*os.File, []byte) (int, error) {
						test.Fatal("consumed bytes after an opened-file metadata change")
						return 0, nil
					}
				} else {
					original := operations.read
					operations.read = func(file *os.File, buffer []byte) (int, error) {
						count, err := original(file, buffer)
						name := administrativeReadFileName(file)
						if !changed && count > 0 && (boundary == "post-read" && name == "first.bin" || boundary == "later-file" && name == "later.bin") {
							mutate()
						}
						return count, err
					}
				}
				entries, err := readUntracked(test.Context(), directory, []string{"nested/first.bin", "nested/later.bin"}, 64, operations)
				if !changed {
					test.Errorf("%s mutation at %s was not exercised", field, boundary)
				}
				assertUntrackedFaultFailure(test, entries, err, ErrUntrackedInvalid)
			})
		}
	}
}

func TestReadUntrackedChangeDirectoryMetadataAfterRead(test *testing.T) {
	for _, target := range []string{"root", "parent"} {
		test.Run(target, func(test *testing.T) {
			directory := newUntrackedFaultFixture(test)
			filename := directory
			if target == "parent" {
				filename = filepath.Join(directory, "nested")
			}
			initial := untrackedChangeFixtureInfo(test, filename)
			operations := trackedUntrackedFaultOperations(test)
			original, changed := operations.read, false
			operations.read = func(file *os.File, buffer []byte) (int, error) {
				count, err := original(file, buffer)
				if !changed && count > 0 {
					if err := os.Chtimes(filename, initial.ModTime(), initial.ModTime().Add(2*time.Second)); err != nil {
						test.Fatal(err)
					}
					current := untrackedChangeFixtureInfo(test, filename)
					if current.ModTime().Equal(initial.ModTime()) || !os.SameFile(initial, current) || current.Mode() != initial.Mode() || current.Size() != initial.Size() {
						test.Fatal("fixture did not isolate directory mtime change")
					}
					changed = true
				}
				return count, err
			}
			entries, err := readUntracked(test.Context(), directory, []string{"nested/first.bin"}, 64, operations)
			if !changed {
				test.Error("directory metadata mutation did not execute")
			}
			assertUntrackedFaultFailure(test, entries, err, ErrUntrackedInvalid)
		})
	}
}

func TestReadUntrackedChangePinnedFixtureIdentity(test *testing.T) {
	for _, target := range []string{"file", "directory"} {
		for _, boundary := range []string{"open-handle", "replacement"} {
			test.Run(target+"/"+boundary, func(test *testing.T) {
				directory := newUntrackedFaultFixture(test)
				filename := filepath.Join(directory, "nested")
				if target == "file" {
					filename = filepath.Join(filename, "first.bin")
				}
				initial := untrackedChangeFixtureInfo(test, filename)
				if boundary == "open-handle" {
					file, err := os.Open(filename)
					if err != nil {
						test.Fatal(err)
					}
					test.Cleanup(func() {
						if err := file.Close(); err != nil {
							test.Error(err)
						}
					})
				} else {
					replace := prepareAdministrativeReadReplacement(test, filename)
					replace()
				}
				current := untrackedChangeFixtureInfo(test, filename)
				if os.SameFile(initial, current) != (boundary == "open-handle") {
					test.Fatalf("pinned fixture identity is incorrect at %s", boundary)
				}
				if current.Mode() != initial.Mode() || current.Size() != initial.Size() || !current.ModTime().Equal(initial.ModTime()) {
					test.Fatal("identity control must preserve mode, size, and modification time")
				}
			})
		}
	}
}

func untrackedChangeFixtureInfo(test *testing.T, filename string) fs.FileInfo {
	test.Helper()
	file, err := os.Open(filename)
	if err != nil {
		test.Fatal(err)
	}
	information, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil || closeErr != nil {
		test.Fatalf("fixture metadata for %q: stat=%v, close=%v", filename, statErr, closeErr)
	}
	return information
}

func TestReadUntrackedChangeReadGrowthAndTruncation(test *testing.T) {
	for _, change := range []string{"growth", "growth-at-budget", "truncation"} {
		test.Run(change, func(test *testing.T) {
			directory := newUntrackedFaultFixture(test)
			filename := filepath.Join(directory, "nested", "later.bin")
			if err := os.WriteFile(filename, bytes.Repeat([]byte{0xfe}, 128), 0o600); err != nil {
				test.Fatal(err)
			}
			maximumBytes := int64(256)
			if change == "growth-at-budget" {
				maximumBytes = 134
			}
			operations := trackedUntrackedFaultOperations(test)
			original, changed, totalRead := operations.read, false, int64(0)
			operations.read = func(file *os.File, buffer []byte) (int, error) {
				if len(buffer) == 0 || int64(len(buffer)) > maximumBytes-totalRead+1 {
					test.Fatalf("read exceeds remaining aggregate budget plus probe: %d at %d", len(buffer), totalRead)
				}
				if administrativeReadFileName(file) == "later.bin" && !changed {
					size := int64(129)
					if change == "truncation" {
						size = 1
					}
					if err := os.Truncate(filename, size); err != nil {
						test.Fatal(err)
					}
					information, err := os.Stat(filename)
					if err != nil || information.Size() != size {
						test.Fatalf("fixture size mutation: %v", err)
					}
					changed = true
				}
				count, err := original(file, buffer)
				totalRead += int64(count)
				if totalRead > maximumBytes+1 {
					test.Fatalf("consumed beyond aggregate budget plus probe: %d", totalRead)
				}
				return count, err
			}
			entries, err := readUntracked(test.Context(), directory, []string{"nested/first.bin", "nested/later.bin"}, maximumBytes, operations)
			if !changed {
				test.Error("file size mutation did not execute")
			}
			cause := ErrUntrackedInvalid
			if change == "growth-at-budget" {
				cause = ErrUntrackedLimit
			}
			assertUntrackedFaultFailure(test, entries, err, cause)
		})
	}
}

func TestReadUntrackedChangeUnsupportedMetadata(test *testing.T) {
	for _, mode := range []fs.FileMode{fs.ModeNamedPipe, fs.ModeSocket, fs.ModeDevice, fs.ModeDevice | fs.ModeCharDevice, fs.ModeIrregular} {
		test.Run(mode.String(), func(test *testing.T) {
			directory := newUntrackedFaultFixture(test)
			operations := trackedUntrackedFaultOperations(test)
			original, injected := operations.lstat, false
			operations.lstat = func(parent *os.Root, name string) (fs.FileInfo, error) {
				information, err := original(parent, name)
				if name == "first.bin" && err == nil {
					injected = true
					return untrackedFaultModeInformation{FileInfo: information, mode: mode | 0o600}, nil
				}
				return information, err
			}
			operations.openFile = func(*os.Root, string) (*os.File, error) {
				test.Fatal("opened an explicitly unsupported native type")
				return nil, nil
			}
			entries, err := readUntracked(test.Context(), directory, []string{"nested/first.bin"}, 64, operations)
			if !injected {
				test.Error("unsupported metadata injection did not execute")
			}
			assertUntrackedFaultFailure(test, entries, err, ErrUntrackedInvalid)
		})
	}
}

type untrackedFaultModeInformation struct {
	fs.FileInfo
	mode fs.FileMode
}

func (information untrackedFaultModeInformation) Mode() fs.FileMode {
	return information.mode
}
