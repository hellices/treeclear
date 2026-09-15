package snapshot

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureSourceUnitRootObservationSubstitution(test *testing.T) {
	for _, rootName := range []string{"repository", "common", "worktree", "admin"} {
		for _, boundary := range []string{"first-list", "second-list", "final-untracked"} {
			test.Run(rootName+"/"+boundary, func(test *testing.T) {
				fixture := newSourceUnitFixture(test)
				roots := map[string]string{"repository": fixture.expected.RepositoryRoot, "common": fixture.expected.CommonGitDir, "worktree": fixture.expected.Path, "admin": fixture.expected.AdminDir}
				root := roots[rootName]
				replacement, _ := sourceUnitRootCopy(test, root)
				changed := false
				fixture.before = func(name string, pass int) error {
					if sourceUnitRootBoundary(boundary, name, pass) {
						changed = true
					}
					return nil
				}
				readers := fixture.readers()
				open := readers.roots.open
				observations := 0
				readers.roots.open = func(path string) (*os.File, error) {
					if changed && path == root {
						observations++
						return open(replacement)
					}
					return open(path)
				}
				actual, err := captureSource(test.Context(), sourceUnitGit{fixture}, fixture.expected, 1<<20, readers)
				assertSourceUnitFailure(test, actual, err, ErrSourceChanged)
				if !changed || observations == 0 {
					test.Fatal("substituted root observation was not exercised")
				}
			})
		}
	}
}

func sourceUnitRootBoundary(boundary, name string, pass int) bool {
	return boundary == "first-list" && name == "list" && pass == 1 || boundary == "second-list" && name == "list" && pass == 2 || boundary == "final-untracked" && name == "untracked" && pass == 2
}

func sourceUnitRootCopy(test *testing.T, root string) (string, fs.FileInfo) {
	test.Helper()
	original, err := os.Stat(root)
	if err != nil {
		test.Fatal(err)
	}
	replacement := filepath.Join(test.TempDir(), "replacement")
	if err := os.CopyFS(replacement, os.DirFS(root)); err != nil {
		test.Fatal(err)
	}
	if err := os.Chmod(replacement, original.Mode().Perm()); err != nil {
		test.Fatal(err)
	}
	if err := os.Chtimes(replacement, original.ModTime(), original.ModTime()); err != nil {
		test.Fatal(err)
	}
	copied, err := os.Stat(replacement)
	if err != nil {
		test.Fatal(err)
	}
	if os.SameFile(original, copied) || original.Mode() != copied.Mode() || original.Size() != copied.Size() || !original.ModTime().Equal(copied.ModTime()) {
		test.Fatal("root copy must have distinct native identity and matching mode, size and mtime")
	}
	return replacement, original
}

func sourceUnitRootRenameControl(test *testing.T, root string) {
	test.Helper()
	if err := os.Rename(root, root+"-original"); err != nil {
		test.Fatalf("root move without capture handles: %v", err)
	}
	if err := os.Rename(root+"-original", root); err != nil {
		test.Fatalf("restore root after rename control: %v", err)
	}
}
