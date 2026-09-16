//go:build darwin || linux

package git

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestRejectSplitIndexNativeSpecialBackingEntries(test *testing.T) {
	for _, kind := range []string{"dangling link", "fifo", "empty suffix"} {
		test.Run(kind, func(test *testing.T) {
			directory := test.TempDir()
			administrative := test.TempDir()
			path := filepath.Join(administrative, "sharedindex.backing")
			var err error
			switch kind {
			case "dangling link":
				err = os.Symlink("missing-owned-target", path)
			case "fifo":
				err = syscall.Mkfifo(path, 0o600)
			case "empty suffix":
				err = os.WriteFile(filepath.Join(administrative, "sharedindex."), nil, 0o600)
			}
			if err != nil {
				test.Fatal(err)
			}
			client, commands := readonlyIndexDirectoryClient(test, directory, administrative)
			if err := client.rejectSplitIndex(test.Context(), directory); !errors.Is(err, errors.ErrUnsupported) || len(*commands) != 1 {
				test.Fatalf("special backing entry returned %v after %q", err, *commands)
			}
		})
	}
}

func TestReadonlyIndexDirectoryNativeRootReplacement(test *testing.T) {
	for _, stage := range []string{"before open", "alias before open", "after enumeration"} {
		test.Run(stage, func(test *testing.T) {
			directory := readonlyIndexCanonicalTemporaryDirectory(test)
			operations := defaultReadonlyIndexOperations()
			replace := func() {
				original := directory + "-original"
				if err := os.Rename(directory, original); err != nil {
					test.Fatal(err)
				}
				if stage == "alias before open" {
					if err := os.Symlink(original, directory); err != nil {
						test.Fatal(err)
					}
				} else if err := os.Mkdir(directory, 0o700); err != nil {
					test.Fatal(err)
				}
			}
			nativeOpen := operations.open
			operations.open = func(path string) (*os.File, error) {
				if stage != "after enumeration" {
					replace()
				}
				return nativeOpen(path)
			}
			operations.readNames = func(file *os.File, limit int) ([]string, error) {
				names, err := file.Readdirnames(limit)
				if stage == "after enumeration" && err == io.EOF {
					replace()
				}
				return names, err
			}
			if err := rejectSplitIndexDirectory(test.Context(), directory, operations); !errors.Is(err, ErrWorktreeChanged) {
				test.Fatalf("native directory replacement error = %v; want ErrWorktreeChanged", err)
			}
		})
	}
}

func TestReadonlyIndexDirectoryNativeDeniedRoot(test *testing.T) {
	if os.Geteuid() == 0 {
		test.Skip("native mode denial requires an unprivileged process")
	}
	directory := readonlyIndexCanonicalTemporaryDirectory(test)
	test.Cleanup(func() {
		if err := os.Chmod(directory, 0o700); err != nil {
			test.Error(err)
		}
	})
	if err := os.Chmod(directory, 0); err != nil {
		test.Fatal(err)
	}
	if err := rejectSplitIndexDirectory(test.Context(), directory, defaultReadonlyIndexOperations()); !errors.Is(err, fs.ErrPermission) || errors.Is(err, errors.ErrUnsupported) {
		test.Fatalf("inaccessible administrative directory error = %v", err)
	}
}

func TestReadonlyIndexDirectoryNativeLateFIFORefusesWithoutBlocking(test *testing.T) {
	directory := readonlyIndexCanonicalTemporaryDirectory(test)
	operations := defaultReadonlyIndexOperations()
	nativeOpen := operations.open
	operations.open = func(path string) (*os.File, error) {
		if err := os.Rename(path, path+"-original"); err != nil {
			return nil, err
		}
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			return nil, err
		}
		return nativeOpen(path)
	}
	completed := make(chan error, 1)
	go func() {
		completed <- rejectSplitIndexDirectory(test.Context(), directory, operations)
	}()
	select {
	case err := <-completed:
		if err == nil || errors.Is(err, errors.ErrUnsupported) {
			test.Fatalf("replaced FIFO root error = %v", err)
		}
	case <-time.After(2 * time.Second):
		release, err := os.OpenFile(directory, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err != nil {
			test.Fatalf("release blocked fixture FIFO: %v", err)
		}
		defer release.Close()
		select {
		case <-completed:
		case <-time.After(2 * time.Second):
			test.Fatal("fixture FIFO reader remained blocked after release")
		}
		test.Fatal("administrative directory opening blocked on a replaced FIFO")
	}
}
