package fssecure

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDarwinPrivateFilesRejectExtendedACLs(test *testing.T) {
	path := filepath.Join(privateDirectoryFixture(test), "data")
	writePrivateFixture(test, path, []byte("private"))
	if contents, err := ReadPrivateFile(path, 32); err != nil || string(contents) != "private" {
		test.Fatalf("file without ACL: %q, %v", contents, err)
	}
	if output, err := exec.Command("/bin/chmod", "+a", "everyone allow read", path).CombinedOutput(); err != nil {
		test.Fatalf("add ACL to temporary fixture: %v\n%s", err, output)
	}
	before := darwinACLSnapshot(test, path)
	if contents, err := ReadPrivateFile(path, 32); err == nil || contents != nil {
		test.Fatalf("ReadPrivateFile exposed ACL-readable bytes: %q, %v", contents, err)
	}
	if after := darwinACLSnapshot(test, path); after != before {
		test.Fatal("read modified an existing ACL")
	}
	assertContents(test, path, []byte("private"))
}

func TestDarwinPrivateDirectoryRejectsInheritedACLs(test *testing.T) {
	parent := privateDirectoryFixture(test)
	if output, err := exec.Command("/bin/chmod", "+a", "everyone allow read,write,execute,file_inherit,directory_inherit", parent).CombinedOutput(); err != nil {
		test.Fatalf("add inheritable ACL to temporary fixture: %v\n%s", err, output)
	}
	before := darwinACLSnapshot(test, parent)
	if err := EnsurePrivateDirectory(parent); err == nil {
		test.Fatal("accepted an extended ACL as mode-only privacy")
	}
	if err := WritePrivateFile(filepath.Join(parent, "data"), []byte("private")); err == nil {
		test.Fatal("published below an unsafe ACL")
	}
	if after := darwinACLSnapshot(test, parent); after != before {
		test.Fatal("modified an unsupported ACL")
	}
	assertOnlyNames(test, parent)
}

func TestDarwinPrivateDirectoryAcceptsSystemTemporaryAlias(test *testing.T) {
	parent := test.TempDir()
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil {
		test.Fatal(err)
	}
	path := filepath.Join(parent, "state")
	if err := EnsurePrivateDirectory(path); err != nil {
		test.Fatalf("EnsurePrivateDirectory under system temporary path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(resolved, "state")); err != nil {
		test.Fatal(err)
	}
	assertPrivateObject(test, path, true)
}

func darwinACLSnapshot(test *testing.T, path string) string {
	test.Helper()
	output, err := exec.Command("/bin/ls", "-lde", path).CombinedOutput()
	if err != nil {
		test.Fatalf("inspect ACL on temporary fixture: %v\n%s", err, output)
	}
	return string(output)
}
