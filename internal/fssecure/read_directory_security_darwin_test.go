package fssecure

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReadPrivateDirectoryRequiresPrivateDirectories(test *testing.T) {
	for _, location := range []string{"parent", "bundle"} {
		for _, kind := range []string{"broad permissions", "extended ACL"} {
			test.Run(location+"/"+kind, func(test *testing.T) {
				path := readDirectoryFixture(test, []PrivateFile{{Name: "payload", Contents: []byte("keep")}})
				target := path
				if location == "parent" {
					target = filepath.Dir(path)
				}
				if kind == "broad permissions" {
					if err := os.Chmod(target, 0o755); err != nil {
						test.Fatal(err)
					}
				} else {
					addDirectoryFixtureACL(test, target)
				}
				before := readDirectoryFixtureState(test, path)
				files, err := ReadPrivateDirectory(context.Background(), path, []PrivateFileLimit{{Name: "payload", MaximumBytes: 4}}, 4)
				if files != nil || !errors.Is(err, fs.ErrPermission) {
					test.Fatalf("unsafe %s = %v, %v; want nil, ErrPermission", location, files, err)
				}
				assertReadDirectoryUnchanged(test, path, before)
				assertContents(test, filepath.Join(path, "payload"), []byte("keep"))
			})
		}
	}
}

func TestReadPrivateDirectoryRejectsUnsafeChildren(test *testing.T) {
	for _, kind := range []string{"symlink", "dangling symlink", "directory", "fifo", "hardlink", "broad permissions", "read-only permissions", "executable", "extended ACL"} {
		test.Run(kind, func(test *testing.T) {
			path := readDirectoryFixture(test, []PrivateFile{{Name: "payload", Contents: []byte("keep")}})
			child := filepath.Join(path, "payload")
			switch kind {
			case "symlink", "dangling symlink", "directory", "fifo":
				if err := os.Remove(child); err != nil {
					test.Fatal(err)
				}
				outside := filepath.Join(filepath.Dir(path), "outside")
				switch kind {
				case "symlink":
					writePrivateFixture(test, outside, []byte("outside"))
					makeSymlinkFixture(test, outside, child)
				case "dangling symlink":
					makeSymlinkFixture(test, outside, child)
				case "directory":
					if err := os.Mkdir(child, 0o700); err != nil {
						test.Fatal(err)
					}
				case "fifo":
					if err := unix.Mkfifo(child, 0o600); err != nil {
						test.Fatal(err)
					}
				}
			case "hardlink":
				if err := os.Link(child, filepath.Join(filepath.Dir(path), "outside")); err != nil {
					test.Fatal(err)
				}
			case "broad permissions", "read-only permissions", "executable":
				mode := os.FileMode(0o644)
				if kind == "read-only permissions" {
					mode = 0o400
				} else if kind == "executable" {
					mode = 0o700
				}
				if err := os.Chmod(child, mode); err != nil {
					test.Fatal(err)
				}
			case "extended ACL":
				addDirectoryFixtureACL(test, child)
			}
			before := readDirectoryFixtureState(test, path)
			files, err := ReadPrivateDirectory(context.Background(), path, []PrivateFileLimit{{Name: "payload", MaximumBytes: 32}}, 32)
			if files != nil || err == nil || errors.Is(err, errors.ErrUnsupported) {
				test.Fatalf("unsafe child = %v, %v; want nil and native refusal", files, err)
			}
			assertReadDirectoryUnchanged(test, path, before)
			if kind == "symlink" || kind == "hardlink" {
				want := "outside"
				if kind == "hardlink" {
					want = "keep"
				}
				assertContents(test, filepath.Join(filepath.Dir(path), "outside"), []byte(want))
			}
		})
	}
}

func TestReadPrivateDirectoryRequiresExactEntries(test *testing.T) {
	for _, kind := range []string{"missing", "extra file", "extra directory", "different case"} {
		test.Run(kind, func(test *testing.T) {
			path := readDirectoryFixture(test, []PrivateFile{{Name: "payload", Contents: []byte("keep")}, {Name: "empty"}})
			limits := []PrivateFileLimit{{Name: "payload", MaximumBytes: 8}, {Name: "empty", MaximumBytes: 8}}
			switch kind {
			case "missing":
				if err := os.Remove(filepath.Join(path, "empty")); err != nil {
					test.Fatal(err)
				}
			case "extra file":
				writePrivateFixture(test, filepath.Join(path, "extra"), []byte("preserve"))
			case "extra directory":
				if err := os.Mkdir(filepath.Join(path, "extra"), 0o700); err != nil {
					test.Fatal(err)
				}
			case "different case":
				limits[0].Name = "PAYLOAD"
			}
			before := readDirectoryFixtureState(test, path)
			files, err := ReadPrivateDirectory(context.Background(), path, limits, 16)
			if files != nil || !errors.Is(err, fs.ErrInvalid) {
				test.Fatalf("inexact entries = %v, %v; want nil, ErrInvalid", files, err)
			}
			assertReadDirectoryUnchanged(test, path, before)
			assertContents(test, filepath.Join(path, "payload"), []byte("keep"))
		})
	}
}

func TestReadPrivateDirectoryNeverCreatesOrRepairsStorage(test *testing.T) {
	parent := privateDirectoryFixture(test)
	limits := []PrivateFileLimit{{Name: "payload", MaximumBytes: 8}}
	for _, path := range []string{filepath.Join(parent, "absent"), filepath.Join(parent, "absent", "snapshot")} {
		files, err := ReadPrivateDirectory(context.Background(), path, limits, 8)
		if files != nil || !errors.Is(err, fs.ErrNotExist) {
			test.Fatalf("missing storage = %v, %v; want nil, ErrNotExist", files, err)
		}
	}
	assertOnlyNames(test, parent)
	path := filepath.Join(parent, "file")
	writePrivateFixture(test, path, []byte("preserve"))
	files, err := ReadPrivateDirectory(context.Background(), path, limits, 8)
	if files != nil || err == nil || errors.Is(err, errors.ErrUnsupported) {
		test.Fatalf("file as bundle = %v, %v; want native refusal", files, err)
	}
	assertContents(test, path, []byte("preserve"))
	assertOnlyNames(test, parent, "file")
}

func TestReadPrivateDirectoryResolvesOnlyParentAliases(test *testing.T) {
	want := []PrivateFile{{Name: "payload", Contents: []byte("keep")}}
	path := readDirectoryFixture(test, want)
	aliasRoot := privateDirectoryFixture(test)
	alias := filepath.Join(aliasRoot, "parent-alias")
	makeSymlinkFixture(test, filepath.Dir(path), alias)
	limits := []PrivateFileLimit{{Name: "payload", MaximumBytes: 4}}
	files, err := ReadPrivateDirectory(context.Background(), filepath.Join(alias, "snapshot"), limits, 4)
	if err != nil {
		test.Fatal(err)
	}
	assertReadDirectoryFiles(test, files, want)
	bundleAlias := filepath.Join(filepath.Dir(path), "bundle-alias")
	makeSymlinkFixture(test, path, bundleAlias)
	files, err = ReadPrivateDirectory(context.Background(), bundleAlias, limits, 4)
	if files != nil || err == nil {
		test.Fatalf("bundle symlink = %v, %v; want refusal", files, err)
	}
	assertContents(test, filepath.Join(path, "payload"), want[0].Contents)
}

func TestReadPrivateDirectoryRejectsOversizedSparseFile(test *testing.T) {
	path := readDirectoryFixture(test, []PrivateFile{{Name: "payload"}})
	if err := os.Truncate(filepath.Join(path, "payload"), 1<<33); err != nil {
		test.Fatal(err)
	}
	before := readDirectoryFixtureState(test, path)
	files, err := ReadPrivateDirectory(context.Background(), path, []PrivateFileLimit{{Name: "payload", MaximumBytes: 64}}, 64)
	if files != nil || !errors.Is(err, ErrPrivateDirectoryLimit) {
		test.Fatalf("sparse overflow = %v, %v; want nil, ErrPrivateDirectoryLimit", files, err)
	}
	assertReadDirectoryUnchanged(test, path, before)
}

func TestReadPrivateDirectoryPublicationRoundTrip(test *testing.T) {
	want := directoryFilesFixture()
	path, err := PublishPrivateDirectory(context.Background(), filepath.Join(privateDirectoryFixture(test), "snapshot"), want)
	if err != nil {
		test.Fatal(err)
	}
	limits := make([]PrivateFileLimit, len(want))
	for index, file := range want {
		limits[index] = PrivateFileLimit{Name: file.Name, MaximumBytes: 1 << 20}
	}
	before := readDirectoryFixtureState(test, path)
	actual, err := ReadPrivateDirectory(context.Background(), path, limits, 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	assertReadDirectoryFiles(test, actual, want)
	assertReadDirectoryUnchanged(test, path, before)
}
