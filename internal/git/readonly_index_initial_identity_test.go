package git

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestReadonlyIndexInitialObservationRejectsSameMetadataReplacement(test *testing.T) {
	fixture := readonlyIndexCanonicalTemporaryDirectory(test)
	directory := filepath.Join(fixture, "admin")
	replacement := filepath.Join(fixture, "replacement")
	for _, path := range []string{directory, replacement} {
		if err := os.Mkdir(path, 0o700); err != nil {
			test.Fatal(err)
		}
	}
	initial := readonlyIndexInitialPinnedInfo(test, directory)
	if err := os.Chtimes(replacement, initial.ModTime(), initial.ModTime()); err != nil {
		test.Fatal(err)
	}
	replacementInfo := readonlyIndexInitialPinnedInfo(test, replacement)
	if os.SameFile(initial, replacementInfo) || initial.Mode() != replacementInfo.Mode() || initial.Size() != replacementInfo.Size() || !initial.ModTime().Equal(replacementInfo.ModTime()) {
		test.Fatal("replacement must have a different native identity with identical mode, size and modification time")
	}
	metadataOperations := defaultReadonlyIndexOperations()
	nativeLstat := metadataOperations.lstat
	var observedInitial fs.FileInfo
	metadataOperations.lstat = func(path string) (fs.FileInfo, error) {
		information, err := nativeLstat(path)
		if observedInitial == nil {
			observedInitial = information
		}
		return information, err
	}
	nativeOpen := metadataOperations.open
	metadataOpens := 0
	metadataOperations.open = func(path string) (*os.File, error) {
		metadataOpens++
		if metadataOpens == 1 {
			if err := os.Rename(directory, filepath.Join(fixture, "moved")); err != nil {
				test.Fatal(err)
			}
			if err := os.Rename(replacement, directory); err != nil {
				test.Fatal(err)
			}
		}
		return nativeOpen(path)
	}
	operations := defaultReadonlyIndexOperations()
	operations.lstat = func(path string) (fs.FileInfo, error) {
		return readonlyIndexDirectoryInfoWithOperations(path, metadataOperations)
	}
	preflightOpens, enumerations := 0, 0
	operations.open = func(path string) (*os.File, error) {
		preflightOpens++
		return nativeOpen(path)
	}
	operations.readNames = func(file *os.File, limit int) ([]string, error) {
		enumerations++
		return file.Readdirnames(limit)
	}
	err := rejectSplitIndexDirectory(test.Context(), directory, operations)
	if !errors.Is(err, ErrWorktreeChanged) || metadataOpens != 1 || preflightOpens != 0 || enumerations != 0 {
		test.Errorf("same-metadata replacement authorized preflight: metadata opens %d, preflight opens %d, enumerations %d, error %v", metadataOpens, preflightOpens, enumerations, err)
	}
	current := readonlyIndexInitialPinnedInfo(test, directory)
	if observedInitial == nil || !os.SameFile(observedInitial, initial) || os.SameFile(observedInitial, current) || !os.SameFile(current, replacementInfo) || observedInitial.Mode() != current.Mode() || observedInitial.Size() != current.Size() || !observedInitial.ModTime().Equal(current.ModTime()) {
		test.Fatal("fixture did not preserve the first native identity or isolate a same-metadata directory replacement")
	}
}
