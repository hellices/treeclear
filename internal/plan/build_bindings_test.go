package plan

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/process"
)

func TestBuilderBlocksUnboundKnownProcessRecords(test *testing.T) {
	for _, state := range []domain.EvidenceState{domain.EvidenceActive, domain.EvidenceInactive, domain.EvidenceNotApplicable} {
		test.Run(string(state), func(test *testing.T) {
			builder, request, base, clock := builderFixture(test)
			worktrees := []domain.Worktree{base, builderSibling(base, "second"), builderSibling(base, "third")}
			builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
				return worktrees, nil
			})
			unbound := filepath.Join(filepath.Dir(base.Path), "not-in-inventory")
			collection := process.Collection{Complete: true, ByWorktree: map[string][]domain.ProcessEvidence{
				unbound: {{PID: 42, State: state, CreatedAt: clock.Now().Add(-30 * 24 * time.Hour), Executable: "synthetic", CWD: unbound, Fingerprint: "process-proof"}},
			}}
			before := collection.ByWorktree[unbound][0]
			builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
				return collection, nil
			})
			value, err := builder.Build(test.Context(), request)
			requireBuilderBlocked(test, value, err, len(worktrees))
			if !strings.Contains(err.Error(), "unbound process") || !strings.Contains(strings.Join(value.Warnings, "\n"), "unbound process") {
				test.Fatalf("unbound evidence diagnostic lost: %v, %v", value.Warnings, err)
			}
			if !reflect.DeepEqual(before, collection.ByWorktree[unbound][0]) {
				test.Fatal("builder changed the collector's unbound record")
			}
		})
	}
}

func TestBuilderBlocksUnrecognizedEmptyProcessBindings(test *testing.T) {
	builder, request, base, _ := builderFixture(test)
	builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
		return []domain.Worktree{base, builderSibling(base, "second")}, nil
	})
	builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
		return process.Collection{Complete: true, ByWorktree: map[string][]domain.ProcessEvidence{"not-in-inventory": nil}}, nil
	})
	value, err := builder.Build(test.Context(), request)
	requireBuilderBlocked(test, value, err, 2)
}

func TestBuilderBlocksConflictingKnownPIDBindings(test *testing.T) {
	for _, field := range []string{"state", "created_at", "executable", "cwd", "fingerprint", "error"} {
		for _, scope := range []string{"same_worktree", "two_worktrees"} {
			test.Run(field+"_"+scope, func(test *testing.T) {
				builder, request, base, clock := builderFixture(test)
				worktrees := []domain.Worktree{base, builderSibling(base, "second"), builderSibling(base, "third")}
				builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
					return worktrees, nil
				})
				first := domain.ProcessEvidence{PID: 42, State: domain.EvidenceInactive, CreatedAt: clock.Now().Add(-30 * 24 * time.Hour), Executable: "synthetic", CWD: base.Path, Fingerprint: "process-proof"}
				second := first
				switch field {
				case "state":
					second.State = domain.EvidenceActive
				case "created_at":
					second.CreatedAt = second.CreatedAt.Add(time.Second)
				case "executable":
					second.Executable = "different-program"
				case "cwd":
					second.CWD = worktrees[1].Path
				case "fingerprint":
					second.Fingerprint = "different-proof"
				case "error":
					second.Error = "conflicting inspection diagnostic"
				}
				collection := process.Collection{Complete: true, ByWorktree: map[string][]domain.ProcessEvidence{base.Path: {first}}}
				path := base.Path
				if scope == "two_worktrees" {
					path = worktrees[1].Path
				}
				collection.ByWorktree[path] = append(collection.ByWorktree[path], second)
				builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
					return collection, nil
				})
				value, err := builder.Build(test.Context(), request)
				requireBuilderBlocked(test, value, err, len(worktrees))
				if !strings.Contains(err.Error(), "conflicting process") {
					test.Fatalf("conflicting identity diagnostic lost: %v", err)
				}
			})
		}
	}
}

func TestBuilderAllowsIdenticalKnownPIDBindings(test *testing.T) {
	builder, request, base, clock := builderFixture(test)
	second := builderSibling(base, "second")
	builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
		return []domain.Worktree{base, second}, nil
	})
	evidence := domain.ProcessEvidence{PID: 42, State: domain.EvidenceInactive, CreatedAt: clock.Now().Add(-30 * 24 * time.Hour), Executable: "synthetic", CWD: base.Path, Fingerprint: "process-proof"}
	builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
		return process.Collection{Complete: true, ByWorktree: map[string][]domain.ProcessEvidence{
			base.Path: {evidence, evidence}, second.Path: {evidence},
		}}, nil
	})
	value, err := builder.Build(test.Context(), request)
	if err != nil || value.Summary.Safe != 2 {
		test.Fatalf("identical bound records were treated as conflicting: %#v, %v", value.Summary, err)
	}
	requireBuilderFingerprints(test, value)
}

func TestBuilderBlocksInvalidBindingPIDs(test *testing.T) {
	for _, evidence := range []domain.ProcessEvidence{
		{State: domain.EvidenceActive}, {State: domain.EvidenceInactive}, {State: domain.EvidenceNotApplicable},
		{State: domain.EvidenceUnknown}, {PID: -42, State: domain.EvidenceInactive},
		{PID: -42, State: domain.EvidenceUnknown, Error: "inspection failure"},
	} {
		builder, request, base, _ := builderFixture(test)
		builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
			return []domain.Worktree{base, builderSibling(base, "second")}, nil
		})
		builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
			return process.Collection{Complete: true, ByWorktree: map[string][]domain.ProcessEvidence{base.Path: {evidence}}}, nil
		})
		value, err := builder.Build(test.Context(), request)
		requireBuilderBlocked(test, value, err, 2)
		if !strings.Contains(err.Error(), "invalid process PID") {
			test.Fatalf("invalid PID diagnostic lost: %#v, %v", evidence, err)
		}
	}
}

func TestBuilderKeepsIndependentWorktreeFailureSentinelsLocal(test *testing.T) {
	builder, request, base, _ := builderFixture(test)
	second, healthy := builderSibling(base, "second"), builderSibling(base, "third")
	if err := os.Mkdir(healthy.Path, 0o700); err != nil {
		test.Fatal(err)
	}
	builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
		return []domain.Worktree{base, second, healthy}, nil
	})
	builder.Processes = process.Collector{Source: builderSourceFunc(func(context.Context) ([]process.Info, error) {
		return nil, nil
	})}
	value, err := builder.Build(test.Context(), request)
	if err == nil || value.Summary.Protected != 2 || value.Summary.Safe != 1 {
		test.Fatalf("independent PID-zero root failures lost locality: %#v, %v", value.Summary, err)
	}
	for _, candidate := range value.Candidates {
		if candidate.Worktree.Path == healthy.Path && (candidate.Decision.Classification != domain.Safe || candidate.Action != "none" || candidate.Snapshot.Required) {
			test.Fatalf("healthy neighbor was blocked: %#v", candidate)
		}
	}
	requireBuilderFingerprints(test, value)
}
