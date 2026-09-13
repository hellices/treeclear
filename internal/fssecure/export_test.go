//go:build darwin || linux || windows

package fssecure

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWritePrivateExportPreservesParentSecurity(test *testing.T) {
	parent := test.TempDir()
	makeBroadFixture(test, parent, true)
	before := securitySnapshot(test, parent)
	staging := filepath.Join(test.TempDir(), "private")
	destination := filepath.Join(parent, "report")
	contents := []byte("private canonical bytes\x00\n")
	if err := WritePrivateExport(staging, destination, contents); err != nil {
		test.Fatal(err)
	}
	assertPrivateObject(test, destination, false)
	assertContents(test, destination, contents)
	if after := securitySnapshot(test, parent); after != before {
		test.Fatalf("export changed parent security: before %q, after %q", before, after)
	}
	entries, err := os.ReadDir(staging)
	if err != nil || len(entries) != 0 {
		test.Fatalf("export staging leftovers = %v, %v", entries, err)
	}
}

func TestWritePrivateExportIsAnIndependentCopy(test *testing.T) {
	staging := filepath.Join(test.TempDir(), "private")
	source := filepath.Join(staging, "original.json")
	contents := []byte("unchanged canonical plan")
	if err := WritePrivateFile(source, contents); err != nil {
		test.Fatal(err)
	}
	destination := filepath.Join(test.TempDir(), "missing", "nested", "report")
	if err := WritePrivateExport(staging, destination, contents); err != nil {
		test.Fatal(err)
	}
	assertPrivateObject(test, filepath.Dir(destination), true)
	sourceInfo, err := os.Stat(source)
	if err != nil {
		test.Fatal(err)
	}
	destinationInfo, err := os.Stat(destination)
	if err != nil || os.SameFile(sourceInfo, destinationInfo) {
		test.Fatalf("export shares canonical inode: %v", err)
	}
	if err := os.WriteFile(destination, []byte("edited export"), 0o600); err != nil {
		test.Fatal(err)
	}
	assertContents(test, source, contents)
}

func TestWritePrivateExportNeverReplacesExistingObjects(test *testing.T) {
	for _, kind := range []string{"file", "directory", "symlink"} {
		test.Run(kind, func(test *testing.T) {
			parent := test.TempDir()
			destination := filepath.Join(parent, "existing")
			switch kind {
			case "file":
				writePrivateFixture(test, destination, []byte("original"))
			case "directory":
				if err := os.Mkdir(destination, 0o700); err != nil {
					test.Fatal(err)
				}
			case "symlink":
				makeSymlinkFixture(test, filepath.Join(parent, "missing-target"), destination)
			}
			staging := filepath.Join(parent, "not-created")
			err := WritePrivateExport(staging, destination, []byte("replacement"))
			if !errors.Is(err, fs.ErrExist) {
				test.Fatalf("export over %s = %v", kind, err)
			}
			if _, err := os.Stat(staging); !errors.Is(err, fs.ErrNotExist) {
				test.Fatalf("existing destination created staging state: %v", err)
			}
			if kind == "file" {
				assertContents(test, destination, []byte("original"))
			}
		})
	}
}

func TestWritePrivateExportSupportsParentAlias(test *testing.T) {
	parent := test.TempDir()
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		test.Fatal(err)
	}
	makeBroadFixture(test, target, true)
	before := securitySnapshot(test, target)
	alias := filepath.Join(parent, "alias")
	makeSymlinkFixture(test, target, alias)
	contents := []byte("export through parent alias")
	if err := WritePrivateExport(filepath.Join(parent, "private"), filepath.Join(alias, "report"), contents); err != nil {
		test.Fatal(err)
	}
	actual, err := ReadPrivateFile(filepath.Join(target, "report"), 1024)
	if err != nil || !bytes.Equal(actual, contents) {
		test.Fatalf("exported bytes = %q, %v", actual, err)
	}
	if after := securitySnapshot(test, target); after != before {
		test.Fatal("export hardened an existing aliased parent")
	}
}

func TestWritePrivateExportRelativeStaging(test *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		test.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestWritePrivateExportRelativeStagingHelper$")
	command.Dir = test.TempDir()
	command.Env = append(os.Environ(), "TREECLEAR_TEST_EXPORT_RELATIVE_HELPER=1")
	if output, err := command.CombinedOutput(); err != nil {
		test.Fatalf("relative export subprocess: %v\n%s", err, output)
	}
}

func TestWritePrivateExportRelativeStagingHelper(test *testing.T) {
	if os.Getenv("TREECLEAR_TEST_EXPORT_RELATIVE_HELPER") != "1" {
		return
	}
	directory, err := os.Getwd()
	if err != nil {
		test.Fatal(err)
	}
	destination := filepath.Join(directory, "report")
	contents := []byte("relative private staging")
	if err := WritePrivateExport("state", destination, contents); err != nil {
		test.Fatal(err)
	}
	assertContents(test, destination, contents)
	assertPrivateObject(test, destination, false)
	assertOnlyNames(test, filepath.Join(directory, "state"))
}
