//go:build darwin || linux || windows

package snapshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/git"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestCaptureSourceNativeComposition(test *testing.T) {
	for _, scenario := range []struct {
		name    string
		dirty   bool
		symlink bool
		locked  bool
	}{
		{name: "clean"},
		{name: "dirty", dirty: true},
		{name: "native_symlink", dirty: true, symlink: true},
		{name: "locked_current_read_only", locked: true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			if scenario.symlink && runtime.GOOS == "windows" {
				test.Skip("Unix symlink fixture; portable dirty and clean capture run natively on Windows")
			}
			fixture := newCaptureNativeFixture(test, scenario.dirty, scenario.symlink)
			if scenario.locked {
				fixture.repository.Git(test, "worktree", "lock", "--reason", "Owned capture fixture", "--", fixture.worktree)
				fixture.expected = captureNativeInspect(test, fixture.repository, fixture.worktree)
				fixture.expected.Current = true
				fixture.expected.EstimatedBytes = 123
			}
			before := fixture.state(test)
			test.Cleanup(func() { fixture.assertState(test, before) })
			control := git.NewClient(nil)
			_, worktreeRaw, err := control.ListWorktreesRaw(test.Context(), fixture.repository.Root)
			if err != nil {
				test.Fatal(err)
			}
			status, err := control.StatusSnapshot(test.Context(), fixture.worktree)
			if err != nil {
				test.Fatal(err)
			}
			staged, err := control.Diff(test.Context(), fixture.worktree, true)
			if err != nil {
				test.Fatal(err)
			}
			unstaged, err := control.Diff(test.Context(), fixture.worktree, false)
			if err != nil {
				test.Fatal(err)
			}
			wantedPaths := []string(nil)
			wantedNames := []string(nil)
			if scenario.dirty {
				wantedPaths = []string{"assets/nested/payload 한글.bin", "notes.txt"}
				if scenario.symlink {
					wantedPaths = append(wantedPaths, "assets/nested/missing-link")
					slices.Sort(wantedPaths)
				}
				wantedNames = append(slices.Clone(wantedPaths), "assets", "assets/nested")
				if !bytes.Contains(staged, []byte("GIT binary patch")) || !bytes.Contains(unstaged, []byte("GIT binary patch")) {
					test.Fatal("fixture did not produce both binary patches")
				}
			}
			if !slices.Equal(status.UntrackedPaths, wantedPaths) {
				test.Fatalf("native selected paths = %q, want %q", status.UntrackedPaths, wantedPaths)
			}
			wantedEntries := untrackedReadIntegrationEntries(test, fixture.worktree, wantedNames)
			runner := &captureNativeRunner{}
			value, err := CaptureSource(test.Context(), git.NewClient(runner), fixture.expected, 1<<20)
			if err != nil {
				test.Fatal(err)
			}
			if !bytes.Equal(value.WorktreeList, worktreeRaw) || !reflect.DeepEqual(value.Status, status) ||
				!bytes.Equal(value.StagedPatch, staged) || !bytes.Equal(value.UnstagedPatch, unstaged) {
				test.Fatal("capture did not retain the exact native Git observations")
			}
			if !reflect.DeepEqual(value.AdministrativeEntries, before.administrative) ||
				!slices.EqualFunc(value.UntrackedEntries, wantedEntries, func(left, right UntrackedEntry) bool {
					return reflect.DeepEqual(left, right)
				}) {
				test.Fatal("capture changed selected bytes, kinds, modes, parent directories or link text")
			}
			if runner.lists != 2 || runner.diffs != 4 {
				test.Fatalf("capture used %d list and %d diff commands, want two complete bounded passes", runner.lists, runner.diffs)
			}
			exactBytes := captureNativeBytes(value)
			if _, err := CaptureSource(test.Context(), control, fixture.expected, exactBytes); err != nil {
				test.Fatalf("exact source-content byte boundary rejected: %v", err)
			}
			limited, err := CaptureSource(test.Context(), control, fixture.expected, exactBytes-1)
			if !errors.Is(err, ErrSourceInvalid) || !errors.Is(err, ErrSourceLimit) || !reflect.DeepEqual(limited, SourceCapture{}) {
				test.Fatalf("one-byte source excess returned usable data or wrong errors: %v", err)
			}
		})
	}
}

func TestCaptureSourceNativeRejectsMidCaptureChanges(test *testing.T) {
	for _, scenario := range []struct {
		name    string
		symlink bool
		mutate  func(*testing.T, captureNativeFixture)
	}{
		{name: "same_size_untracked_bytes", mutate: func(test *testing.T, fixture captureNativeFixture) {
			captureNativeWrite(test, fixture.worktree, "notes.txt", []byte("changed notes\n"))
		}},
		{name: "same_count_untracked_selection", mutate: func(test *testing.T, fixture captureNativeFixture) {
			if err := os.Rename(filepath.Join(fixture.worktree, "notes.txt"), filepath.Join(fixture.worktree, "renamed.txt")); err != nil {
				test.Fatal(err)
			}
		}},
		{name: "same_count_unstaged_patch", mutate: func(test *testing.T, fixture captureNativeFixture) {
			captureNativeWrite(test, fixture.worktree, "tracked.bin", bytes.Repeat([]byte{0, 9, 0xf9, '\r'}, 257))
		}},
		{name: "staged_index", mutate: func(test *testing.T, fixture captureNativeFixture) {
			fixture.repository.Git(test, "-C", fixture.worktree, "add", "--", "tracked.bin")
		}},
		{name: "head", mutate: func(test *testing.T, fixture captureNativeFixture) {
			fixture.repository.Git(test, "-C", fixture.worktree, "commit", "--allow-empty", "-m", "Controlled mid-capture change")
		}},
		{name: "administrative_bytes", mutate: func(test *testing.T, fixture captureNativeFixture) {
			captureNativeWrite(test, fixture.expected.AdminDir, "diagnostic-note", []byte("controlled metadata change\n"))
		}},
		{name: "symlink_target", symlink: true, mutate: func(test *testing.T, fixture captureNativeFixture) {
			filename := filepath.Join(fixture.worktree, "assets", "nested", "missing-link")
			if err := os.Remove(filename); err != nil {
				test.Fatal(err)
			}
			if err := os.Symlink("../changed.txt", filename); err != nil {
				test.Fatal(err)
			}
		}},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			if scenario.symlink && runtime.GOOS == "windows" {
				test.Skip("Unix symlink mutation; all portable mutation controls run on Windows")
			}
			fixture := newCaptureNativeFixture(test, true, scenario.symlink)
			mutated := false
			var afterMutation captureNativeState
			runner := &captureNativeRunner{}
			runner.before = func(request execx.Request) error {
				if request.Args[0] == "worktree" && runner.lists == 2 {
					scenario.mutate(test, fixture)
					mutated = true
					afterMutation = fixture.state(test)
				}
				return nil
			}
			value, err := CaptureSource(test.Context(), git.NewClient(runner), fixture.expected, 1<<20)
			if !mutated || runner.lists != 2 {
				test.Fatal("capture did not reach the deterministic second-pass mutation")
			}
			if !errors.Is(err, ErrSourceInvalid) || !errors.Is(err, ErrSourceChanged) || !reflect.DeepEqual(value, SourceCapture{}) {
				test.Fatalf("changed native source returned usable data or wrong errors: %v", err)
			}
			fixture.assertState(test, afterMutation)
		})
	}
}

func TestCaptureSourceNativeLateCancellationAndReadFailure(test *testing.T) {
	for _, cancelOperation := range []bool{false, true} {
		test.Run(fmt.Sprintf("cancel=%t", cancelOperation), func(test *testing.T) {
			fixture := newCaptureNativeFixture(test, true, false)
			before := fixture.state(test)
			test.Cleanup(func() { fixture.assertState(test, before) })
			ctx, cancel := context.WithCancel(test.Context())
			defer cancel()
			failure := errors.New("injected second-pass Git read failure")
			runner := &captureNativeRunner{}
			reached := false
			runner.after = func(request execx.Request) error {
				if request.Args[0] == "diff" && runner.diffs == 4 {
					reached = true
					if cancelOperation {
						cancel()
						return nil
					}
					return failure
				}
				return nil
			}
			value, err := CaptureSource(ctx, git.NewClient(runner), fixture.expected, 1<<20)
			wanted := failure
			if cancelOperation {
				wanted = context.Canceled
			}
			if !reached || !errors.Is(err, wanted) || !errors.Is(err, ErrSourceInvalid) || !reflect.DeepEqual(value, SourceCapture{}) {
				test.Fatalf("late native error/cancellation lost its cause or retained partial capture: reached=%t, error=%v", reached, err)
			}
		})
	}
}

type captureNativeFixture struct {
	repository *testutil.Repository
	worktree   string
	expected   domain.Worktree
}

type captureNativeState struct {
	sources        []UntrackedEntry
	administrative []AdminEntry
	modified       map[string]time.Time
	references     string
}

func newCaptureNativeFixture(test *testing.T, dirty, symlink bool) captureNativeFixture {
	test.Helper()
	repository := testutil.NewRepository(test)
	captureNativeWrite(test, repository.Root, ".gitignore", []byte("ignored.tmp\nignored-tree/\n"))
	captureNativeWrite(test, repository.Root, "tracked.bin", bytes.Repeat([]byte{0, 1, 0xff, '\r', '\n'}, 257))
	repository.Git(test, "add", "--", ".gitignore", "tracked.bin")
	repository.Git(test, "commit", "-m", "Add native capture fixtures")
	worktree := repository.AddWorktree(test, "source capture 한글", "topic/source-capture")
	captureNativeWrite(test, worktree, "ignored.tmp", []byte("ignored content\n"))
	if dirty {
		captureNativeWrite(test, worktree, "tracked.bin", bytes.Repeat([]byte{0, 2, 0xfe, '\n'}, 257))
		repository.Git(test, "-C", worktree, "add", "--", "tracked.bin")
		captureNativeWrite(test, worktree, "tracked.bin", bytes.Repeat([]byte{0, 3, 0xfd, '\r'}, 257))
		for _, directory := range []string{"assets/nested", "ignored-tree"} {
			if err := os.MkdirAll(filepath.Join(worktree, filepath.FromSlash(directory)), 0o750); err != nil {
				test.Fatal(err)
			}
		}
		captureNativeWrite(test, worktree, "assets/nested/payload 한글.bin", []byte{0, 4, 0xfc, '\r', '\n'})
		captureNativeWrite(test, worktree, "notes.txt", []byte("initial notes\n"))
		captureNativeWrite(test, worktree, "ignored-tree/cache.bin", []byte{0, 5, 0xfb})
		if symlink {
			if err := os.Symlink("../missing.txt", filepath.Join(worktree, "assets", "nested", "missing-link")); err != nil {
				test.Fatal(err)
			}
		}
	}
	return captureNativeFixture{repository: repository, worktree: worktree, expected: captureNativeInspect(test, repository, worktree)}
}

func captureNativeInspect(test *testing.T, repository *testutil.Repository, worktree string) domain.Worktree {
	test.Helper()
	client := git.NewClient(nil)
	listed, err := client.ListWorktrees(test.Context(), repository.Root)
	if err != nil {
		test.Fatal(err)
	}
	for _, worktreeRecord := range listed {
		if worktreeRecord.Path == worktree {
			worktreeRecord.PathSafe = true
			expected, err := client.InspectWorktree(test.Context(), repository.Root, worktreeRecord)
			if err != nil || !expected.GitStateKnown {
				test.Fatalf("native expected source inspection: %v", err)
			}
			return expected
		}
	}
	test.Fatal("native linked fixture was not listed")
	return domain.Worktree{}
}

func captureNativeWrite(test *testing.T, directory, name string, contents []byte) {
	test.Helper()
	if err := os.WriteFile(filepath.Join(directory, filepath.FromSlash(name)), contents, 0o600); err != nil {
		test.Fatal(err)
	}
}

func (fixture captureNativeFixture) state(test *testing.T) captureNativeState {
	test.Helper()
	modified := make(map[string]time.Time)
	if err := filepath.WalkDir(fixture.expected.AdminDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		information, err := entry.Info()
		if err != nil {
			return err
		}
		modified[path] = information.ModTime()
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	return captureNativeState{
		sources:        verifyBundleNativeSourceEntries(test, fixture.worktree),
		administrative: gitManifestFixture(test, fixture.repository, fixture.worktree).AdministrativeEntries,
		modified:       modified,
		references:     fixture.repository.Git(test, "for-each-ref", "--format=%(refname) %(objectname)"),
	}
}

func (fixture captureNativeFixture) assertState(test *testing.T, wanted captureNativeState) {
	test.Helper()
	if actual := fixture.state(test); !reflect.DeepEqual(actual, wanted) {
		test.Error("capture changed source bytes/modes/links, ignored content, administrative/index bytes or mtimes, or Git refs")
	}
}

func captureNativeBytes(value SourceCapture) int64 {
	total := int64(len(value.WorktreeList) + len(value.Status.Raw) + len(value.StagedPatch) + len(value.UnstagedPatch))
	for _, entry := range value.AdministrativeEntries {
		total += int64(len(entry.Data))
	}
	for _, entry := range value.UntrackedEntries {
		total += int64(len(entry.Data) + len(entry.LinkTarget))
	}
	return total
}

type captureNativeRunner struct {
	lists  int
	diffs  int
	before func(execx.Request) error
	after  func(execx.Request) error
}

func (runner *captureNativeRunner) Run(ctx context.Context, request execx.Request) (execx.Result, error) {
	if len(request.Args) == 0 {
		return execx.Result{}, errors.New("native capture runner requires a Git command")
	}
	switch request.Args[0] {
	case "worktree":
		if len(request.Args) < 2 || request.Args[1] != "list" {
			return execx.Result{}, errors.New("capture attempted a worktree mutation")
		}
		runner.lists++
	case "diff":
		runner.diffs++
	case "config":
		if !slices.Contains(request.Args, "--get-regexp") {
			return execx.Result{}, errors.New("capture attempted a config mutation")
		}
	case "rev-parse", "ls-files", "status", "symbolic-ref", "for-each-ref", "show":
	default:
		return execx.Result{}, fmt.Errorf("unexpected capture command %q", request.Args[0])
	}
	if runner.before != nil {
		if err := runner.before(request); err != nil {
			return execx.Result{}, err
		}
	}
	result, err := (execx.OSRunner{}).Run(ctx, request)
	if err == nil && runner.after != nil {
		err = runner.after(request)
	}
	return result, err
}
