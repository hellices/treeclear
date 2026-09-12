# Treeclear Contributor Instructions

## Current stage

This repository starts with Stage 000 development tooling, not a working
cleanup product. Read `docs/plans/README.md` and the active plan before edits.
Canonical product architecture lives in `docs/architecture/`; design details
in `docs/design/`; public contracts in `docs/specs/`; execution records in
`docs/plans/`. Keep tool-specific instructions as thin references to this file.

## Build and test

- Use Go 1.26.5 (language version 1.26.0) and Git 2.36 or newer.
- Full local and native CI gate: `go run ./tools/harness verify`.
- Quick targeted tests: `go test -count=1 ./internal/<changed-package>`.
- Acceptance tests: `go run ./tools/harness test`.
- Race tests: `go run ./tools/harness race`.
- Formatting check: `go run ./tools/harness fmt`; fix with
  `gofmt -w <changed-go-files>`.
- Native build: `go run ./tools/harness build`.
- macOS/Windows amd64/arm64 compilation: `go run ./tools/harness cross`.
- Configured readiness: `go run ./tools/harness status` (not a test result).
- Use the same Go commands in PowerShell; Make targets are optional shortcuts.
- Add dependencies only when used. Do not silently change the toolchain pin.

## Safety invariants

- Unknown, inaccessible, malformed, or conflicting evidence blocks cleanup.
- Adapters return evidence only and never receive core mutation APIs.
- Apply is offline, revalidates the exact full plan, and verifies every
  required snapshot before any removal.
- Never add a force-removal path for ordinary cleanup.
- Never remove primary, current, dirty, locked, or active worktrees.
- Preserve local branches; agent-session deletion is outside the MVP.
- Keep snapshots and evidence local. Never upload user data or credentials.

## Test isolation

- Use `internal/testutil` and temporary directories for real Git fixtures.
- Never scan or clean developer workspaces, provider databases, or real
  scheduler jobs during verification. Use synthetic data and injected errors.
- Do not mutate global Git config or process-wide cwd/environment in helpers.
- Control time explicitly with `testutil.Clock`; do not sleep to age fixtures.
- Record operation order and inject failures with `testutil.Recorder`.
- Write a failing regression test before implementing behavior or a fix.
- Do not replace native Windows tests with cross-compilation claims.

## Stage and review gates

- Maintain `tests/acceptance/requirements.json`; activate the stage whose
  product files are being introduced. Gates are cumulative.
- Missing, skipped, failed, or zero selected tests are not passing evidence.
- New safety tests must assert behavior, not merely log or return success.
- Do not remove or weaken a requirement to make a gate green. Any selector
  rename or scope change needs an explicit rationale in the PR and review.
- Obtain independent review from a separate reviewer/agent for each coherent
  unit, then a whole-stage review. Fix blocking findings and rerun tests.
- Create one PR per stage with exact commands, native results, limitations,
  and the reviewed commit range. Do not advance past unresolved findings.
- Label AI review explicitly and record human approval separately. A pending
  review request, self-review, or AI review is not human approval.
- Do not merge, force-push, publish releases, or change review requirements
  without user authorization. Do not add arbitrary reviewers or credentials.

## Change hygiene

- Keep changes focused on the current plan and avoid placeholder product APIs.
- Use the existing isolated worktree; do not create nested worktrees.
- Keep generated binaries, private test logs, and agent scratch out of Git.
- Keep this file under 200 lines and preserve canonical documentation links.
