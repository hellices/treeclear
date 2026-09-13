package fssecure

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritePrivateExportPreservesDarwinParentACL(test *testing.T) {
	parent := test.TempDir()
	makeBroadFixture(test, parent, true)
	if output, err := exec.CommandContext(test.Context(), "/bin/chmod", "+a", "everyone allow read,write,execute,file_inherit,directory_inherit", parent).CombinedOutput(); err != nil {
		test.Fatalf("set ACL on temporary export parent: %v\n%s", err, output)
	}
	beforeSecurity := securitySnapshot(test, parent)
	_, beforeACL, found := strings.Cut(darwinACLSnapshot(test, parent), "\n")
	if !found || beforeACL == "" {
		test.Fatal("fixture did not contain an ACL")
	}
	staging := filepath.Join(test.TempDir(), "private")
	destination := filepath.Join(parent, "report")
	if err := WritePrivateExport(staging, destination, []byte("private export")); err != nil {
		test.Fatal(err)
	}
	_, afterACL, found := strings.Cut(darwinACLSnapshot(test, parent), "\n")
	if !found || afterACL != beforeACL || securitySnapshot(test, parent) != beforeSecurity {
		test.Fatal("export changed existing parent permissions or ACL")
	}
	contents, err := ReadPrivateFile(destination, 1024)
	if err != nil || string(contents) != "private export" {
		test.Fatalf("export inherited unsafe parent ACL: %q, %v", contents, err)
	}
	assertOnlyNames(test, staging)
}
