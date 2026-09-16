package git

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"reflect"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestReadonlyIndexWindowsNativeRootAttributesBeforeIO(test *testing.T) {
	for _, scenario := range []struct {
		name       string
		attributes uint32
	}{
		{"ordinary", 0},
		{"reparse", windows.FILE_ATTRIBUTE_REPARSE_POINT},
		{"device", windows.FILE_ATTRIBUTE_DEVICE},
		{"reparse and device", windows.FILE_ATTRIBUTE_REPARSE_POINT | windows.FILE_ATTRIBUTE_DEVICE},
	} {
		for _, stage := range []string{"path observation", "opened handle"} {
			test.Run(scenario.name+"/"+stage, func(test *testing.T) {
				directory := readonlyIndexCanonicalTemporaryDirectory(test)
				ordinary, observed := readonlyIndexWindowsRootSnapshots(test, directory, scenario.attributes)
				operations := defaultReadonlyIndexOperations()
				openDirectory, closeDirectory := operations.open, operations.close
				opened, closed, scanned, pathReads, handleReads := 0, 0, 0, 0, 0
				operations.lstat = func(string) (fs.FileInfo, error) {
					pathReads++
					if stage == "path observation" && pathReads == 1 {
						return observed, nil
					}
					return ordinary, nil
				}
				operations.open = func(path string) (*os.File, error) {
					opened++
					return openDirectory(path)
				}
				operations.stat = func(*os.File) (fs.FileInfo, error) {
					handleReads++
					if stage == "opened handle" {
						return observed, nil
					}
					return ordinary, nil
				}
				operations.readNames = func(*os.File, int) ([]string, error) {
					scanned++
					return nil, io.EOF
				}
				operations.close = func(file *os.File) error {
					closed++
					return closeDirectory(file)
				}
				err := rejectSplitIndexDirectory(test.Context(), directory, operations)
				if closed != opened {
					test.Errorf("native root handles: opened %d, closed %d", opened, closed)
				}
				if scenario.attributes == 0 {
					if err != nil || opened != 1 || scanned != 1 || handleReads != 2 || pathReads != 3 {
						test.Fatalf("ordinary native root: opened %d, scanned %d, handle reads %d, path reads %d, error %v", opened, scanned, handleReads, pathReads, err)
					}
					return
				}
				wantOpened := 0
				if stage == "opened handle" {
					wantOpened = 1
				}
				if err == nil || opened != wantOpened || scanned != 0 || handleReads != wantOpened || pathReads != 1 {
					test.Fatalf("unsafe native attributes %#x reached index-read authorization: opened %d, scanned %d, handle reads %d, path reads %d, error %v", scenario.attributes, opened, scanned, handleReads, pathReads, err)
				}
			})
		}
	}
}

func TestReadonlyIndexWindowsUnavailableRootMetadata(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		native any
	}{
		{"nil", nil},
		{"typed nil", (*syscall.Win32FileAttributeData)(nil)},
		{"wrong shape", struct{}{}},
	} {
		for _, stage := range []string{"path observation", "opened handle"} {
			test.Run(scenario.name+"/"+stage, func(test *testing.T) {
				directory := readonlyIndexCanonicalTemporaryDirectory(test)
				ordinary, _ := readonlyIndexWindowsRootSnapshots(test, directory, 0)
				operations := defaultReadonlyIndexOperations()
				nativeReads := 0
				unknown := readonlyIndexRootMetadata{FileInfo: ordinary, native: scenario.native, reads: &nativeReads}
				operations.lstat = func(string) (fs.FileInfo, error) {
					if stage == "path observation" {
						return unknown, nil
					}
					return ordinary, nil
				}
				if stage == "path observation" {
					observed, err := readonlyIndexRootInfo(test.Context(), directory, operations)
					if err == nil || observed != nil || nativeReads == 0 {
						test.Fatalf("unavailable native attributes accepted: observed %#v, native reads %d, error %v", observed, nativeReads, err)
					}
					return
				}
				operations.stat = func(*os.File) (fs.FileInfo, error) { return unknown, nil }
				operations.readNames = func(*os.File, int) ([]string, error) {
					test.Fatal("scanned before validating opened handle native metadata")
					return nil, nil
				}
				closed := 0
				closeDirectory := operations.close
				operations.close = func(file *os.File) error {
					closed++
					return closeDirectory(file)
				}
				err := rejectSplitIndexDirectory(test.Context(), directory, operations)
				if err == nil || errors.Is(err, ErrWorktreeChanged) || nativeReads == 0 || closed != 1 {
					test.Fatalf("unavailable handle metadata fell through to identity comparison: native reads %d, closed %d, error %v", nativeReads, closed, err)
				}
			})
		}
	}
}

func readonlyIndexWindowsRootSnapshots(test *testing.T, directory string, attributes uint32) (fs.FileInfo, fs.FileInfo) {
	test.Helper()
	file, err := os.Open(directory)
	if err != nil {
		test.Fatal(err)
	}
	ordinary, ordinaryErr := file.Stat()
	observed, observedErr := file.Stat()
	if err := errors.Join(ordinaryErr, observedErr, file.Close()); err != nil {
		test.Fatal(err)
	}
	value := reflect.ValueOf(observed).Elem()
	field := value.FieldByName("FileAttributes")
	if !field.IsValid() || !field.CanSet() || field.Kind() != reflect.Uint32 {
		test.Fatal("Go native FileInfo fixture has no writable exported FileAttributes")
	}
	field.SetUint(field.Uint() | uint64(attributes))
	if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		tag := value.FieldByName("ReparseTag")
		if !tag.IsValid() || !tag.CanSet() || tag.Kind() != reflect.Uint32 {
			test.Fatal("Go native FileInfo fixture has no writable exported ReparseTag")
		}
		tag.SetUint(0x80000013)
	}
	native, ok := observed.Sys().(*syscall.Win32FileAttributeData)
	if !ok || native == nil || native.FileAttributes&attributes != attributes {
		test.Fatal("native attribute snapshot fixture did not retain injected attributes")
	}
	if ordinary.Mode().Type() != fs.ModeDir || observed.Mode() != ordinary.Mode() || observed.Size() != ordinary.Size() || !observed.ModTime().Equal(ordinary.ModTime()) || !os.SameFile(ordinary, observed) {
		test.Fatal("native attribute fixture must retain directory mode, metadata, and native SameFile identity")
	}
	return ordinary, observed
}
