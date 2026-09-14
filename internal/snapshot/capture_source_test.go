package snapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/git"
	"github.com/hellices/treeclear/internal/pathutil"
)

type sourceUnitFixture struct {
	test      *testing.T
	expected  domain.Worktree
	listed    []domain.Worktree
	inspected domain.Worktree
	contents  SourceCapture
	calls     []string
	pass      int
	before    func(string, int) error
}

type sourceUnitGit struct{ fixture *sourceUnitFixture }

func newSourceUnitFixture(test *testing.T) *sourceUnitFixture {
	test.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		test.Skip("guarded source roots require a supported native platform")
	}
	root, err := pathutil.Canonical(test.TempDir())
	if err != nil {
		test.Fatal(err)
	}
	repository := filepath.Join(root, "repository")
	common := filepath.Join(repository, ".git")
	admin := filepath.Join(common, "worktrees", "topic")
	worktree := filepath.Join(root, "topic")
	for _, directory := range []string{repository, common, admin, worktree} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			test.Fatal(err)
		}
	}
	index := []byte{0, 0xff, 'i', 'n', 'd', 'e', 'x'}
	expected := domain.Worktree{
		Path: worktree, RepositoryRoot: repository, CommonGitDir: common, AdminDir: admin,
		Head: strings.Repeat("a", 40), Branch: "topic", Upstream: "refs/remotes/origin/topic",
		PathSafe: true, GitStateKnown: true, Recoverable: true,
		Status:    domain.GitStatus{Staged: 1, Unstaged: 1, Untracked: 2},
		IndexHash: fmt.Sprintf("%x", sha256.Sum256(index)), AdminHash: strings.Repeat("b", 64),
		LastCommitAt: time.Unix(1234567890, 0).UTC(), MetadataModifiedAt: time.Unix(1234567891, 7).UTC(),
	}
	listed := []domain.Worktree{
		{Path: repository, RepositoryRoot: repository, CommonGitDir: common, Head: expected.Head, Branch: "main", Primary: true},
		{Path: worktree, RepositoryRoot: repository, CommonGitDir: common, Head: expected.Head, Branch: expected.Branch},
	}
	return &sourceUnitFixture{
		test: test, expected: expected, inspected: expected, listed: listed,
		contents: SourceCapture{
			WorktreeList: []byte("worktree " + repository + "\x00HEAD " + expected.Head + "\x00branch refs/heads/main\x00\x00worktree " + worktree + "\x00HEAD " + expected.Head + "\x00branch refs/heads/topic\x00\x00"),
			Status: git.StatusSnapshot{
				Status: expected.Status, Raw: []byte("? nested/data.bin\x00? link\x00"),
				UntrackedPaths: []string{"nested/data.bin", "link"},
			},
			StagedPatch: []byte("staged\x00\xff\r\n"), UnstagedPatch: []byte("unstaged\x00\xfe\n"),
			AdministrativeEntries: []AdminEntry{
				{Path: ".", Kind: "directory", Mode: fs.ModeDir | 0o700},
				{Path: "HEAD", Kind: "file", Mode: 0o600, Data: []byte("ref: refs/heads/topic\n")},
				{Path: "commondir", Kind: "file", Mode: 0o600, Data: []byte("../..\n")},
				{Path: "gitdir", Kind: "file", Mode: 0o600, Data: []byte(worktree + "/.git\n")},
				{Path: "index", Kind: "file", Mode: 0o600, Data: index},
			},
			UntrackedEntries: []UntrackedEntry{
				{Path: "link", Kind: "symlink", Mode: fs.ModeSymlink | 0o777, LinkTarget: "nested/data.bin"},
				{Path: "nested", Kind: "directory", Mode: fs.ModeDir | 0o750},
				{Path: "nested/data.bin", Kind: "file", Mode: 0o640, Data: []byte{0, 0xff, 1}},
			},
		},
	}
}

func (fixture *sourceUnitFixture) call(name string) error {
	fixture.calls = append(fixture.calls, name)
	if name == "list" {
		fixture.pass++
	}
	if fixture.before != nil {
		return fixture.before(name, fixture.pass)
	}
	return nil
}

func (client sourceUnitGit) ListWorktreesRaw(ctx context.Context, repository string) ([]domain.Worktree, []byte, error) {
	fixture := client.fixture
	if repository != fixture.expected.RepositoryRoot {
		fixture.test.Fatalf("list repository = %q", repository)
	}
	err := fixture.call("list")
	return fixture.listed, fixture.contents.WorktreeList, err
}

func (client sourceUnitGit) InspectWorktree(ctx context.Context, repository string, listed domain.Worktree) (domain.Worktree, error) {
	fixture := client.fixture
	if repository != fixture.expected.RepositoryRoot || listed.Path != fixture.expected.Path || listed.Head != fixture.listed[1].Head || listed.GitStateKnown || listed.AdminDir != "" {
		fixture.test.Fatalf("inspection did not receive the fresh listed target: %#v", listed)
	}
	err := fixture.call("inspect")
	return fixture.inspected, err
}

func (client sourceUnitGit) StatusSnapshot(ctx context.Context, worktree string) (git.StatusSnapshot, error) {
	fixture := client.fixture
	if worktree != fixture.expected.Path {
		fixture.test.Fatalf("status worktree = %q", worktree)
	}
	err := fixture.call("status")
	return fixture.contents.Status, err
}

func (client sourceUnitGit) Diff(ctx context.Context, worktree string, staged bool) ([]byte, error) {
	fixture := client.fixture
	if worktree != fixture.expected.Path {
		fixture.test.Fatalf("diff worktree = %q", worktree)
	}
	if staged {
		err := fixture.call("staged")
		return fixture.contents.StagedPatch, err
	}
	err := fixture.call("unstaged")
	return fixture.contents.UnstagedPatch, err
}

func (fixture *sourceUnitFixture) readers() sourceCaptureReaders {
	readers := defaultSourceCaptureReaders()
	readers.administrative = func(ctx context.Context, directory string) ([]AdminEntry, error) {
		if directory != fixture.expected.AdminDir {
			fixture.test.Fatalf("administrative directory = %q", directory)
		}
		err := fixture.call("administrative")
		return fixture.contents.AdministrativeEntries, err
	}
	readers.untracked = func(ctx context.Context, directory string, paths []string, maximumBytes int64) ([]UntrackedEntry, error) {
		if directory != fixture.expected.Path || !slices.Equal(paths, fixture.contents.Status.UntrackedPaths) {
			fixture.test.Fatalf("untracked request = %q, %q", directory, paths)
		}
		err := fixture.call("untracked")
		return fixture.contents.UntrackedEntries, err
	}
	return readers
}

func (fixture *sourceUnitFixture) capture(ctx context.Context, budget int64) (SourceCapture, error) {
	return captureSource(ctx, sourceUnitGit{fixture}, fixture.expected, budget, fixture.readers())
}

func sourceUnitBytes(capture SourceCapture) int64 {
	total := int64(len(capture.WorktreeList) + len(capture.Status.Raw) + len(capture.StagedPatch) + len(capture.UnstagedPatch))
	for _, entry := range capture.AdministrativeEntries {
		total += int64(len(entry.Data))
	}
	for _, entry := range capture.UntrackedEntries {
		total += int64(len(entry.Data) + len(entry.LinkTarget))
	}
	return total
}

func sourceUnitClone(capture SourceCapture) SourceCapture {
	capture.WorktreeList = bytes.Clone(capture.WorktreeList)
	capture.Status.Raw = bytes.Clone(capture.Status.Raw)
	capture.Status.UntrackedPaths = slices.Clone(capture.Status.UntrackedPaths)
	capture.StagedPatch = bytes.Clone(capture.StagedPatch)
	capture.UnstagedPatch = bytes.Clone(capture.UnstagedPatch)
	capture.AdministrativeEntries = slices.Clone(capture.AdministrativeEntries)
	for index := range capture.AdministrativeEntries {
		capture.AdministrativeEntries[index].Data = bytes.Clone(capture.AdministrativeEntries[index].Data)
	}
	capture.UntrackedEntries = slices.Clone(capture.UntrackedEntries)
	for index := range capture.UntrackedEntries {
		capture.UntrackedEntries[index].Data = bytes.Clone(capture.UntrackedEntries[index].Data)
	}
	return capture
}

func assertSourceUnitFailure(test *testing.T, actual SourceCapture, err error, causes ...error) {
	test.Helper()
	if !reflect.DeepEqual(actual, SourceCapture{}) || !errors.Is(err, ErrSourceInvalid) {
		test.Fatalf("failure returned partial capture or lost invalid sentinel: %#v, %v", actual, err)
	}
	for _, cause := range causes {
		if !errors.Is(err, cause) {
			test.Fatalf("error %v does not preserve %v", err, cause)
		}
	}
}

func TestCaptureSourceUnitRootOperationFailures(test *testing.T) {
	for _, operation := range []string{"open", "stat", "close"} {
		baseline := newSourceUnitFixture(test)
		baselineReaders := baseline.readers()
		var count int
		original := baselineReaders.roots
		switch operation {
		case "open":
			baselineReaders.roots.open = func(directory string) (*os.File, error) { count++; return original.open(directory) }
		case "stat":
			baselineReaders.roots.stat = func(file *os.File) (fs.FileInfo, error) { count++; return original.stat(file) }
		case "close":
			baselineReaders.roots.close = func(file *os.File) error { count++; return original.close(file) }
		}
		if _, err := captureSource(test.Context(), sourceUnitGit{baseline}, baseline.expected, 1<<20, baselineReaders); err != nil {
			test.Fatal(err)
		}
		for failedCall := 1; failedCall <= count; failedCall++ {
			for _, canceled := range []bool{false, true} {
				test.Run(fmt.Sprintf("%s/%d/cancel-%v", operation, failedCall, canceled), func(test *testing.T) {
					fixture := newSourceUnitFixture(test)
					ctx, cancel := context.WithCancel(test.Context())
					defer cancel()
					readers := fixture.readers()
					original := readers.roots
					var observed int
					failed := false
					opened := make(map[*os.File]int)
					event := func(name string) error {
						if failed && name != "close" {
							test.Fatalf("root operation %s continued after failure", name)
						}
						if name != operation {
							return nil
						}
						observed++
						if observed != failedCall {
							return nil
						}
						failed = true
						if canceled {
							cancel()
						}
						return fs.ErrPermission
					}
					readers.roots.open = func(directory string) (*os.File, error) {
						file, err := original.open(directory)
						if file != nil {
							opened[file] = 0
						}
						return file, errors.Join(err, event("open"))
					}
					readers.roots.stat = func(file *os.File) (fs.FileInfo, error) {
						information, err := original.stat(file)
						return information, errors.Join(err, event("stat"))
					}
					readers.roots.close = func(file *os.File) error {
						opened[file]++
						return errors.Join(original.close(file), event("close"))
					}
					actual, err := captureSource(ctx, sourceUnitGit{fixture}, fixture.expected, 1<<20, readers)
					assertSourceUnitFailure(test, actual, err, fs.ErrPermission)
					if !failed || canceled && !errors.Is(err, context.Canceled) {
						test.Fatalf("root failure/cancellation was not preserved: %v", err)
					}
					for file, closes := range opened {
						if closes != 1 {
							test.Fatalf("root handle %q closed %d times", file.Name(), closes)
						}
					}
				})
			}
		}
	}
}

func TestCaptureSourceUnitMissingRootObservations(test *testing.T) {
	for _, operation := range []string{"open", "stat"} {
		test.Run(operation, func(test *testing.T) {
			fixture := newSourceUnitFixture(test)
			readers := fixture.readers()
			if operation == "open" {
				readers.roots.open = func(string) (*os.File, error) { return nil, nil }
			} else {
				readers.roots.stat = func(*os.File) (fs.FileInfo, error) { return nil, nil }
			}
			actual, err := captureSource(test.Context(), sourceUnitGit{fixture}, fixture.expected, 1<<20, readers)
			assertSourceUnitFailure(test, actual, err)
			if len(fixture.calls) != 0 {
				test.Fatal("missing root observations reached collectors")
			}
		})
	}
}

func TestCaptureSourceUnitZeroRemainingAndEmptyIndex(test *testing.T) {
	fixture := newSourceUnitFixture(test)
	fixture.contents.Status = git.StatusSnapshot{}
	fixture.expected.Status = domain.GitStatus{}
	fixture.inspected.Status = fixture.expected.Status
	fixture.contents.StagedPatch = nil
	fixture.contents.UnstagedPatch = nil
	fixture.contents.UntrackedEntries = nil
	fixture.contents.AdministrativeEntries[4].Data = []byte{}
	fixture.expected.IndexHash = fmt.Sprintf("%x", sha256.Sum256(nil))
	fixture.inspected.IndexHash = fixture.expected.IndexHash
	readers := fixture.readers()
	untracked := readers.untracked
	readers.untracked = func(ctx context.Context, directory string, paths []string, maximumBytes int64) ([]UntrackedEntry, error) {
		if maximumBytes != 1 {
			test.Fatalf("empty reader must retain a valid independent budget: %d", maximumBytes)
		}
		return untracked(ctx, directory, paths, maximumBytes)
	}
	actual, err := captureSource(test.Context(), sourceUnitGit{fixture}, fixture.expected, sourceUnitBytes(fixture.contents), readers)
	if err != nil || !reflect.DeepEqual(actual, fixture.contents) {
		test.Fatalf("exact zero remaining budget or empty index rejected: %#v, %v", actual, err)
	}
}

func TestCaptureSourceUnitSecondPassBudgetPreflight(test *testing.T) {
	for _, boundary := range []string{"list", "status", "staged", "unstaged", "administrative", "untracked"} {
		test.Run(boundary, func(test *testing.T) {
			fixture := newSourceUnitFixture(test)
			budget := sourceUnitBytes(fixture.contents)
			fixture.before = func(name string, pass int) error {
				if name != boundary || pass != 2 {
					return nil
				}
				oversized := make([]byte, budget+1)
				switch boundary {
				case "list":
					fixture.contents.WorktreeList = oversized
				case "status":
					fixture.contents.Status.Raw = oversized
				case "staged":
					fixture.contents.StagedPatch = oversized
				case "unstaged":
					fixture.contents.UnstagedPatch = oversized
				case "administrative":
					fixture.contents.AdministrativeEntries[1].Data = oversized
				case "untracked":
					fixture.contents.UntrackedEntries[2].Data = oversized
				}
				return nil
			}
			actual, err := fixture.capture(test.Context(), budget)
			assertSourceUnitFailure(test, actual, err, ErrSourceLimit)
			if fixture.pass != 2 || fixture.calls[len(fixture.calls)-1] != boundary {
				test.Fatalf("second pass did not independently preflight %s: %q", boundary, fixture.calls)
			}
		})
	}
}

func TestCaptureSourceUnitExactCodecCapacities(test *testing.T) {
	for _, capacity := range []string{"administrative-bytes", "administrative-entries", "untracked-entries"} {
		test.Run(capacity, func(test *testing.T) {
			fixture := newSourceUnitFixture(test)
			switch capacity {
			case "administrative-bytes":
				otherBytes := sourceUnitBytes(SourceCapture{AdministrativeEntries: fixture.contents.AdministrativeEntries}) - int64(len(fixture.contents.AdministrativeEntries[1].Data))
				fixture.contents.AdministrativeEntries[1].Data = bytes.Repeat([]byte("h"), maximumAdministrativeBytes-int(otherBytes))
			case "administrative-entries":
				for len(fixture.contents.AdministrativeEntries) < maximumAdministrativeEntries {
					fixture.contents.AdministrativeEntries = append(fixture.contents.AdministrativeEntries, AdminEntry{Path: fmt.Sprintf("extra-%04d", len(fixture.contents.AdministrativeEntries)), Kind: "file", Mode: 0o600})
				}
			case "untracked-entries":
				fixture.contents.Status.UntrackedPaths = nil
				fixture.contents.UntrackedEntries = nil
				for index := range maximumUntrackedEntries {
					name := fmt.Sprintf("extra-%04d", index)
					fixture.contents.Status.UntrackedPaths = append(fixture.contents.Status.UntrackedPaths, name)
					fixture.contents.UntrackedEntries = append(fixture.contents.UntrackedEntries, UntrackedEntry{Path: name, Kind: "file", Mode: 0o600})
				}
				fixture.expected.Status.Untracked = maximumUntrackedEntries
				fixture.inspected.Status = fixture.expected.Status
				fixture.contents.Status.Status = fixture.expected.Status
			}
			actual, err := fixture.capture(test.Context(), sourceUnitBytes(fixture.contents))
			if err != nil || !reflect.DeepEqual(actual, fixture.contents) {
				test.Fatalf("exact %s capacity was rejected: %v", capacity, err)
			}
		})
	}
}

func TestCaptureSourceUnitExactTwoPasses(test *testing.T) {
	fixture := newSourceUnitFixture(test)
	want := sourceUnitClone(fixture.contents)
	expected := fixture.expected
	actual, err := fixture.capture(test.Context(), sourceUnitBytes(want))
	if err != nil || !reflect.DeepEqual(actual, want) {
		test.Fatalf("exact bounded source capture = %#v, %v; want %#v", actual, err, want)
	}
	sequence := []string{"list", "inspect", "status", "staged", "unstaged", "administrative", "untracked"}
	if !slices.Equal(fixture.calls, append(slices.Clone(sequence), sequence...)) {
		test.Fatalf("collection sequence = %q", fixture.calls)
	}
	if !reflect.DeepEqual(fixture.expected, expected) {
		test.Fatal("input worktree was changed")
	}
}

func TestCaptureSourceUnitNilConcreteClient(test *testing.T) {
	fixture := newSourceUnitFixture(test)
	for _, client := range []*git.Client{nil, {}} {
		actual, err := CaptureSource(test.Context(), client, fixture.expected, 1<<20)
		assertSourceUnitFailure(test, actual, err)
	}
}

func TestCaptureSourceUnitInvalidExpected(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*domain.Worktree)
	}{
		{"unknown", func(value *domain.Worktree) { value.GitStateKnown = false }},
		{"unsafe", func(value *domain.Worktree) { value.PathSafe = false }},
		{"primary", func(value *domain.Worktree) { value.Primary = true }},
		{"prunable", func(value *domain.Worktree) { value.Prunable = true }},
		{"collection-error", func(value *domain.Worktree) { value.CollectionErrors = []string{"failure"} }},
		{"empty-collection-error", func(value *domain.Worktree) { value.CollectionErrors = []string{""} }},
		{"head-empty", func(value *domain.Worktree) { value.Head = "" }},
		{"head-zero", func(value *domain.Worktree) { value.Head = strings.Repeat("0", 40) }},
		{"head-abbreviated", func(value *domain.Worktree) { value.Head = "abc123" }},
		{"head-invalid", func(value *domain.Worktree) { value.Head = strings.Repeat("z", 40) }},
		{"head-uppercase", func(value *domain.Worktree) { value.Head = strings.Repeat("A", 40) }},
		{"branch-empty", func(value *domain.Worktree) { value.Branch = "" }},
		{"branch-invalid", func(value *domain.Worktree) { value.Branch = "topic..bad" }},
		{"detached-branch", func(value *domain.Worktree) { value.Detached = true }},
		{"unlocked-reason", func(value *domain.Worktree) { value.LockReason = "contradictory lock evidence" }},
		{"index-empty", func(value *domain.Worktree) { value.IndexHash = "" }},
		{"index-invalid", func(value *domain.Worktree) { value.IndexHash = strings.Repeat("z", 64) }},
		{"admin-empty", func(value *domain.Worktree) { value.AdminHash = "" }},
		{"admin-prefixed", func(value *domain.Worktree) { value.AdminHash = "sha256:" + value.AdminHash }},
		{"negative-staged", func(value *domain.Worktree) { value.Status.Staged = -1 }},
		{"negative-unstaged", func(value *domain.Worktree) { value.Status.Unstaged = -1 }},
		{"negative-unmerged", func(value *domain.Worktree) { value.Status.Unmerged = -1 }},
		{"negative-untracked", func(value *domain.Worktree) { value.Status.Untracked = -1 }},
		{"repository-empty", func(value *domain.Worktree) { value.RepositoryRoot = "" }},
		{"common-relative", func(value *domain.Worktree) { value.CommonGitDir = "relative" }},
		{"worktree-nul", func(value *domain.Worktree) { value.Path += "\x00" }},
		{"admin-trailing-slash", func(value *domain.Worktree) { value.AdminDir += string(filepath.Separator) }},
		{"admin-common", func(value *domain.Worktree) { value.AdminDir = value.CommonGitDir }},
		{"admin-outside", func(value *domain.Worktree) { value.AdminDir = value.Path }},
		{"worktree-primary", func(value *domain.Worktree) { value.Path = value.RepositoryRoot }},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			fixture := newSourceUnitFixture(test)
			scenario.change(&fixture.expected)
			actual, err := fixture.capture(test.Context(), 1<<20)
			assertSourceUnitFailure(test, actual, err)
			if len(fixture.calls) != 0 {
				test.Fatalf("invalid expected identity reached collectors: %q", fixture.calls)
			}
		})
	}
}

func TestCaptureSourceUnitReadabilityIsNotEligibility(test *testing.T) {
	fixture := newSourceUnitFixture(test)
	fixture.expected.Current = true
	fixture.expected.EstimatedBytes = 1 << 40
	fixture.expected.Locked = true
	fixture.expected.LockReason = "active developer"
	fixture.expected.Detached = true
	fixture.expected.Branch = ""
	fixture.expected.Head = strings.Repeat("c", 64)
	fixture.inspected = fixture.expected
	fixture.inspected.Current = false
	fixture.inspected.EstimatedBytes = 0
	fixture.listed[1].Locked = true
	fixture.listed[1].LockReason = fixture.expected.LockReason
	fixture.listed[1].Detached = true
	fixture.listed[1].Branch = ""
	fixture.listed[1].Head = fixture.expected.Head
	fixture.before = func(name string, pass int) error {
		if name == "inspect" && pass == 2 {
			fixture.inspected.Current = true
			fixture.inspected.EstimatedBytes = 123
		}
		return nil
	}
	actual, err := fixture.capture(test.Context(), 1<<20)
	if err != nil || !reflect.DeepEqual(actual, fixture.contents) {
		test.Fatalf("dirty/locked/current detached read was rejected: %#v, %v", actual, err)
	}
}

func TestCaptureSourceUnitRegistrationConflicts(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*sourceUnitFixture)
	}{
		{"empty", func(fixture *sourceUnitFixture) { fixture.listed = nil }},
		{"missing", func(fixture *sourceUnitFixture) { fixture.listed = fixture.listed[:1] }},
		{"duplicate-target", func(fixture *sourceUnitFixture) { fixture.listed = append(fixture.listed, fixture.listed[1]) }},
		{"duplicate-primary", func(fixture *sourceUnitFixture) { fixture.listed = append(fixture.listed, fixture.listed[0]) }},
		{"duplicate-primary-path", func(fixture *sourceUnitFixture) {
			duplicate := fixture.listed[0]
			duplicate.Primary = false
			fixture.listed = append(fixture.listed, duplicate)
		}},
		{"missing-primary", func(fixture *sourceUnitFixture) { fixture.listed[0].Primary = false }},
		{"primary-path", func(fixture *sourceUnitFixture) { fixture.listed[0].Path = fixture.expected.Path }},
		{"primary-common", func(fixture *sourceUnitFixture) { fixture.listed[0].CommonGitDir = fixture.expected.AdminDir }},
		{"target-repository", func(fixture *sourceUnitFixture) { fixture.listed[1].RepositoryRoot = fixture.expected.Path }},
		{"target-common", func(fixture *sourceUnitFixture) { fixture.listed[1].CommonGitDir = fixture.expected.AdminDir }},
		{"target-primary", func(fixture *sourceUnitFixture) { fixture.listed[1].Primary = true }},
		{"target-prunable", func(fixture *sourceUnitFixture) { fixture.listed[1].Prunable = true }},
		{"target-head", func(fixture *sourceUnitFixture) { fixture.listed[1].Head = strings.Repeat("d", 40) }},
		{"target-branch", func(fixture *sourceUnitFixture) { fixture.listed[1].Branch = "other" }},
		{"target-detached", func(fixture *sourceUnitFixture) { fixture.listed[1].Detached = true }},
		{"target-locked", func(fixture *sourceUnitFixture) { fixture.listed[1].Locked = true }},
		{"target-lock-reason", func(fixture *sourceUnitFixture) { fixture.listed[1].LockReason = "new lock" }},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			fixture := newSourceUnitFixture(test)
			scenario.change(fixture)
			actual, err := fixture.capture(test.Context(), 1<<20)
			assertSourceUnitFailure(test, actual, err)
			if !slices.Equal(fixture.calls, []string{"list"}) {
				test.Fatalf("registration failure advanced collectors: %q", fixture.calls)
			}
		})
	}
}

func TestCaptureSourceUnitInspectionConflicts(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*domain.Worktree)
	}{
		{"unknown", func(value *domain.Worktree) { value.GitStateKnown = false }},
		{"unsafe", func(value *domain.Worktree) { value.PathSafe = false }},
		{"error", func(value *domain.Worktree) { value.CollectionErrors = []string{"failure"} }},
		{"path", func(value *domain.Worktree) { value.Path = value.RepositoryRoot }},
		{"repository", func(value *domain.Worktree) { value.RepositoryRoot = value.Path }},
		{"common", func(value *domain.Worktree) { value.CommonGitDir = value.AdminDir }},
		{"admin", func(value *domain.Worktree) { value.AdminDir = value.CommonGitDir }},
		{"head", func(value *domain.Worktree) { value.Head = strings.Repeat("e", 40) }},
		{"branch", func(value *domain.Worktree) { value.Branch = "other" }},
		{"detached", func(value *domain.Worktree) { value.Detached = true }},
		{"primary", func(value *domain.Worktree) { value.Primary = true }},
		{"prunable", func(value *domain.Worktree) { value.Prunable = true }},
		{"lock", func(value *domain.Worktree) { value.Locked = true }},
		{"lock-reason", func(value *domain.Worktree) { value.LockReason = "other" }},
		{"upstream", func(value *domain.Worktree) { value.Upstream = "refs/remotes/other/topic" }},
		{"status", func(value *domain.Worktree) { value.Status.Staged++ }},
		{"index", func(value *domain.Worktree) { value.IndexHash = strings.Repeat("e", 64) }},
		{"admin-hash", func(value *domain.Worktree) { value.AdminHash = strings.Repeat("e", 64) }},
		{"recoverable", func(value *domain.Worktree) { value.Recoverable = false }},
		{"commit-time", func(value *domain.Worktree) { value.LastCommitAt = value.LastCommitAt.Add(time.Second) }},
		{"metadata-time", func(value *domain.Worktree) { value.MetadataModifiedAt = value.MetadataModifiedAt.Add(time.Nanosecond) }},
	} {
		for _, changedPass := range []int{1, 2} {
			test.Run(fmt.Sprintf("%s/pass-%d", scenario.name, changedPass), func(test *testing.T) {
				fixture := newSourceUnitFixture(test)
				fixture.before = func(name string, pass int) error {
					if name == "inspect" && pass == changedPass {
						scenario.change(&fixture.inspected)
					}
					return nil
				}
				actual, err := fixture.capture(test.Context(), 1<<20)
				assertSourceUnitFailure(test, actual, err, ErrSourceChanged)
				if fixture.calls[len(fixture.calls)-1] != "inspect" || fixture.pass != changedPass {
					test.Fatalf("inspection conflict did not stop immediately: %q", fixture.calls)
				}
			})
		}
	}
}

func TestCaptureSourceUnitCollectorErrorsAndCancellation(test *testing.T) {
	sequence := []string{"list", "inspect", "status", "staged", "unstaged", "administrative", "untracked"}
	for _, failedPass := range []int{1, 2} {
		for position, boundary := range sequence {
			for _, failure := range []struct {
				name   string
				cause  error
				cancel bool
				limit  bool
			}{
				{name: "io", cause: fs.ErrPermission},
				{name: "git-text-not-limit", cause: errors.New("Git metadata exceeds collection limit")},
				{name: "command-limit", cause: execx.ErrOutputLimit, limit: true},
				{name: "admin-limit", cause: ErrManifestLimit, limit: true},
				{name: "untracked-limit", cause: ErrUntrackedLimit, limit: true},
				{name: "deadline", cause: context.DeadlineExceeded},
				{name: "cancel", cancel: true},
				{name: "cancel-and-error", cause: fs.ErrPermission, cancel: true},
			} {
				test.Run(fmt.Sprintf("pass-%d/%s/%s", failedPass, boundary, failure.name), func(test *testing.T) {
					fixture := newSourceUnitFixture(test)
					ctx, cancel := context.WithCancel(test.Context())
					defer cancel()
					fixture.before = func(name string, pass int) error {
						if name == boundary && pass == failedPass {
							if failure.cancel {
								cancel()
							}
							return failure.cause
						}
						return nil
					}
					actual, err := fixture.capture(ctx, 1<<20)
					assertSourceUnitFailure(test, actual, err)
					if failure.cause != nil && !errors.Is(err, failure.cause) || failure.cancel && !errors.Is(err, context.Canceled) || errors.Is(err, ErrSourceLimit) != failure.limit {
						test.Fatalf("incorrect failure classification: %v", err)
					}
					if len(fixture.calls) != (failedPass-1)*len(sequence)+position+1 {
						test.Fatalf("failure advanced or retried collectors: %q", fixture.calls)
					}
				})
			}
		}
	}
	test.Run("already-canceled", func(test *testing.T) {
		fixture := newSourceUnitFixture(test)
		ctx, cancel := context.WithCancel(test.Context())
		cancel()
		actual, err := fixture.capture(ctx, 1<<20)
		assertSourceUnitFailure(test, actual, err, context.Canceled)
		if len(fixture.calls) != 0 {
			test.Fatal("canceled capture invoked a collector")
		}
	})
}

func TestCaptureSourceUnitBudgetPreflight(test *testing.T) {
	for _, budget := range []int64{-1, 0, int64(int(^uint(0) >> 1))} {
		test.Run(fmt.Sprintf("invalid-%d", budget), func(test *testing.T) {
			fixture := newSourceUnitFixture(test)
			actual, err := fixture.capture(test.Context(), budget)
			assertSourceUnitFailure(test, actual, err, ErrSourceLimit)
			if len(fixture.calls) != 0 {
				test.Fatal("invalid budget reached collectors")
			}
		})
	}
	for _, boundary := range []string{"list", "status", "staged", "unstaged", "administrative", "untracked"} {
		test.Run(boundary, func(test *testing.T) {
			fixture := newSourceUnitFixture(test)
			var budget int64
			for _, contribution := range []struct {
				name string
				size int64
			}{
				{"list", int64(len(fixture.contents.WorktreeList))},
				{"status", int64(len(fixture.contents.Status.Raw))},
				{"staged", int64(len(fixture.contents.StagedPatch))},
				{"unstaged", int64(len(fixture.contents.UnstagedPatch))},
				{"administrative", sourceUnitBytes(SourceCapture{AdministrativeEntries: fixture.contents.AdministrativeEntries})},
				{"untracked", sourceUnitBytes(SourceCapture{UntrackedEntries: fixture.contents.UntrackedEntries})},
			} {
				budget += contribution.size
				if contribution.name == boundary {
					break
				}
			}
			actual, err := fixture.capture(test.Context(), budget-1)
			assertSourceUnitFailure(test, actual, err, ErrSourceLimit)
			if fixture.pass != 1 || fixture.calls[len(fixture.calls)-1] != boundary {
				test.Fatalf("budget not checked at %s: %q", boundary, fixture.calls)
			}
		})
	}
}

func TestCaptureSourceUnitIndependentCodecLimits(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*sourceUnitFixture)
		cause  error
		last   string
	}{
		{"admin-count", func(fixture *sourceUnitFixture) {
			fixture.contents.AdministrativeEntries = make([]AdminEntry, maximumAdministrativeEntries+1)
		}, ErrManifestLimit, "administrative"},
		{"admin-bytes", func(fixture *sourceUnitFixture) {
			fixture.contents.AdministrativeEntries[1].Data = make([]byte, maximumAdministrativeBytes)
		}, ErrManifestLimit, "administrative"},
		{"selection-count", func(fixture *sourceUnitFixture) {
			fixture.contents.Status.UntrackedPaths = make([]string, maximumUntrackedEntries+1)
			fixture.expected.Status.Untracked = len(fixture.contents.Status.UntrackedPaths)
			fixture.inspected.Status = fixture.expected.Status
			fixture.contents.Status.Status = fixture.expected.Status
		}, ErrUntrackedLimit, "status"},
		{"selection-parents", func(fixture *sourceUnitFixture) {
			fixture.contents.Status.UntrackedPaths = make([]string, maximumUntrackedEntries)
			for index := range fixture.contents.Status.UntrackedPaths {
				fixture.contents.Status.UntrackedPaths[index] = fmt.Sprintf("parent/file-%04d", index)
			}
			fixture.expected.Status.Untracked = len(fixture.contents.Status.UntrackedPaths)
			fixture.inspected.Status = fixture.expected.Status
			fixture.contents.Status.Status = fixture.expected.Status
		}, ErrUntrackedLimit, "status"},
		{"untracked-count", func(fixture *sourceUnitFixture) {
			fixture.contents.UntrackedEntries = make([]UntrackedEntry, maximumUntrackedEntries+1)
		}, ErrUntrackedLimit, "untracked"},
		{"untracked-path", func(fixture *sourceUnitFixture) {
			fixture.contents.UntrackedEntries[0].Path = strings.Repeat("x", maximumUntrackedText+1)
		}, ErrUntrackedLimit, "untracked"},
		{"link-text", func(fixture *sourceUnitFixture) {
			fixture.contents.UntrackedEntries[0].LinkTarget = strings.Repeat("x", maximumUntrackedText+1)
		}, ErrUntrackedLimit, "untracked"},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			fixture := newSourceUnitFixture(test)
			scenario.change(fixture)
			actual, err := fixture.capture(test.Context(), 64<<20)
			assertSourceUnitFailure(test, actual, err, ErrSourceLimit, scenario.cause)
			if fixture.calls[len(fixture.calls)-1] != scenario.last {
				test.Fatalf("capacity failure advanced collectors: %q", fixture.calls)
			}
		})
	}
}

func TestCaptureSourceUnitStatusAndSelectionValidation(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*SourceCapture)
		last   string
	}{
		{"status-counts", func(value *SourceCapture) { value.Status.Status.Staged++ }, "status"},
		{"selection-count", func(value *SourceCapture) { value.Status.UntrackedPaths = value.Status.UntrackedPaths[:1] }, "status"},
		{"selection-duplicate", func(value *SourceCapture) { value.Status.UntrackedPaths[1] = value.Status.UntrackedPaths[0] }, "status"},
		{"selection-alias", func(value *SourceCapture) { value.Status.UntrackedPaths[1] = "NESTED/other" }, "status"},
		{"selection-ancestor", func(value *SourceCapture) { value.Status.UntrackedPaths[1] = "nested" }, "status"},
		{"selection-escape", func(value *SourceCapture) { value.Status.UntrackedPaths[0] = "../escape" }, "status"},
		{"selection-git", func(value *SourceCapture) { value.Status.UntrackedPaths[0] = ".git/config" }, "status"},
		{"missing-leaf", func(value *SourceCapture) { value.UntrackedEntries = value.UntrackedEntries[:2] }, "untracked"},
		{"missing-parent", func(value *SourceCapture) { value.UntrackedEntries = slices.Delete(value.UntrackedEntries, 1, 2) }, "untracked"},
		{"extra-parent", func(value *SourceCapture) {
			value.UntrackedEntries = append(value.UntrackedEntries, UntrackedEntry{Path: "extra", Kind: "directory", Mode: fs.ModeDir | 0o700})
		}, "untracked"},
		{"wrong-leaf", func(value *SourceCapture) { value.UntrackedEntries[2].Path = "nested/other" }, "untracked"},
		{"leaf-directory", func(value *SourceCapture) {
			value.UntrackedEntries[2] = UntrackedEntry{Path: "nested/data.bin", Kind: "directory", Mode: fs.ModeDir | 0o700}
		}, "untracked"},
		{"parent-file", func(value *SourceCapture) {
			value.UntrackedEntries[1] = UntrackedEntry{Path: "nested", Kind: "file", Mode: 0o600}
		}, "untracked"},
		{"escaping-link", func(value *SourceCapture) { value.UntrackedEntries[0].LinkTarget = "../outside" }, "untracked"},
		{"malformed-admin", func(value *SourceCapture) { value.AdministrativeEntries[1].Path = "../outside" }, "administrative"},
		{"missing-admin-head", func(value *SourceCapture) {
			value.AdministrativeEntries = slices.Delete(value.AdministrativeEntries, 1, 2)
		}, "administrative"},
		{"admin-mode", func(value *SourceCapture) { value.AdministrativeEntries[1].Mode |= fs.ModeSymlink }, "administrative"},
		{"missing-index", func(value *SourceCapture) { value.AdministrativeEntries = value.AdministrativeEntries[:4] }, "administrative"},
		{"inconsistent-index", func(value *SourceCapture) { value.AdministrativeEntries[4].Data[0]++ }, "administrative"},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			fixture := newSourceUnitFixture(test)
			scenario.change(&fixture.contents)
			actual, err := fixture.capture(test.Context(), 1<<20)
			assertSourceUnitFailure(test, actual, err)
			if fixture.calls[len(fixture.calls)-1] != scenario.last {
				test.Fatalf("invalid collector result advanced: %q", fixture.calls)
			}
		})
	}
}

func TestCaptureSourceUnitChangedContentAndMetadata(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*SourceCapture)
	}{
		{"list-raw", func(value *SourceCapture) { value.WorktreeList[0]++ }},
		{"status-raw", func(value *SourceCapture) { value.Status.Raw[0]++ }},
		{"selection-order", func(value *SourceCapture) { slices.Reverse(value.Status.UntrackedPaths) }},
		{"staged", func(value *SourceCapture) { value.StagedPatch[0]++ }},
		{"unstaged", func(value *SourceCapture) { value.UnstagedPatch[0]++ }},
		{"admin-data", func(value *SourceCapture) { value.AdministrativeEntries[1].Data[0]++ }},
		{"admin-mode", func(value *SourceCapture) { value.AdministrativeEntries[1].Mode ^= 0o100 }},
		{"admin-path", func(value *SourceCapture) {
			value.AdministrativeEntries = append(value.AdministrativeEntries, AdminEntry{Path: "extra", Kind: "file", Mode: 0o600})
		}},
		{"untracked-bytes", func(value *SourceCapture) { value.UntrackedEntries[2].Data[0]++ }},
		{"untracked-mode", func(value *SourceCapture) { value.UntrackedEntries[2].Mode ^= 0o100 }},
		{"parent-mode", func(value *SourceCapture) { value.UntrackedEntries[1].Mode ^= 0o100 }},
		{"link-text", func(value *SourceCapture) { value.UntrackedEntries[0].LinkTarget = "nested/other.bin" }},
		{"leaf-kind", func(value *SourceCapture) {
			value.UntrackedEntries[0] = UntrackedEntry{Path: "link", Kind: "file", Mode: 0o600, Data: []byte("nested/data.bin")}
		}},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			fixture := newSourceUnitFixture(test)
			fixture.before = func(name string, pass int) error {
				if name == "list" && pass == 2 {
					scenario.change(&fixture.contents)
				}
				return nil
			}
			actual, err := fixture.capture(test.Context(), 1<<20)
			assertSourceUnitFailure(test, actual, err, ErrSourceChanged)
			if fixture.pass != 2 {
				test.Fatalf("changed collection retried: %q", fixture.calls)
			}
		})
	}
	test.Run("final-collector", func(test *testing.T) {
		fixture := newSourceUnitFixture(test)
		fixture.before = func(name string, pass int) error {
			if name == "untracked" && pass == 2 {
				fixture.contents.UntrackedEntries[2].Data[0]++
			}
			return nil
		}
		actual, err := fixture.capture(test.Context(), 1<<20)
		assertSourceUnitFailure(test, actual, err, ErrSourceChanged)
	})
}

func TestCaptureSourceUnitOwnsAllResults(test *testing.T) {
	fixture := newSourceUnitFixture(test)
	want := sourceUnitClone(fixture.contents)
	actual, err := fixture.capture(test.Context(), 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	actual.WorktreeList[0]++
	actual.Status.Raw[0]++
	actual.Status.UntrackedPaths[0] = "changed"
	actual.StagedPatch[0]++
	actual.UnstagedPatch[0]++
	actual.AdministrativeEntries[1].Data[0]++
	actual.AdministrativeEntries[0].Mode ^= 0o100
	actual.UntrackedEntries[2].Data[0]++
	actual.UntrackedEntries[0].LinkTarget = "changed"
	if !reflect.DeepEqual(fixture.contents, want) {
		test.Fatal("returned capture aliases collector inputs")
	}
	second, err := fixture.capture(test.Context(), 1<<20)
	if err != nil || !reflect.DeepEqual(second, want) {
		test.Fatalf("caller mutation contaminated another capture: %#v, %v", second, err)
	}
}

func TestCaptureSourceUnitClonesBeforeNextCollector(test *testing.T) {
	for _, boundary := range []string{"inspect", "staged", "unstaged", "administrative", "untracked"} {
		test.Run(boundary, func(test *testing.T) {
			fixture := newSourceUnitFixture(test)
			original := sourceUnitClone(fixture.contents)
			fixture.before = func(name string, pass int) error {
				if name == "list" {
					copy(fixture.contents.WorktreeList, original.WorktreeList)
					copy(fixture.contents.Status.Raw, original.Status.Raw)
					copy(fixture.contents.StagedPatch, original.StagedPatch)
					copy(fixture.contents.UnstagedPatch, original.UnstagedPatch)
					copy(fixture.contents.AdministrativeEntries[1].Data, original.AdministrativeEntries[1].Data)
				}
				if name == boundary {
					switch boundary {
					case "inspect":
						fixture.contents.WorktreeList[0] += byte(pass)
					case "staged":
						fixture.contents.Status.Raw[0] += byte(pass)
					case "unstaged":
						fixture.contents.StagedPatch[0] += byte(pass)
					case "administrative":
						fixture.contents.UnstagedPatch[0] += byte(pass)
					case "untracked":
						fixture.contents.AdministrativeEntries[1].Data[0] += byte(pass)
					}
				}
				return nil
			}
			actual, err := fixture.capture(test.Context(), 1<<20)
			if err != nil || !reflect.DeepEqual(actual, original) {
				test.Fatalf("earlier result was not cloned before %s: %#v, %v", boundary, actual, err)
			}
		})
	}
	test.Run("selection-input", func(test *testing.T) {
		fixture := newSourceUnitFixture(test)
		original := sourceUnitClone(fixture.contents)
		readers := fixture.readers()
		read := readers.untracked
		readers.untracked = func(ctx context.Context, directory string, paths []string, maximumBytes int64) ([]UntrackedEntry, error) {
			entries, err := read(ctx, directory, paths, maximumBytes)
			paths[0] = "mutated by reader"
			return entries, err
		}
		actual, err := captureSource(test.Context(), sourceUnitGit{fixture}, fixture.expected, 1<<20, readers)
		if err != nil || !reflect.DeepEqual(actual, original) || !reflect.DeepEqual(fixture.contents, original) {
			test.Fatalf("reader argument aliases retained selection: %#v, %v", actual, err)
		}
	})
}

func TestCaptureSourceUnitRetainsSelectedPaths(test *testing.T) {
	fixture := newSourceUnitFixture(test)
	original := sourceUnitClone(fixture.contents)
	fixture.before = func(name string, pass int) error {
		if name == "list" {
			copy(fixture.contents.Status.UntrackedPaths, original.Status.UntrackedPaths)
		}
		if name == "staged" {
			fixture.contents.Status.UntrackedPaths[0] = "later collector reused path slice"
		}
		return nil
	}
	readers := fixture.readers()
	readers.untracked = func(ctx context.Context, directory string, paths []string, maximumBytes int64) ([]UntrackedEntry, error) {
		if directory != fixture.expected.Path || !slices.Equal(paths, original.Status.UntrackedPaths) {
			test.Fatalf("retained Git selection was overwritten: %q", paths)
		}
		err := fixture.call("untracked")
		return fixture.contents.UntrackedEntries, err
	}
	actual, err := captureSource(test.Context(), sourceUnitGit{fixture}, fixture.expected, 1<<20, readers)
	if err != nil || !reflect.DeepEqual(actual, original) {
		test.Fatalf("status selection aliases a collector slice: %#v, %v", actual, err)
	}
}

func TestCaptureSourceUnitClonesBeforeRootChecks(test *testing.T) {
	fixture := newSourceUnitFixture(test)
	original := sourceUnitClone(fixture.contents)
	fixture.before = func(name string, pass int) error {
		if name == "list" {
			copy(fixture.contents.UntrackedEntries[2].Data, original.UntrackedEntries[2].Data)
			fixture.contents.UntrackedEntries[0].LinkTarget = original.UntrackedEntries[0].LinkTarget
			fixture.contents.AdministrativeEntries[1].Path = original.AdministrativeEntries[1].Path
		}
		if name == "untracked" {
			fixture.contents.AdministrativeEntries[1].Path = "overwritten after administrative read"
		}
		return nil
	}
	readers := fixture.readers()
	stat := readers.roots.stat
	readers.roots.stat = func(file *os.File) (fs.FileInfo, error) {
		if len(fixture.calls) != 0 && fixture.calls[len(fixture.calls)-1] == "untracked" {
			fixture.contents.UntrackedEntries[2].Data[0]++
			fixture.contents.UntrackedEntries[0].LinkTarget = "overwritten after untracked read"
		}
		return stat(file)
	}
	actual, err := captureSource(test.Context(), sourceUnitGit{fixture}, fixture.expected, 1<<20, readers)
	if err != nil || !reflect.DeepEqual(actual, original) {
		test.Fatalf("collected entries alias later callbacks: %#v, %v", actual, err)
	}
}

func TestCaptureSourceUnitPhysicalRoots(test *testing.T) {
	for _, rootName := range []string{"repository", "common", "worktree", "admin"} {
		for _, boundary := range []string{"before", "first-list", "second-list", "final-untracked"} {
			test.Run(rootName+"/"+boundary, func(test *testing.T) {
				fixture := newSourceUnitFixture(test)
				roots := map[string]string{"repository": fixture.expected.RepositoryRoot, "common": fixture.expected.CommonGitDir, "worktree": fixture.expected.Path, "admin": fixture.expected.AdminDir}
				root := roots[rootName]
				attempts := 0
				blocked := false
				var original fs.FileInfo
				if boundary == "before" {
					if err := os.RemoveAll(root); err != nil {
						test.Fatal(err)
					}
				} else {
					sourceUnitRootRenameControl(test, root)
					replacement, information := sourceUnitRootCopy(test, root)
					original = information
					fixture.before = func(name string, pass int) error {
						if sourceUnitRootBoundary(boundary, name, pass) {
							attempts++
							if attempts != 1 {
								test.Fatal("root replacement repeated")
							}
							if err := os.Rename(root, root+"-original"); err != nil {
								if runtime.GOOS == "windows" && (rootName == "repository" || rootName == "common") && errors.Is(err, fs.ErrPermission) {
									blocked = true
									return nil
								}
								test.Fatal(err)
							}
							if err := os.Rename(replacement, root); err != nil {
								test.Fatal(err)
							}
						}
						return nil
					}
				}
				actual, err := fixture.capture(test.Context(), 1<<20)
				if boundary == "before" {
					assertSourceUnitFailure(test, actual, err, fs.ErrNotExist)
				} else if blocked {
					if err != nil || !reflect.DeepEqual(actual, fixture.contents) {
						test.Fatalf("native-blocked replacement changed capture: %v", err)
					}
					current, err := os.Stat(root)
					if err != nil || !os.SameFile(original, current) {
						test.Fatalf("native-blocked replacement changed root identity: %v", err)
					}
					if _, err := os.Lstat(root + "-original"); !errors.Is(err, fs.ErrNotExist) {
						test.Fatalf("native-blocked replacement moved the original: %v", err)
					}
					sourceUnitRootRenameControl(test, root)
					test.Log("Windows blocked the ancestor move while descendants were pinned; pre/post rename controls passed")
				} else {
					assertSourceUnitFailure(test, actual, err, ErrSourceChanged)
				}
				if boundary != "before" && attempts != 1 {
					test.Fatal("root replacement boundary was not exercised")
				}
			})
		}
		test.Run(rootName+"/alias", func(test *testing.T) {
			fixture := newSourceUnitFixture(test)
			roots := map[string]*string{"repository": &fixture.expected.RepositoryRoot, "common": &fixture.expected.CommonGitDir, "worktree": &fixture.expected.Path, "admin": &fixture.expected.AdminDir}
			root := roots[rootName]
			alias := *root + "-alias"
			if err := os.Symlink(*root, alias); err != nil {
				if runtime.GOOS == "windows" {
					test.Skipf("native symlink unavailable: %v", err)
				}
				test.Fatal(err)
			}
			*root = alias
			actual, err := fixture.capture(test.Context(), 1<<20)
			assertSourceUnitFailure(test, actual, err)
			if len(fixture.calls) != 0 {
				test.Fatal("aliased source reached a collector")
			}
		})
	}
}
