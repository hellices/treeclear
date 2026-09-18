# Explicit Selection Preview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record exact worktree selection and the no-backup disposal choice in authenticated, permanently non-executable version-2 previews.

**Architecture:** Extend the existing domain, builder, private plan store, and CLI rather than creating another planning framework. Preserve version-1 serialization and inspection, while newly generated plans contain explicit selection and disposal records and only `action=none`. Ordinary policy classification is displayed as inventory information, not as explicit-removal eligibility.

**Tech Stack:** Go 1.26.5 (language 1.26.0), Git 2.36 or newer, existing Cobra and standard-library packages; no new dependencies.

## Global Constraints

- Use the existing isolated worktree; do not create nested worktrees.
- Write a failing test before implementing behavior or fixing a bug.
- Use `internal/testutil` and temporary directories for Git fixtures.
- Never test against real workspaces, agent databases, or scheduler jobs.
- Do not mutate global Git config or process-wide cwd/environment in helpers.
- Unknown, inaccessible, malformed, or conflicting evidence blocks cleanup.
- Explicit whole-worktree removal defaults to no backup; `--skip-dirty` and requested backup are separate plan-bound choices.
- Preserve local branches in every removal mode.
- This PR exposes no mutation, force, confirmation, backup, or restore implementation.
- Every v2 candidate has `action=none` and an empty, non-required snapshot plan, including selected clean candidates.
- V1 plans remain byte-compatible for authenticated inspection; neither v1 nor v2 previews may be loaded for apply.
- The independent snapshot-manifest version remains unchanged.
- Native macOS arm64/amd64 and Windows compatibility CI remain required.
- Issue #23's unavailable source assessment is not retried, rerouted, or cleared by this slice. Issue #18 and the other release prerequisites remain open.

## Approval and Stage Boundary

On September 18, 2026 the maintainer approved continuing from the written
[removal contract](../specs/2026-09-18-explicit-worktree-removal.md).
Documentation PR #28 was merged at `1d4fca390e28db9b68a7ee7456df0892d1f7cae9`
after its independent AI documentation review and three native CI jobs.
That review is not human technical approval or a verdict on #23.

This is the first read-only part of delivery step 2. It deliberately does
not implement the later dedicated explicit-removal eligibility evaluator.
The existing short-circuit policy remains unchanged: it must not be used
to grant dirty targets permission by discarding its first reason. Native
identity/content capture, shared locking, journaling, Git removal, and
confirmation remain in separately reviewed, prerequisite-gated slices.
Preview-only plans must never acquire executable semantics after an upgrade.

## File and Interface Map

| Files | Responsibility |
| --- | --- |
| `internal/domain/plan.go`, `internal/domain/removal.go` | Optional v2 plan and candidate records; v1 omits them |
| `internal/plan/preview.go`, `internal/plan/preview_test.go` | Selection matching, schema contract, and apply-load refusal |
| `internal/plan/build.go`, `internal/plan/build_test.go`, `internal/plan/build_bindings_test.go` | Generate v2 previews using unchanged collectors and ordinary classification |
| `internal/plan/store.go`, `internal/plan/fingerprint.go`, `internal/plan/preview_integrity_test.go` | Authenticated migration, semantic validation, selected-candidate fingerprint binding |
| `internal/cli/plan.go`, `internal/cli/explain.go`, `internal/cli/selection_test.go` | Literal flags, relative resolution, safe rendering and versioned explanation |
| Existing CLI tests | Change only assertions superseded by the new preview contract |
| `tests/e2e/plan_test.go`, `tests/e2e/installed_runtime_test.go`, `tests/e2e/explicit_preview_test.go` | Real Git and locally installed binary read-only acceptance |
| `README.md`, plan index, active core plan, architecture amendment, removal spec | Exact availability and approval records |

The domain additions are:

```go
type RemovalPlan struct {
    Intent             string   `json:"intent"`
    ContentDisposition string   `json:"contentDisposition"`
    BackupMode         string   `json:"backupMode"`
    SkipDirty          bool     `json:"skipDirty"`
    SelectedPaths      []string `json:"selectedPaths"`
    Execution          string   `json:"execution"`
}

type CandidateSelection struct {
    Selected   bool   `json:"selected"`
    SkipReason string `json:"skipReason"`
}
```

`domain.Plan.Removal` is `*domain.RemovalPlan` with `json:"removal,omitempty"`.
`domain.Candidate.Selection` is `*domain.CandidateSelection` with
`json:"selection,omitempty"`. Append the optional field to the existing
candidate fingerprint preconditions, preserving the v1 byte representation
when absent. Put the plan field before the final integrity record.

Allowed strings are `inventory-preview` or `explicit-worktree-removal` for
intent; `discard-all`, `none`, and `preview-only` for disposition, backup,
and execution. An unselected or included candidate has empty `skipReason`;
a selected candidate with known dirty status and `skipDirty=true` has
`skipReason=dirty`. Neither selection nor a skip result claims eligibility.

Extend `plan.Request` with `SelectedPaths []string`, `SkipDirty bool`, and
`BackupRequested bool`. Paths passed to the builder are canonical absolute
registered roots. CLI resolution uses `resolveInputPath` followed by
`pathutil.Canonical`, always with the resolved invocation directory. The
builder clones and sorts selection, rejects duplicates/overlap, requires
exact inventory matches, and rejects a primary-root selection. It does not
read new native source identities or change a collector.

Add `Store.LoadForApply(context.Context, string) (domain.Plan, error)` as a
read-only rejection boundary: authenticate/load first, then return a zero
plan and `ErrPlanNotExecutable` for both historical v1 and preview-only v2.
There is no apply command and no successful execution path in this slice.

## Task 1: Versioned Authenticated Preview

**Consumes:** Existing `Builder.Build`, `CandidateFingerprint`, `Store.Save`,
`Store.Load`, and inventory/process interfaces.

**Produces:** The domain fields and request fields above; builder-generated
v2 previews; validation in `validateStoredPlan`; `Store.LoadForApply`.

- [x] Add a regression test before changing the builder:

```go
func TestBuilderDefaultIsNonExecutablePreview(test *testing.T) {
    builder, request, _, _ := builderFixture(test)
    value, err := builder.Build(context.Background(), request)
    if err != nil {
        test.Fatal(err)
    }
    if value.SchemaVersion != 2 || value.Candidates[0].Action != "none" || value.Candidates[0].Snapshot.Required {
        test.Fatalf("inventory unexpectedly authorizes removal: %#v", value)
    }
}
```

- [x] Run `go test -count=1 ./internal/plan -run '^TestBuilderDefaultIsNonExecutablePreview$'`; expect failure because the v1 builder proposes `remove` with a mandatory snapshot.
- [x] Add table tests for default/explicit selection, dirty categories, ignored-only clean status, skip-dirty, unselected preservation, missing/duplicate/overlapping/primary roots, scheduled explicit requests, and backup refusal before collection.
- [x] Implement only the preview request validation and selection mapping. All actions remain `none`, all snapshot fields are zero, and reclaimable bytes are zero; retain ordinary classification counts and incomplete-collection diagnostics.
- [x] Add failing store tests for missing/unknown disposal fields, execution upgrades, inconsistent selection/skip records, changed actions, stale fingerprints, and v1 fields being attached to a legacy schema. Implement strict v2 validation before saving or accepting an authenticated document.
- [x] Preserve the existing v1 known-canonical-MAC vector unchanged. Add v2 round-trip, unsigned field-tamper, canonically re-signed missing-field, and load-for-apply refusal tests. Keep the original v1 snapshot requirement when explaining a historical plan.
- [x] Run `go test -count=1 ./internal/domain ./internal/plan`; expect success. Existing policy tests and v1 integrity vectors remain intact, and only superseded preview-action assertions change.
- [x] Review the core diff for spec compliance and quality before final integration.

## Task 2: Literal Selection and Honest CLI Output

**Consumes:** `Request.SelectedPaths`, `Request.SkipDirty`,
`Request.BackupRequested`, `Plan.Removal`, and `Candidate.Selection` from
Task 1. This task can proceed concurrently in a disjoint CLI write scope.

**Produces:** Repeatable `--worktree`, `--skip-dirty`, explicitly unavailable
`--backup`, v2 human plan/explain and JSON explanation.

- [x] Add failing tests using `planFixture` and temporary directories. The initial CLI check is:

```go
func TestPlanBackupRequestFailsBeforeCollection(test *testing.T) {
    dependencies, inventory := planFixture(test)
    _, output, _, err := runPlan(test, dependencies, "--backup")
    if err == nil || !strings.Contains(err.Error(), "backup is not implemented") || len(output) != 0 || inventory.calls != 0 {
        test.Fatalf("backup request was not refused explicitly: %v", err)
    }
}
```

- [x] Run `go test -count=1 ./internal/cli -run '^TestPlanBackupRequestFailsBeforeCollection$'`; expect failure because the old CLI only reports an unknown flag.
- [x] Bind literal selection with `command.Flags().StringArrayVar(&worktrees, "worktree", nil, "Exact linked-worktree root to select in a read-only preview (repeatable)")`; do not use CSV parsing. Bind boolean skip/backup flags; refuse requested backup and skip-dirty without targets before collectors or plan-store writes.
- [x] Resolve targets against `runtime.WorkingDirectory`, canonicalize, and pass them only via the explicit request, never config/environment. Reject missing/NUL/invalid UTF-8 target paths with quoted diagnostics.
- [x] Human output must identify `preview-only`, selected versus skipped versus unselected, no-backup discard-all intent including ignored files, branch preservation, and unavailable apply/eligibility. Escape paths/control text with the existing quoting helpers.
- [x] For v2 `explain --format json`, emit the following record; preserve the historical v1 candidate-only result:

```go
struct {
    SchemaVersion int                  `json:"schemaVersion"`
    PlanID        string               `json:"planId"`
    Removal       *domain.RemovalPlan   `json:"removal"`
    Candidate     domain.Candidate     `json:"candidate"`
}{value.SchemaVersion, value.ID, value.Removal, candidate}
```

- [x] Test literal commas/spaces, repeated and relative arguments, default false skip-dirty, explicit false booleans, duplicate aliases, unknown roots, primary selection, control-character rendering, and unsupported force/yes/apply requests. `scan` stays unchanged.
- [x] Run `go test -count=1 ./internal/cli`; expect success after Task 1 integration. Review the CLI diff against the exact interface rather than weakening assertions to accommodate mismatches.

## Task 3: Native Read-only Acceptance and Delivery

**Consumes:** The integrated v2 CLI and existing `internal/testutil` and
installed-runtime harness. No new harness or acceptance-gate framework.

**Produces:** Native fixture evidence, updated availability docs, a scoped PR
with independent review and required CI.

- [x] Extend the existing real-Git test to select one linked worktree and prove every action remains `none`, all branches/registrations/index bytes remain unchanged, and v2 explain returns the recorded candidate and disposal policy.
- [x] Add disposable dirty, untracked, and ignored sentinel files (including synthetic `.env` and dependency content), an unselected worktree, and primary/current/locked sentinels. Test default and skip-dirty previews with complete synthetic process evidence; then exercise the locally installed binary with native process collection, accepting only protective incomplete-collection outcomes when visibility is unavailable.
- [x] Verify snapshot/recovery artifacts are absent and every sentinel remains byte-identical. Refuse `--backup` without a plan; refuse nonexistent apply. No test may attempt actual product deletion in this slice.
- [x] Run `go test -count=1 ./tests/e2e -run 'Test.*(Plan|Explicit|Installed)'`; expect success. This proves read-only selection, not deletion or source-safety qualification.
- [x] Record written-contract approval and exact implemented flags/schema/output in the existing docs. Keep dedicated eligibility, shared source-review #23, complete process evidence #18, confirmation, mutation, optional recovery, and release qualification visibly pending.
- [x] Run `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`, `go build ./...`, `gofmt -l .`, and `git diff --check`, using a fresh child of the macOS user temporary directory. Formatting must print nothing.
- [ ] Obtain independent AI review of the scoped diff, labeled as AI rather than human approval. Address blocking findings and verify again. Do not request the excluded #23 assessment or treat this review as clearing it.
- [ ] Commit and create the scoped PR under the maintainer's standing authorization. Record exact commands/results/limitations and this plan reference. Wait for all three native jobs before any authorized merge; do not force-push, override protections, or publish a release.

## Execution Record

Implementation and local verification are complete. PR publication, final
independent review, and native CI are external checkpoints; their exact-head
evidence belongs in the PR record, not a self-attestation in its own commit.

- Base PR #28's actual merge passed native CI run 35336399682.
- Before implementation, `go test -count=1 ./internal/domain ./internal/plan ./internal/cli ./tests/e2e` passed on macOS arm64 with Go 1.26.5.
- Core red tests demonstrated the old inventory removal proposal, ignored selection, unsupported v2 storage, unchanged selection fingerprints, and legacy acceptance of added disposal fields. The new apply-load API was absent before its test/implementation.
- Core normal and race tests passed with `go test -count=1 ./internal/domain ./internal/plan` and `go test -race -count=1 ./internal/domain ./internal/plan`.
- Core independent AI review found no P1/P2 issues and two minor test-isolation gaps. Both were fixed and re-reviewed with no remaining findings. This was not human approval or an assessment of #23/#18.
- CLI's expected red was `unknown flag: --backup`; real-Git E2E's expected red was `unknown flag: --worktree`. Focused CLI normal/race and E2E tests subsequently passed. A parser-order assertion was corrected to check refusal and absence of output, inventory/process calls, and state rather than which unsupported token Cobra reports first.
- Fresh complete local verification passed on macOS arm64: `go test -count=1 ./...` and `go test -race -count=1 ./...` (18 tested packages, two without tests), `go vet ./...`, `go build ./...`, empty `gofmt -l .`, and staged/unstaged whitespace checks. No dependency or workflow changes were required.
- Actual `make install` binaries in disposable directories exercised scan, default/explicit/skip-dirty plans, explain, and unsupported-request refusal. Dirty/untracked/ignored contents, all fixture worktrees, local branches and index bytes survived; only private plan/key control records were created. Native process incompleteness remained protective. This is not product-deletion acceptance or production qualification.
- The final whole-slice AI review and exact-head native macOS/Windows CI remain PR checkpoints. Issues #23, #18, #25 and deferred Windows product qualification #15 are unchanged.
