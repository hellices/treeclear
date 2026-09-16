//go:build darwin || linux

package git

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestReadonlyIndexInitialObservationRejectsNativeDisagreement(test *testing.T) {
	for _, field := range []string{"identity", "mode", "size"} {
		test.Run(field, func(test *testing.T) {
			fixture := readonlyIndexCanonicalTemporaryDirectory(test)
			directory := filepath.Join(fixture, "admin")
			if err := os.Mkdir(directory, 0o700); err != nil {
				test.Fatal(err)
			}
			operations := defaultReadonlyIndexOperations()
			var initial, pinned fs.FileInfo
			operations.lstat = func(path string) (fs.FileInfo, error) {
				information, err := os.Lstat(path)
				initial = information
				return information, err
			}
			nativeOpen := operations.open
			operations.open = func(path string) (*os.File, error) {
				switch field {
				case "identity":
					replacement := filepath.Join(fixture, "replacement")
					if err := os.Mkdir(replacement, initial.Mode().Perm()); err != nil {
						test.Fatal(err)
					}
					if err := os.Chtimes(replacement, initial.ModTime(), initial.ModTime()); err != nil {
						test.Fatal(err)
					}
					if err := os.Rename(directory, filepath.Join(fixture, "moved")); err != nil {
						test.Fatal(err)
					}
					if err := os.Rename(replacement, directory); err != nil {
						test.Fatal(err)
					}
				case "mode":
					if err := os.Chmod(directory, initial.Mode().Perm()|0o050); err != nil {
						test.Fatal(err)
					}
				case "size":
					for entry := range maxAdminEntries {
						name := fmt.Sprintf("%04d-native-directory-size-fixture", entry)
						if err := os.WriteFile(filepath.Join(directory, name), nil, 0o600); err != nil {
							test.Fatal(err)
						}
						current, err := os.Lstat(directory)
						if err != nil {
							test.Fatal(err)
						}
						if current.Size() != initial.Size() {
							break
						}
					}
					if err := os.Chtimes(directory, initial.ModTime(), initial.ModTime()); err != nil {
						test.Fatal(err)
					}
				}
				return nativeOpen(path)
			}
			operations.stat = func(file *os.File) (fs.FileInfo, error) {
				information, err := file.Stat()
				pinned = information
				return information, err
			}
			observed, err := readonlyIndexDirectoryInfoWithOperations(directory, operations)
			if initial == nil || pinned == nil || !initial.ModTime().Equal(pinned.ModTime()) {
				test.Fatal("native disagreement fixture must retain mtime")
			}
			if os.SameFile(initial, pinned) != (field != "identity") || (initial.Mode() == pinned.Mode()) != (field != "mode") || (initial.Size() == pinned.Size()) != (field != "size") {
				test.Fatalf("native fixture did not isolate %s disagreement", field)
			}
			if !errors.Is(err, ErrWorktreeChanged) || observed != nil {
				test.Errorf("initial native %s disagreement accepted: observation %v, error %v", field, observed, err)
			}
		})
	}
}
