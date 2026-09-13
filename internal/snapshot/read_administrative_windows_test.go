package snapshot

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/hellices/treeclear/internal/pathutil"
)

func TestReadAdministrativeFocusedWindowsLongLocalPath(test *testing.T) {
	directory := newAdministrativeReadFixture(test)
	parent := filepath.Dir(directory)
	for len(parent) < 320 {
		parent = filepath.Join(parent, strings.Repeat("long-", 12))
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		test.Fatal(err)
	}
	longDirectory := filepath.Join(parent, "admin")
	if err := os.Rename(directory, longDirectory); err != nil {
		test.Fatal(err)
	}
	canonical, err := pathutil.Canonical(administrativeReadWindowsPath(longDirectory))
	if err != nil {
		test.Fatal(err)
	}
	if len(canonical) <= 260 {
		test.Fatalf("fixture path must exceed MAX_PATH: %q", canonical)
	}
	expected := administrativeReadFixtureEntries(test, canonical, []string{".", "HEAD", "commondir", "gitdir"})
	entries, err := ReadAdministrative(test.Context(), canonical)
	if err != nil || !reflect.DeepEqual(entries, expected) {
		test.Fatalf("long local path: got %#v, %v; want %#v", entries, err, expected)
	}
	entries, err = ReadAdministrative(test.Context(), administrativeReadWindowsPath(canonical))
	assertAdministrativeReadFailure(test, entries, err, nil)
}

func TestReadAdministrativeFocusedWindowsReparseAttributes(test *testing.T) {
	directory := newAdministrativeReadFixture(test)
	file, err := os.Open(filepath.Join(directory, "HEAD"))
	if err != nil {
		test.Fatal(err)
	}
	information, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil || closeErr != nil {
		test.Fatalf("fixture information: %v, %v", statErr, closeErr)
	}
	attributes := *information.Sys().(*syscall.Win32FileAttributeData)
	attributes.FileAttributes |= syscall.FILE_ATTRIBUTE_REPARSE_POINT
	reparse := administrativeReadWindowsInformation{FileInfo: information, attributes: &attributes}
	if !reparse.Mode().IsRegular() {
		test.Fatal("fixture must exercise a reparse record with ordinary FileMode")
	}
	if err := validateAdministrativeReadInfo(reparse); err == nil {
		test.Fatal("accepted the native reparse attribute on a regular-mode file")
	}
	unknown := administrativeReadWindowsInformation{FileInfo: information}
	if err := validateAdministrativeReadInfo(unknown); err == nil {
		test.Fatal("accepted unavailable native attributes")
	}
}

func TestReadAdministrativeFocusedWindowsSymlinks(test *testing.T) {
	for _, target := range []string{"root", "file", "directory"} {
		test.Run(target, func(test *testing.T) {
			directory := newAdministrativeReadFixture(test)
			link, destination := filepath.Join(directory, "index"), filepath.Join(directory, "HEAD")
			if target == "root" {
				link, destination = filepath.Join(test.TempDir(), "alias"), directory
			} else if target == "directory" {
				link, destination = filepath.Join(directory, "logs"), test.TempDir()
			}
			if err := os.Symlink(destination, link); err != nil {
				if errors.Is(err, syscall.Errno(1314)) {
					test.Skip("native runner does not grant symlink creation privilege")
				}
				test.Fatal(err)
			}
			if target == "root" {
				directory = link
			}
			entries, err := ReadAdministrative(test.Context(), directory)
			assertAdministrativeReadFailure(test, entries, err, nil)
		})
	}
}

type administrativeReadWindowsInformation struct {
	fs.FileInfo
	attributes *syscall.Win32FileAttributeData
}

func (information administrativeReadWindowsInformation) Sys() any {
	return information.attributes
}
