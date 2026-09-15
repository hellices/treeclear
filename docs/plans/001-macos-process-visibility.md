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
- [ ] Independently review and obtain passing native macOS/Windows CI for
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
