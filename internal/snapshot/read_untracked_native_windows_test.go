package snapshot

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestReadUntrackedNativeWindowsUnsafeAttributes(test *testing.T) {
	for _, attribute := range []struct {
		name  string
		value uint32
	}{
		{"reparse", windows.FILE_ATTRIBUTE_REPARSE_POINT},
		{"device", windows.FILE_ATTRIBUTE_DEVICE},
		{"reparse-device", windows.FILE_ATTRIBUTE_REPARSE_POINT | windows.FILE_ATTRIBUTE_DEVICE},
		{"unknown", 0},
	} {
		for _, boundary := range []string{"root-stat", "parent-lstat", "leaf-lstat", "opened-leaf-stat"} {
			test.Run(attribute.name+"/"+boundary, func(test *testing.T) {
				directory := newUntrackedFaultFixture(test)
				operations := trackedUntrackedFaultOperations(test)
				injected := false
				unsafeInformation := func(information fs.FileInfo) fs.FileInfo {
					attributes, ok := information.Sys().(*syscall.Win32FileAttributeData)
					if !ok || attributes == nil {
						test.Fatal("fixture has no native Windows attributes")
					}
					copyAttributes := *attributes
					copyAttributes.FileAttributes |= attribute.value
					unsafe := administrativeReadWindowsInformation{FileInfo: information, attributes: &copyAttributes}
					if attribute.name == "unknown" {
						unsafe.attributes = nil
					}
					if (boundary == "leaf-lstat" || boundary == "opened-leaf-stat") && !unsafe.Mode().IsRegular() {
						test.Fatal("unsafe attributes must retain regular-looking FileMode")
					}
					injected = true
					return unsafe
				}
				originalStat, originalLstat := operations.stat, operations.lstat
				operations.stat = func(file *os.File) (fs.FileInfo, error) {
					information, err := originalStat(file)
					if err == nil && (boundary == "root-stat" && administrativeReadFileName(file) == "source" || boundary == "opened-leaf-stat" && administrativeReadFileName(file) == "first.bin") {
						return unsafeInformation(information), nil
					}
					return information, err
				}
				operations.lstat = func(parent *os.Root, name string) (fs.FileInfo, error) {
					information, err := originalLstat(parent, name)
					if err == nil && (boundary == "parent-lstat" && name == "nested" || boundary == "leaf-lstat" && name == "first.bin") {
						return unsafeInformation(information), nil
					}
					return information, err
				}
				operations.read = func(*os.File, []byte) (int, error) {
					test.Fatal("consumed bytes with unsafe Windows metadata")
					return 0, nil
				}
				entries, err := readUntracked(test.Context(), directory, []string{"nested/first.bin"}, 64, operations)
				if !injected {
					test.Error("unsafe native metadata injection was not exercised")
				}
				assertUntrackedFaultFailure(test, entries, err, ErrUntrackedInvalid)
			})
		}
	}
}

func TestReadUntrackedNativeWindowsSymlinks(test *testing.T) {
	for _, target := range []string{"root", "parent", "leaf"} {
		test.Run(target, func(test *testing.T) {
			directory := newUntrackedFaultFixture(test)
			filename := directory
			if target == "parent" {
				filename = filepath.Join(directory, "nested")
			} else if target == "leaf" {
				filename = filepath.Join(directory, "nested", "first.bin")
			}
			backup := filename + "-original"
			if err := os.Rename(filename, backup); err != nil {
				test.Fatal(err)
			}
			if err := os.Symlink(backup, filename); err != nil {
				if errors.Is(err, syscall.Errno(1314)) {
					test.Skip("native runner does not grant symlink creation privilege")
				}
				test.Fatal(err)
			}
			information, err := os.Lstat(filename)
			if err != nil || information.Mode()&fs.ModeSymlink == 0 {
				test.Fatalf("fixture did not create a native symlink: %v", err)
			}
			operations := trackedUntrackedFaultOperations(test)
			operations.readlink = func(*os.Root, string) (string, error) {
				test.Fatal("Windows reader attempted to accept reparse link text")
				return "", nil
			}
			operations.read = func(*os.File, []byte) (int, error) {
				test.Fatal("Windows reader consumed bytes through a reparse point")
				return 0, nil
			}
			entries, err := readUntracked(test.Context(), directory, []string{"nested/first.bin"}, 64, operations)
			assertUntrackedFaultFailure(test, entries, err, nil)
		})
	}
}
