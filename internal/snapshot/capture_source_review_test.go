//go:build darwin || linux || windows

package snapshot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/git"
)

func TestCaptureSourceNativeCommonDirectoryConflict(test *testing.T) {
	fixture := newCaptureNativeFixture(test, true, false)
	alternate := filepath.Join(test.TempDir(), "other-common")
	if err := os.CopyFS(alternate, os.DirFS(fixture.expected.CommonGitDir)); err != nil {
		test.Fatal(err)
	}
	alternate, err := filepath.EvalSymlinks(alternate)
	if err != nil {
		test.Fatal(err)
	}
	changed := false
	var afterChange captureNativeState
	indexCommands := 0
	runner := &captureNativeRunner{}
	runner.before = func(request execx.Request) error {
		if !changed && request.Directory == fixture.worktree && request.Args[0] == "rev-parse" && slices.Contains(request.Args, "--git-common-dir") {
			captureNativeWrite(test, fixture.expected.AdminDir, "commondir", []byte(alternate+"\n"))
			changed = true
			afterChange = fixture.state(test)
		}
		if slices.Contains([]string{"ls-files", "status", "diff"}, request.Args[0]) {
			indexCommands++
		}
		return nil
	}
	actual, err := CaptureSource(test.Context(), git.NewClient(runner), fixture.expected, 1<<20)
	if !changed || indexCommands != 0 || !errors.Is(err, ErrSourceInvalid) || !errors.Is(err, ErrSourceChanged) || !errors.Is(err, git.ErrWorktreeChanged) || !reflect.DeepEqual(actual, SourceCapture{}) {
		test.Fatalf("effective common conflict was not rejected before index reads: changed=%t reads=%d error=%v", changed, indexCommands, err)
	}
	fixture.assertState(test, afterChange)
}

func TestCaptureSourceNativeSplitIndexPreservation(test *testing.T) {
	fixture := newCaptureNativeFixture(test, false, false)
	fixture.repository.Git(test, "-C", fixture.worktree, "update-index", "--split-index")
	shared, err := filepath.Glob(filepath.Join(fixture.expected.AdminDir, "sharedindex.*"))
	if err != nil || len(shared) != 1 {
		test.Fatalf("split-index fixture: %q, %v", shared, err)
	}
	before := fixture.state(test)
	defer fixture.assertState(test, before)
	indexCommands := 0
	runner := &captureNativeRunner{}
	runner.before = func(request execx.Request) error {
		if slices.Contains([]string{"ls-files", "status", "diff"}, request.Args[0]) {
			indexCommands++
		}
		return nil
	}
	actual, err := CaptureSource(test.Context(), git.NewClient(runner), fixture.expected, 1<<20)
	if indexCommands != 0 || !errors.Is(err, errors.ErrUnsupported) || !errors.Is(err, ErrSourceInvalid) || !reflect.DeepEqual(actual, SourceCapture{}) {
		test.Fatalf("split index was not rejected before native index reads: reads=%d error=%v", indexCommands, err)
	}
}

func TestCaptureSourceNativeInspectionChangeCause(test *testing.T) {
	fixture := newCaptureNativeFixture(test, true, false)
	changed := false
	var afterChange captureNativeState
	runner := &captureNativeRunner{}
	runner.before = func(request execx.Request) error {
		if !changed && request.Args[0] == "rev-parse" && slices.Contains(request.Args, "--verify") && slices.Contains(request.Args, "HEAD") {
			fixture.repository.Git(test, "-C", fixture.worktree, "commit", "--allow-empty", "-m", "Changed capture fixture HEAD")
			changed = true
			afterChange = fixture.state(test)
		}
		return nil
	}
	actual, err := CaptureSource(test.Context(), git.NewClient(runner), fixture.expected, 1<<20)
	if !changed || !errors.Is(err, git.ErrWorktreeChanged) || !errors.Is(err, ErrSourceChanged) || !errors.Is(err, ErrSourceInvalid) || !reflect.DeepEqual(actual, SourceCapture{}) {
		test.Fatalf("native inspection change lost its typed cause: changed=%t error=%v", changed, err)
	}
	fixture.assertState(test, afterChange)
}

func TestCaptureSourceUnitNativeChangeClassification(test *testing.T) {
	for _, scenario := range []struct {
		name    string
		failure error
		changed bool
	}{
		{"native", git.ErrWorktreeChanged, true},
		{"wrapped", fmt.Errorf("observed state: %w", git.ErrWorktreeChanged), true},
		{"opaque text", errors.New(git.ErrWorktreeChanged.Error()), false},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			fixture := newSourceUnitFixture(test)
			fixture.before = func(name string, pass int) error {
				if name == "inspect" {
					return scenario.failure
				}
				return nil
			}
			actual, err := fixture.capture(test.Context(), 1<<20)
			assertSourceUnitFailure(test, actual, err, scenario.failure)
			if errors.Is(err, ErrSourceChanged) != scenario.changed {
				test.Fatalf("native change cause classification = %v", err)
			}
		})
	}
}
