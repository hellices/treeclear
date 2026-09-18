# macOS process visibility follow-up

> Execute within the existing isolated worktree using test-driven development
> and independent AI review for each slice. This follows Plan 001 Tasks 5/7C
> and issue #18, not the independent snapshot review of PR #14.

**Goal:** Make complete, explicitly authorized macOS process observation
possible without weakening unknown evidence or elevating Git and state writes.

**Architecture:** Follow
[the permission design](../specs/2026-09-16-macos-process-visibility.md).
First deliver a Darwin-specific native source behind `process.Source`, then
prove and independently review the narrow elevated collection boundary.

**Tools and constraints:** Go 1.26.5, language 1.26.0, Git 2.36 or newer,
existing gopsutil and x/sys dependencies, ordinary Go tests, temporary
`internal/testutil` fixtures, existing native Windows CI. No real-workspace
tests, automatic local elevation, daemon, privileged installation, cached
process evidence, policy relaxation, or custom acceptance framework.

## Slice A: Correct native Darwin enumeration

Files: `internal/process/native_darwin.go`,
`internal/process/native_other.go`,
`internal/process/native_darwin_test.go`, `internal/process/native_other_test.go`,
`internal/process/kernel_darwin_test.go`,
`internal/process/native_sysctl_darwin.go`,
`internal/process/native_sysctl_darwin_test.go`, `go.mod`,
`internal/cli/scan.go`, `internal/cli/plan.go`, and
`tests/e2e/installed_runtime_test.go`.

Interface: `process.NativeSource() process.Source`. Darwin returns a fresh
Darwin source; other platforms retain `GopsutilSource`. Two independent native
PID/start-time snapshots bracket all inspection. Bound full retries and total
time; preserve last-attempt evidence and global failure after exhaustion.
Only positively established native kernel/zombie records can be excluded.
Keep shared collector and policy invalid-PID/unknown handling unchanged.

- [x] Add the installed-binary kernel-record regression and observe it fail:
  `go test -count=1 -run '^TestInstalledPreviewRunsNativeScanPlanExplain$' ./tests/e2e`.
- [x] Write native-source tests for kernel proof, zombies, field errors,
  duplicate/invalid/missing identities, birth/exit/reuse, whole-attempt
  isolation, exhausted churn, cancellation and native enumeration bounds.
- [x] Implement the Darwin source and select it for both scan and plan.
- [x] Run `go test -count=1 ./internal/process ./internal/cli ./tests/e2e`,
  then all AGENTS.md validation commands and actual installed-binary testing.
- [x] Independently review and obtain passing native macOS/Windows CI for
  the scoped PR before merging. Record this as an incomplete fix for #18.

## Slice B: Prove the permission boundary

- [ ] Prototype the complete authorized path, including path canonicalization
  and filesystem checks in the collector. Successful root API calls alone are
  insufficient. Never bypass the ordinary collector's required validation.
- [ ] Resolve the helper launch, request scope/identity, bounded private
  protocol, binary trust, caller ownership and child cleanup contract in the
  design. Independently review this boundary before exposing it.
- [ ] Test failure cases first, implement only the reviewed boundary, and
  retain protective ordinary-user behavior without any implicit elevation.
- [ ] Require complete scan/plan from actual installed binaries on disposable
  native macOS runners with explicit test opt-in. Verify active/protected
  fixtures, private user-owned plan state, export/explain/tamper handling,
  non-mutation and fresh evidence on every invocation.
- [ ] Run all required checks, independent review and native CI, then record
  exact qualifications and limitations in #18. Local privileged tests require
  separate operator authorization; never claim an unperformed test passed.

Slice B's implementation is intentionally contingent on the end-to-end
prototype. This is not permission to ship an unproven helper or close #18
after only Slice A or successful protective failures.

### Slice B1: Test permission feasibility before product integration

Use `superpowers:executing-plans` in this existing worktree. This slice is a
test-only native experiment, not a new privileged product entry point.

Files: `tests/processprobe/main_darwin.go`,
`tests/processprobe/main_darwin_test.go`,
`tests/processprobe/roles_darwin.go`, `tests/processprobe/roles_darwin_test.go`,
`tests/e2e/process_probe_darwin_test.go`, `.github/workflows/ci.yml`,
this plan, the permission spec, and `docs/installation.md`.

Consumes: the unchanged `process.NativeSource()` and
`process.Collector.Collect(context.Context, []domain.Worktree)`.
Produces: a bounded report, aggregate-only by default, from an actual installed
test executable, never reusable process evidence or a candidate decision. The
explicit one-time name-disclosure exception below is test-only.

- [x] Independently review the test-only permission and child-lifetime design.
- [x] Write failing ordinary Go tests for non-root/caller mismatch, malformed,
  oversized/non-canonical/trailing requests, challenge/version binding,
  unknown/error preservation, PID-creation mismatch, output limits and strict
  rejection of partial collection. Run `go test -count=1 ./tests/processprobe`.
- [x] Implement only the fixed-purpose probe and make those tests pass; keep
  the normal install target and all production packages unchanged.
- [x] Add an opt-in native Go test that installs the probe as the ordinary
  user, creates disposable Git fixtures, retains an owned active process,
  verifies microsecond identity, and checks non-mutation and ownership. Its
  elevated invocation is `/usr/bin/sudo -n -- <exact-installed-probe>` with
  private stdin/stdout, fixed environment/cwd and bounded lifetime.
- [x] Add a manual-only CI input for that test and enforce its hosted/manual
  job context inside the test before build, fixtures or sudo. Pure fake-env
  tests reject local, PR, push, scheduled, self-hosted and wrong-job contexts.
  These are accidental-execution interlocks, not authentication; this slice
  provides no local elevated test path. Normal macOS/Windows CI stays
  mandatory and never elevates. Inside the manually dispatched probe job, run
  `TREECLEAR_TEST_PROCESS_PROBE=1 go test -count=1 -run '^TestAuthorizedProcessProbe$' -v ./tests/e2e`.
  Partial output is a test failure, not a passing qualification.
- [ ] Run all AGENTS.md checks and the existing installed preview regression.
  Independently review the implementation before explicitly dispatching the
  elevated probe. Record exact commit, binary hash, runner and only sanitized
  aggregates. Keep #18 open regardless of this feasibility-only result.

### PR #22 failed feasibility investigation

The explicit run `35036670612` at `8719930` failed its installed administrator
probe with complete enumeration and a matched owned child, but one retained
error and one global unknown. The root-side expiry and ordinary native
macOS/Windows checks passed. This is a failed feasibility qualification, not a
build failure or evidence that installation authorization solves issue #18.

- [x] Preserve the failure and add test-first, fixed-label aggregate diagnostics
  for the first failed field and native error wording. Validate reply labels,
  bounds and totals before logging; never expose raw records or errors.
- [x] Independently review the diagnostic-only change and explicitly rerun the
  installed probe on the disposable hosted macOS job to identify the failure.
- [x] Reproduce and correct the pinned library's discarded native executable
  errno. Retain incomplete evidence; this is not a feasibility fix.
- [ ] Establish the cause of the retained executable ENOENT and correct a proven
  defect, if any. Do not filter the process, relax path checks, accept incomplete
  output, or alter host authorization.
- [ ] Rerun all AGENTS.md checks and native macOS/Windows CI, publish only
  sanitized results, and retain the merge hold unless feasibility is established.

The reviewed diagnostic commit `c3ab077` was tested in explicit run
`35038656217` on macOS 15.7.9 arm64. The installed probe hash was
`32df914b2fcdc15ebcc072c136c7dd8c5e7c9e4ed66bd4f0bbba05a5ea3dd254`.
The same one global unknown remained, now classified as an executable/native
read failure. Both ordinary native CI jobs passed; the administrator probe
remained a failure. This does not identify a specific errno or affected process.

The narrowly scoped source follow-up adds `internal/process/native_path_darwin.go`
and its tests, and selects it only for `darwinReader.ExeWithContext`. An actual
nonexistent-PID regression first reproduced lost ESRCH in the pinned library.
Native-error preservation, bounded/terminated results, cancellation and stale
errno are tested before implementation; the probe exposes only allowlisted errno
counts. This is an explicit exception to B1's original unchanged-production
slice: it improves existing executable-read diagnostics, not permissions or
cleanup eligibility. Independent review and an explicitly authorized installed
rerun are required before drawing conclusions about the CI failure.

Independent AI review found no material blockers in the native-error change
at `bc08f79`; it is not human approval. Explicit run `35040642065`, job
`104619488126`, on macOS 15.7.9 (24G830) arm64 with Go 1.26.5 tested installed
probe SHA-256
`fe6f7f92c7f19ff7a3f6d86c10259d286197d1a5048ec166fa0790e0f869e20e`.
The executable-first failure is now native `ENOENT:1`; one retained error,
one uninspectable process and one global unknown remain. Enumeration and the
owned active identity matched, root-side expiry passed, and ordinary native
macOS/Windows checks passed. Authorized feasibility still failed. The errno
alone does not identify the process or establish the cause on that runner.

A controlled ordinary-user regression copies `/bin/sleep` into a private
temporary directory, launches only that copy, and unlinks only its executable
while the same owned PID/start-time remains alive. Its native ENOENT fails
against the prior reader and is preserved by the correction; the real collector
retains unknown evidence for its active worktree. This reproduces an error
class, not the hosted runner's underlying cause. It grants no privilege and
does not touch unrelated processes or files.

The next test-only diagnostic distinguishes membership of the probe, its
current parent and the request's test-driver PID in the existing uninspectable
map. Only three booleans leave the private protocol, never their PIDs or raw
evidence. They are advisory PID membership, not creation-identity matches,
authentication, ownership or proof of relevance. Parent and driver may be the
same process. The receiver rejects any true flag with a zero uninspectable
count before logging; flags never change selection or completeness. Review
and an explicit installed rerun remain required for this follow-up.

The reviewed actor-diagnostic head `bfb724f` was tested in explicit run
`35043471222`, job `104628168728`, on macOS 15.7.9 (24G830) arm64. Installed
probe SHA-256 was
`fe8d00d4c50376d119ce40bf280528dd73112e2b207a6f25e8a112d4f708bdff`.
The same native executable ENOENT and single global unknown remained, with
`probe=false parent=false driver=false`. Ordinary native macOS/Windows CI,
root-side expiry and fixture checks passed; feasibility still failed. Follow-up
AI static review found no established harness early-unlink or result-interpretation
defect. These observations do not identify the affected process or its cause.

The next test-only diagnostic samples at most 16 retained uninspectable
identities in deterministic PID order. Fixed-size native metadata reads bracket
one parent lookup with two target reads; the target must still match the saved
PID/start-time and the two target records must agree. Both target and parent
names become fixed role hints such as CI worker, CI listener, Go tool, shell or
other; raw names and identifiers do not enter default reports. Unavailable, changed and
unsampled counts remain explicit. Sampling respects the existing context and
watchdog and never changes source selection, collector validation or completeness.
The parent validates both role vocabularies, positive bounded counts and totals
equal to the retained uninspectable count before logging. It also enforces the
shared 16-entry sampling budget, identical unsampled and identity-changed
counts, and at least as many unavailable parents as unavailable targets.
Inputs above 65,536 entries and a missing metadata reader are wholly unsampled.
Name-based hints are
not executable attestation, ownership, authentication or evidence of irrelevance.
Review and a new explicit installed run are required before attributing the CI
failure; no role, including a CI runner or system-init hint, permits exclusion.

### One-time name diagnostic approved on September 16, 2026

Run `35054273576` at `1c17d75` retained one executable ENOENT and one global
unknown, with both target and parent classified as `other`. It did not identify
the process or cause. The user explicitly approved one additional disposable
hosted-macOS run that discloses only the failing target's and parent's kernel
display names in public CI logs. No local elevation is authorized.

Files: `tests/processprobe/main_darwin.go`, `tests/processprobe/roles_darwin.go`,
their Darwin tests, `tests/processprobe/names_darwin_test.go`,
`tests/e2e/process_probe_darwin_test.go`,
`tests/e2e/process_probe_names_darwin_test.go`, `.github/workflows/ci.yml`, this plan
and the permission spec.

The private request adds optional `include_process_names`; the report adds
optional `uninspectable_names` target/parent pairs. The existing sampler
receives the explicit boolean and returns names alongside its role counts,
without extra native reads. The receiver separately receives its own consent
boolean. Names must be nonempty ASCII letters/digits/`._-`, at most 16 bytes;
unsafe names are withheld, not truncated or exposed through errors. Existing
PID/start-time checks, 16-entry sampling cap, context and watchdog remain.

- [x] Write failing producer/receiver tests for default non-disclosure,
  explicit opt-in, unsafe names, identity changes, parent validation,
  cancellation, sample bounds and malformed or unapproved replies. Run
  `go test -count=1 ./tests/processprobe ./tests/e2e` without elevation.
- [x] Implement the optional names using only existing validated metadata.
  Add default-false `process_visibility_names` workflow input with a public-log
  warning; bind it to `TREECLEAR_TEST_PROCESS_PROBE_NAMES=1` only when true.
  Existing hosted/manual interlocks remain mandatory and invalid flags fail
  before installing or invoking the probe. Partial evidence still fails.
- [x] Run all AGENTS.md checks and obtain independent AI review of this delta
  before committing/pushing and dispatching the experiment.
- [x] Dispatch exactly one reviewed hosted experiment with both explicit
  inputs true; record exact head, installed hash and the permitted diagnostics.
  Investigate a specific hypothesis from the hints without treating names as
  executable identity, authorization or exclusion evidence. Further public-name
  runs require renewed approval. Keep issue #18 and the merge hold open unless
  a proven defect is corrected and feasibility is actually established.

The one-time consent was consumed by run `35059409672`, job `104676349306`,
at reviewed head `962f79d6dd82c18c80066ccdb63075b917ee1f47` on September 16,
2026. The macOS 15.7.9 (24G830) arm64 runner used Go 1.26.5 and installed
probe SHA-256
`f8c597dc6aca2d07c0db142b599c6c60a4a16b9c87096f0d59a3406d799bc363`.
Feasibility still failed: one executable ENOENT and one enumeration-first
failure, two returned/retained errors, one uninspectable entry and two global
unknowns. Enumeration was incomplete; the owned active identity still matched.
All three harness-actor flags were false and both role histograms were `other:1`.
One approved process/parent display-name pair appears only in the opt-in job
log, not this execution record. Names do not attest the executable or establish
why its native path lookup failed. A possible hosted provisioning/diagnostic
helper is an investigation hypothesis, not a proven runner-update/unlink defect.

Root-side expiry passed in 30.16 seconds. Fixture preservation, ordinary-user
ownership, installed bytes and no-state-write checks passed before the failing
completeness assertion. Native macOS job `104676349334` and Windows job
`104676349121` passed. All required local checks passed on macOS 26.6.2
(25G83) arm64; the actual installed-preview protective regression and ten
race-enabled owned live/unlinked-executable regressions also passed. The latter
still reproduces an error class, not this runner's cause.

Independent AI static review reported no material P0-P2 findings; it is not
human approval. The committed diagnostic patch matches reviewed SHA-256
`d32e7332dd6b874e2317c81e18154b7dcf57eca215891e11525d5367e9a01841`.
Issue #18 remains open and PR #22 remains unmerged. The original feasibility
failure is unresolved; no process filtering, path-check bypass, runner change,
retry-until-success or incomplete-result qualification was introduced. Further
name-disclosing runs require renewed approval. Provider-side evidence of the
suspected helper's identity and executable lifetime is the next investigation
need, not permission to broaden diagnostics or escalate locally.

### Slice A review and merge completion

PR #21 merged as `26c03e4af934eb8e5561d2c15c5b1b209534e841`, with a tree
identical to reviewed head `cc02ff2651815005a4b65f98090a7146f5a92c3a`.
Independent AI R2 resolved R1's native retry/allocation finding and reported
no new actionable findings. AI review is not human approval. Required native
macOS/Windows CI passed on head (run `34993143814`) and actual merge
(`34994182910`). These later results supersede the pending R2/CI notes in the
dated local execution record below; they do not complete Slice B.

## Slice A local execution record

On September 16, 2026 (KST), macOS 26.6.2 arm64, the installed regression
failed before the implementation because a valid native kernel record became
an invalid user PID. The native kernel identity contract test and corrected
installed regression then passed. The observed scan still exited 1 with
`complete=false` and 192 warnings; plan exited 1 with an authenticated partial
plan. This is protective behavior, not complete operation or a fix for the
remaining permission boundary. No privilege elevation was attempted locally.

`go test -count=1 ./...` and `go test -race -count=1 ./...` each passed 17
tested packages (two have no tests). `go vet ./...`, `go build ./...`, empty
`gofmt -l .`, and `git diff --check` also passed. The installed regression
actually ran installed executables with isolated Git fixtures, correlated the
owned active process, checked original/exported plans and explanations,
rejected tampering, and verified fixture preservation. Independent code review
and remote native CI are separate remaining gates for this slice.

Independent AI code review R1 found a P2 bound violation in the pinned
`x/sys` native table helper: its internal `ENOMEM` retry loop is unbounded and
allocates before Treeclear's record-count check. The correction binds the
fixed native `sysctl` ABI using the already-pinned purego dependency (now
direct), validates the size before allocating, bounds growth retries, checks
cancellation between reads, and rejects malformed returned byte counts.
New fault-injection tests cover these cases; real native kernel/source tests,
the full uncached/race suite, vet, build, formatting and whitespace checks
pass again. R2 and final-head CI are still pending; this is not human approval.
