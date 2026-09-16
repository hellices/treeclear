package fssecure

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func directoryFilesFixture() []PrivateFile {
	return []PrivateFile{
		{Name: "z-first.bin", Contents: []byte{0, 1, 2, 255, '\n', '\r'}},
		{Name: "empty", Contents: []byte{}},
		{Name: "nil", Contents: nil},
		{Name: "a-second.patch", Contents: bytes.Repeat([]byte("private\x00\xff"), 8192)},
		{Name: "manifest.json", Contents: []byte("{\"verified\":true}\n")},
	}
}

func TestPublishPrivateDirectoryPreservesBytesAndPrivacy(test *testing.T) {
	parent := privateDirectoryFixture(test)
	files := directoryFilesFixture()
	path, err := PublishPrivateDirectory(context.Background(), filepath.Join(parent, "snapshot"), files)
	if err != nil {
		test.Fatal(err)
	}
	canonicalParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		test.Fatal(err)
	}
	if path != filepath.Join(canonicalParent, "snapshot") || !filepath.IsAbs(path) {
		test.Fatalf("published path = %q", path)
	}
	assertDirectoryPublication(test, path, files)
	assertOnlyNames(test, parent, "snapshot")
}

func TestPublishPrivateDirectoryResolvesParentAliasBeforeTraversal(test *testing.T) {
	root := privateDirectoryFixture(test)
	physical := filepath.Join(root, "physical")
	nested := filepath.Join(physical, "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		test.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	makeSymlinkFixture(test, nested, alias)
	path, err := PublishPrivateDirectory(context.Background(), alias+"/../snapshot", directoryFilesFixture())
	if err != nil {
		test.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(physical)
	if err != nil || path != filepath.Join(canonical, "snapshot") {
		test.Fatalf("resolved publication = %q, %v", path, err)
	}
	assertOnlyNames(test, root, "alias", "physical")
}

func TestPublishPrivateDirectoryPreservesExistingTargets(test *testing.T) {
	for _, kind := range []string{"empty directory", "nonempty directory", "file", "link", "dangling link"} {
		test.Run(kind, func(test *testing.T) {
			parent := privateDirectoryFixture(test)
			target := filepath.Join(parent, "snapshot")
			switch kind {
			case "empty directory", "nonempty directory":
				if err := os.Mkdir(target, 0o755); err != nil {
					test.Fatal(err)
				}
				if kind == "nonempty directory" {
					writePrivateFixture(test, filepath.Join(target, "keep"), []byte("keep"))
				}
			case "file":
				writePrivateFixture(test, target, []byte("keep"))
			case "link":
				makeSymlinkFixture(test, test.TempDir(), target)
			case "dangling link":
				makeSymlinkFixture(test, filepath.Join(test.TempDir(), "missing"), target)
			}
			before, err := os.Lstat(target)
			if err != nil {
				test.Fatal(err)
			}
			path, err := PublishPrivateDirectory(context.Background(), target, directoryFilesFixture())
			if path != "" || !errors.Is(err, fs.ErrExist) {
				test.Fatalf("collision = %q, %v; want ErrExist", path, err)
			}
			after, err := os.Lstat(target)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
				test.Fatalf("existing target changed: %v", err)
			}
			if kind == "file" {
				assertContents(test, target, []byte("keep"))
			}
			if kind == "nonempty directory" {
				assertContents(test, filepath.Join(target, "keep"), []byte("keep"))
			}
			assertOnlyNames(test, parent, "snapshot")
		})
	}
}

func TestPublishPrivateDirectoryRequiresExistingPrivateParent(test *testing.T) {
	for _, kind := range []string{"missing", "file", "broad", "ACL"} {
		test.Run(kind, func(test *testing.T) {
			root := privateDirectoryFixture(test)
			parent := filepath.Join(root, "parent")
			if kind == "file" {
				writePrivateFixture(test, parent, []byte("keep"))
			} else if kind != "missing" {
				if err := os.Mkdir(parent, 0o700); err != nil {
					test.Fatal(err)
				}
				if kind == "broad" {
					if err := os.Chmod(parent, 0o755); err != nil {
						test.Fatal(err)
					}
				}
				if kind == "ACL" {
					if output, err := exec.Command("/bin/chmod", "+a", "everyone allow read,write,execute,file_inherit,directory_inherit", parent).CombinedOutput(); err != nil {
						test.Fatalf("add temporary ACL: %v\n%s", err, output)
					}
				}
			}
			var before string
			if kind != "missing" {
				before = darwinACLSnapshot(test, parent)
			}
			path, err := PublishPrivateDirectory(context.Background(), filepath.Join(parent, "snapshot"), directoryFilesFixture())
			if path != "" || err == nil {
				test.Fatalf("unsafe parent accepted: %q, %v", path, err)
			}
			if kind == "missing" {
				assertOnlyNames(test, root)
			} else {
				if after := darwinACLSnapshot(test, parent); after != before {
					test.Fatal("existing parent security changed")
				}
				if kind == "file" {
					assertContents(test, parent, []byte("keep"))
				} else {
					assertOnlyNames(test, parent)
				}
			}
		})
	}
}

func TestPublishPrivateDirectoryConcurrentCollision(test *testing.T) {
	parent := privateDirectoryFixture(test)
	target := filepath.Join(parent, "snapshot")
	var workers sync.WaitGroup
	results := make(chan error, 8)
	for number := range 8 {
		workers.Go(func() {
			files := []PrivateFile{{Name: "payload", Contents: []byte(fmt.Sprintf("writer-%d", number))}}
			path, err := PublishPrivateDirectory(context.Background(), target, files)
			if err != nil && path != "" || err == nil && path == "" {
				results <- fmt.Errorf("inconsistent publication result: %q, %v", path, err)
				return
			}
			results <- err
		})
	}
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, fs.ErrExist) {
			test.Errorf("unexpected publication failure: %v", err)
		}
	}
	if successes != 1 {
		test.Fatalf("successful publications = %d; want 1", successes)
	}
	assertOnlyNames(test, parent, "snapshot")
	assertOnlyNames(test, target, "payload")
}

func assertDirectoryPublication(test *testing.T, path string, files []PrivateFile) {
	test.Helper()
	assertPrivateObject(test, path, true)
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != len(files) {
		test.Fatalf("published entries = %v, %v", entries, err)
	}
	for _, file := range files {
		child := filepath.Join(path, file.Name)
		assertContents(test, child, file.Contents)
		assertPrivateObject(test, child, false)
	}
}
