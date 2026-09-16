package fssecure

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectoryPublicationRejectsEarlierPayloadChangedDuringManifestWrite(test *testing.T) {
	for _, change := range []string{"bytes", "replacement", "symlink"} {
		test.Run(change, func(test *testing.T) {
			publisher, target, files := directoryPublisherFixture(test)
			nativeWrite := publisher.operations.write
			outside := filepath.Join(privateDirectoryFixture(test), "outside")
			writePrivateFixture(test, outside, []byte("outside"))
			var staging, retained string
			publisher.operations.write = func(file *os.File, contents []byte) (int, error) {
				count, err := nativeWrite(file, contents)
				if err != nil || file.Name() != "manifest.json" {
					return count, err
				}
				staging = directoryStagingPath(test, filepath.Dir(target))
				payload := filepath.Join(staging, files[0].Name)
				if change != "bytes" {
					retained = filepath.Join(filepath.Dir(outside), "retained")
					if err := os.Rename(payload, retained); err != nil {
						test.Fatal(err)
					}
				}
				if change == "symlink" {
					makeSymlinkFixture(test, outside, payload)
				} else {
					writePrivateFixture(test, payload, []byte("changed private payload"))
				}
				return count, nil
			}
			path, err := publisher.publish(context.Background(), target, files)
			if staging == "" || path != "" || err == nil {
				test.Fatalf("changed earlier payload accepted: %q, %v; staging %q", path, err, staging)
			}
			assertDirectoryAbsent(test, target)
			assertPrivateObject(test, staging, true)
			for _, file := range files[1:] {
				assertContents(test, filepath.Join(staging, file.Name), file.Contents)
			}
			if change == "symlink" {
				link, err := os.Readlink(filepath.Join(staging, files[0].Name))
				if err != nil || link != outside {
					test.Fatalf("uncertain symlink changed during cleanup: %q, %v", link, err)
				}
			} else {
				assertContents(test, filepath.Join(staging, files[0].Name), []byte("changed private payload"))
			}
			if retained != "" {
				assertContents(test, retained, files[0].Contents)
			}
			assertContents(test, outside, []byte("outside"))
		})
	}
}

func TestDirectoryPublicationReadBackRunsAfterManifestBeforeSync(test *testing.T) {
	publisher, target, files := directoryPublisherFixture(test)
	nativeWrite, nativeRead, nativeSync := publisher.operations.write, publisher.operations.read, publisher.operations.sync
	failure := errors.New("complete-set read-back failure")
	manifestWritten, readAfterManifest, stagingSynced := false, false, false
	publisher.operations.write = func(file *os.File, contents []byte) (int, error) {
		count, err := nativeWrite(file, contents)
		if file.Name() == "manifest.json" {
			manifestWritten = true
		}
		return count, err
	}
	publisher.operations.read = func(file *os.File, contents []byte) (int, error) {
		if manifestWritten && file.Name() == files[0].Name {
			readAfterManifest = true
			return 0, failure
		}
		return nativeRead(file, contents)
	}
	publisher.operations.sync = func(file *os.File) error {
		if strings.HasPrefix(file.Name(), ".treeclear-directory-") {
			stagingSynced = true
		}
		return nativeSync(file)
	}
	path, err := publisher.publish(context.Background(), target, files)
	if path != "" || !errors.Is(err, failure) || !manifestWritten || !readAfterManifest || stagingSynced {
		test.Fatalf("missing final read-back before sync: %q, %v; manifest %t, read %t, sync %t", path, err, manifestWritten, readAfterManifest, stagingSynced)
	}
	assertOnlyNames(test, filepath.Dir(target))
}

func TestDirectoryPublicationRetainsEagerDirectoryIdentity(test *testing.T) {
	for _, boundary := range []string{"parent open", "staging open"} {
		test.Run(boundary, func(test *testing.T) {
			root, err := filepath.EvalSymlinks(privateDirectoryFixture(test))
			if err != nil {
				test.Fatal(err)
			}
			parent := filepath.Join(root, "parent")
			if err := os.Mkdir(parent, 0o700); err != nil {
				test.Fatal(err)
			}
			publisher := directoryPublisher{operations: nativeDirectoryOperations()}
			nativeOpen := publisher.operations.openat
			var replacement, original string
			publisher.operations.openat = func(descriptor int, name string, flags int, mode uint32) (*os.File, error) {
				if replacement == "" && (boundary == "parent open" && name == parent || boundary == "staging open" && strings.HasPrefix(name, ".treeclear-directory-")) {
					replacement = name
					if !filepath.IsAbs(replacement) {
						replacement = filepath.Join(parent, name)
					}
					original = filepath.Join(root, "original")
					if err := os.Rename(replacement, original); err != nil {
						test.Fatal(err)
					}
					if err := os.Mkdir(replacement, 0o700); err != nil {
						test.Fatal(err)
					}
					writePrivateFixture(test, filepath.Join(replacement, "keep"), []byte("keep replacement"))
				}
				return nativeOpen(descriptor, name, flags, mode)
			}
			path, err := publisher.publish(context.Background(), filepath.Join(parent, "snapshot"), directoryFilesFixture())
			if path != "" || err == nil || replacement == "" {
				test.Fatalf("directory replacement accepted: %q, %v", path, err)
			}
			assertContents(test, filepath.Join(replacement, "keep"), []byte("keep replacement"))
			assertOnlyNames(test, replacement, "keep")
			assertOnlyNames(test, original)
		})
	}
}

func TestDirectoryPublicationPreservesReplacedStaging(test *testing.T) {
	for _, replacement := range []string{"directory", "symlink"} {
		test.Run(replacement, func(test *testing.T) {
			publisher, target, files := directoryPublisherFixture(test)
			parent := filepath.Dir(target)
			outside := privateDirectoryFixture(test)
			writePrivateFixture(test, filepath.Join(outside, "keep"), []byte("outside"))
			nativeWrite := publisher.operations.write
			var staging string
			displaced := filepath.Join(parent, "displaced")
			publisher.operations.write = func(file *os.File, contents []byte) (int, error) {
				count, err := nativeWrite(file, contents)
				if staging != "" || err != nil {
					return count, err
				}
				staging = directoryStagingPath(test, parent)
				if err := os.Rename(staging, displaced); err != nil {
					test.Fatal(err)
				}
				if replacement == "symlink" {
					makeSymlinkFixture(test, outside, staging)
				} else {
					if err := os.Mkdir(staging, 0o700); err != nil {
						test.Fatal(err)
					}
					writePrivateFixture(test, filepath.Join(staging, "keep"), []byte("replacement"))
				}
				return count, nil
			}
			path, err := publisher.publish(context.Background(), target, files)
			if path != "" || err == nil || staging == "" {
				test.Fatalf("replaced staging accepted: %q, %v", path, err)
			}
			assertDirectoryAbsent(test, target)
			assertContents(test, filepath.Join(displaced, files[0].Name), files[0].Contents)
			assertContents(test, filepath.Join(outside, "keep"), []byte("outside"))
			if replacement == "directory" {
				assertOnlyNames(test, staging, "keep")
				assertContents(test, filepath.Join(staging, "keep"), []byte("replacement"))
			} else if destination, err := os.Readlink(staging); err != nil || destination != outside {
				test.Fatalf("replacement link changed: %q, %v", destination, err)
			}
		})
	}
}

func TestDirectoryPublicationPreservesUnsafeFileOnFailedWrite(test *testing.T) {
	publisher, target, files := directoryPublisherFixture(test)
	nativeWrite := publisher.operations.write
	failure := errors.New("write failure after ACL change")
	var staging, before string
	publisher.operations.write = func(file *os.File, contents []byte) (int, error) {
		count, err := nativeWrite(file, contents)
		if err != nil {
			return count, err
		}
		staging = directoryStagingPath(test, filepath.Dir(target))
		payload := filepath.Join(staging, file.Name())
		addDirectoryFixtureACL(test, payload)
		before = darwinACLSnapshot(test, payload)
		return count, failure
	}
	path, err := publisher.publish(context.Background(), target, files)
	if path != "" || !errors.Is(err, failure) || staging == "" {
		test.Fatalf("failed unsafe write = %q, %v", path, err)
	}
	payload := filepath.Join(staging, files[0].Name)
	assertContents(test, payload, files[0].Contents)
	if after := darwinACLSnapshot(test, payload); after != before {
		test.Fatal("cleanup modified an unsafe ACL")
	}
	assertDirectoryAbsent(test, target)
}

func addDirectoryFixtureACL(test *testing.T, path string) {
	test.Helper()
	if output, err := exec.Command("/bin/chmod", "+a", "everyone allow read", path).CombinedOutput(); err != nil {
		test.Fatalf("add ACL to temporary object: %v\n%s", err, output)
	}
}

func assertDirectoryAbsent(test *testing.T, path string) {
	test.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		test.Fatalf("unexpected path %q: %v", path, err)
	}
}
