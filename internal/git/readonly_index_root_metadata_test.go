package git

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestReadonlyIndexRootMetadataUnavailableBeforeOpen(test *testing.T) {
	for _, stage := range []string{"path observation", "directory open"} {
		test.Run(stage, func(test *testing.T) {
			directory := readonlyIndexCanonicalTemporaryDirectory(test)
			operations := defaultReadonlyIndexOperations()
			native, err := operations.lstat(directory)
			if err != nil {
				test.Fatal(err)
			}
			operations.lstat = func(string) (fs.FileInfo, error) {
				return readonlyIndexRootMetadata{FileInfo: native}, nil
			}
			if stage == "path observation" {
				observed, err := readonlyIndexRootInfo(test.Context(), directory, operations)
				if !errors.Is(err, fs.ErrInvalid) || errors.Is(err, ErrWorktreeChanged) || observed != nil {
					test.Fatalf("unavailable native metadata accepted as root observation: %#v, %v", observed, err)
				}
				return
			}
			opened := 0
			unexpectedOpen := errors.New("opened before validating unavailable native root metadata")
			operations.open = func(string) (*os.File, error) {
				opened++
				return nil, unexpectedOpen
			}
			operations.readNames = func(*os.File, int) ([]string, error) {
				test.Fatal("scanned after unavailable native root metadata")
				return nil, nil
			}
			err = rejectSplitIndexDirectory(test.Context(), directory, operations)
			if !errors.Is(err, fs.ErrInvalid) || errors.Is(err, ErrWorktreeChanged) || opened != 0 || errors.Is(err, unexpectedOpen) {
				test.Fatalf("unavailable native root metadata reached open: opened %d, error %v", opened, err)
			}
		})
	}
}

func TestReadonlyIndexPinnedMetadataUnavailable(test *testing.T) {
	for _, stage := range []struct {
		name     string
		statCall int
	}{
		{name: "initial handle", statCall: 1},
		{name: "final handle", statCall: 2},
	} {
		for _, shape := range []string{"nil information", "nil native metadata"} {
			test.Run(stage.name+"/"+shape, func(test *testing.T) {
				directory := readonlyIndexCanonicalTemporaryDirectory(test)
				operations := defaultReadonlyIndexOperations()
				nativeStat := operations.stat
				statCalls := 0
				operations.stat = func(file *os.File) (fs.FileInfo, error) {
					statCalls++
					information, err := nativeStat(file)
					if err != nil || statCalls != stage.statCall {
						return information, err
					}
					if shape == "nil information" {
						return nil, nil
					}
					return readonlyIndexRootMetadata{FileInfo: information}, nil
				}
				nativeReadNames := operations.readNames
				readCalls := 0
				operations.readNames = func(file *os.File, limit int) ([]string, error) {
					readCalls++
					return nativeReadNames(file, limit)
				}
				closeFailure := errors.New("close failure after missing native evidence")
				closeCalls := 0
				operations.close = func(file *os.File) error {
					closeCalls++
					return errors.Join(file.Close(), closeFailure)
				}
				err := rejectSplitIndexDirectory(test.Context(), directory, operations)
				if !errors.Is(err, fs.ErrInvalid) || errors.Is(err, ErrWorktreeChanged) {
					test.Errorf("unavailable pinned metadata must be invalid, not changed: %v", err)
				}
				if !errors.Is(err, closeFailure) || closeCalls != 1 {
					test.Errorf("close cause lost: calls %d, error %v", closeCalls, err)
				}
				if statCalls != stage.statCall || readCalls != stage.statCall-1 {
					test.Errorf("continued after missing native evidence: stat calls %d, read calls %d", statCalls, readCalls)
				}
			})
		}
	}
}

func TestValidateReadNativeInfoAcceptsNativeKinds(test *testing.T) {
	directory := readonlyIndexCanonicalTemporaryDirectory(test)
	regular := filepath.Join(directory, "regular")
	if err := os.WriteFile(regular, []byte("owned fixture"), 0o600); err != nil {
		test.Fatal(err)
	}
	for _, fixture := range []struct {
		name string
		path string
		mode fs.FileMode
	}{
		{name: "directory", path: directory, mode: fs.ModeDir},
		{name: "regular file", path: regular},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			file, err := os.Open(fixture.path)
			if err != nil {
				test.Fatal(err)
			}
			information, statErr := file.Stat()
			if err := errors.Join(statErr, file.Close()); err != nil {
				test.Fatal(err)
			}
			if information.Mode().Type() != fixture.mode || information.Sys() == nil {
				test.Fatalf("fixture lacks expected native file kind: %#v", information)
			}
			if err := validateReadNativeInfo(information); err != nil {
				test.Fatalf("ordinary native file kind refused: %v", err)
			}
		})
	}
}

type readonlyIndexRootMetadata struct {
	fs.FileInfo
	native any
	reads  *int
}

func (information readonlyIndexRootMetadata) Sys() any {
	if information.reads != nil {
		*information.reads++
	}
	return information.native
}
