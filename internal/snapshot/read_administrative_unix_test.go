//go:build darwin || linux

package snapshot

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"
)

func TestReadAdministrativeFocusedUnixSpecialModes(test *testing.T) {
	directory := newAdministrativeReadFixture(test)
	addAdministrativeReadNestedFixture(test, directory)
	for name, mode := range map[string]fs.FileMode{
		".": 0o750 | fs.ModeSetgid | fs.ModeSticky, "HEAD": 0o640 | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky,
		"logs": 0o770 | fs.ModeSetgid | fs.ModeSticky,
	} {
		if err := os.Chmod(filepath.Join(directory, name), mode); err != nil {
			test.Fatal(err)
		}
	}
	expected := administrativeReadFixtureEntries(test, directory, []string{".", "HEAD", "commondir", "gitdir", "logs", "logs/entry"})
	actual, err := ReadAdministrative(test.Context(), directory)
	if err != nil || !reflect.DeepEqual(actual, expected) {
		test.Fatalf("special diagnostic modes changed: %#v, %v", actual, err)
	}
}

func TestReadAdministrativeFocusedUnixUnsafeObjects(test *testing.T) {
	for _, target := range []string{"root-symlink", "file-symlink", "directory-symlink", "fifo", "invalid-name"} {
		test.Run(target, func(test *testing.T) {
			directory := newAdministrativeReadFixture(test)
			switch target {
			case "root-symlink":
				alias := filepath.Join(test.TempDir(), "alias")
				if err := os.Symlink(directory, alias); err != nil {
					test.Fatal(err)
				}
				directory = alias
			case "file-symlink":
				if err := os.Symlink("HEAD", filepath.Join(directory, "index")); err != nil {
					test.Fatal(err)
				}
			case "directory-symlink":
				if err := os.Symlink(test.TempDir(), filepath.Join(directory, "logs")); err != nil {
					test.Fatal(err)
				}
			case "fifo":
				if err := syscall.Mkfifo(filepath.Join(directory, "pipe"), 0o600); err != nil {
					test.Fatal(err)
				}
			case "invalid-name":
				if err := os.WriteFile(filepath.Join(directory, `bad\name`), nil, 0o600); err != nil {
					test.Fatal(err)
				}
			}
			entries, err := ReadAdministrative(test.Context(), directory)
			assertAdministrativeReadFailure(test, entries, err, nil)
		})
	}
}

func TestReadAdministrativeFocusedUnixSymlinkSubstitution(test *testing.T) {
	for _, target := range []string{"root", "directory", "file"} {
		test.Run(target, func(test *testing.T) {
			directory := newAdministrativeReadFixture(test)
			addAdministrativeReadNestedFixture(test, directory)
			filename := directory
			if target == "directory" {
				filename = filepath.Join(directory, "logs")
			} else if target == "file" {
				filename = filepath.Join(directory, "HEAD")
			}
			changed := false
			substitute := func() {
				if err := os.Rename(filename, filename+"-original"); err != nil {
					test.Fatal(err)
				}
				if err := os.Symlink(filepath.Base(filename)+"-original", filename); err != nil {
					test.Fatal(err)
				}
				changed = true
			}
			operations := defaultAdministrativeReadOperations()
			trackAdministrativeReadHandles(test, &operations)
			switch target {
			case "root":
				original := operations.openRoot
				operations.openRoot = func(name string) (*os.Root, error) { substitute(); return original(name) }
			case "directory":
				original := operations.openDirectory
				operations.openDirectory = func(parent *os.Root, name string) (*os.Root, error) { substitute(); return original(parent, name) }
			case "file":
				original := operations.openFile
				operations.openFile = func(parent *os.Root, name string) (*os.File, error) {
					if name == "HEAD" {
						substitute()
					}
					return original(parent, name)
				}
			}
			originalRead := operations.read
			operations.read = func(file *os.File, buffer []byte) (int, error) {
				if target != "directory" || administrativeReadFileName(file) == "entry" {
					test.Fatal("consumed bytes through a symlink to the original observed object")
				}
				return originalRead(file, buffer)
			}
			entries, err := readAdministrative(test.Context(), directory, operations)
			assertAdministrativeReadFailure(test, entries, err, nil)
			if !changed {
				test.Fatal("symlink substitution did not run")
			}
		})
	}
}

func TestReadAdministrativeFocusedUnixFIFOSubstitution(test *testing.T) {
	for _, target := range []string{"root", "directory", "file"} {
		test.Run(target, func(test *testing.T) {
			directory := newAdministrativeReadFixture(test)
			addAdministrativeReadNestedFixture(test, directory)
			filename := directory
			if target == "directory" {
				filename = filepath.Join(directory, "logs")
			} else if target == "file" {
				filename = filepath.Join(directory, "HEAD")
			}
			backup := filepath.Join(test.TempDir(), "previous")
			changed := false
			substitute := func() error {
				if err := os.Rename(filename, backup); err != nil {
					return err
				}
				if err := syscall.Mkfifo(filename, 0o600); err != nil {
					return err
				}
				changed = true
				return nil
			}
			operations := defaultAdministrativeReadOperations()
			switch target {
			case "root":
				original := operations.openRoot
				operations.openRoot = func(name string) (*os.Root, error) {
					if err := substitute(); err != nil {
						return nil, err
					}
					return original(name)
				}
			case "directory":
				original := operations.openDirectory
				operations.openDirectory = func(parent *os.Root, name string) (*os.Root, error) {
					if err := substitute(); err != nil {
						return nil, err
					}
					return original(parent, name)
				}
			case "file":
				original := operations.openFile
				operations.openFile = func(parent *os.Root, name string) (*os.File, error) {
					if name == "HEAD" {
						if err := substitute(); err != nil {
							return nil, err
						}
					}
					return original(parent, name)
				}
			}
			ctx, cancel := context.WithTimeout(test.Context(), 2*time.Second)
			defer cancel()
			type result struct {
				entries []AdminEntry
				err     error
			}
			finished := make(chan result, 1)
			go func() {
				entries, err := readAdministrative(ctx, directory, operations)
				finished <- result{entries, err}
			}()
			select {
			case outcome := <-finished:
				assertAdministrativeReadFailure(test, outcome.entries, outcome.err, nil)
				if !changed {
					test.Fatal("FIFO substitution did not run")
				}
			case <-ctx.Done():
				descriptor, err := syscall.Open(filename, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
				if err == nil {
					defer syscall.Close(descriptor)
				}
				select {
				case <-finished:
				case <-time.After(2 * time.Second):
				}
				test.Fatal("opening a substituted FIFO blocked")
			}
		})
	}
}
