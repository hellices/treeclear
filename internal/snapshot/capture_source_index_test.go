//go:build darwin || linux || windows

package snapshot

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hellices/treeclear/internal/git"
)

func TestCaptureSourceNativeOrdinaryIndexPreservation(test *testing.T) {
	for _, refresh := range []string{"default", "true", "false"} {
		test.Run(refresh, func(test *testing.T) {
			fixture := newCaptureNativeFixture(test, false, false)
			if refresh != "default" {
				fixture.repository.Git(test, "config", "diff.autoRefreshIndex", refresh)
			}
			trackedPath := filepath.Join(fixture.worktree, "seed.txt")
			file, err := os.Open(trackedPath)
			if err != nil {
				test.Fatal(err)
			}
			original, statErr := file.Stat()
			if err := errors.Join(statErr, file.Close()); err != nil {
				test.Fatal(err)
			}
			contents, err := os.ReadFile(trackedPath)
			if err != nil {
				test.Fatal(err)
			}
			replacement := filepath.Join(fixture.worktree, "owned-editor-replacement")
			if err := os.WriteFile(replacement, contents, 0o600); err != nil {
				test.Fatal(err)
			}
			if err := os.Rename(replacement, trackedPath); err != nil {
				test.Fatal(err)
			}
			replaced, err := os.Stat(trackedPath)
			if err != nil || os.SameFile(original, replaced) {
				test.Fatalf("editor fixture did not replace the tracked inode: %v", err)
			}
			fixture.expected = captureNativeInspect(test, fixture.repository, fixture.worktree)
			if !fixture.expected.Status.Clean() {
				test.Fatalf("stat-only replacement is not content-clean: %#v", fixture.expected.Status)
			}
			shared, err := filepath.Glob(filepath.Join(fixture.expected.AdminDir, "sharedindex.*"))
			if err != nil || len(shared) != 0 {
				test.Fatalf("ordinary-index fixture has shared backing: %q, %v", shared, err)
			}
			before := fixture.state(test)
			defer fixture.assertState(test, before)
			actual, err := CaptureSource(test.Context(), git.NewClient(nil), fixture.expected, 1<<20)
			if err != nil || reflect.DeepEqual(actual, SourceCapture{}) {
				test.Errorf("stable ordinary-index source capture failed: %v", err)
			}
		})
	}
}
