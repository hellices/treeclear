//go:build darwin || linux

package fssecure

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateRelativePathsUsePhysicalWorkingDirectory(test *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		test.Fatal(err)
	}
	for _, operation := range []string{"resolve", "read", "write", "directory", "export", "parent-directory"} {
		test.Run(operation, func(test *testing.T) {
			root, err := filepath.EvalSymlinks(test.TempDir())
			if err != nil {
				test.Fatal(err)
			}
			outer := filepath.Join(root, "outer")
			target := filepath.Join(outer, "target")
			if err := EnsurePrivateDirectory(target); err != nil {
				test.Fatal(err)
			}
			makeBroadFixture(test, root, true)
			makeBroadFixture(test, outer, true)
			alias := filepath.Join(root, "alias")
			makeSymlinkFixture(test, target, alias)
			if operation == "read" {
				writePrivateFixture(test, filepath.Join(outer, "report"), []byte("physical parent"))
				writePrivateFixture(test, filepath.Join(root, "report"), []byte("wrong logical parent"))
			}
			command := exec.CommandContext(test.Context(), executable, "-test.run=^TestPrivateRelativePathsHelper$")
			command.Dir = target
			for _, value := range os.Environ() {
				if !strings.HasPrefix(value, "PWD=") && !strings.HasPrefix(value, "TREECLEAR_TEST_CWD_") {
					command.Env = append(command.Env, value)
				}
			}
			command.Env = append(command.Env, "PWD="+alias, "TREECLEAR_TEST_CWD_ROOT="+root, "TREECLEAR_TEST_CWD_OPERATION="+operation)
			if output, err := command.CombinedOutput(); err != nil {
				test.Fatalf("aliased cwd subprocess: %v\n%s", err, output)
			}
		})
	}
}

func TestPrivateRelativePathsHelper(test *testing.T) {
	operation := os.Getenv("TREECLEAR_TEST_CWD_OPERATION")
	if operation == "" {
		return
	}
	root := os.Getenv("TREECLEAR_TEST_CWD_ROOT")
	outer := filepath.Join(root, "outer")
	alias := filepath.Join(root, "alias")
	workingDirectory, err := os.Getwd()
	if err != nil || workingDirectory != alias {
		test.Fatalf("cwd alias fixture = %q, %v; want %q", workingDirectory, err, alias)
	}
	wantPath := filepath.Join(outer, "report")
	wrongPath := filepath.Join(root, "report")
	reference := ".." + string(filepath.Separator) + "report"
	beforeRoot := securitySnapshot(test, root)
	switch operation {
	case "resolve":
		actual, err := ResolvePrivatePath(reference)
		if err != nil || actual != wantPath {
			test.Fatalf("ResolvePrivatePath(%q) = %q, %v; want %q", reference, actual, err, wantPath)
		}
	case "read":
		ordinary, err := os.ReadFile(reference)
		if err != nil || string(ordinary) != "physical parent" {
			test.Fatalf("OS path fixture = %q, %v", ordinary, err)
		}
		actual, err := ReadPrivateFile(reference, 1024)
		if err != nil || string(actual) != "physical parent" {
			test.Fatalf("ReadPrivateFile(%q) = %q, %v; OS reads %q", reference, actual, err, ordinary)
		}
	case "write":
		if err := WritePrivateFile(reference, []byte("private bytes")); err != nil {
			test.Fatal(err)
		}
	case "directory":
		if err := EnsurePrivateDirectory(reference + string(filepath.Separator) + "nested"); err != nil {
			test.Fatal(err)
		}
	case "export":
		if err := WritePrivateExport("state", reference, []byte("private bytes")); err != nil {
			test.Fatal(err)
		}
	case "parent-directory":
		if err := EnsurePrivateDirectory(".."); err != nil {
			test.Fatal(err)
		}
		if after := securitySnapshot(test, root); after != beforeRoot {
			test.Errorf("hardened unrelated logical parent: before %q, after %q", beforeRoot, after)
		}
		assertPrivateObject(test, outer, true)
		return
	default:
		test.Fatalf("unknown operation %q", operation)
	}
	if operation != "resolve" && operation != "read" {
		if _, err := os.Lstat(wrongPath); !errors.Is(err, fs.ErrNotExist) {
			test.Errorf("operation touched wrong logical-parent object: %v", err)
		}
		if _, err := os.Lstat(wantPath); err != nil {
			test.Errorf("physical-parent object absent: %v", err)
		}
		if after := securitySnapshot(test, root); after != beforeRoot {
			test.Errorf("unrelated logical-parent security changed: before %q, after %q", beforeRoot, after)
		}
	}
}
