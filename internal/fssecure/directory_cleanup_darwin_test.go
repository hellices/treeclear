package fssecure

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDirectoryPublicationRejectsChangedDirectorySecurity(test *testing.T) {
	for _, subject := range []string{"parent mode", "parent ACL", "staging mode", "staging ACL"} {
		test.Run(subject, func(test *testing.T) {
			publisher, target, files := directoryPublisherFixture(test)
			nativeWrite := publisher.operations.write
			var changed, staging, before string
			publisher.operations.write = func(file *os.File, contents []byte) (int, error) {
				count, err := nativeWrite(file, contents)
				if err != nil || changed != "" {
					return count, err
				}
				staging = directoryStagingPath(test, filepath.Dir(target))
				changed = staging
				if strings.HasPrefix(subject, "parent") {
					changed = filepath.Dir(target)
				}
				if strings.HasSuffix(subject, "mode") {
					if err := os.Chmod(changed, 0o755); err != nil {
						test.Fatal(err)
					}
				} else {
					addDirectoryFixtureACL(test, changed)
				}
				before = darwinACLSnapshot(test, changed)
				return count, nil
			}
			path, err := publisher.publish(context.Background(), target, files)
			if path != "" || err == nil || changed == "" {
				test.Fatalf("changed directory security accepted: %q, %v", path, err)
			}
			if after := darwinACLSnapshot(test, changed); after != before {
				test.Fatal("existing directory security was repaired or changed")
			}
			assertContents(test, filepath.Join(staging, files[0].Name), files[0].Contents)
			assertDirectoryAbsent(test, target)
		})
	}
}

func TestDirectoryPublicationRejectsParentReplacementDuringWrite(test *testing.T) {
	for _, change := range []string{"replacement", "ancestor symlink"} {
		test.Run(change, func(test *testing.T) {
			root, err := filepath.EvalSymlinks(privateDirectoryFixture(test))
			if err != nil {
				test.Fatal(err)
			}
			ancestor := filepath.Join(root, "ancestor")
			parent := filepath.Join(ancestor, "parent")
			if err := os.MkdirAll(parent, 0o700); err != nil {
				test.Fatal(err)
			}
			publisher := directoryPublisher{operations: nativeDirectoryOperations()}
			nativeWrite := publisher.operations.write
			var retained string
			publisher.operations.write = func(file *os.File, contents []byte) (int, error) {
				count, err := nativeWrite(file, contents)
				if err != nil || retained != "" {
					return count, err
				}
				if change == "replacement" {
					retained = filepath.Join(root, "retained")
					if err := os.Rename(parent, retained); err != nil {
						test.Fatal(err)
					}
					if err := os.Mkdir(parent, 0o700); err != nil {
						test.Fatal(err)
					}
					writePrivateFixture(test, filepath.Join(parent, "keep"), []byte("replacement"))
				} else {
					displaced := filepath.Join(root, "displaced")
					if err := os.Rename(ancestor, displaced); err != nil {
						test.Fatal(err)
					}
					makeSymlinkFixture(test, displaced, ancestor)
					retained = filepath.Join(displaced, "parent")
				}
				return count, nil
			}
			files := directoryFilesFixture()
			target := filepath.Join(parent, "snapshot")
			path, err := publisher.publish(context.Background(), target, files)
			if path != "" || err == nil || retained == "" {
				test.Fatalf("changed parent path accepted: %q, %v", path, err)
			}
			assertContents(test, filepath.Join(directoryStagingPath(test, retained), files[0].Name), files[0].Contents)
			assertDirectoryAbsent(test, target)
			if change == "replacement" {
				assertOnlyNames(test, parent, "keep")
				assertContents(test, filepath.Join(parent, "keep"), []byte("replacement"))
			}
		})
	}
}

func TestDirectoryPublicationLeavesUnknownStagingEntries(test *testing.T) {
	for _, kind := range []string{"file", "directory", "hardlink"} {
		test.Run(kind, func(test *testing.T) {
			publisher, target, files := directoryPublisherFixture(test)
			nativeWrite := publisher.operations.write
			var staging string
			publisher.operations.write = func(file *os.File, contents []byte) (int, error) {
				count, err := nativeWrite(file, contents)
				if err != nil || staging != "" {
					return count, err
				}
				staging = directoryStagingPath(test, filepath.Dir(target))
				unknown := filepath.Join(staging, "unknown")
				switch kind {
				case "directory":
					if err := os.Mkdir(unknown, 0o700); err != nil {
						test.Fatal(err)
					}
					writePrivateFixture(test, filepath.Join(unknown, "keep"), []byte("unknown"))
				case "hardlink":
					if err := os.Link(filepath.Join(staging, file.Name()), unknown); err != nil {
						test.Fatal(err)
					}
				default:
					writePrivateFixture(test, unknown, []byte("unknown"))
				}
				return count, nil
			}
			path, err := publisher.publish(context.Background(), target, files)
			if path != "" || err == nil || staging == "" {
				test.Fatalf("unknown staging entry accepted: %q, %v", path, err)
			}
			assertContents(test, filepath.Join(staging, files[0].Name), files[0].Contents)
			switch kind {
			case "directory":
				assertContents(test, filepath.Join(staging, "unknown", "keep"), []byte("unknown"))
			case "hardlink":
				assertContents(test, filepath.Join(staging, "unknown"), files[0].Contents)
			default:
				assertContents(test, filepath.Join(staging, "unknown"), []byte("unknown"))
			}
			assertDirectoryAbsent(test, target)
		})
	}
}

func TestDirectoryPublicationLeavesUnpinnedStaging(test *testing.T) {
	publisher, target, files := directoryPublisherFixture(test)
	nativeOpen := publisher.operations.openat
	failure := errors.New("staging open failed")
	publisher.operations.openat = func(descriptor int, name string, flags int, mode uint32) (*os.File, error) {
		if strings.HasPrefix(name, ".treeclear-directory-") {
			return nil, failure
		}
		return nativeOpen(descriptor, name, flags, mode)
	}
	path, err := publisher.publish(context.Background(), target, files)
	if path != "" || !errors.Is(err, failure) {
		test.Fatalf("failed staging open = %q, %v", path, err)
	}
	staging := directoryStagingPath(test, filepath.Dir(target))
	assertPrivateObject(test, staging, true)
	assertOnlyNames(test, staging)
	assertDirectoryAbsent(test, target)
}

func TestDirectoryPublicationCleanupErrorsRetainPrivateStaging(test *testing.T) {
	for _, boundary := range []string{"file unlink", "directory unlink", "file close"} {
		test.Run(boundary, func(test *testing.T) {
			publisher, target, files := directoryPublisherFixture(test)
			readFailure, cleanupFailure := errors.New("read failed"), errors.New("cleanup failed")
			native := publisher.operations
			readFailed := false
			publisher.operations.read = func(file *os.File, contents []byte) (int, error) {
				count, err := native.read(file, contents)
				readFailed = true
				return count, errors.Join(err, readFailure)
			}
			publisher.operations.unlinkat = func(descriptor int, name string, flags int) error {
				if boundary == "file unlink" && flags == 0 || boundary == "directory unlink" && flags == unix.AT_REMOVEDIR {
					return cleanupFailure
				}
				return native.unlinkat(descriptor, name, flags)
			}
			publisher.operations.close = func(file *os.File) error {
				err := native.close(file)
				if readFailed && boundary == "file close" && file.Name() == files[0].Name {
					return errors.Join(err, cleanupFailure)
				}
				return err
			}
			path, err := publisher.publish(context.Background(), target, files)
			if path != "" || !readFailed || !errors.Is(err, readFailure) || !errors.Is(err, cleanupFailure) {
				test.Fatalf("cleanup error lost: %q, %v", path, err)
			}
			staging := directoryStagingPath(test, filepath.Dir(target))
			assertPrivateObject(test, staging, true)
			if boundary == "directory unlink" {
				assertOnlyNames(test, staging)
			} else {
				assertContents(test, filepath.Join(staging, files[0].Name), files[0].Contents)
			}
			assertDirectoryAbsent(test, target)
		})
	}
}

func TestDirectoryPublicationStagingNameCollisionsAreBounded(test *testing.T) {
	publisher, target, files := directoryPublisherFixture(test)
	attempts := 0
	publisher.operations.mkdirat = func(descriptor int, name string, mode uint32) error {
		attempts++
		return fs.ErrExist
	}
	path, err := publisher.publish(context.Background(), target, files)
	if path != "" || !errors.Is(err, fs.ErrExist) || attempts != 10 {
		test.Fatalf("unbounded staging collisions: %q, %v; attempts %d", path, err, attempts)
	}
	assertOnlyNames(test, filepath.Dir(target))
}
