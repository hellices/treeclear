//go:build darwin || linux || windows

package snapshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadAdministrativeFocusedIOFailures(test *testing.T) {
	for _, boundary := range []string{
		"metadata-open", "metadata-stat", "metadata-close", "root-open", "root-stat",
		"root-recheck-open", "root-final-open", "directory-open", "directory-file-open",
		"directory-stat", "directory-close", "names", "names-with-data", "names-late",
		"entry-stat", "entry-pre-read-stat", "entry-post-read-stat", "entry-final-stat",
		"file-open", "file-pre-read-stat", "file-post-read-stat", "read", "read-with-data", "read-eof",
		"file-close", "root-close", "root-open-with-handle", "file-open-with-handle", "directory-open-with-handle",
	} {
		test.Run(boundary, func(test *testing.T) {
			directory := newAdministrativeReadFixture(test)
			addAdministrativeReadNestedFixture(test, directory)
			operations := defaultAdministrativeReadOperations()
			trackAdministrativeReadHandles(test, &operations)
			cause := errors.New("injected " + boundary)
			injectAdministrativeReadFailure(&operations, boundary, cause)
			entries, err := readAdministrative(test.Context(), directory, operations)
			assertAdministrativeReadFailure(test, entries, err, cause)
		})
	}
}

func TestReadAdministrativeFocusedReadAndCloseCauses(test *testing.T) {
	directory := newAdministrativeReadFixture(test)
	operations := defaultAdministrativeReadOperations()
	trackAdministrativeReadHandles(test, &operations)
	readCause, closeCause := errors.New("read failure"), errors.New("close failure")
	injectAdministrativeReadFailure(&operations, "read", readCause)
	injectAdministrativeReadFailure(&operations, "file-close", closeCause)
	entries, err := readAdministrative(test.Context(), directory, operations)
	assertAdministrativeReadFailure(test, entries, err, readCause)
	if !errors.Is(err, closeCause) {
		test.Fatalf("lost close failure: %v", err)
	}
}

func TestReadAdministrativeFocusedDirectoryRevalidationFailures(test *testing.T) {
	for _, boundary := range []struct {
		parent string
		name   string
		call   int
	}{
		{"admin", ".", 2}, {"admin", ".", 3},
		{"logs", ".", 1}, {"logs", ".", 2}, {"logs", ".", 3},
		{"admin", "logs", 2}, {"admin", "logs", 3},
	} {
		test.Run(fmt.Sprintf("%s/%s/%d", boundary.parent, boundary.name, boundary.call), func(test *testing.T) {
			directory := newAdministrativeReadFixture(test)
			addAdministrativeReadNestedFixture(test, directory)
			operations := defaultAdministrativeReadOperations()
			trackAdministrativeReadHandles(test, &operations)
			cause := errors.New("directory observation failure")
			original, calls := operations.lstat, 0
			operations.lstat = func(parent *os.Root, name string) (fs.FileInfo, error) {
				if filepath.Base(filepath.Clean(parent.Name())) == boundary.parent && name == boundary.name {
					calls++
					if calls == boundary.call {
						return nil, cause
					}
				}
				return original(parent, name)
			}
			entries, err := readAdministrative(test.Context(), directory, operations)
			assertAdministrativeReadFailure(test, entries, err, cause)
		})
	}
}

func TestReadAdministrativeFocusedCancellationBoundaries(test *testing.T) {
	for _, boundary := range []string{"metadata-open", "root-open", "directory-open", "file-open", "stat", "lstat", "names", "read", "file-close", "root-close"} {
		test.Run(boundary, func(test *testing.T) {
			directory := newAdministrativeReadFixture(test)
			addAdministrativeReadNestedFixture(test, directory)
			ctx, cancel := context.WithCancel(test.Context())
			defer cancel()
			operations := defaultAdministrativeReadOperations()
			trackAdministrativeReadHandles(test, &operations)
			switch boundary {
			case "metadata-open":
				original := operations.openRootMetadata
				operations.openRootMetadata = func(name string) (*os.File, error) {
					file, err := original(name)
					cancel()
					return file, err
				}
			case "root-open":
				original := operations.openRoot
				operations.openRoot = func(name string) (*os.Root, error) {
					root, err := original(name)
					cancel()
					return root, err
				}
			case "directory-open":
				original := operations.openDirectory
				operations.openDirectory = func(parent *os.Root, name string) (*os.Root, error) {
					root, err := original(parent, name)
					cancel()
					return root, err
				}
			case "file-open":
				original := operations.openFile
				operations.openFile = func(parent *os.Root, name string) (*os.File, error) {
					file, err := original(parent, name)
					cancel()
					return file, err
				}
			case "stat":
				original := operations.stat
				operations.stat = func(file *os.File) (fs.FileInfo, error) {
					information, err := original(file)
					cancel()
					return information, err
				}
			case "lstat":
				original := operations.lstat
				operations.lstat = func(parent *os.Root, name string) (fs.FileInfo, error) {
					information, err := original(parent, name)
					cancel()
					return information, err
				}
			case "names":
				original := operations.readNames
				operations.readNames = func(file *os.File, count int) ([]string, error) {
					names, err := original(file, count)
					cancel()
					return names, err
				}
			case "read":
				original := operations.read
				operations.read = func(file *os.File, buffer []byte) (int, error) {
					count, err := original(file, buffer)
					cancel()
					return count, err
				}
			case "file-close":
				original := operations.closeFile
				operations.closeFile = func(file *os.File) error {
					err := original(file)
					cancel()
					return err
				}
			case "root-close":
				original := operations.closeRoot
				operations.closeRoot = func(root *os.Root) error {
					err := original(root)
					cancel()
					return err
				}
			}
			entries, err := readAdministrative(ctx, directory, operations)
			assertAdministrativeReadFailure(test, entries, err, context.Canceled)
		})
	}
}

func TestReadAdministrativeFocusedInvalidEnumeratedNames(test *testing.T) {
	for _, names := range [][]string{
		{""}, {"."}, {".."}, {"a/b"}, {`a\b`}, {"a\x00b"}, {"a\xff"}, {"a\nb"}, {"a\x7fb"},
		{"trailing."}, {"trailing "}, {"NUL"}, {"COM1.txt"}, {"lpt¹"}, {"CONIN$"}, {"file:stream"},
		{"bad?"}, {"bad*"}, {"bad|"}, {"bad<"}, {"bad>"}, {"bad\""}, {strings.Repeat("x", 4097)},
		{"HEAD", "HEAD"}, {"HEAD", "head"}, {"Σ", "ς"}, {"logs", "LOGS"},
	} {
		test.Run(fmt.Sprintf("%q", names[0][:min(len(names[0]), 60)])+fmt.Sprint(len(names)), func(test *testing.T) {
			directory := newAdministrativeReadFixture(test)
			operations := defaultAdministrativeReadOperations()
			trackAdministrativeReadHandles(test, &operations)
			operations.readNames = func(file *os.File, count int) ([]string, error) {
				if _, err := file.Stat(); err != nil {
					test.Fatal(err)
				}
				return names, io.EOF
			}
			original := operations.lstat
			operations.lstat = func(parent *os.Root, name string) (fs.FileInfo, error) {
				if name != "." {
					test.Fatalf("inspected an entry before rejecting unsafe names: %q", name)
				}
				return original(parent, name)
			}
			entries, err := readAdministrative(test.Context(), directory, operations)
			assertAdministrativeReadFailure(test, entries, err, nil)
		})
	}
}

func TestReadAdministrativeFocusedEnumerationReservation(test *testing.T) {
	directory := newAdministrativeReadFixture(test)
	if err := os.Mkdir(filepath.Join(directory, "000-nested"), 0o700); err != nil {
		test.Fatal(err)
	}
	operations := defaultAdministrativeReadOperations()
	trackAdministrativeReadHandles(test, &operations)
	rootNames := []string{"000-nested"}
	for entryIndex := 1; entryIndex < maximumAdministrativeEntries-2; entryIndex++ {
		rootNames = append(rootNames, fmt.Sprintf("unvisited-%04d", entryIndex))
	}
	returnedNames := 0
	operations.readNames = func(file *os.File, count int) ([]string, error) {
		if count <= 0 || count > 64 {
			test.Fatalf("unbounded enumeration request: %d", count)
		}
		if administrativeReadFileName(file) == "000-nested" {
			if returnedNames != maximumAdministrativeEntries-2 {
				test.Fatalf("descended before reserving pending siblings: %d", returnedNames)
			}
			if count > 2 {
				test.Fatalf("did not bound enumeration against reserved entries: %d", count)
			}
			return []string{"one", "two"}, io.EOF
		}
		batch := rootNames[:min(count, len(rootNames))]
		rootNames = rootNames[len(batch):]
		returnedNames += len(batch)
		if len(rootNames) == 0 {
			return batch, io.EOF
		}
		return batch, nil
	}
	original := operations.lstat
	operations.lstat = func(parent *os.Root, name string) (fs.FileInfo, error) {
		if name != "." && name != "000-nested" {
			test.Fatalf("opened a pending or over-budget entry: %q", name)
		}
		return original(parent, name)
	}
	entries, err := readAdministrative(test.Context(), directory, operations)
	assertAdministrativeReadFailure(test, entries, err, ErrManifestLimit)
}

func TestReadAdministrativeFocusedNoProgress(test *testing.T) {
	for _, boundary := range []string{"names", "read"} {
		test.Run(boundary, func(test *testing.T) {
			operations := defaultAdministrativeReadOperations()
			trackAdministrativeReadHandles(test, &operations)
			if boundary == "names" {
				operations.readNames = func(file *os.File, count int) ([]string, error) { return nil, nil }
			} else {
				operations.read = func(file *os.File, buffer []byte) (int, error) { return 0, nil }
			}
			entries, err := readAdministrative(test.Context(), newAdministrativeReadFixture(test), operations)
			assertAdministrativeReadFailure(test, entries, err, io.ErrNoProgress)
		})
	}
}

func TestReadAdministrativeFocusedShortReads(test *testing.T) {
	directory := newAdministrativeReadFixture(test)
	operations := defaultAdministrativeReadOperations()
	trackAdministrativeReadHandles(test, &operations)
	original := operations.read
	operations.read = func(file *os.File, buffer []byte) (int, error) {
		return original(file, buffer[:min(len(buffer), 1)])
	}
	entries, err := readAdministrative(test.Context(), directory, operations)
	expected := administrativeReadFixtureEntries(test, directory, []string{".", "HEAD", "commondir", "gitdir"})
	if err != nil || !reflect.DeepEqual(entries, expected) {
		test.Fatalf("short reads changed diagnostics: %#v, %v", entries, err)
	}
}

func addAdministrativeReadNestedFixture(test *testing.T, directory string) {
	test.Helper()
	if err := os.Mkdir(filepath.Join(directory, "logs"), 0o700); err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "logs", "entry"), []byte("nested log\n"), 0o600); err != nil {
		test.Fatal(err)
	}
}

func administrativeReadFileName(file *os.File) string {
	return filepath.Base(filepath.Clean(file.Name()))
}

func injectAdministrativeReadFailure(operations *administrativeReadOperations, boundary string, cause error) {
	switch boundary {
	case "metadata-open", "root-recheck-open", "root-final-open":
		original, calls := operations.openRootMetadata, 0
		operations.openRootMetadata = func(name string) (*os.File, error) {
			calls++
			if boundary == "metadata-open" || boundary == "root-recheck-open" && calls == 2 || boundary == "root-final-open" && calls == 3 {
				return nil, cause
			}
			return original(name)
		}
	case "root-open", "root-open-with-handle":
		original := operations.openRoot
		operations.openRoot = func(name string) (*os.Root, error) {
			if boundary == "root-open" {
				return nil, cause
			}
			root, err := original(name)
			return root, errors.Join(err, cause)
		}
	case "directory-open", "directory-open-with-handle":
		original := operations.openDirectory
		operations.openDirectory = func(parent *os.Root, name string) (*os.Root, error) {
			if boundary == "directory-open" {
				return nil, cause
			}
			root, err := original(parent, name)
			return root, errors.Join(err, cause)
		}
	case "file-open", "directory-file-open", "file-open-with-handle":
		original := operations.openFile
		operations.openFile = func(parent *os.Root, name string) (*os.File, error) {
			if boundary == "directory-file-open" && name == "." || boundary != "directory-file-open" && name == "HEAD" {
				if boundary == "file-open-with-handle" {
					file, err := original(parent, name)
					return file, errors.Join(err, cause)
				}
				return nil, cause
			}
			return original(parent, name)
		}
	case "metadata-stat", "directory-stat", "file-pre-read-stat", "file-post-read-stat":
		original, calls := operations.stat, 0
		operations.stat = func(file *os.File) (fs.FileInfo, error) {
			name := administrativeReadFileName(file)
			if name == "HEAD" {
				calls++
			}
			if boundary == "metadata-stat" || boundary == "directory-stat" && name == "logs" || boundary == "file-pre-read-stat" && name == "HEAD" && calls == 1 || boundary == "file-post-read-stat" && name == "HEAD" && calls == 2 {
				return nil, cause
			}
			return original(file)
		}
	case "root-stat", "entry-stat", "entry-pre-read-stat", "entry-post-read-stat", "entry-final-stat":
		original, calls := operations.lstat, 0
		operations.lstat = func(parent *os.Root, name string) (fs.FileInfo, error) {
			if name == "HEAD" {
				calls++
			}
			if boundary == "root-stat" && name == "." || name == "HEAD" && (boundary == "entry-stat" && calls == 1 || boundary == "entry-pre-read-stat" && calls == 2 || boundary == "entry-post-read-stat" && calls == 3 || boundary == "entry-final-stat" && calls == 4) {
				return nil, cause
			}
			return original(parent, name)
		}
	case "names", "names-with-data", "names-late":
		original, calls := operations.readNames, 0
		operations.readNames = func(file *os.File, count int) ([]string, error) {
			calls++
			if boundary == "names" || boundary == "names-late" && calls > 1 {
				return nil, cause
			}
			names, err := original(file, count)
			if boundary == "names-with-data" {
				return names, cause
			}
			return names, err
		}
	case "read", "read-with-data", "read-eof":
		original := operations.read
		operations.read = func(file *os.File, buffer []byte) (int, error) {
			if boundary == "read" {
				return 0, cause
			}
			count, err := original(file, buffer)
			if boundary == "read-with-data" || err == io.EOF {
				return count, cause
			}
			return count, err
		}
	case "metadata-close", "directory-close", "file-close":
		original := operations.closeFile
		operations.closeFile = func(file *os.File) error {
			name, err := administrativeReadFileName(file), original(file)
			if boundary == "metadata-close" && name == "admin" || boundary == "directory-close" && name == "logs" || boundary == "file-close" && name == "HEAD" {
				return errors.Join(err, cause)
			}
			return err
		}
	case "root-close":
		original := operations.closeRoot
		operations.closeRoot = func(root *os.Root) error { return errors.Join(original(root), cause) }
	}
}

func trackAdministrativeReadHandles(test *testing.T, operations *administrativeReadOperations) {
	test.Helper()
	var files []*os.File
	var roots []*os.Root
	openMetadata, openRoot, openDirectory, openFile := operations.openRootMetadata, operations.openRoot, operations.openDirectory, operations.openFile
	operations.openRootMetadata = func(name string) (*os.File, error) {
		file, err := openMetadata(name)
		if file != nil {
			files = append(files, file)
		}
		return file, err
	}
	operations.openRoot = func(name string) (*os.Root, error) {
		root, err := openRoot(name)
		if root != nil {
			roots = append(roots, root)
		}
		return root, err
	}
	operations.openDirectory = func(parent *os.Root, name string) (*os.Root, error) {
		root, err := openDirectory(parent, name)
		if root != nil {
			roots = append(roots, root)
		}
		return root, err
	}
	operations.openFile = func(parent *os.Root, name string) (*os.File, error) {
		file, err := openFile(parent, name)
		if file != nil {
			files = append(files, file)
		}
		return file, err
	}
	test.Cleanup(func() {
		for _, file := range files {
			if err := file.Close(); !errors.Is(err, fs.ErrClosed) {
				test.Errorf("file handle was not closed: %s, %v", file.Name(), err)
			}
		}
		for _, root := range roots {
			if _, err := root.Lstat("."); !errors.Is(err, fs.ErrClosed) {
				test.Errorf("root handle was not closed: %s, %v", root.Name(), err)
				root.Close()
			}
		}
	})
}
