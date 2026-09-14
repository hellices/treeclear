//go:build darwin || linux

package snapshot

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadUntrackedNativeUnixReadlinkBoundaries(test *testing.T) {
	for _, boundary := range []string{"error", "text-and-error", "cancel", "pre-readlink-replacement", "post-readlink-replacement", "later-file-replacement"} {
		test.Run(boundary, func(test *testing.T) {
			directory := newUntrackedFaultFixture(test)
			filename := filepath.Join(directory, "nested", "a-link")
			if err := os.Symlink("first.bin", filename); err != nil {
				test.Fatal(err)
			}
			initial, err := os.Lstat(filename)
			if err != nil {
				test.Fatal(err)
			}
			ctx, cancel := context.WithCancel(test.Context())
			defer cancel()
			operations := trackedUntrackedFaultOperations(test)
			cause := errors.New("injected readlink error")
			expected := error(ErrUntrackedInvalid)
			injected, linkRead := false, false
			replace := func() {
				parentInformation, err := os.Stat(filepath.Dir(filename))
				if err != nil {
					test.Fatal(err)
				}
				if err := os.Rename(filename, filepath.Join(test.TempDir(), "previous-link")); err != nil {
					test.Fatal(err)
				}
				if err := os.Symlink("later.bin", filename); err != nil {
					test.Fatal(err)
				}
				if err := os.Chtimes(filepath.Dir(filename), parentInformation.ModTime(), parentInformation.ModTime()); err != nil {
					test.Fatal(err)
				}
				current, err := os.Lstat(filename)
				if err != nil {
					test.Fatal(err)
				}
				if os.SameFile(initial, current) || current.Mode() != initial.Mode() || current.Size() != initial.Size() {
					test.Fatal("link replacement did not change identity while preserving size and mode")
				}
				injected = true
			}
			originalLink := operations.readlink
			operations.readlink = func(parent *os.Root, name string) (string, error) {
				if name != "a-link" {
					test.Fatalf("unexpected readlink: %q", name)
				}
				if boundary == "error" {
					injected = true
					expected = cause
					return "", fmt.Errorf("native readlink: %w", cause)
				}
				if boundary == "pre-readlink-replacement" {
					replace()
				}
				target, err := originalLink(parent, name)
				if err != nil || target != "first.bin" && target != "later.bin" {
					test.Fatalf("fixture link read: %q, %v", target, err)
				}
				linkRead = true
				switch boundary {
				case "text-and-error":
					injected = true
					expected = cause
					return target, fmt.Errorf("native readlink with text: %w", cause)
				case "cancel":
					injected = true
					expected = context.Canceled
					cancel()
				case "post-readlink-replacement":
					replace()
				}
				return target, err
			}
			originalOpen := operations.openFile
			operations.openFile = func(parent *os.Root, name string) (*os.File, error) {
				if name != "later.bin" {
					test.Fatalf("dereferenced a link or unrequested target: %q", name)
				}
				if boundary == "later-file-replacement" {
					if !linkRead {
						test.Fatal("later-file mutation did not follow the link read")
					}
					replace()
				}
				return originalOpen(parent, name)
			}
			entries, err := readUntracked(ctx, directory, []string{"nested/a-link", "nested/later.bin"}, 64, operations)
			if !injected {
				test.Errorf("%s boundary did not execute", boundary)
			}
			assertUntrackedFaultFailure(test, entries, err, expected)
		})
	}
}

func TestReadUntrackedNativeUnixPreOpenSubstitutions(test *testing.T) {
	for _, kind := range []string{"symlink", "fifo"} {
		for _, target := range []string{"root-metadata", "root", "parent", "leaf"} {
			test.Run(kind+"/"+target, func(test *testing.T) {
				directory := newUntrackedFaultFixture(test)
				filename := directory
				if target == "parent" {
					filename = filepath.Join(directory, "nested")
				} else if target == "leaf" {
					filename = filepath.Join(directory, "nested", "first.bin")
				}
				changed := false
				substitute := func() {
					backup := filename + "-original"
					if err := os.Rename(filename, backup); err != nil {
						test.Fatal(err)
					}
					var err error
					if kind == "fifo" {
						err = syscall.Mkfifo(filename, 0o600)
					} else {
						err = os.Symlink(filepath.Base(backup), filename)
					}
					if err != nil {
						test.Fatal(err)
					}
					information, err := os.Lstat(filename)
					if err != nil {
						test.Fatal(err)
					}
					if kind == "fifo" && information.Mode()&fs.ModeNamedPipe == 0 || kind == "symlink" && information.Mode()&fs.ModeSymlink == 0 {
						test.Fatal("native substitution did not create the intended object")
					}
					changed = true
				}
				operations := trackedUntrackedFaultOperations(test)
				switch target {
				case "root-metadata":
					original := operations.openRootMetadata
					operations.openRootMetadata = func(name string) (*os.File, error) {
						substitute()
						return original(name)
					}
				case "root":
					original := operations.openRoot
					operations.openRoot = func(name string) (*os.Root, error) {
						substitute()
						return original(name)
					}
				case "parent":
					original := operations.openDirectory
					operations.openDirectory = func(parent *os.Root, name string) (*os.Root, error) {
						substitute()
						return original(parent, name)
					}
				case "leaf":
					original := operations.openFile
					operations.openFile = func(parent *os.Root, name string) (*os.File, error) {
						if name == "first.bin" {
							substitute()
						}
						return original(parent, name)
					}
				}
				operations.read = func(*os.File, []byte) (int, error) {
					test.Fatal("consumed bytes from a substituted link/FIFO")
					return 0, nil
				}
				operations.readlink = func(*os.Root, string) (string, error) {
					test.Fatal("accepted a link substituted for an observed regular file")
					return "", nil
				}
				entries, err := readUntracked(test.Context(), directory, []string{"nested/first.bin"}, 64, operations)
				if !changed {
					test.Error("native pre-open substitution was not exercised")
				}
				assertUntrackedFaultFailure(test, entries, err, nil)
			})
		}
	}
}
