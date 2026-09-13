package plan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/process"
	"github.com/hellices/treeclear/internal/testutil"
)

type builderInventoryFunc func(context.Context, []string) ([]domain.Worktree, []error)

func (loader builderInventoryFunc) Load(ctx context.Context, roots []string) ([]domain.Worktree, []error) {
	return loader(ctx, roots)
}

type builderProcessFunc func(context.Context, []domain.Worktree) (process.Collection, []error)

func (collector builderProcessFunc) Collect(ctx context.Context, worktrees []domain.Worktree) (process.Collection, []error) {
	return collector(ctx, worktrees)
}

func builderFixture(test *testing.T) (Builder, Request, domain.Worktree, *testutil.Clock) {
	test.Helper()
	root := test.TempDir()
	clock := testutil.NewClock(time.Date(2026, time.September, 13, 12, 0, 0, 0, time.FixedZone("fixture", 9*60*60)))
	repository := filepath.Join(root, "repository")
	worktree := domain.Worktree{
		Path: filepath.Join(root, "feature"), RepositoryRoot: repository,
		CommonGitDir: filepath.Join(repository, ".git"), AdminDir: filepath.Join(repository, ".git", "worktrees", "feature"),
		Head: "head", Branch: "feature", Upstream: "origin/main", PathSafe: true, GitStateKnown: true,
		Recoverable: true, LastCommitAt: clock.Now().Add(-30 * 24 * time.Hour),
		MetadataModifiedAt: clock.Now().Add(-20 * 24 * time.Hour), EstimatedBytes: 128,
		IndexHash: "index", AdminHash: "admin",
	}
	request := Request{
		Roots: []string{root}, IntendedApplyMode: domain.ApplyInteractive, AdapterLockDigest: "adapter-lock-digest",
		Settings: domain.PolicySettings{
			InactivityThreshold: 7 * 24 * time.Hour, PlanExpiry: 15 * time.Minute,
			BaseBranches: []string{"release", "main"}, MinimumTrustGrade: domain.TrustVersionedPrivate, SnapshotMaxBytes: 64 << 20,
		},
	}
	builder := Builder{
		Inventory: builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
			return []domain.Worktree{worktree}, nil
		}),
		Processes: builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
			return process.Collection{Complete: true}, nil
		}),
		Now: clock.Now, Version: "builder-test",
	}
	return builder, request, worktree, clock
}

func builderSibling(worktree domain.Worktree, name string) domain.Worktree {
	worktree.Path = filepath.Join(filepath.Dir(worktree.Path), name)
	worktree.AdminDir = filepath.Join(filepath.Dir(worktree.AdminDir), name)
	return worktree
}

func requireBuilderZeroPlan(test *testing.T, value domain.Plan, err error) {
	test.Helper()
	if err == nil || !reflect.DeepEqual(value, domain.Plan{}) {
		test.Fatalf("Build() = %#v, %v; want zero plan and error", value, err)
	}
}

func requireBuilderFingerprints(test *testing.T, value domain.Plan) {
	test.Helper()
	for _, candidate := range value.Candidates {
		fingerprint, err := CandidateFingerprint(candidate)
		if err != nil || candidate.Fingerprint != fingerprint {
			test.Fatalf("candidate %q fingerprint = %q, want %q, %v", candidate.ID, candidate.Fingerprint, fingerprint, err)
		}
	}
}

func requireBuilderBlocked(test *testing.T, value domain.Plan, err error, count int) {
	test.Helper()
	if err == nil || !validPlanID(value.ID) || len(value.Candidates) != count {
		test.Fatalf("Build() returned %d candidates, ID %q, error %v", len(value.Candidates), value.ID, err)
	}
	if value.Summary != (domain.PlanSummary{Protected: count}) {
		test.Fatalf("incomplete plan summary = %#v", value.Summary)
	}
	for _, candidate := range value.Candidates {
		if candidate.Decision.Classification != domain.Protected || candidate.Action != "none" || candidate.Snapshot.Required || len(candidate.Evidence.Warnings) == 0 {
			test.Fatalf("incomplete collection did not protect candidate: %#v", candidate)
		}
	}
	requireBuilderFingerprints(test, value)
}

func TestBuilderClassifiesAndSelectsOnlySafeCandidates(test *testing.T) {
	builder, request, base, _ := builderFixture(test)
	safe := builderSibling(base, "alpha-safe")
	review := builderSibling(base, "beta-review")
	review.Detached, review.EstimatedBytes = true, math.MaxInt64
	protected := builderSibling(base, "gamma-protected")
	protected.Status.Untracked, protected.EstimatedBytes = 2, math.MaxInt64
	worktrees := []domain.Worktree{protected, safe, review}
	builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
		return worktrees, nil
	})
	value, err := builder.Build(context.Background(), request)
	if err != nil {
		test.Fatal(err)
	}
	if value.Summary != (domain.PlanSummary{Safe: 1, Review: 1, Protected: 1, ReclaimableBytes: safe.EstimatedBytes}) {
		test.Fatalf("summary = %#v", value.Summary)
	}
	if len(value.Candidates) != 3 {
		test.Fatalf("candidates = %#v", value.Candidates)
	}
	for index, classification := range []domain.Classification{domain.Safe, domain.Review, domain.Protected} {
		candidate := value.Candidates[index]
		if candidate.ID == "" || candidate.Decision.Classification != classification {
			test.Fatalf("candidate %d = %#v", index, candidate)
		}
		wantAction := "none"
		if classification == domain.Safe {
			wantAction = "remove"
		}
		if candidate.Action != wantAction || candidate.Snapshot.Required != (classification == domain.Safe) {
			test.Fatalf("candidate selection = %#v", candidate)
		}
		if candidate.Snapshot.MaximumBytes != request.Settings.SnapshotMaxBytes || candidate.Snapshot.UntrackedFiles != candidate.Worktree.Status.Untracked {
			test.Fatalf("snapshot requirements = %#v", candidate.Snapshot)
		}
		if len(candidate.Evidence.Adapters) != 0 || len(candidate.Evidence.Agents) != 0 {
			test.Fatalf("core-only builder invented adapters: %#v", candidate.Evidence)
		}
	}
	if warnings := strings.Join(value.Warnings, "\n"); !strings.Contains(strings.ToLower(warnings), "core-only") || !strings.Contains(warnings, "adapter") {
		test.Fatalf("missing core-only limitation: %q", warnings)
	}
	requireBuilderFingerprints(test, value)
}

func TestBuilderRecordsPolicyModeVersionAndExpiry(test *testing.T) {
	for _, mode := range []domain.ApplyMode{domain.ApplyInteractive, domain.ApplyScheduled} {
		test.Run(string(mode), func(test *testing.T) {
			builder, request, _, clock := builderFixture(test)
			request.IntendedApplyMode = mode
			value, err := builder.Build(context.Background(), request)
			if err != nil {
				test.Fatal(err)
			}
			digest, err := PolicyDigest(request.Settings)
			if err != nil {
				test.Fatal(err)
			}
			if value.SchemaVersion != 1 || value.ToolVersion != builder.Version || value.IntendedApplyMode != mode || value.AdapterLockDigest != request.AdapterLockDigest || value.PolicyDigest != digest {
				test.Fatalf("plan metadata = %#v", value)
			}
			if !value.GeneratedAt.Equal(clock.Now()) || value.GeneratedAt.Location() != time.UTC || !value.ExpiresAt.Equal(clock.Now().Add(request.Settings.PlanExpiry)) {
				test.Fatalf("plan lifetime = %v to %v", value.GeneratedAt, value.ExpiresAt)
			}
			if !slices.Equal(value.Scope.Roots, request.Roots) || value.Integrity != (domain.PlanIntegrity{}) {
				test.Fatalf("scope/integrity = %#v / %#v", value.Scope, value.Integrity)
			}
		})
	}
}

func TestBuilderUsesExplicitPolicyAndDefaultClock(test *testing.T) {
	builder, request, _, _ := builderFixture(test)
	request.Settings.InactivityThreshold = 60 * 24 * time.Hour
	value, err := builder.Build(context.Background(), request)
	if err != nil || value.Summary.Protected != 1 || value.Candidates[0].Decision.Reasons[0].Code != "recent" {
		test.Fatalf("explicit inactivity threshold ignored: %#v, %v", value, err)
	}
	builder.Now = nil
	before := time.Now()
	value, err = builder.Build(context.Background(), request)
	after := time.Now()
	if err != nil || value.GeneratedAt.Before(before) || value.GeneratedAt.After(after) || !value.ExpiresAt.Equal(value.GeneratedAt.Add(request.Settings.PlanExpiry)) {
		test.Fatalf("default clock = %v, %v", value.GeneratedAt, err)
	}
}

func TestBuilderRejectsInvalidRequestsBeforeCollection(test *testing.T) {
	scenarios := []struct {
		name   string
		change func(*Builder, *Request)
	}{
		{"missing inventory", func(builder *Builder, request *Request) { builder.Inventory = nil }},
		{"typed nil inventory", func(builder *Builder, request *Request) { builder.Inventory = builderInventoryFunc(nil) }},
		{"missing processes", func(builder *Builder, request *Request) { builder.Processes = nil }},
		{"typed nil processes", func(builder *Builder, request *Request) { builder.Processes = (*process.Collector)(nil) }},
		{"no roots", func(builder *Builder, request *Request) { request.Roots = nil }},
		{"empty root", func(builder *Builder, request *Request) { request.Roots = []string{""} }},
		{"relative root", func(builder *Builder, request *Request) { request.Roots = []string{"relative"} }},
		{"unclean root", func(builder *Builder, request *Request) { request.Roots[0] += string(filepath.Separator) + "." }},
		{"NUL root", func(builder *Builder, request *Request) { request.Roots[0] += "\x00" }},
		{"invalid root encoding", func(builder *Builder, request *Request) { request.Roots[0] += "\xff" }},
		{"missing apply mode", func(builder *Builder, request *Request) { request.IntendedApplyMode = "" }},
		{"unknown apply mode", func(builder *Builder, request *Request) { request.IntendedApplyMode = "future-mode" }},
		{"zero inactivity", func(builder *Builder, request *Request) { request.Settings.InactivityThreshold = 0 }},
		{"negative inactivity", func(builder *Builder, request *Request) { request.Settings.InactivityThreshold = -time.Second }},
		{"zero expiry", func(builder *Builder, request *Request) { request.Settings.PlanExpiry = 0 }},
		{"negative expiry", func(builder *Builder, request *Request) { request.Settings.PlanExpiry = -time.Second }},
		{"zero snapshot limit", func(builder *Builder, request *Request) { request.Settings.SnapshotMaxBytes = 0 }},
		{"negative snapshot limit", func(builder *Builder, request *Request) { request.Settings.SnapshotMaxBytes = -1 }},
		{"missing trust grade", func(builder *Builder, request *Request) { request.Settings.MinimumTrustGrade = "" }},
		{"unknown trust grade", func(builder *Builder, request *Request) { request.Settings.MinimumTrustGrade = "future-grade" }},
		{"invalid policy encoding", func(builder *Builder, request *Request) { request.Settings.BaseBranches = []string{"\xff"} }},
		{"invalid version encoding", func(builder *Builder, request *Request) { builder.Version = "\xff" }},
		{"invalid adapter digest encoding", func(builder *Builder, request *Request) { request.AdapterLockDigest = "\xff" }},
		{"zero clock", func(builder *Builder, request *Request) { builder.Now = testutil.NewClock(time.Time{}).Now }},
		{"unencodable clock", func(builder *Builder, request *Request) {
			builder.Now = testutil.NewClock(time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)).Now
		}},
		{"unencodable expiry", func(builder *Builder, request *Request) {
			builder.Now = testutil.NewClock(time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)).Now
		}},
	}
	for _, scenario := range scenarios {
		test.Run(scenario.name, func(test *testing.T) {
			builder, request, _, _ := builderFixture(test)
			builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
				test.Fatal("invalid request reached inventory")
				return nil, nil
			})
			builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
				test.Fatal("invalid request reached processes")
				return process.Collection{}, nil
			})
			scenario.change(&builder, &request)
			value, err := builder.Build(context.Background(), request)
			requireBuilderZeroPlan(test, value, err)
		})
	}
}

func TestBuilderCancellationReturnsNoPlan(test *testing.T) {
	for _, phase := range []string{"before collection", "inventory", "processes"} {
		test.Run(phase, func(test *testing.T) {
			builder, request, worktree, _ := builderFixture(test)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("collection failed before cancellation")
			builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
				if phase == "before collection" {
					test.Fatal("cancelled build reached inventory")
				}
				if phase == "inventory" {
					cancel()
					return []domain.Worktree{worktree}, []error{nil, failure}
				}
				return []domain.Worktree{worktree}, nil
			})
			builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
				if phase != "processes" {
					test.Fatal("cancelled build reached processes")
				}
				cancel()
				return process.Collection{Complete: true}, []error{nil, failure}
			})
			if phase == "before collection" {
				cancel()
			}
			value, err := builder.Build(ctx, request)
			requireBuilderZeroPlan(test, value, err)
			if !errors.Is(err, context.Canceled) || (phase != "before collection" && !errors.Is(err, failure)) {
				test.Fatalf("cancellation lost errors: %v", err)
			}
		})
	}
	builder, request, _, _ := builderFixture(test)
	value, err := builder.Build(nil, request)
	requireBuilderZeroPlan(test, value, err)
}

func TestBuilderCollectsAllInventoryBeforeBoundedProcesses(test *testing.T) {
	builder, request, first, _ := builderFixture(test)
	worktrees := []domain.Worktree{first, builderSibling(first, "second")}
	inventoryComplete := false
	builder.Inventory = builderInventoryFunc(func(ctx context.Context, roots []string) ([]domain.Worktree, []error) {
		if !slices.Equal(roots, request.Roots) {
			test.Fatalf("inventory roots = %v", roots)
		}
		inventoryComplete = true
		return worktrees, nil
	})
	var processContext context.Context
	builder.Processes = builderProcessFunc(func(ctx context.Context, collected []domain.Worktree) (process.Collection, []error) {
		if !inventoryComplete || !reflect.DeepEqual(collected, worktrees) {
			test.Fatalf("process collection received partial inventory: %#v", collected)
		}
		deadline, exists := ctx.Deadline()
		if !exists || time.Until(deadline) > 30*time.Second || time.Until(deadline) < 29*time.Second {
			test.Fatalf("process context deadline = %v, present %v", deadline, exists)
		}
		processContext = ctx
		return process.Collection{Complete: true}, nil
	})
	if _, err := builder.Build(context.Background(), request); err != nil {
		test.Fatal(err)
	}
	if !errors.Is(processContext.Err(), context.Canceled) {
		test.Fatal("process context was not released")
	}
}

func TestBuilderPartialInventoryAndProcessErrorsRemainInspectable(test *testing.T) {
	builder, request, first, _ := builderFixture(test)
	inventoryFailure := errors.New("inventory permission denied")
	processFailure := errors.New("process inspection denied")
	builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
		return []domain.Worktree{first, builderSibling(first, "second")}, []error{nil, inventoryFailure}
	})
	builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
		return process.Collection{Complete: true}, []error{processFailure, nil}
	})
	value, err := builder.Build(context.Background(), request)
	requireBuilderBlocked(test, value, err, 2)
	if !errors.Is(err, inventoryFailure) || !errors.Is(err, processFailure) {
		test.Fatalf("collection error identity lost: %v", err)
	}
	for _, diagnostic := range []string{inventoryFailure.Error(), processFailure.Error()} {
		if !strings.Contains(strings.Join(value.Warnings, "\n"), diagnostic) {
			test.Fatalf("plan lost diagnostic %q: %v", diagnostic, value.Warnings)
		}
		for _, candidate := range value.Candidates {
			if !strings.Contains(strings.Join(candidate.Evidence.Warnings, "\n"), diagnostic) {
				test.Fatalf("candidate lost blocking diagnostic %q: %#v", diagnostic, candidate)
			}
		}
	}
}

func TestBuilderDetectsSilentProcessFailures(test *testing.T) {
	scenarios := []struct {
		name       string
		collection func(domain.Worktree) process.Collection
		diagnostic string
	}{
		{"incomplete", func(domain.Worktree) process.Collection { return process.Collection{} }, "incomplete"},
		{"collection diagnostics", func(domain.Worktree) process.Collection {
			return process.Collection{Complete: true, Errors: []string{"enumerator diagnostic"}}
		}, "enumerator diagnostic"},
		{"global unknown", func(domain.Worktree) process.Collection {
			return process.Collection{Complete: true, GlobalUnknown: []domain.ProcessEvidence{{State: domain.EvidenceUnknown, Error: "global denial"}}}
		}, "global denial"},
		{"silent global unknown", func(domain.Worktree) process.Collection {
			return process.Collection{Complete: true, GlobalUnknown: []domain.ProcessEvidence{{State: domain.EvidenceUnknown}}}
		}, "unknown"},
		{"uninspectable", func(domain.Worktree) process.Collection {
			return process.Collection{Complete: true, Uninspectable: map[int32]domain.ProcessEvidence{42: {PID: 42, State: domain.EvidenceUnknown, Error: "hidden process"}}}
		}, "hidden process"},
		{"worktree unknown", func(worktree domain.Worktree) process.Collection {
			return process.Collection{Complete: true, ByWorktree: map[string][]domain.ProcessEvidence{worktree.Path: {{PID: 42, State: domain.EvidenceUnknown, Error: "cwd access denied"}}}}
		}, "cwd access denied"},
		{"error with inactive state", func(worktree domain.Worktree) process.Collection {
			return process.Collection{Complete: true, ByWorktree: map[string][]domain.ProcessEvidence{worktree.Path: {{PID: 42, State: domain.EvidenceInactive, Error: "inspection was not complete"}}}}
		}, "inspection was not complete"},
		{"unknown state", func(worktree domain.Worktree) process.Collection {
			return process.Collection{Complete: true, ByWorktree: map[string][]domain.ProcessEvidence{worktree.Path: {{PID: 42, State: "future-state"}}}}
		}, "unknown"},
	}
	for _, scenario := range scenarios {
		test.Run(scenario.name, func(test *testing.T) {
			builder, request, worktree, _ := builderFixture(test)
			builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
				return scenario.collection(worktree), []error{nil}
			})
			value, err := builder.Build(context.Background(), request)
			requireBuilderBlocked(test, value, err, 1)
			if !strings.Contains(strings.Join(value.Warnings, "\n"), scenario.diagnostic) || !strings.Contains(err.Error(), scenario.diagnostic) {
				test.Fatalf("missing process diagnostic %q: %v / %v", scenario.diagnostic, value.Warnings, err)
			}
		})
	}
}

func TestBuilderWorktreeErrorsBlockEvenWhenGitStateClaimsKnown(test *testing.T) {
	builder, request, worktree, _ := builderFixture(test)
	worktree.CollectionErrors = []string{"index inspection failed"}
	builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
		return []domain.Worktree{worktree}, []error{nil}
	})
	value, err := builder.Build(context.Background(), request)
	requireBuilderBlocked(test, value, err, 1)
	if !strings.Contains(strings.Join(value.Candidates[0].Evidence.Warnings, "\n"), worktree.CollectionErrors[0]) {
		test.Fatalf("worktree diagnostics were dropped: %#v", value.Candidates[0])
	}
}

func TestBuilderMissingGitProofsProtectOnlyAffectedWorktrees(test *testing.T) {
	scenarios := []struct {
		name       string
		diagnostic string
		clear      func(*domain.Worktree)
	}{
		{"Head", "HEAD", func(worktree *domain.Worktree) { worktree.Head = "" }},
		{"IndexHash", "index hash", func(worktree *domain.Worktree) { worktree.IndexHash = "" }},
		{"AdminHash", "administrative hash", func(worktree *domain.Worktree) { worktree.AdminHash = "" }},
	}
	for _, scenario := range scenarios {
		test.Run(scenario.name, func(test *testing.T) {
			builder, request, base, _ := builderFixture(test)
			worktrees := []domain.Worktree{builderSibling(base, "a-missing-proof"), builderSibling(base, "b-healthy")}
			builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
				return worktrees, nil
			})
			before, err := builder.Build(context.Background(), request)
			if err != nil || before.Summary.Safe != 2 {
				test.Fatalf("complete proofs = %#v, %v", before.Summary, err)
			}
			scenario.clear(&worktrees[0])
			value, err := builder.Build(context.Background(), request)
			if err == nil || !validPlanID(value.ID) || len(value.Candidates) != 2 {
				test.Fatalf("missing %s: summary = %#v, error = %v; want inspectable plan and error", scenario.name, value.Summary, err)
			}
			candidate := value.Candidates[0]
			if candidate.Decision.Classification != domain.Protected || candidate.Action != "none" || candidate.Snapshot.Required {
				test.Fatalf("missing %s did not protect worktree: %#v", scenario.name, candidate)
			}
			if !candidate.Worktree.GitStateKnown || !reflect.DeepEqual(candidate.Worktree, worktrees[0]) {
				test.Fatalf("missing proof changed collected Git state: %#v", candidate.Worktree)
			}
			if !strings.Contains(err.Error(), scenario.diagnostic) || !strings.Contains(strings.Join(value.Warnings, "\n"), scenario.diagnostic) || !strings.Contains(strings.Join(candidate.Evidence.Warnings, "\n"), scenario.diagnostic) {
				test.Fatalf("missing %s diagnostic: error = %v, plan = %v, evidence = %v", scenario.name, err, value.Warnings, candidate.Evidence.Warnings)
			}
			if !reflect.DeepEqual(value.Candidates[1], before.Candidates[1]) {
				test.Fatalf("missing proof changed healthy candidate: %#v", value.Candidates[1])
			}
			if value.Summary != (domain.PlanSummary{Protected: 1, Safe: 1, ReclaimableBytes: worktrees[1].EstimatedBytes}) {
				test.Fatalf("missing proof summary = %#v", value.Summary)
			}
			if candidate.ID != before.Candidates[0].ID || candidate.Fingerprint == before.Candidates[0].Fingerprint {
				test.Fatalf("missing proof identity/fingerprint = %#v", candidate)
			}
			requireBuilderFingerprints(test, value)
		})
	}
}

func TestBuilderStableCandidateIDsOrderAndUniquePlanIDs(test *testing.T) {
	builder, request, first, _ := builderFixture(test)
	worktrees := []domain.Worktree{builderSibling(first, "z-last"), builderSibling(first, "a-first")}
	builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
		return worktrees, nil
	})
	identifiers := make(map[string]bool)
	var original domain.Plan
	for iteration := range 10 {
		slices.Reverse(worktrees)
		value, err := builder.Build(context.Background(), request)
		if err != nil || !validPlanID(value.ID) || identifiers[value.ID] {
			test.Fatalf("invalid/repeated plan ID %q, error %v", value.ID, err)
		}
		identifiers[value.ID] = true
		if len(value.Candidates) != 2 || value.Candidates[0].Worktree.Path >= value.Candidates[1].Worktree.Path || value.Candidates[0].ID == value.Candidates[1].ID {
			test.Fatalf("candidate order/identity = %#v", value.Candidates)
		}
		value.ID = ""
		if iteration == 0 {
			original = value
		} else if !reflect.DeepEqual(value, original) {
			test.Fatalf("unchanged inventory changed plan content: %#v", value)
		}
	}
	for index := range worktrees {
		worktrees[index].Head = "different-head"
		worktrees[index].Status.Staged = 1
	}
	changed, err := builder.Build(context.Background(), request)
	if err != nil {
		test.Fatal(err)
	}
	for index, candidate := range changed.Candidates {
		if candidate.ID != original.Candidates[index].ID || candidate.Fingerprint == original.Candidates[index].Fingerprint {
			test.Fatalf("identity must be stable while preconditions change: %#v", candidate)
		}
	}
	requireBuilderFingerprints(test, changed)
}

func TestBuilderPreservesCallerOwnedSlicesAndEvidence(test *testing.T) {
	builder, request, worktree, _ := builderFixture(test)
	request.Roots = append(request.Roots, filepath.Join(request.Roots[0], "another-root"))
	slices.Reverse(request.Roots)
	worktree.CollectionErrors = []string{"worktree inspection failed", "spare worktree error"}[:1]
	worktrees := []domain.Worktree{builderSibling(worktree, "z-last"), builderSibling(worktree, "a-first")}
	collection := process.Collection{
		Complete: true, Errors: []string{"process diagnostic", "spare process diagnostic"}[:1],
		ByWorktree: map[string][]domain.ProcessEvidence{
			worktrees[0].Path: {{PID: 42, CreatedAt: worktree.LastCommitAt, State: domain.EvidenceInactive, Fingerprint: "process"}},
		},
	}
	inputs := func() []byte {
		encoded, err := json.Marshal([]any{request, worktrees, collection})
		if err != nil {
			test.Fatal(err)
		}
		return encoded
	}
	before := inputs()
	builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) { return worktrees, nil })
	builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) { return collection, nil })
	value, err := builder.Build(context.Background(), request)
	requireBuilderBlocked(test, value, err, 2)
	if !bytes.Equal(before, inputs()) {
		test.Fatal("Build mutated caller input")
	}
	value.Scope.Roots[0] = "changed root"
	value.Candidates[1].Worktree.CollectionErrors[0] = "changed worktree diagnostic"
	value.Candidates[1].Evidence.Processes[0].Fingerprint = "changed process"
	if !bytes.Equal(before, inputs()) {
		test.Fatal("returned plan aliases caller input")
	}
}

func TestBuilderRejectsReclaimableByteOverflow(test *testing.T) {
	builder, request, first, _ := builderFixture(test)
	first.EstimatedBytes = math.MaxInt64
	second := builderSibling(first, "second")
	second.EstimatedBytes = 1
	builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
		return []domain.Worktree{first, second}, nil
	})
	value, err := builder.Build(context.Background(), request)
	requireBuilderZeroPlan(test, value, err)
	if !strings.Contains(err.Error(), "overflow") {
		test.Fatalf("overflow diagnostic = %v", err)
	}
}

func TestBuilderRejectsMalformedOrDuplicateWorktreePaths(test *testing.T) {
	scenarios := []struct {
		name   string
		change func(domain.Worktree) []domain.Worktree
	}{
		{"missing path", func(worktree domain.Worktree) []domain.Worktree {
			worktree.Path = ""
			return []domain.Worktree{worktree}
		}},
		{"relative path", func(worktree domain.Worktree) []domain.Worktree {
			worktree.Path = "relative"
			return []domain.Worktree{worktree}
		}},
		{"noncanonical path", func(worktree domain.Worktree) []domain.Worktree {
			worktree.Path += string(filepath.Separator) + "."
			return []domain.Worktree{worktree}
		}},
		{"NUL path", func(worktree domain.Worktree) []domain.Worktree {
			worktree.Path += "\x00"
			return []domain.Worktree{worktree}
		}},
		{"invalid path encoding", func(worktree domain.Worktree) []domain.Worktree {
			worktree.Path += "\xff"
			return []domain.Worktree{worktree}
		}},
		{"duplicate identity", func(worktree domain.Worktree) []domain.Worktree {
			return []domain.Worktree{worktree, worktree}
		}},
		{"conflicting path", func(worktree domain.Worktree) []domain.Worktree {
			conflict := worktree
			conflict.Head, conflict.CommonGitDir = "conflicting-head", worktree.CommonGitDir+"-other"
			return []domain.Worktree{worktree, conflict}
		}},
		{"shared admin identity", func(worktree domain.Worktree) []domain.Worktree {
			conflict := worktree
			conflict.Path += "-other"
			return []domain.Worktree{worktree, conflict}
		}},
	}
	for _, scenario := range scenarios {
		test.Run(scenario.name, func(test *testing.T) {
			builder, request, worktree, _ := builderFixture(test)
			builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
				return scenario.change(worktree), nil
			})
			builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
				test.Fatal("ambiguous inventory reached process collection")
				return process.Collection{}, nil
			})
			value, err := builder.Build(context.Background(), request)
			requireBuilderZeroPlan(test, value, err)
		})
	}
}

func TestBuilderMissingOrMalformedWorktreeEvidenceRemainsInspectable(test *testing.T) {
	scenarios := []struct {
		name   string
		change func(*domain.Worktree)
	}{
		{"missing repository", func(worktree *domain.Worktree) { worktree.RepositoryRoot = "" }},
		{"missing common directory", func(worktree *domain.Worktree) { worktree.CommonGitDir = "" }},
		{"missing admin directory", func(worktree *domain.Worktree) { worktree.AdminDir = "" }},
		{"relative metadata identity", func(worktree *domain.Worktree) { worktree.AdminDir = "relative" }},
		{"primary path claimed linked", func(worktree *domain.Worktree) { worktree.Path = worktree.RepositoryRoot }},
		{"primary admin claimed linked", func(worktree *domain.Worktree) { worktree.AdminDir = worktree.CommonGitDir }},
		{"unknown Git state", func(worktree *domain.Worktree) { worktree.GitStateKnown = false }},
		{"missing commit time", func(worktree *domain.Worktree) { worktree.LastCommitAt = time.Time{} }},
		{"missing metadata time", func(worktree *domain.Worktree) { worktree.MetadataModifiedAt = time.Time{} }},
		{"negative size", func(worktree *domain.Worktree) { worktree.EstimatedBytes = -1 }},
		{"negative untracked count", func(worktree *domain.Worktree) { worktree.Status.Untracked = -1 }},
		{"silent worktree error", func(worktree *domain.Worktree) { worktree.CollectionErrors = []string{""} }},
	}
	for _, scenario := range scenarios {
		test.Run(scenario.name, func(test *testing.T) {
			builder, request, worktree, _ := builderFixture(test)
			scenario.change(&worktree)
			builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
				return []domain.Worktree{worktree}, nil
			})
			value, err := builder.Build(context.Background(), request)
			requireBuilderBlocked(test, value, err, 1)
		})
	}
}

func TestBuilderUnencodableEvidenceReturnsNoPlan(test *testing.T) {
	for _, field := range []string{"worktree", "process", "diagnostic"} {
		test.Run(field, func(test *testing.T) {
			builder, request, worktree, _ := builderFixture(test)
			failure := errors.New("retained collection failure")
			if field == "worktree" {
				worktree.LastCommitAt = time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
			}
			builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
				return []domain.Worktree{worktree}, []error{failure}
			})
			builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
				collection := process.Collection{Complete: true}
				if field == "process" {
					collection.ByWorktree = map[string][]domain.ProcessEvidence{worktree.Path: {{State: domain.EvidenceInactive, Executable: "\xff"}}}
				}
				if field == "diagnostic" {
					collection.Errors = []string{"\xff"}
				}
				return collection, nil
			})
			value, err := builder.Build(context.Background(), request)
			requireBuilderZeroPlan(test, value, err)
			if !errors.Is(err, failure) {
				test.Fatalf("encoding failure lost collection diagnostic: %v", err)
			}
		})
	}
}

func TestBuilderKeepsProcessActivityScoped(test *testing.T) {
	builder, request, first, clock := builderFixture(test)
	second := builderSibling(first, "second")
	builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
		return []domain.Worktree{first, second}, nil
	})
	builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
		return process.Collection{Complete: true, ByWorktree: map[string][]domain.ProcessEvidence{
			first.Path: {{PID: 42, CreatedAt: clock.Now().Add(-time.Hour), CWD: first.Path, State: domain.EvidenceActive, Executable: "editor", Fingerprint: "active-process"}},
		}}, nil
	})
	value, err := builder.Build(context.Background(), request)
	if err != nil || value.Summary != (domain.PlanSummary{Protected: 1, Safe: 1, ReclaimableBytes: second.EstimatedBytes}) {
		test.Fatalf("scoped activity summary = %#v, %v", value.Summary, err)
	}
	if len(value.Candidates[0].Evidence.Processes) != 1 || value.Candidates[0].Decision.Reasons[0].Code != "active_process" || value.Candidates[1].Action != "remove" {
		test.Fatalf("process correlation = %#v", value.Candidates)
	}
	requireBuilderFingerprints(test, value)
}

func TestBuilderIsolatesDependencyArguments(test *testing.T) {
	builder, request, worktree, _ := builderFixture(test)
	originalRoot := request.Roots[0]
	builder.Inventory = builderInventoryFunc(func(ctx context.Context, roots []string) ([]domain.Worktree, []error) {
		roots[0] = "loader-mutated-root"
		return []domain.Worktree{worktree}, nil
	})
	builder.Processes = builderProcessFunc(func(ctx context.Context, worktrees []domain.Worktree) (process.Collection, []error) {
		worktrees[0].Head = "collector-mutated-head"
		return process.Collection{Complete: true}, nil
	})
	value, err := builder.Build(context.Background(), request)
	if err != nil || request.Roots[0] != originalRoot || value.Scope.Roots[0] != originalRoot || !reflect.DeepEqual(value.Candidates[0].Worktree, worktree) {
		test.Fatalf("dependency mutated planning input: %#v, %v", value, err)
	}
}

func TestBuilderProcessDeadlineFailureIsNotParentCancellation(test *testing.T) {
	builder, request, _, _ := builderFixture(test)
	builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
		return process.Collection{Complete: true}, []error{context.DeadlineExceeded}
	})
	value, err := builder.Build(context.Background(), request)
	requireBuilderBlocked(test, value, err, 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		test.Fatalf("process deadline diagnostic = %v", err)
	}
}

type builderNilError struct {
	message string
}

func (failure *builderNilError) Error() string {
	return failure.message
}

func TestBuilderIgnoresNilErrorEntriesWithoutPanicking(test *testing.T) {
	builder, request, worktree, _ := builderFixture(test)
	var failure *builderNilError
	builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
		return []domain.Worktree{worktree}, []error{nil, failure}
	})
	builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
		return process.Collection{Complete: true}, []error{failure, nil}
	})
	value, err := builder.Build(context.Background(), request)
	if err != nil || value.Summary.Safe != 1 || value.Candidates[0].Action != "remove" {
		test.Fatalf("nil diagnostic became a failure: %#v, %v", value, err)
	}
}

func TestBuilderAcceptsExplicitTrustGradesAndEmptyBaseBranches(test *testing.T) {
	for _, grade := range []domain.TrustGrade{
		domain.TrustSupportedAPI, domain.TrustSupportedAppServer, domain.TrustSupportedCLI,
		domain.TrustExperimentalAPI, domain.TrustVersionedPrivate, domain.TrustUnversionedPrivate, domain.TrustProcessOnly,
	} {
		test.Run(string(grade), func(test *testing.T) {
			builder, request, _, _ := builderFixture(test)
			request.Settings.MinimumTrustGrade, request.Settings.BaseBranches = grade, nil
			request.AdapterLockDigest = ""
			value, err := builder.Build(context.Background(), request)
			if err != nil || value.Summary.Safe != 1 || value.AdapterLockDigest != "" {
				test.Fatalf("explicit core-only policy = %#v, %v", value, err)
			}
			digest, err := PolicyDigest(request.Settings)
			if err != nil || value.PolicyDigest != digest {
				test.Fatalf("explicit policy digest = %q, want %q, %v", value.PolicyDigest, digest, err)
			}
		})
	}
}

func TestBuilderEmptyInventorySkipsProcesses(test *testing.T) {
	for _, failure := range []error{nil, errors.New("inventory unavailable")} {
		builder, request, _, _ := builderFixture(test)
		builder.Inventory = builderInventoryFunc(func(context.Context, []string) ([]domain.Worktree, []error) {
			return nil, []error{nil, failure}
		})
		builder.Processes = builderProcessFunc(func(context.Context, []domain.Worktree) (process.Collection, []error) {
			test.Fatal("empty inventory reached process collector")
			return process.Collection{}, nil
		})
		value, err := builder.Build(context.Background(), request)
		if (err == nil) != (failure == nil) || !errors.Is(err, failure) || !validPlanID(value.ID) || value.Candidates == nil || len(value.Candidates) != 0 || value.Summary != (domain.PlanSummary{}) {
			test.Fatalf("empty inventory plan = %#v, %v", value, err)
		}
	}
}
