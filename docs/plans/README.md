# Treeclear Implementation Plans

These plans turn the accepted
[Treeclear Architecture](../architecture/2026-09-12-treeclear.md) into
independently testable delivery stages.

Execute them in sequence:

0. [Minimal Development Baseline](000-development-harness.md)
1. [Safety Core](001-treeclear-core.md)
2. [Agent Adapters](002-agent-adapters.md)
3. [Adapter Lifecycle](003-adapter-lifecycle.md)
4. [Operations and Release](004-operations-and-release.md)

Each plan must leave the repository buildable and tested. Plans are execution
records, while `docs/architecture/` and `docs/specs/` remain the durable source
of product and protocol behavior.

## Explicit-removal scope amendment — 2026-09-18

The user-approved direction is whole-content removal of explicitly selected
linked worktrees, without backup by default. Dirty, untracked, and ignored
contents are included; `--skip-dirty` and requested backup are independent
plan-bound options. Local branches and retained target/evidence protections
remain. See [the written contract](../specs/2026-09-18-explicit-worktree-removal.md),
approved for implementation by the maintainer on September 18, 2026.
The first scoped implementation follows the
[explicit-selection preview plan](001-explicit-plan-preview.md).

The current Plan 001 slice implements versioned explicit selection and
read-only plan/explain behavior. Dedicated eligibility and then mutation
remain separately reviewed slices with their existing prerequisites.
Do not execute the old Tasks 8-11 sketches unchanged: mandatory snapshot
orchestration and restore move to the optional backup track, and default
dirty disposal needs explicit selection rather than a global policy bypass.
Delivered version-1 plans and historical completion records are unchanged;
new apply must reject those plans instead of reinterpreting snapshot intent.

The original docs-only amendment delivered no deletion or CLI flag. The
selection preview now records literal `--worktree` and `--skip-dirty`, refuses
requested backup, and never produces an executable action. Source-review
#23 continues to gate dependent shared identity/source-read work; #18's
process-completeness requirement is unchanged. No replacement review or
reader may be used to bypass the previously unavailable source assessment.
Later plans must carry the new versioned contract without broadening
scheduled safe-only cleanup. Requested backup, if implemented, still requires
complete verified recovery before the first removal.

## First-release platform scope

Execute Plans 001-004 for the macOS-first supported release. Windows product
qualification is deferred to
[follow-up #15](https://github.com/hellices/treeclear/issues/15), including its
native filesystem review, runtime acceptance, Task Scheduler integration, and
signed Windows artifacts. Linux remains outside the first release.

Preserve existing Windows code, fixtures, native CI, and completed execution
records. Future Windows-only steps remain explicitly deferred, not completed
or silently skipped. Shared safety behavior and macOS acceptance are not
deferred; the current required checks and independent-review workflow stay in
force. In particular, this scope amendment neither merges PR #14 nor resolves
its two outstanding review threads.

Before exposing mutation commands, Plan 001 Tasks 9-10 must test and enforce
unsupported-platform refusal for apply, restore, and trash prune; Plan 004
Task 6 owns the same boundary for scheduling. These guards are pending
implementation, not functionality provided by this documentation amendment.

## Early macOS source-install preview

[The macOS process-visibility follow-up](001-macos-process-visibility.md)
tracks issue #18's native enumeration correction and the separately reviewed
permission boundary. Ordinary-user partial collection remains protective;
this follow-up does not advance the independent snapshot review of PR #14.

The user-requested source-install slice of
[Plan 004 Task 8](004-operations-and-release.md#early-slice-native-source-installation)
is brought forward from the release stage, based only on reviewed `main`.
It installs the existing non-removing CLI for local evaluation. It does not
depend on or merge PR #14, complete Plan 001, or waive any open review. Signed
archives, native architecture qualification, SBOMs, attestations, and release
publication retain their original dependencies and acceptance requirements.

## Early native macOS CI coverage

The independent [native macOS architecture CI slice](004-operations-and-release.md#early-slice-native-macos-architecture-coverage)
is also brought forward from Plan 004 Task 7. It adds actual Intel macOS
execution alongside Apple Silicon and the existing Windows compatibility
job, without replacing native tests with cross-builds. It does not complete
the dependent product stages, clear process visibility #18 or source review
#23, or supply the signing/notarization prerequisites tracked in #25.

## Cross-plan CLI ownership

| Command or flag | Owning plan |
|---|---|
| `treeclear`, `version` | 001 |
| `scan --root --inactivity-threshold --format` | 001 |
| `plan --root --inactivity-threshold --format --output` | 001; existing read-only preview |
| `plan --worktree --skip-dirty` | 001 explicit-selection preview; no removal actions |
| `plan --backup` | 001 explicit-selection preview; requested backup fails as unavailable |
| `explain --plan --format` | 001 |
| Proposed `apply --plan --format --yes` | 001 explicit-removal amendment |
| `restore`, `trash list`, `trash prune` | 001 optional backup track |
| `adapters list`, `adapters doctor` | 002 |
| `adapters init`, `validate`, `test`, `trust` | 003 |
| `adapters update`, `adapters rollback` | 003 |
| `integration install`, `status`, `remove` | 004 |
| `schedule install`, `status`, `remove`, `run` | 004 |

## Safety traceability

| Architecture invariant | Persisted field or mechanism | Revalidated by | Primary test owner |
|---|---|---|---|
| Unknown never means inactive | `EvidenceState=unknown`, `GitStateKnown`, and reason codes | Policy engine | 001 Tasks 4-6, 002 Tasks 4-10 |
| Process enumeration must be complete | `process.Collection.Complete` plus global unknown sentinel | Correlator and policy | 001 Tasks 5-6 |
| No primary/current/locked/active/unknown removal | Full `candidatePreconditions` and complete evidence | Apply revalidator | 001 Tasks 6 and 9 |
| Dirty disposal only through explicit selection | New schema binding exact selection, disposition, and `skipDirty`; not yet implemented | Eligibility evaluator and apply revalidator | 001 explicit-removal slices |
| Exact plan and policy | Canonical `PolicySettings`, `PolicyDigest`, candidate fingerprint, expiry | `EnvironmentVerifier` | 001 Tasks 7 and 9 |
| Exact adapters and commands | Adapter/source/bundle provenance, `AdapterLockDigest`, trust digest, executable, argv, cwd, and environment identities | `EnvironmentVerifier` | 002 Tasks 3-6, 003 Tasks 2 and 5 |
| Custom trust scope matches apply mode | `Plan.IntendedApplyMode` and scoped trust record | Collector plus apply engine | 002 Task 6, 003 Task 5 |
| Update and apply never overlap | Shared `internal/statelock` lock | Apply and updater | 001 Task 9, 003 Task 7 |
| No silent change of backup intent | Versioned backup mode; reject version-1 mutation and unsupported backup requests | Plan reader and apply engine | 001 explicit-removal slices |
| Requested backup before mutation | Verified complete backup receipt plus policy/evidence digests; no unbacked fallback | Apply engine | 001 optional backup track |
| Branch preserved | Removal argv omits branch deletion | Git client | 001 Tasks 3, 9, and 11 |
| Apply stays offline | No updater dependency in apply and network-failing e2e transport | Apply e2e | 003 Task 7 |
| Apply never executes provider commands | Local-readonly collection plus file-only executable verification | Apply e2e | 002 Tasks 5-6, 003 Task 7 |
| Adapters have no core mutation API | Evidence-only SPI | Compile-time interfaces and security tests | 002 Tasks 1-6 |
| Scheduled cleanup defaults plan-only | Scheduler `JobKind` and `Mode` | Schedule service | 004 Tasks 3-6 |
| Signed update rollback | Version store state and signed index version | Updater transaction | 003 Tasks 1-4 |
