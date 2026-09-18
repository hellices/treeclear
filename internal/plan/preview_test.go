package plan

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/process"
)

func TestBuilderDefaultIsNonExecutablePreview(test *testing.T) {
	builder, request, _, _ := builderFixture(test)
	value, err := builder.Build(context.Background(), request)
	if err != nil {
		test.Fatal(err)
	}
	if value.SchemaVersion != 2 || value.Candidates[0].Action != "none" || value.Candidates[0].Snapshot.Required {
		test.Fatalf("inventory unexpectedly authorizes removal: schema=%d action=%q snapshot=%t", value.SchemaVersion, value.Candidates[0].Action, value.Candidates[0].Snapshot.Required)
	}
	want := &domain.RemovalPlan{
		Intent: domain.RemovalIntentInventory, ContentDisposition: domain.ContentDiscardAll,
		BackupMode: domain.BackupNone, SelectedPaths: []string{}, Execution: domain.ExecutionPreviewOnly,
	}
	if !reflect.DeepEqual(value.Removal, want) || value.Candidates[0].Selection == nil || value.Candidates[0].Selection.Selected || value.Summary.ReclaimableBytes != 0 {
		test.Fatalf("inventory preview contract = %#v, selection = %#v", value.Removal, value.Candidates[0].Selection)
	}
}

func TestBuilderExplicitSelectionIsLiteralBoundAndReadOnly(test *testing.T) {
	builder, request, base, _ := builderFixture(test)
	selected := builderSibling(base, "selected, with spaces")
	ignoredOnly := builderSibling(base, "ignored-only")
	unselected := builderSibling(base, "unselected")
	selected.Status = domain.GitStatus{Staged: 1, Unstaged: 1, Untracked: 1}
	request.SelectedPaths = []string{selected.Path, ignoredOnly.Path}
	request.SkipDirty = true
	before := slices.Clone(request.SelectedPaths)
	builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
		return []domain.Worktree{unselected, selected, ignoredOnly}, nil
	})
	value, err := builder.Build(context.Background(), request)
	if err != nil {
		test.Fatal(err)
	}
	wantPaths := []string{ignoredOnly.Path, selected.Path}
	if value.Removal.Intent != domain.RemovalIntentExplicit || !value.Removal.SkipDirty || !reflect.DeepEqual(value.Removal.SelectedPaths, wantPaths) {
		test.Fatalf("selection was lost or split: %#v", value.Removal)
	}
	for _, candidate := range value.Candidates {
		wantSelected := candidate.Worktree.Path != unselected.Path
		wantSkip := ""
		if candidate.Worktree.Path == selected.Path {
			wantSkip = "dirty"
		}
		if candidate.Selection == nil || candidate.Selection.Selected != wantSelected || candidate.Selection.SkipReason != wantSkip {
			test.Fatalf("selection for %q = %#v", candidate.Worktree.Path, candidate.Selection)
		}
		if candidate.Action != "none" || candidate.Snapshot != (domain.SnapshotPlan{}) {
			test.Fatalf("preview acquired removal or backup action: %#v", candidate)
		}
	}
	if !reflect.DeepEqual(request.SelectedPaths, before) || value.Summary.ReclaimableBytes != 0 {
		test.Fatal("preview changed input selection or claims executable reclaimable bytes")
	}
	value.Removal.SelectedPaths[0] = "changed"
	if !reflect.DeepEqual(request.SelectedPaths, before) {
		test.Fatal("returned selection aliases the request")
	}
	requireBuilderFingerprints(test, value)
}

func TestBuilderSkipDirtyRecordsKnownGitCategoriesOnly(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		status domain.GitStatus
	}{
		{"clean-or-ignored-only", domain.GitStatus{}},
		{"staged", domain.GitStatus{Staged: 1}},
		{"unstaged", domain.GitStatus{Unstaged: 1}},
		{"unmerged", domain.GitStatus{Unmerged: 1}},
		{"untracked", domain.GitStatus{Untracked: 1}},
	} {
		for _, skipDirty := range []bool{false, true} {
			test.Run(scenario.name+"/skip="+map[bool]string{false: "false", true: "true"}[skipDirty], func(test *testing.T) {
				builder, request, worktree, _ := builderFixture(test)
				worktree.Status = scenario.status
				request.SelectedPaths, request.SkipDirty = []string{worktree.Path}, skipDirty
				builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
					return []domain.Worktree{worktree}, nil
				})
				value, err := builder.Build(context.Background(), request)
				if err != nil {
					test.Fatal(err)
				}
				selection := value.Candidates[0].Selection
				if selection == nil || !selection.Selected || (selection.SkipReason == "dirty") != (skipDirty && !scenario.status.Clean()) {
					test.Fatalf("selection = %#v", selection)
				}
				if value.Candidates[0].Action != "none" || value.Candidates[0].Snapshot.Required {
					test.Fatal("disposal choice became execution permission")
				}
			})
		}
	}
}

func TestBuilderRefusesInvalidSelectionRequestsBeforeCollection(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*Request)
	}{
		{"backup", func(request *Request) { request.BackupRequested = true }},
		{"skip-without-selection", func(request *Request) { request.SkipDirty = true }},
		{"empty", func(request *Request) { request.SelectedPaths = []string{""} }},
		{"relative", func(request *Request) { request.SelectedPaths = []string{"relative"} }},
		{"nul", func(request *Request) { request.SelectedPaths = []string{request.Roots[0] + "\x00"} }},
		{"invalid-text", func(request *Request) { request.SelectedPaths = []string{request.Roots[0] + "\xff"} }},
		{"duplicate", func(request *Request) { request.SelectedPaths = []string{request.Roots[0], request.Roots[0]} }},
		{"overlap", func(request *Request) {
			request.SelectedPaths = []string{request.Roots[0], filepath.Join(request.Roots[0], "nested")}
		}},
		{"scheduled", func(request *Request) {
			request.IntendedApplyMode = domain.ApplyScheduled
			request.SelectedPaths = []string{request.Roots[0]}
		}},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			builder, request, _, _ := builderFixture(test)
			scenario.change(&request)
			builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
				test.Fatal("invalid request reached inventory")
				return nil, nil
			})
			value, err := builder.Build(context.Background(), request)
			requireBuilderZeroPlan(test, value, err)
			if !errors.Is(err, ErrPlanInvalid) {
				test.Fatalf("request error = %v", err)
			}
		})
	}
}

func TestBuilderRequiresExactLinkedSelectionBeforeProcessCollection(test *testing.T) {
	for _, scenario := range []string{"missing", "prefix", "descendant", "glob", "primary", "duplicate-case"} {
		test.Run(scenario, func(test *testing.T) {
			if scenario == "duplicate-case" && runtime.GOOS != "windows" {
				test.Skip("Windows path case rule")
			}
			builder, request, worktree, _ := builderFixture(test)
			path := worktree.Path
			switch scenario {
			case "missing":
				path = filepath.Join(request.Roots[0], "missing")
			case "prefix":
				path = filepath.Dir(path)
			case "descendant":
				path = filepath.Join(path, "nested")
			case "glob":
				path += "*"
			case "primary":
				worktree.Primary = true
				worktree.Path = worktree.RepositoryRoot
				worktree.AdminDir = worktree.CommonGitDir
				path = worktree.Path
			}
			request.SelectedPaths = []string{path}
			if scenario == "duplicate-case" {
				request.SelectedPaths = append(request.SelectedPaths, strings.ToUpper(path))
			}
			builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
				return []domain.Worktree{worktree}, nil
			})
			builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
				test.Fatal("invalid selection reached process collection")
				return process.Collection{}, nil
			})
			value, err := builder.Build(context.Background(), request)
			requireBuilderZeroPlan(test, value, err)
		})
	}
}

func TestBuilderSelectionDoesNotBypassDirtyUnknownOrActiveEvidence(test *testing.T) {
	for _, unknown := range []bool{false, true} {
		builder, request, worktree, clock := builderFixture(test)
		worktree.Status.Untracked = 1
		request.SelectedPaths = []string{worktree.Path}
		builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
			return []domain.Worktree{worktree}, nil
		})
		builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
			return process.Collection{Complete: !unknown, ByWorktree: map[string][]domain.ProcessEvidence{worktree.Path: {{
				PID: 42, State: domain.EvidenceActive, CWD: worktree.Path,
				CreatedAt: clock.Now(), Executable: filepath.Join(request.Roots[0], "synthetic"), Fingerprint: "synthetic",
			}}}}, nil
		})
		value, err := builder.Build(context.Background(), request)
		if (err != nil) != unknown || len(value.Candidates) != 1 {
			test.Fatalf("evidence collection = %#v, %v", value, err)
		}
		candidate := value.Candidates[0]
		if candidate.Selection == nil || !candidate.Selection.Selected || candidate.Decision.Classification != domain.Protected || candidate.Action != "none" || len(candidate.Evidence.Processes) == 0 {
			test.Fatalf("selection lost retained protection/evidence: %#v", candidate)
		}
	}
}

func TestBuilderCopiesSelectionBeforeCallingDependencies(test *testing.T) {
	builder, request, worktree, _ := builderFixture(test)
	request.SelectedPaths = []string{worktree.Path}
	builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
		request.SelectedPaths[0] = "changed by dependency"
		return []domain.Worktree{worktree}, nil
	})
	value, err := builder.Build(context.Background(), request)
	if err != nil || !reflect.DeepEqual(value.Removal.SelectedPaths, []string{worktree.Path}) || !value.Candidates[0].Selection.Selected {
		test.Fatalf("builder retained mutable caller selection: %#v, %v", value.Removal, err)
	}
}
