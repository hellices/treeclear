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
	"testing"
)

func TestReadUntrackedFaultSentinelIO(test *testing.T) {
	for _, boundary := range []string{"metadata-open", "root-open", "parent-open", "file-open", "read"} {
		test.Run(boundary, func(test *testing.T) {
			directory := newUntrackedFaultFixture(test)
			operations := trackedUntrackedFaultOperations(test)
			cause := errors.New("injected " + boundary)
			wrapped := fmt.Errorf("native operation: %w", cause)
			injected := false
			switch boundary {
			case "metadata-open":
				operations.openRootMetadata = func(string) (*os.File, error) {
					injected = true
					return nil, wrapped
				}
			case "root-open":
				operations.openRoot = func(string) (*os.Root, error) {
					injected = true
					return nil, wrapped
				}
			case "parent-open":
				operations.openDirectory = func(*os.Root, string) (*os.Root, error) {
					injected = true
					return nil, wrapped
				}
			case "file-open":
				operations.openFile = func(*os.Root, string) (*os.File, error) {
					injected = true
					return nil, wrapped
				}
			case "read":
				operations.read = func(*os.File, []byte) (int, error) {
					injected = true
					return 0, wrapped
				}
			}
			entries, err := readUntracked(test.Context(), directory, []string{"nested/first.bin"}, 64, operations)
			if !injected {
				test.Errorf("%s injection was not reached", boundary)
			}
			assertUntrackedFaultFailure(test, entries, err, cause)
		})
	}
}

func TestReadUntrackedFaultAcquiredHandleErrors(test *testing.T) {
	for _, boundary := range []string{"metadata-open", "root-open", "parent-open", "file-open"} {
		test.Run(boundary, func(test *testing.T) {
			directory := newUntrackedFaultFixture(test)
			operations := trackedUntrackedFaultOperations(test)
			cause := errors.New("acquired resource with error")
			wrapped := fmt.Errorf("%s: %w", boundary, cause)
			injected := false
			switch boundary {
			case "metadata-open":
				original := operations.openRootMetadata
				operations.openRootMetadata = func(name string) (*os.File, error) {
					file, err := original(name)
					if err != nil || file == nil {
						test.Fatalf("fixture metadata acquisition: %v", err)
					}
					injected = true
					return file, wrapped
				}
			case "root-open":
				original := operations.openRoot
				operations.openRoot = func(name string) (*os.Root, error) {
					root, err := original(name)
					if err != nil || root == nil {
						test.Fatalf("fixture root acquisition: %v", err)
					}
					injected = true
					return root, wrapped
				}
			case "parent-open":
				original := operations.openDirectory
				operations.openDirectory = func(parent *os.Root, name string) (*os.Root, error) {
					root, err := original(parent, name)
					if err != nil || root == nil {
						test.Fatalf("fixture parent acquisition: %v", err)
					}
					injected = true
					return root, wrapped
				}
			case "file-open":
				original := operations.openFile
				operations.openFile = func(parent *os.Root, name string) (*os.File, error) {
					file, err := original(parent, name)
					if err != nil || file == nil {
						test.Fatalf("fixture file acquisition: %v", err)
					}
					injected = true
					return file, wrapped
				}
			}
			entries, err := readUntracked(test.Context(), directory, []string{"nested/first.bin"}, 64, operations)
			if !injected {
				test.Errorf("%s did not return an acquired resource plus error", boundary)
			}
			assertUntrackedFaultFailure(test, entries, err, cause)
		})
	}
}

func TestReadUntrackedFaultMetadataAndCloseErrors(test *testing.T) {
	for _, boundary := range []string{
		"metadata-stat", "root-lstat", "parent-lstat", "leaf-lstat", "file-stat",
		"metadata-close", "file-close", "parent-close", "root-close", "final-root-open", "post-read-leaf-lstat", "final-leaf-lstat",
	} {
		test.Run(boundary, func(test *testing.T) {
			directory := newUntrackedFaultFixture(test)
			operations := trackedUntrackedFaultOperations(test)
			cause := errors.New("injected " + boundary)
			wrapped := fmt.Errorf("native metadata/close: %w", cause)
			injected, consumed, laterConsumed := false, false, false
			originalRead := operations.read
			operations.read = func(file *os.File, buffer []byte) (int, error) {
				count, err := originalRead(file, buffer)
				consumed = consumed || count > 0
				laterConsumed = laterConsumed || administrativeReadFileName(file) == "later.bin" && count > 0
				return count, err
			}
			switch boundary {
			case "metadata-stat", "file-stat":
				original := operations.stat
				operations.stat = func(file *os.File) (fs.FileInfo, error) {
					if boundary == "metadata-stat" && administrativeReadFileName(file) == "source" || boundary == "file-stat" && administrativeReadFileName(file) == "first.bin" {
						injected = true
						return nil, wrapped
					}
					return original(file)
				}
			case "root-lstat", "parent-lstat", "leaf-lstat", "post-read-leaf-lstat", "final-leaf-lstat":
				original := operations.lstat
				operations.lstat = func(parent *os.Root, name string) (fs.FileInfo, error) {
					if boundary == "root-lstat" && name == "." || boundary == "parent-lstat" && name == "nested" || boundary == "leaf-lstat" && name == "first.bin" || boundary == "post-read-leaf-lstat" && name == "first.bin" && consumed || boundary == "final-leaf-lstat" && name == "first.bin" && laterConsumed {
						injected = true
						return nil, wrapped
					}
					return original(parent, name)
				}
			case "metadata-close", "file-close":
				original := operations.closeFile
				operations.closeFile = func(file *os.File) error {
					err := original(file)
					if boundary == "metadata-close" && administrativeReadFileName(file) == "source" || boundary == "file-close" && administrativeReadFileName(file) == "first.bin" {
						injected = true
						return errors.Join(err, wrapped)
					}
					return err
				}
			case "parent-close", "root-close":
				original := operations.closeRoot
				operations.closeRoot = func(root *os.Root) error {
					err := original(root)
					name := filepath.Base(filepath.Clean(root.Name()))
					if boundary == "parent-close" && name == "nested" || boundary == "root-close" && name == "source" {
						injected = true
						return errors.Join(err, wrapped)
					}
					return err
				}
			case "final-root-open":
				original := operations.openRootMetadata
				operations.openRootMetadata = func(name string) (*os.File, error) {
					if consumed {
						injected = true
						return nil, wrapped
					}
					return original(name)
				}
			}
			entries, err := readUntracked(test.Context(), directory, []string{"nested/first.bin", "nested/later.bin"}, 64, operations)
			if !injected {
				test.Errorf("%s injection was not reached", boundary)
			}
			if (boundary == "file-close" || boundary == "root-close" || boundary == "parent-close") && !consumed {
				test.Error("close failure must follow successful content reads")
			}
			assertUntrackedFaultFailure(test, entries, err, cause)
		})
	}
}

func TestReadUntrackedFaultReadResults(test *testing.T) {
	for _, boundary := range []string{"negative-count", "oversized-count", "no-progress", "early-eof", "partial-error", "later-file-error", "eof-error"} {
		test.Run(boundary, func(test *testing.T) {
			directory := newUntrackedFaultFixture(test)
			operations := trackedUntrackedFaultOperations(test)
			cause := errors.New("injected read failure")
			expected := error(ErrUntrackedInvalid)
			original, calls, injected, firstBytes := operations.read, 0, false, 0
			operations.read = func(file *os.File, buffer []byte) (int, error) {
				calls++
				if calls > 128 {
					test.Fatal("reader did not stop after bounded no-progress/error calls")
				}
				switch boundary {
				case "negative-count":
					injected = true
					return -1, nil
				case "oversized-count":
					injected = true
					return len(buffer) + 1, nil
				case "no-progress":
					injected = true
					expected = io.ErrNoProgress
					return 0, nil
				case "early-eof":
					injected = true
					return 0, io.EOF
				}
				count, err := original(file, buffer[:min(len(buffer), 2)])
				if administrativeReadFileName(file) == "first.bin" {
					firstBytes += count
				}
				if boundary == "partial-error" && count > 0 || boundary == "later-file-error" && administrativeReadFileName(file) == "later.bin" && count > 0 || boundary == "eof-error" && errors.Is(err, io.EOF) {
					injected = true
					expected = cause
					return count, fmt.Errorf("read boundary: %w", cause)
				}
				return count, err
			}
			entries, err := readUntracked(test.Context(), directory, []string{"nested/first.bin", "nested/later.bin"}, 64, operations)
			if !injected {
				test.Errorf("%s injection was not reached", boundary)
			}
			if (boundary == "later-file-error" || boundary == "eof-error") && firstBytes != 6 {
				test.Errorf("earlier bytes were not fully read: %d", firstBytes)
			}
			assertUntrackedFaultFailure(test, entries, err, expected)
		})
	}
}

func TestReadUntrackedFaultJoinedReadCloseCauses(test *testing.T) {
	directory := newUntrackedFaultFixture(test)
	operations := trackedUntrackedFaultOperations(test)
	readCause, closeCause := errors.New("partial read failed"), errors.New("close failed")
	readInjected, closeInjected := false, false
	originalRead, originalClose := operations.read, operations.closeFile
	operations.read = func(file *os.File, buffer []byte) (int, error) {
		count, err := originalRead(file, buffer[:min(len(buffer), 2)])
		if count == 0 || err != nil {
			test.Fatalf("fixture partial read: %d, %v", count, err)
		}
		readInjected = true
		return count, fmt.Errorf("native read: %w", readCause)
	}
	operations.closeFile = func(file *os.File) error {
		err := originalClose(file)
		if administrativeReadFileName(file) == "first.bin" {
			closeInjected = true
			return errors.Join(err, fmt.Errorf("native close: %w", closeCause))
		}
		return err
	}
	entries, err := readUntracked(test.Context(), directory, []string{"nested/first.bin"}, 64, operations)
	if !readInjected || !closeInjected {
		test.Errorf("injections did not execute: read=%v close=%v", readInjected, closeInjected)
	}
	assertUntrackedFaultFailure(test, entries, err, readCause)
	if !errors.Is(err, closeCause) {
		test.Fatalf("lost close cause: %v", err)
	}
}

func TestReadUntrackedFaultMidCallCancellation(test *testing.T) {
	for _, boundary := range []string{"metadata-open", "root-open", "parent-open", "file-open", "stat", "lstat", "chunked-read", "file-close", "root-close"} {
		test.Run(boundary, func(test *testing.T) {
			directory := newUntrackedFaultFixture(test)
			ctx, cancel := context.WithCancel(test.Context())
			defer cancel()
			operations := trackedUntrackedFaultOperations(test)
			injected := false
			cancelHere := func() {
				injected = true
				cancel()
			}
			switch boundary {
			case "metadata-open":
				original := operations.openRootMetadata
				operations.openRootMetadata = func(name string) (*os.File, error) {
					file, err := original(name)
					cancelHere()
					return file, err
				}
			case "root-open":
				original := operations.openRoot
				operations.openRoot = func(name string) (*os.Root, error) {
					root, err := original(name)
					cancelHere()
					return root, err
				}
			case "parent-open":
				original := operations.openDirectory
				operations.openDirectory = func(parent *os.Root, name string) (*os.Root, error) {
					root, err := original(parent, name)
					cancelHere()
					return root, err
				}
			case "file-open":
				original := operations.openFile
				operations.openFile = func(parent *os.Root, name string) (*os.File, error) {
					file, err := original(parent, name)
					cancelHere()
					return file, err
				}
			case "stat":
				original := operations.stat
				operations.stat = func(file *os.File) (fs.FileInfo, error) {
					information, err := original(file)
					cancelHere()
					return information, err
				}
			case "lstat":
				original := operations.lstat
				operations.lstat = func(parent *os.Root, name string) (fs.FileInfo, error) {
					information, err := original(parent, name)
					cancelHere()
					return information, err
				}
			case "chunked-read":
				original := operations.read
				operations.read = func(file *os.File, buffer []byte) (int, error) {
					if ctx.Err() != nil {
						test.Fatal("read continued after mid-chunk cancellation")
					}
					count, err := original(file, buffer[:min(len(buffer), 2)])
					if count == 0 || err != nil {
						test.Fatalf("fixture chunk read: %d, %v", count, err)
					}
					cancelHere()
					return count, err
				}
			case "file-close":
				original := operations.closeFile
				operations.closeFile = func(file *os.File) error {
					err := original(file)
					if administrativeReadFileName(file) == "first.bin" {
						cancelHere()
					}
					return err
				}
			case "root-close":
				original := operations.closeRoot
				operations.closeRoot = func(root *os.Root) error {
					err := original(root)
					cancelHere()
					return err
				}
			}
			entries, err := readUntracked(ctx, directory, []string{"nested/first.bin"}, 64, operations)
			if !injected {
				test.Errorf("%s cancellation boundary was not reached", boundary)
			}
			assertUntrackedFaultFailure(test, entries, err, context.Canceled)
		})
	}
}

func newUntrackedFaultFixture(test *testing.T) string {
	test.Helper()
	directory := filepath.Join(test.TempDir(), "source")
	if err := os.MkdirAll(filepath.Join(directory, "nested"), 0o700); err != nil {
		test.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"nested/first.bin": {0x00, 0xff, 'a', '\n', 'b', 'c'},
		"nested/later.bin": []byte("later contents"),
		"unrequested.bin":  []byte("must not read"),
	} {
		if err := os.WriteFile(filepath.Join(directory, filepath.FromSlash(name)), data, 0o600); err != nil {
			test.Fatal(err)
		}
	}
	return directory
}

func trackedUntrackedFaultOperations(test *testing.T) untrackedReadOperations {
	test.Helper()
	operations := defaultUntrackedReadOperations()
	trackAdministrativeReadHandles(test, &operations.administrativeReadOperations)
	operations.readNames = func(*os.File, int) ([]string, error) {
		test.Fatal("untracked reader enumerated a directory")
		return nil, nil
	}
	originalLstat, originalOpen := operations.lstat, operations.openFile
	operations.lstat = func(parent *os.Root, name string) (fs.FileInfo, error) {
		if name == "unrequested.bin" {
			test.Fatal("untracked reader inspected an unrequested leaf")
		}
		return originalLstat(parent, name)
	}
	operations.openFile = func(parent *os.Root, name string) (*os.File, error) {
		if name == "unrequested.bin" {
			test.Fatal("untracked reader opened an unrequested leaf")
		}
		return originalOpen(parent, name)
	}
	return operations
}

func assertUntrackedFaultFailure(test *testing.T, entries []UntrackedEntry, err, cause error) {
	test.Helper()
	if entries != nil || err == nil || cause != nil && !errors.Is(err, cause) {
		test.Fatalf("want nil entries and error containing %v; got %d entries (nil=%v), %v", cause, len(entries), entries == nil, err)
	}
}
