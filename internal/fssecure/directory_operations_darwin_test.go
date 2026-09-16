package fssecure

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

	"golang.org/x/sys/unix"
)

func TestDirectoryPublicationUsesOrderedDescriptorOperations(test *testing.T) {
	publisher, target, files := directoryPublisherFixture(test)
	native := publisher.operations
	parentDescriptor, stagingDescriptor := -1, -1
	var created, verified []string
	writeHandles := make(map[*os.File]bool)
	readCounts := make(map[*os.File]int)
	expectedSizes := make(map[string]int)
	for _, file := range files {
		expectedSizes[file.Name] = len(file.Contents)
	}
	stagingSynced, parentSynced, renamed := false, false, false
	publisher.operations.mkdirat = func(descriptor int, name string, mode uint32) error {
		if descriptor != parentDescriptor || mode != 0o700 || filepath.Base(name) != name {
			test.Fatalf("mkdir is not private and parent-relative: %d, %q, %#o", descriptor, name, mode)
		}
		return native.mkdirat(descriptor, name, mode)
	}
	publisher.operations.openat = func(descriptor int, name string, flags int, mode uint32) (*os.File, error) {
		if flags&(unix.O_NOFOLLOW|unix.O_NOFOLLOW_ANY) == 0 || flags&unix.O_CLOEXEC == 0 {
			test.Fatalf("open lacks nofollow/CLOEXEC: %q, %#x", name, flags)
		}
		if flags&unix.O_CREAT != 0 {
			if descriptor != stagingDescriptor || flags&unix.O_EXCL == 0 || mode != 0o600 {
				test.Fatalf("child create is not exclusive, private and staging-relative: %d, %q, %#x, %#o", descriptor, name, flags, mode)
			}
			if len(created) > 0 && !containsDirectoryName(verified, created[len(created)-1]) {
				test.Fatal("created the next input before closing the previous read-back")
			}
			created = append(created, name)
		}
		file, err := native.openat(descriptor, name, flags, mode)
		if err != nil {
			return file, err
		}
		if name == filepath.Dir(target) {
			parentDescriptor = int(file.Fd())
		} else if strings.HasPrefix(name, ".treeclear-directory-") {
			stagingDescriptor = int(file.Fd())
		} else if flags&unix.O_DIRECTORY == 0 {
			if descriptor != stagingDescriptor {
				test.Fatalf("child open is not staging-relative: %q", name)
			}
			writeHandles[file] = flags&unix.O_CREAT != 0
			if flags&unix.O_CREAT == 0 {
				readCounts[file] = 0
			}
		}
		return file, nil
	}
	publisher.operations.read = func(file *os.File, buffer []byte) (int, error) {
		if readCounts[file]+len(buffer) > expectedSizes[file.Name()]+1 {
			test.Fatalf("read-back was not bounded to expected bytes plus one: %q", file.Name())
		}
		count, err := native.read(file, buffer)
		readCounts[file] += count
		return count, err
	}
	publisher.operations.close = func(file *os.File) error {
		if writer, known := writeHandles[file]; known && !writer {
			if readCounts[file] != expectedSizes[file.Name()] {
				test.Fatalf("read-back was not exact before close: %q", file.Name())
			}
			verified = append(verified, file.Name())
		}
		return native.close(file)
	}
	publisher.operations.sync = func(file *os.File) error {
		if int(file.Fd()) == stagingDescriptor {
			stagingSynced = true
		} else if int(file.Fd()) == parentDescriptor {
			parentSynced = true
		}
		return native.sync(file)
	}
	publisher.operations.renameat = func(sourceDescriptor int, source string, destinationDescriptor int, destination string, flags uint32) error {
		if sourceDescriptor != parentDescriptor || destinationDescriptor != parentDescriptor || flags&unix.RENAME_EXCL == 0 || destination != "snapshot" || !stagingSynced || !parentSynced {
			test.Fatalf("publication is not synced, parent-relative and exclusive: %d, %q, %d, %q, %#x", sourceDescriptor, source, destinationDescriptor, destination, flags)
		}
		if len(created) != len(files) || created[len(created)-1] != "manifest.json" {
			test.Fatalf("manifest was not written last: %q", created)
		}
		renamed = true
		return native.renameat(sourceDescriptor, source, destinationDescriptor, destination, flags)
	}
	path, err := publisher.publish(context.Background(), target, files)
	if err != nil || path != target || !renamed {
		test.Fatalf("publication = %q, %v; renamed %t", path, err, renamed)
	}
	var expected []string
	for _, file := range files {
		expected = append(expected, file.Name)
	}
	if !reflect.DeepEqual(created, expected) {
		test.Fatalf("creation order = %q; want %q", created, expected)
	}
	assertDirectoryPublication(test, target, files)
}

func TestDirectoryPublicationOperationFailures(test *testing.T) {
	for _, boundary := range []string{"mkdir", "write", "short write", "file sync", "write close", "read open", "read", "read mismatch", "read truncated", "read excess", "read close", "staging sync", "parent pre-sync", "rename", "parent post-sync", "staging close", "parent close"} {
		test.Run(boundary, func(test *testing.T) {
			publisher, target, files := directoryPublisherFixture(test)
			failure := errors.New("injected " + boundary)
			injected, published := false, false
			injectDirectoryBoundary(test, &publisher.operations, target, boundary, func() error {
				injected = true
				return failure
			}, &published)
			path, err := publisher.publish(context.Background(), target, files)
			if !injected || path != "" || err == nil {
				test.Fatalf("failure not propagated: injected %t, path %q, error %v", injected, path, err)
			}
			switch boundary {
			case "short write":
				if !errors.Is(err, io.ErrShortWrite) {
					test.Fatalf("short write error = %v", err)
				}
			case "read mismatch", "read truncated", "read excess":
				if !errors.Is(err, fs.ErrInvalid) {
					test.Fatalf("inexact read-back error = %v", err)
				}
			default:
				if !errors.Is(err, failure) {
					test.Fatalf("lost original failure: %v", err)
				}
			}
			if published {
				assertDirectoryPublication(test, target, files)
				assertOnlyNames(test, filepath.Dir(target), "snapshot")
			} else if boundary == "write" || boundary == "short write" || boundary == "file sync" {
				expected := files[0]
				if boundary == "write" {
					expected.Contents = nil
				} else if boundary == "short write" {
					expected.Contents = expected.Contents[:len(expected.Contents)-1]
				}
				assertUnsealedDirectoryPublication(test, target, expected)
			} else {
				assertOnlyNames(test, filepath.Dir(target))
			}
		})
	}
}

func TestDirectoryPublicationCancellationBoundaries(test *testing.T) {
	for _, boundary := range []string{"parent open", "mkdir", "staging open", "file open", "write", "file sync", "write close", "read open", "read", "read close", "staging sync", "parent pre-sync", "rename", "parent post-sync", "staging close", "parent close"} {
		test.Run(boundary, func(test *testing.T) {
			publisher, target, files := directoryPublisherFixture(test)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			injected, published := false, false
			injectDirectoryBoundary(test, &publisher.operations, target, boundary, func() error {
				injected = true
				cancel()
				return nil
			}, &published)
			path, err := publisher.publish(ctx, target, files)
			if !injected || path != "" || !errors.Is(err, context.Canceled) {
				test.Fatalf("cancellation = %q, %v; injected %t", path, err, injected)
			}
			if published {
				assertDirectoryPublication(test, target, files)
				assertOnlyNames(test, filepath.Dir(target), "snapshot")
			} else if boundary == "file open" || boundary == "write" || boundary == "file sync" {
				expected := files[0]
				if boundary == "file open" {
					expected.Contents = nil
				}
				assertUnsealedDirectoryPublication(test, target, expected)
			} else {
				assertOnlyNames(test, filepath.Dir(target))
			}
		})
	}
}

func assertUnsealedDirectoryPublication(test *testing.T, target string, expected PrivateFile) {
	test.Helper()
	staging := directoryStagingPath(test, filepath.Dir(target))
	assertOnlyNames(test, filepath.Dir(target), filepath.Base(staging))
	assertPrivateObject(test, staging, true)
	assertOnlyNames(test, staging, expected.Name)
	assertPrivateObject(test, filepath.Join(staging, expected.Name), false)
	assertContents(test, filepath.Join(staging, expected.Name), expected.Contents)
	assertDirectoryAbsent(test, target)
}

func injectDirectoryBoundary(test *testing.T, operations *directoryOperations, target, boundary string, inject func() error, published *bool) {
	test.Helper()
	native := *operations
	triggered := false
	trigger := func(current string) error {
		if current != boundary || triggered {
			return nil
		}
		triggered = true
		return inject()
	}
	roles := make(map[*os.File]string)
	operations.openat = func(descriptor int, name string, flags int, mode uint32) (*os.File, error) {
		role := "read"
		if name == filepath.Dir(target) {
			role = "parent"
		} else if name == "." {
			role = "listing"
		} else if flags&unix.O_DIRECTORY != 0 {
			role = "staging"
		} else if flags&unix.O_CREAT != 0 {
			role = "file"
		}
		if role == "read" {
			if err := trigger("read open"); err != nil {
				return nil, err
			}
		}
		file, err := native.openat(descriptor, name, flags, mode)
		if err == nil {
			roles[file] = role
			if injected := trigger(role + " open"); injected != nil {
				return nil, errors.Join(injected, native.close(file))
			}
		}
		return file, err
	}
	operations.mkdirat = func(descriptor int, name string, mode uint32) error {
		if err := trigger("mkdir"); err != nil {
			return err
		}
		return native.mkdirat(descriptor, name, mode)
	}
	operations.write = func(file *os.File, contents []byte) (int, error) {
		if err := trigger("write"); err != nil {
			return 0, err
		}
		if boundary == "short write" && !triggered {
			_ = trigger("short write")
			return native.write(file, contents[:len(contents)-1])
		}
		return native.write(file, contents)
	}
	operations.read = func(file *os.File, buffer []byte) (int, error) {
		if err := trigger("read"); err != nil {
			return 0, err
		}
		if boundary == "read truncated" && !triggered {
			_ = trigger(boundary)
			return 0, io.EOF
		}
		if boundary == "read excess" {
			_ = trigger(boundary)
			clear(buffer)
			return len(buffer), nil
		}
		count, err := native.read(file, buffer)
		if boundary == "read mismatch" && !triggered && count > 0 {
			_ = trigger(boundary)
			buffer[0] ^= 255
		}
		return count, err
	}
	operations.sync = func(file *os.File) error {
		current := roles[file] + " sync"
		if roles[file] == "parent" {
			current = "parent pre-sync"
			if *published {
				current = "parent post-sync"
			}
		}
		if err := trigger(current); err != nil {
			return err
		}
		return native.sync(file)
	}
	operations.close = func(file *os.File) error {
		err := native.close(file)
		current := roles[file] + " close"
		if roles[file] == "file" {
			current = "write close"
		}
		if current == "staging close" && !*published {
			return err
		}
		return errors.Join(err, trigger(current))
	}
	operations.renameat = func(sourceDescriptor int, source string, destinationDescriptor int, destination string, flags uint32) error {
		if err := trigger("rename"); err != nil {
			return err
		}
		err := native.renameat(sourceDescriptor, source, destinationDescriptor, destination, flags)
		if err == nil {
			*published = true
		}
		return err
	}
}

func TestDirectoryPublicationRenameCollisionPreservesEmptyDirectory(test *testing.T) {
	publisher, target, files := directoryPublisherFixture(test)
	native := publisher.operations.renameat
	var before fs.FileInfo
	publisher.operations.renameat = func(sourceDescriptor int, source string, destinationDescriptor int, destination string, flags uint32) error {
		if err := os.Mkdir(target, 0o700); err != nil {
			test.Fatal(err)
		}
		var err error
		before, err = os.Lstat(target)
		if err != nil {
			test.Fatal(err)
		}
		return native(sourceDescriptor, source, destinationDescriptor, destination, flags)
	}
	path, err := publisher.publish(context.Background(), target, files)
	if path != "" || !errors.Is(err, fs.ErrExist) || before == nil {
		test.Fatalf("racing collision = %q, %v", path, err)
	}
	after, err := os.Lstat(target)
	if err != nil || !os.SameFile(before, after) {
		test.Fatalf("racing empty directory was replaced: %v", err)
	}
	assertOnlyNames(test, target)
	assertOnlyNames(test, filepath.Dir(target), "snapshot")
}

func directoryPublisherFixture(test *testing.T) (*directoryPublisher, string, []PrivateFile) {
	test.Helper()
	parent, err := filepath.EvalSymlinks(privateDirectoryFixture(test))
	if err != nil {
		test.Fatal(err)
	}
	return &directoryPublisher{operations: nativeDirectoryOperations()}, filepath.Join(parent, "snapshot"), directoryFilesFixture()
}

func containsDirectoryName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func directoryStagingPath(test *testing.T, parent string) string {
	test.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		test.Fatal(err)
	}
	var staging string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".treeclear-directory-") {
			if staging != "" {
				test.Fatal("multiple staging directories")
			}
			staging = filepath.Join(parent, entry.Name())
		}
	}
	if staging == "" {
		test.Fatal(fmt.Errorf("no staging directory in %q", parent))
	}
	return staging
}
