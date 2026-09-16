package fssecure

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPrivateDirectoryReaderUsesReadOnlyBoundedOperations(test *testing.T) {
	want := []PrivateFile{{Name: "manifest.json", Contents: []byte("123")}, {Name: "payload", Contents: []byte(strings.Repeat("p", 16384))}, {Name: "empty"}}
	path := readDirectoryFixture(test, want)
	limits := []PrivateFileLimit{{Name: "manifest.json", MaximumBytes: 3}, {Name: "payload", MaximumBytes: 16384}, {Name: "empty", MaximumBytes: 16}}
	maximum := int64(16387)
	remaining := maximum
	readBytes := make(map[string]int64)
	fileLimits := make(map[string]int64)
	for _, limit := range limits {
		fileLimits[limit.Name] = limit.MaximumBytes
	}
	operations := nativePrivateDirectoryReadOperations()
	opened := make(map[*os.File]bool)
	operations = observeReadDirectoryOperations(operations, func(string) error { return nil }, opened)
	openat := operations.openat
	operations.openat = func(descriptor int, name string, flags int, mode uint32) (*os.File, error) {
		if flags&(unix.O_CREAT|unix.O_TRUNC|unix.O_WRONLY|unix.O_RDWR|unix.O_APPEND) != 0 || mode != 0 {
			test.Errorf("mutating open flags for %q: %#x, %#o", name, flags, mode)
		}
		if flags&directoryReadFlags != directoryReadFlags {
			test.Errorf("open %q lacks native nofollow/nonblock/cloexec flags: %#x", name, flags)
		}
		if descriptor == unix.AT_FDCWD && name != filepath.Dir(path) {
			test.Errorf("non-parent operation is not descriptor-relative: %q", name)
		}
		if descriptor != unix.AT_FDCWD && filepath.IsAbs(name) {
			test.Errorf("absolute descriptor-relative child: %q", name)
		}
		return openat(descriptor, name, flags, mode)
	}
	read := operations.read
	operations.read = func(file *os.File, buffer []byte) (int, error) {
		allowed := min(fileLimits[file.Name()]-readBytes[file.Name()], remaining) + 1
		if int64(len(buffer)) > allowed || len(buffer) == 0 {
			test.Errorf("read %q requested %d bytes; maximum including sentinel %d", file.Name(), len(buffer), allowed)
		}
		count, err := read(file, buffer)
		readBytes[file.Name()] += int64(count)
		remaining -= int64(count)
		return count, err
	}
	readDir := operations.readDir
	listings := 0
	operations.readDir = func(file *os.File, count int) ([]os.DirEntry, error) {
		listings++
		if count != len(want)+1 {
			test.Errorf("enumeration count = %d; want %d", count, len(want)+1)
		}
		return readDir(file, count)
	}
	reader := privateDirectoryReader{operations: operations}
	actual, err := reader.read(context.Background(), path, limits, maximum)
	if err != nil {
		test.Fatal(err)
	}
	assertReadDirectoryFiles(test, actual, want)
	if len(opened) != 0 || listings < 2 || remaining != 0 {
		test.Fatalf("read boundary: %d unclosed handles, %d enumerations, %d remaining bytes", len(opened), listings, remaining)
	}
}

func TestPrivateDirectoryReaderOperationFailures(test *testing.T) {
	path := readDirectoryFixture(test, []PrivateFile{{Name: "first", Contents: []byte("one")}, {Name: "second", Contents: []byte("two")}})
	limits := []PrivateFileLimit{{Name: "first", MaximumBytes: 3}, {Name: "second", MaximumBytes: 3}}
	before := readDirectoryFixtureState(test, path)
	events := readDirectoryOperationTrace(test, path, limits, 6)
	failure := errors.New("injected private directory I/O failure")
	for failureIndex, event := range events {
		test.Run(fmt.Sprintf("%03d-%s", failureIndex, event), func(test *testing.T) {
			opened := make(map[*os.File]bool)
			calls := 0
			operations := observeReadDirectoryOperations(nativePrivateDirectoryReadOperations(), func(string) error {
				current := calls
				calls++
				if current == failureIndex {
					return failure
				}
				return nil
			}, opened)
			reader := privateDirectoryReader{operations: operations}
			files, err := reader.read(context.Background(), path, limits, 6)
			if files != nil || !errors.Is(err, failure) || calls <= failureIndex {
				test.Fatalf("operation failure = %v, %v after %d calls; want nil and preserved failure", files, err, calls)
			}
			if len(opened) != 0 {
				test.Fatalf("failed read leaked %d handles", len(opened))
			}
			assertReadDirectoryUnchanged(test, path, before)
			assertContents(test, filepath.Join(path, "first"), []byte("one"))
			assertContents(test, filepath.Join(path, "second"), []byte("two"))
		})
	}
}

func TestPrivateDirectoryReaderCancellationBoundaries(test *testing.T) {
	path := readDirectoryFixture(test, []PrivateFile{{Name: "first", Contents: []byte("one")}, {Name: "empty"}})
	limits := []PrivateFileLimit{{Name: "first", MaximumBytes: 3}, {Name: "empty", MaximumBytes: 3}}
	before := readDirectoryFixtureState(test, path)
	events := readDirectoryOperationTrace(test, path, limits, 3)
	for cancelIndex, event := range events {
		test.Run(fmt.Sprintf("%03d-%s", cancelIndex, event), func(test *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opened := make(map[*os.File]bool)
			calls := 0
			operations := observeReadDirectoryOperations(nativePrivateDirectoryReadOperations(), func(string) error {
				current := calls
				calls++
				if current == cancelIndex {
					cancel()
				}
				return nil
			}, opened)
			reader := privateDirectoryReader{operations: operations}
			files, err := reader.read(ctx, path, limits, 3)
			if files != nil || !errors.Is(err, context.Canceled) || calls <= cancelIndex {
				test.Fatalf("cancellation = %v, %v after %d calls; want nil, context.Canceled", files, err, calls)
			}
			if len(opened) != 0 {
				test.Fatalf("canceled read leaked %d handles", len(opened))
			}
			assertReadDirectoryUnchanged(test, path, before)
		})
	}
}

func TestPrivateDirectoryReaderBoundsReturnedBytesNotOnlyStat(test *testing.T) {
	for _, kind := range []string{"file limit", "aggregate limit", "zero remaining"} {
		test.Run(kind, func(test *testing.T) {
			path := readDirectoryFixture(test, []PrivateFile{{Name: "first", Contents: []byte("ok")}, {Name: "second"}})
			limits := []PrivateFileLimit{{Name: "first", MaximumBytes: 2}, {Name: "second", MaximumBytes: 2}}
			maximum := int64(8)
			if kind == "aggregate limit" {
				limits[1].MaximumBytes = 8
				maximum = 4
			} else if kind == "zero remaining" {
				maximum = 2
			}
			operations := nativePrivateDirectoryReadOperations()
			read := operations.read
			observed := 0
			operations.read = func(file *os.File, buffer []byte) (int, error) {
				if file.Name() != "second" {
					return read(file, buffer)
				}
				for index := range buffer {
					buffer[index] = 'x'
				}
				observed += len(buffer)
				return len(buffer), nil
			}
			reader := privateDirectoryReader{operations: operations}
			files, err := reader.read(context.Background(), path, limits, maximum)
			if files != nil || !errors.Is(err, ErrPrivateDirectoryLimit) || observed != int(min(limits[1].MaximumBytes, maximum-2))+1 {
				test.Fatalf("untrusted length read = %v, %v; consumed %d bytes", files, err, observed)
			}
			assertContents(test, filepath.Join(path, "second"), nil)
		})
	}
}

func TestPrivateDirectoryReaderRejectsInvalidReadResults(test *testing.T) {
	readFailure := errors.New("read failed with bytes")
	for _, kind := range []string{"zero progress", "negative count", "excess count", "bytes and failure", "short EOF"} {
		test.Run(kind, func(test *testing.T) {
			path := readDirectoryFixture(test, []PrivateFile{{Name: "payload", Contents: []byte("keep")}})
			operations := nativePrivateDirectoryReadOperations()
			want := error(fs.ErrInvalid)
			operations.read = func(file *os.File, buffer []byte) (int, error) {
				switch kind {
				case "zero progress":
					return 0, nil
				case "negative count":
					return -1, nil
				case "excess count":
					return len(buffer) + 1, nil
				case "bytes and failure":
					buffer[0] = 'k'
					return 1, readFailure
				default:
					buffer[0] = 'k'
					return 1, io.EOF
				}
			}
			if kind == "zero progress" {
				want = io.ErrNoProgress
			} else if kind == "bytes and failure" {
				want = readFailure
			}
			reader := privateDirectoryReader{operations: operations}
			files, err := reader.read(context.Background(), path, []PrivateFileLimit{{Name: "payload", MaximumBytes: 8}}, 8)
			if files != nil || !errors.Is(err, want) {
				test.Fatalf("invalid read = %v, %v; want nil, %v", files, err, want)
			}
		})
	}
}

func TestPrivateDirectoryReaderBoundsEnumerationAndRejectsAmbiguity(test *testing.T) {
	listingFailure := errors.New("listing failure accompanying EOF")
	for _, kind := range []string{"large directory", "duplicate entry", "nil entry", "joined EOF failure"} {
		test.Run(kind, func(test *testing.T) {
			path := readDirectoryFixture(test, []PrivateFile{{Name: "first"}, {Name: "second"}})
			if kind == "large directory" {
				for index := range 64 {
					writePrivateFixture(test, filepath.Join(path, fmt.Sprintf("extra-%02d", index)), nil)
				}
			}
			before := readDirectoryFixtureState(test, path)
			operations := nativePrivateDirectoryReadOperations()
			readDir := operations.readDir
			listings := 0
			operations.readDir = func(file *os.File, count int) ([]os.DirEntry, error) {
				listings++
				if count != 3 {
					test.Fatalf("unbounded enumeration count: %d", count)
				}
				entries, err := readDir(file, count)
				if len(entries) > 3 || kind == "large directory" && len(entries) != 3 {
					test.Fatalf("enumerated %d entries; want bounded prefix", len(entries))
				}
				switch kind {
				case "duplicate entry":
					entries[1] = entries[0]
				case "nil entry":
					entries[0] = nil
				case "joined EOF failure":
					err = errors.Join(io.EOF, listingFailure)
				}
				return entries, err
			}
			reader := privateDirectoryReader{operations: operations}
			files, err := reader.read(context.Background(), path, []PrivateFileLimit{{Name: "first", MaximumBytes: 1}, {Name: "second", MaximumBytes: 1}}, 1)
			want := error(fs.ErrInvalid)
			if kind == "joined EOF failure" {
				want = listingFailure
			}
			if files != nil || !errors.Is(err, want) || listings != 1 {
				test.Fatalf("ambiguous listing = %v, %v after %d listings; want nil, %v", files, err, listings, want)
			}
			assertReadDirectoryUnchanged(test, path, before)
		})
	}
}

func TestPrivateDirectoryReaderPreservesCombinedCauses(test *testing.T) {
	for _, kind := range []string{"limit and read", "cancellation and read", "limit and close"} {
		test.Run(kind, func(test *testing.T) {
			path := readDirectoryFixture(test, []PrivateFile{{Name: "payload", Contents: []byte("x")}})
			before := readDirectoryFixtureState(test, path)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("combined I/O failure")
			operations := nativePrivateDirectoryReadOperations()
			operations.read = func(file *os.File, buffer []byte) (int, error) {
				buffer[0] = 'x'
				buffer[1] = 'y'
				if kind == "cancellation and read" {
					cancel()
					return 1, failure
				}
				if kind == "limit and read" {
					return 2, failure
				}
				return 2, io.EOF
			}
			if kind == "limit and close" {
				closeFile := operations.close
				operations.close = func(file *os.File) error {
					closeErr := closeFile(file)
					if file.Name() == "payload" {
						return errors.Join(closeErr, failure)
					}
					return closeErr
				}
			}
			reader := privateDirectoryReader{operations: operations}
			files, err := reader.read(ctx, path, []PrivateFileLimit{{Name: "payload", MaximumBytes: 1}}, 1)
			want := ErrPrivateDirectoryLimit
			if kind == "cancellation and read" {
				want = context.Canceled
			}
			if files != nil || !errors.Is(err, failure) || !errors.Is(err, want) {
				test.Fatalf("combined failure = %v, %v; want nil, %v and I/O cause", files, err, want)
			}
			assertReadDirectoryUnchanged(test, path, before)
		})
	}
}

func readDirectoryOperationTrace(test *testing.T, path string, limits []PrivateFileLimit, maximum int64) []string {
	test.Helper()
	var events []string
	opened := make(map[*os.File]bool)
	operations := observeReadDirectoryOperations(nativePrivateDirectoryReadOperations(), func(event string) error {
		events = append(events, event)
		return nil
	}, opened)
	reader := privateDirectoryReader{operations: operations}
	if _, err := reader.read(context.Background(), path, limits, maximum); err != nil {
		test.Fatal(err)
	}
	if len(opened) != 0 || len(events) == 0 {
		test.Fatalf("baseline operations = %d events, %d leaked handles", len(events), len(opened))
	}
	return events
}

func observeReadDirectoryOperations(operations privateDirectoryReadOperations, observe func(string) error, opened map[*os.File]bool) privateDirectoryReadOperations {
	return privateDirectoryReadOperations{
		openat: func(descriptor int, name string, flags int, mode uint32) (*os.File, error) {
			if err := observe("open " + name); err != nil {
				return nil, err
			}
			file, err := operations.openat(descriptor, name, flags, mode)
			if file != nil {
				opened[file] = true
			}
			return file, err
		},
		read: func(file *os.File, buffer []byte) (int, error) {
			if err := observe("read " + file.Name()); err != nil {
				return 0, err
			}
			return operations.read(file, buffer)
		},
		readDir: func(file *os.File, count int) ([]os.DirEntry, error) {
			if err := observe("list " + file.Name()); err != nil {
				return nil, err
			}
			return operations.readDir(file, count)
		},
		close: func(file *os.File) error {
			closeErr := operations.close(file)
			delete(opened, file)
			return errors.Join(closeErr, observe("close "+file.Name()))
		},
	}
}
