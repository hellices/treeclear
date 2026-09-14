package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/hellices/treeclear/internal/testutil"
)

func TestReadonlyIndexDirectoryPreservesFailures(test *testing.T) {
	for _, stage := range []string{"lstat-1", "open", "open-with-handle", "stat-1", "lstat-2", "read", "stat-2", "lstat-3", "close"} {
		for _, canceled := range []bool{false, true} {
			test.Run(fmt.Sprintf("%s/canceled-%t", stage, canceled), func(test *testing.T) {
				directory := readonlyIndexCanonicalTemporaryDirectory(test)
				ctx, cancel := context.WithCancel(test.Context())
				defer cancel()
				cause := errors.New("sharedindex. is diagnostic text, not a recognized entry")
				operations := defaultReadonlyIndexOperations()
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
				var opened *os.File
				nativeOpen := operations.open
				operations.open = func(path string) (*os.File, error) {
					if stage == "open-with-handle" {
						file, err := nativeOpen(path)
						opened = file
						return file, errors.Join(err, fail(stage))
					}
					if err := fail("open"); err != nil {
						return nil, err
					}
					file, err := nativeOpen(path)
					opened = file
					return file, err
				}
				lstatCalls := 0
				nativeLstat := operations.lstat
				operations.lstat = func(path string) (fs.FileInfo, error) {
					lstatCalls++
					if err := fail(fmt.Sprintf("lstat-%d", lstatCalls)); err != nil {
						return nil, err
					}
					return nativeLstat(path)
				}
				statCalls := 0
				operations.stat = func(file *os.File) (fs.FileInfo, error) {
					statCalls++
					if err := fail(fmt.Sprintf("stat-%d", statCalls)); err != nil {
						return nil, err
					}
					return file.Stat()
				}
				operations.readNames = func(file *os.File, limit int) ([]string, error) {
					if err := fail("read"); err != nil {
						return nil, err
					}
					return file.Readdirnames(limit)
				}
				closeCalls := 0
				operations.close = func(file *os.File) error {
					closeCalls++
					return errors.Join(file.Close(), fail("close"))
				}
				err := rejectSplitIndexDirectory(ctx, directory, operations)
				if !errors.Is(err, cause) || canceled && !errors.Is(err, context.Canceled) || errors.Is(err, errors.ErrUnsupported) {
					test.Errorf("failure error = %v; want preserved cause/cancellation, not unsupported", err)
				}
				if opened != nil {
					if _, err := opened.Stat(); !errors.Is(err, fs.ErrClosed) || closeCalls != 1 {
						test.Errorf("directory handle closed %d times, stat error %v", closeCalls, err)
					}
				} else if closeCalls != 0 {
					test.Errorf("unopened directory closed %d times", closeCalls)
				}
				failed := false
				for _, operation := range recorder.Operations() {
					if failed && operation != "close" {
						test.Errorf("operation %s ran after failure: %q", operation, recorder.Operations())
					}
					failed = failed || operation == stage
				}
			})
		}
	}
}

func TestReadonlyIndexDirectoryRejectsReplacedIdentities(test *testing.T) {
	for _, stage := range []string{"stat-1", "lstat-2", "stat-2", "lstat-3"} {
		test.Run(stage, func(test *testing.T) {
			directory := readonlyIndexCanonicalTemporaryDirectory(test)
			replacement := readonlyIndexCanonicalTemporaryDirectory(test)
			original, err := os.Stat(directory)
			if err != nil {
				test.Fatal(err)
			}
			if err := os.Chtimes(replacement, original.ModTime(), original.ModTime()); err != nil {
				test.Fatal(err)
			}
			changed, err := os.Stat(replacement)
			if err != nil {
				test.Fatal(err)
			}
			if os.SameFile(original, changed) || original.Mode() != changed.Mode() || original.Size() != changed.Size() || !original.ModTime().Equal(changed.ModTime()) {
				test.Fatal("identity fixture must differ only in native identity")
			}
			operations := defaultReadonlyIndexOperations()
			lstatCalls := 0
			nativeLstat := operations.lstat
			operations.lstat = func(path string) (fs.FileInfo, error) {
				lstatCalls++
				if stage == fmt.Sprintf("lstat-%d", lstatCalls) {
					return changed, nil
				}
				return nativeLstat(path)
			}
			statCalls := 0
			operations.stat = func(file *os.File) (fs.FileInfo, error) {
				statCalls++
				if stage == fmt.Sprintf("stat-%d", statCalls) {
					return changed, nil
				}
				return file.Stat()
			}
			if err := rejectSplitIndexDirectory(test.Context(), directory, operations); !errors.Is(err, ErrWorktreeChanged) || errors.Is(err, errors.ErrUnsupported) {
				test.Fatalf("replaced identity error = %v; want distinct identity refusal", err)
			}
		})
	}
}

func TestReadonlyIndexDirectoryEnumerationBounds(test *testing.T) {
	for _, count := range []int{0, 128, maxAdminEntries, maxAdminEntries + 1} {
		test.Run(fmt.Sprintf("%d-entries", count), func(test *testing.T) {
			directory := readonlyIndexCanonicalTemporaryDirectory(test)
			operations := defaultReadonlyIndexOperations()
			seen, calls := 0, 0
			operations.readNames = func(file *os.File, limit int) ([]string, error) {
				calls++
				if limit < 1 || limit > 128 || seen+limit > maxAdminEntries+1 {
					test.Fatalf("unbounded enumeration: seen %d, requested %d", seen, limit)
				}
				if seen == count {
					return nil, io.EOF
				}
				names := make([]string, min(limit, count-seen))
				for position := range names {
					names[position] = fmt.Sprintf("entry-%d", seen+position)
				}
				seen += len(names)
				return names, nil
			}
			err := rejectSplitIndexDirectory(test.Context(), directory, operations)
			if (err == nil) != (count <= maxAdminEntries) || calls == 0 || seen != count {
				test.Fatalf("bounded read saw %d of %d entries in %d calls, error %v", seen, count, calls, err)
			}
			if errors.Is(err, ErrIndexPreflightLimit) != (count > maxAdminEntries) {
				test.Fatalf("directory count %d error = %v; want typed overflow only above %d", count, err, maxAdminEntries)
			}
		})
	}
}

func TestReadonlyIndexDirectoryRejectsMalformedEnumeration(test *testing.T) {
	for _, scenario := range []struct {
		name  string
		names []string
		want  error
	}{
		{"empty batch", nil, io.ErrNoProgress},
		{"empty name", []string{""}, fs.ErrInvalid},
		{"current directory", []string{"."}, fs.ErrInvalid},
		{"parent directory", []string{".."}, fs.ErrInvalid},
		{"path instead of name", []string{"nested/entry"}, fs.ErrInvalid},
		{"nul name", []string{"nul\x00name"}, fs.ErrInvalid},
		{"duplicate name", []string{"duplicate", "duplicate"}, fs.ErrInvalid},
		{"oversized batch", make([]string, 129), ErrIndexPreflightLimit},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			operations := defaultReadonlyIndexOperations()
			calls := 0
			operations.readNames = func(file *os.File, limit int) ([]string, error) {
				calls++
				if calls != 1 {
					test.Fatal("continued after malformed enumeration")
				}
				return scenario.names, nil
			}
			if err := rejectSplitIndexDirectory(test.Context(), readonlyIndexCanonicalTemporaryDirectory(test), operations); !errors.Is(err, scenario.want) || errors.Is(err, errors.ErrUnsupported) || calls != 1 {
				test.Fatalf("malformed enumeration calls %d, error %v", calls, err)
			}
		})
	}
}

func TestReadonlyIndexDirectoryPreservesCombinedFailures(test *testing.T) {
	for _, backing := range []bool{false, true} {
		test.Run(fmt.Sprintf("backing-%t", backing), func(test *testing.T) {
			ctx, cancel := context.WithCancel(test.Context())
			defer cancel()
			readFailure, closeFailure := errors.New("enumeration failed"), errors.New("close failed")
			operations := defaultReadonlyIndexOperations()
			operations.readNames = func(file *os.File, limit int) ([]string, error) {
				cancel()
				if backing {
					return []string{"sharedindex."}, readFailure
				}
				return nil, errors.Join(io.EOF, readFailure)
			}
			operations.close = func(file *os.File) error {
				return errors.Join(file.Close(), closeFailure)
			}
			err := rejectSplitIndexDirectory(ctx, readonlyIndexCanonicalTemporaryDirectory(test), operations)
			if !errors.Is(err, readFailure) || !errors.Is(err, closeFailure) || !errors.Is(err, context.Canceled) || errors.Is(err, errors.ErrUnsupported) != backing {
				test.Fatalf("combined failure error = %v; want read, close, cancellation and backing=%t", err, backing)
			}
		})
	}
}

func TestReadonlyIndexDirectoryStopsAtBackingName(test *testing.T) {
	for _, name := range []string{"sharedindex.", "SHAREDINDEX.", "\u017fharedindex.orphan"} {
		test.Run(name, func(test *testing.T) {
			operations := defaultReadonlyIndexOperations()
			calls := 0
			operations.readNames = func(file *os.File, limit int) ([]string, error) {
				calls++
				if calls != 1 {
					test.Fatal("continued enumeration after a backing entry")
				}
				return []string{name}, nil
			}
			if err := rejectSplitIndexDirectory(test.Context(), readonlyIndexCanonicalTemporaryDirectory(test), operations); !errors.Is(err, errors.ErrUnsupported) || calls != 1 {
				test.Fatalf("backing enumeration calls %d, error %v", calls, err)
			}
		})
	}
}

func TestReadonlyIndexDirectoryCancellationWithoutOtherErrors(test *testing.T) {
	for _, stage := range []string{"open", "read", "close"} {
		test.Run(stage, func(test *testing.T) {
			ctx, cancel := context.WithCancel(test.Context())
			defer cancel()
			operations := defaultReadonlyIndexOperations()
			nativeOpen := operations.open
			operations.open = func(path string) (*os.File, error) {
				file, err := nativeOpen(path)
				if stage == "open" {
					cancel()
				}
				return file, err
			}
			readCalls, closeCalls := 0, 0
			operations.readNames = func(file *os.File, limit int) ([]string, error) {
				readCalls++
				if stage == "read" {
					cancel()
					return []string{"ordinary-entry"}, nil
				}
				return file.Readdirnames(limit)
			}
			operations.close = func(file *os.File) error {
				closeCalls++
				if stage == "close" {
					cancel()
				}
				return file.Close()
			}
			err := rejectSplitIndexDirectory(ctx, readonlyIndexCanonicalTemporaryDirectory(test), operations)
			if !errors.Is(err, context.Canceled) || closeCalls != 1 || stage == "open" && readCalls != 0 || stage != "open" && readCalls != 1 {
				test.Fatalf("cancellation: reads %d, closes %d, error %v", readCalls, closeCalls, err)
			}
		})
	}
}

func TestReadonlyIndexDirectoryRejectsMissingNativeObservations(test *testing.T) {
	for _, stage := range []string{"lstat", "open", "stat"} {
		test.Run(stage, func(test *testing.T) {
			operations := defaultReadonlyIndexOperations()
			switch stage {
			case "lstat":
				operations.lstat = func(string) (fs.FileInfo, error) { return nil, nil }
			case "open":
				operations.open = func(string) (*os.File, error) { return nil, nil }
			case "stat":
				operations.stat = func(*os.File) (fs.FileInfo, error) { return nil, nil }
			}
			operations.readNames = func(*os.File, int) ([]string, error) {
				test.Fatal("enumerated names without a native root observation")
				return nil, nil
			}
			if err := rejectSplitIndexDirectory(test.Context(), readonlyIndexCanonicalTemporaryDirectory(test), operations); err == nil || errors.Is(err, errors.ErrUnsupported) {
				test.Fatalf("missing native observation error = %v", err)
			}
		})
	}
}

func readonlyIndexCanonicalTemporaryDirectory(test *testing.T) string {
	test.Helper()
	directory, err := filepath.EvalSymlinks(test.TempDir())
	if err != nil {
		test.Fatal(err)
	}
	return directory
}
