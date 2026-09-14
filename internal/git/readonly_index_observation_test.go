package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/testutil"
)

func TestReadonlyIndexInitialObservationRejectsMtimeChange(test *testing.T) {
	for _, canceled := range []bool{false, true} {
		for _, closeFails := range []bool{false, true} {
			test.Run(fmt.Sprintf("canceled-%t/close-fails-%t", canceled, closeFails), func(test *testing.T) {
				directory := readonlyIndexCanonicalTemporaryDirectory(test)
				ctx, cancel := context.WithCancel(test.Context())
				defer cancel()
				closeCause := errors.New("initial observation close failure")
				operations := defaultReadonlyIndexOperations()
				var initial, pinned fs.FileInfo
				operations.lstat = func(path string) (fs.FileInfo, error) {
					information, err := os.Lstat(path)
					initial = information
					return information, err
				}
				nativeOpen := operations.open
				var opened *os.File
				operations.open = func(path string) (*os.File, error) {
					changed := initial.ModTime().Add(-time.Hour)
					if err := os.Chtimes(path, changed, changed); err != nil {
						test.Fatal(err)
					}
					file, err := nativeOpen(path)
					opened = file
					return file, err
				}
				operations.stat = func(file *os.File) (fs.FileInfo, error) {
					information, err := file.Stat()
					pinned = information
					return information, err
				}
				closeCalls := 0
				var nativeCloseErr error
				operations.close = func(file *os.File) error {
					closeCalls++
					nativeCloseErr = file.Close()
					err := nativeCloseErr
					if canceled {
						cancel()
					}
					if closeFails {
						err = errors.Join(err, closeCause)
					}
					return err
				}
				rootOperations := defaultReadonlyIndexOperations()
				rootOperations.lstat = func(path string) (fs.FileInfo, error) {
					return readonlyIndexDirectoryInfoWithOperations(path, operations)
				}
				observed, err := readonlyIndexRootInfo(ctx, directory, rootOperations)
				if initial == nil || pinned == nil || initial.ModTime().Equal(pinned.ModTime()) || initial.Mode() != pinned.Mode() || initial.Size() != pinned.Size() {
					test.Fatal("native fixture did not isolate an initial mtime change")
				}
				if !errors.Is(err, ErrWorktreeChanged) || observed != nil {
					test.Errorf("initial mtime disagreement accepted: observation %v, error %v", observed, err)
				}
				if errors.Is(err, context.Canceled) != canceled || errors.Is(err, closeCause) != closeFails {
					test.Errorf("initial observation lost cancellation or close cause: %v", err)
				}
				if closeCalls != 1 || opened == nil {
					test.Fatalf("initial observation close calls %d, handle %v", closeCalls, opened)
				}
				if _, statErr := opened.Stat(); !readonlyIndexInitialCloseVerified(closeCalls, nativeCloseErr, statErr) {
					test.Errorf("initial observation close calls %d, native close error %v, stat error %v", closeCalls, nativeCloseErr, statErr)
				}
			})
		}
	}
}

func TestReadonlyIndexInitialObservationStopsPreflight(test *testing.T) {
	fixture := readonlyIndexCanonicalTemporaryDirectory(test)
	directory := filepath.Join(fixture, "admin")
	replacement := filepath.Join(fixture, "replacement")
	for _, path := range []string{directory, replacement} {
		if err := os.Mkdir(path, 0o700); err != nil {
			test.Fatal(err)
		}
	}
	initial := readonlyIndexInitialPinnedInfo(test, directory)
	changed := initial.ModTime().Add(-time.Hour)
	if err := os.Chtimes(replacement, changed, changed); err != nil {
		test.Fatal(err)
	}
	replacementInfo := readonlyIndexInitialPinnedInfo(test, replacement)
	if os.SameFile(initial, replacementInfo) || initial.ModTime().Equal(replacementInfo.ModTime()) {
		test.Fatal("replacement must have distinct retained identity and mtime")
	}
	metadataOperations := defaultReadonlyIndexOperations()
	metadataOperations.lstat = os.Lstat
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
		test.Errorf("initial replacement authorized preflight: metadata opens %d, preflight opens %d, enumerations %d, error %v", metadataOpens, preflightOpens, enumerations, err)
	}
	if current := readonlyIndexInitialPinnedInfo(test, directory); !os.SameFile(current, replacementInfo) || os.SameFile(current, initial) {
		test.Fatal("fixture replacement did not retain the independently pinned native identity")
	}
}

func TestReadonlyIndexInitialObservationPreservesFailures(test *testing.T) {
	for _, stage := range []string{"lstat", "open", "stat", "close"} {
		for _, canceled := range []bool{false, true} {
			test.Run(fmt.Sprintf("%s/canceled-%t", stage, canceled), func(test *testing.T) {
				directory := readonlyIndexCanonicalTemporaryDirectory(test)
				ctx, cancel := context.WithCancel(test.Context())
				defer cancel()
				cause := errors.New(ErrWorktreeChanged.Error())
				recorder := testutil.NewRecorder(nil)
				fail := func(operation string) error {
					if err := recorder.Record(operation); err != nil {
						test.Fatal(err)
					}
					if operation == stage {
						if canceled {
							cancel()
						}
						return cause
					}
					return nil
				}
				operations := defaultReadonlyIndexOperations()
				operations.lstat = func(path string) (fs.FileInfo, error) {
					if err := fail("lstat"); err != nil {
						return nil, err
					}
					return os.Lstat(path)
				}
				nativeOpen := operations.open
				var opened *os.File
				operations.open = func(path string) (*os.File, error) {
					if err := fail("open"); err != nil {
						return nil, err
					}
					file, err := nativeOpen(path)
					opened = file
					return file, err
				}
				operations.stat = func(file *os.File) (fs.FileInfo, error) {
					if err := fail("stat"); err != nil {
						return nil, err
					}
					return file.Stat()
				}
				closeCalls := 0
				var nativeCloseErr error
				operations.close = func(file *os.File) error {
					closeCalls++
					nativeCloseErr = file.Close()
					return errors.Join(nativeCloseErr, fail("close"))
				}
				rootOperations := defaultReadonlyIndexOperations()
				rootOperations.lstat = func(path string) (fs.FileInfo, error) {
					return readonlyIndexDirectoryInfoWithOperations(path, operations)
				}
				observed, err := readonlyIndexRootInfo(ctx, directory, rootOperations)
				if !errors.Is(err, cause) || errors.Is(err, context.Canceled) != canceled || errors.Is(err, ErrWorktreeChanged) || observed != nil {
					test.Errorf("initial observation failure was lost or mislabeled: observation %v, error %v", observed, err)
				}
				wantOperations := []string{"lstat", "open", "stat", "close"}
				if stage == "lstat" {
					wantOperations = wantOperations[:1]
				} else if stage == "open" {
					wantOperations = wantOperations[:2]
				}
				if !reflect.DeepEqual(recorder.Operations(), wantOperations) {
					test.Errorf("initial failure operations = %q, want %q", recorder.Operations(), wantOperations)
				}
				if opened != nil {
					if _, statErr := opened.Stat(); !readonlyIndexInitialCloseVerified(closeCalls, nativeCloseErr, statErr) {
						test.Errorf("initial failure close calls %d, native close error %v, stat error %v", closeCalls, nativeCloseErr, statErr)
					}
				} else if closeCalls != 0 {
					test.Errorf("unopened initial observation closed %d times", closeCalls)
				}
			})
		}
	}
}

func TestReadonlyIndexInitialObservationReturnsPinnedInfo(test *testing.T) {
	fixture := readonlyIndexCanonicalTemporaryDirectory(test)
	directory := filepath.Join(fixture, "admin")
	if err := os.Mkdir(directory, 0o700); err != nil {
		test.Fatal(err)
	}
	operations := defaultReadonlyIndexOperations()
	operations.lstat = os.Lstat
	var pinned fs.FileInfo
	operations.stat = func(file *os.File) (fs.FileInfo, error) {
		information, err := file.Stat()
		pinned = information
		return information, err
	}
	observed, err := readonlyIndexDirectoryInfoWithOperations(directory, operations)
	if err != nil || observed == nil || observed != pinned {
		test.Fatalf("ordinary initial observation is not the eager handle metadata: observation %v, pinned %v, error %v", observed, pinned, err)
	}
	if err := os.Rename(directory, filepath.Join(fixture, "moved")); err != nil {
		test.Fatal(err)
	}
	if _, err := os.Lstat(directory); !errors.Is(err, fs.ErrNotExist) {
		test.Fatalf("observed pathname still exists: %v", err)
	}
	if !os.SameFile(observed, pinned) {
		test.Fatal("initial observation reloaded the now-missing pathname")
	}
}

func readonlyIndexInitialPinnedInfo(test *testing.T, directory string) fs.FileInfo {
	test.Helper()
	file, err := os.Open(directory + string(filepath.Separator) + ".")
	if err != nil {
		test.Fatal(err)
	}
	information, err := file.Stat()
	if err := errors.Join(err, file.Close()); err != nil {
		test.Fatal(err)
	}
	return information
}

func TestReadonlyIndexInitialObservationCloseOracle(test *testing.T) {
	for _, scenario := range []struct {
		name          string
		closeHandle   bool
		opaqueStatErr bool
		wantVerified  bool
	}{
		{name: "native close", closeHandle: true, wantVerified: true},
		{name: "opaque closed-handle error", closeHandle: true, opaqueStatErr: true, wantVerified: true},
		{name: "no-op close"},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			directory := readonlyIndexCanonicalTemporaryDirectory(test)
			file, err := os.Open(directory)
			if err != nil {
				test.Fatal(err)
			}
			needsCleanup := true
			test.Cleanup(func() {
				if needsCleanup {
					if err := file.Close(); err != nil {
						test.Errorf("native fixture cleanup close: %v", err)
					}
				}
			})
			closeCalls := 0
			var nativeCloseErr error
			closeHandle := func() {
				closeCalls++
				if scenario.closeHandle {
					nativeCloseErr = file.Close()
					needsCleanup = nativeCloseErr != nil
				}
			}
			closeHandle()
			_, statErr := file.Stat()
			if nativeCloseErr != nil || (statErr != nil) != scenario.closeHandle {
				test.Fatalf("native close fixture mismatch: close error %v, stat error %v", nativeCloseErr, statErr)
			}
			if scenario.opaqueStatErr {
				statErr = &fs.PathError{Op: "stat", Path: directory, Err: errors.New("simulated native invalid-handle error")}
			}
			if verified := readonlyIndexInitialCloseVerified(closeCalls, nativeCloseErr, statErr); verified != scenario.wantVerified {
				test.Errorf("close oracle verified %t, want %t: close calls %d, native close error %v, stat error %v", verified, scenario.wantVerified, closeCalls, nativeCloseErr, statErr)
			}
			if scenario.closeHandle {
				for _, invalidCalls := range []int{0, 2} {
					if readonlyIndexInitialCloseVerified(invalidCalls, nil, statErr) {
						test.Errorf("close oracle accepted %d close calls", invalidCalls)
					}
				}
				if readonlyIndexInitialCloseVerified(1, errors.New("native close failure"), statErr) {
					test.Error("close oracle accepted failed native close")
				}
			}
		})
	}
}

func readonlyIndexInitialCloseVerified(closeCalls int, nativeCloseErr, statErr error) bool {
	return closeCalls == 1 && nativeCloseErr == nil && statErr != nil
}
