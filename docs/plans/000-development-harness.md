# Development Harness Implementation Plan

- Status: In progress
- Sequence: 000, prerequisite to Plans 001–004
- Design: [Development Harness](../design/development-harness.md)
- Depends on: none

**Goal:** Establish reproducible, isolated, fail-closed development validation
and stage-by-stage PR review before implementing product features.

**Architecture:** A standard-library Go verification tool orchestrates native
checks, consumes actual Go test events, and uses a cumulative acceptance
manifest. Shared test fixtures are separate from production code. GitHub
Actions runs the same verification on macOS and Windows.

## Global Constraints

- Module: `github.com/hellices/treeclear`; Go 1.26.0; toolchain 1.26.5.
- No Treeclear product commands or production cleanup logic in this stage.
- No real user worktree, provider database, credential, or scheduler access.
- Do not represent future, empty, or skipped tests as passing safety evidence.
- Use shell-free commands, bounded execution, and the existing worktree.
- Keep the accepted architecture and numbered product plans authoritative.
- Each implementation unit receives independent specification and quality
  review; the PR records AI review separately from human approval.

## Task 1: Executable verification and evidence gates

**Files:** `go.mod`, `internal/harness/`, `tools/harness/main.go`, `Makefile`,
`.gitignore`, `tests/acceptance/requirements.json`.

**Interfaces:** `harness.Execute(context.Context, []string, io.Writer,
io.Writer) int`; a strict manifest with `schemaVersion`, `activeStage`, and
`stages`; requirements with `id`, `description`, `package`, and `test`.

- [ ] Write tests for strict manifest decoding and cumulative stage selection.
  Reject missing, skipped, failed, malformed, wrong-package, and empty test
  evidence, and stages that lack required source packages.
- [ ] Run `go test ./internal/harness` and record the expected missing-code
  failure before implementation.
- [ ] Implement verification commands and bounded shell-free execution.
  `verify` runs `go vet ./...`, `go test -json -count=1 -timeout=5m ./...`,
  evidence validation, `go test -race -count=1 -timeout=5m ./...`, native
  `go build ./...`, and darwin/windows amd64/arm64 builds.
- [ ] Verify the gate against a temporary real module containing a passing,
  skipped, and absent test; ensure a future-stage gate fails in this repo.
- [ ] Run targeted tests, review the diff independently, and resolve findings.

## Task 2: Isolated repository and failure fixtures

**Files:** `internal/testutil/repo.go`, `internal/testutil/repo_test.go`,
`internal/testutil/clock.go`, `internal/testutil/clock_test.go`,
`internal/testutil/recorder.go`, `internal/testutil/recorder_test.go`.

**Interfaces:** `NewRepository(testing.TB) *Repository`, `Repository.Root`,
`Repository.AddWorktree(testing.TB, string, string) string`,
`Repository.Git(testing.TB, ...string) string`,
`NewClock(time.Time) *Clock`, `Clock.Now() time.Time`,
`Clock.Advance(time.Duration)`, `NewRecorder(map[string]error) *Recorder`,
`Recorder.Record(string) error`, and `Recorder.Operations() []string`.

- [ ] Write fixture tests first and observe missing-implementation failures.
- [ ] Implement real temporary Git repositories with a committed seed file,
  linked worktrees, deterministic local identity and timestamps, and isolated
  subprocess environment. Reject paths outside the fixture and do not run
  hooks, signing programs, prompts, or network operations.
- [ ] Test spaces/Unicode paths, dirty and locked worktrees, hostile inherited
  Git configuration, symlink escapes, and cleanup boundaries.
- [ ] Implement a mutex-protected controllable clock and operation recorder.
  Test injected permission errors and defensive copies of recorded state.
- [ ] Run `go test -race -count=1 ./internal/testutil`, obtain independent
  review, and resolve findings.

## Task 3: Native CI, contributor rules, and review records

**Files:** `.github/workflows/ci.yml`, `.github/pull_request_template.md`,
`AGENTS.md`, `CLAUDE.md`, `README.md`, `LICENSE`, `docs/development.md`,
`internal/harness/contracts_test.go`, and existing documentation indexes.

- [ ] Write repository contract tests that fail without the CI and contributor
  files. Check SHA pinning, native matrix, toolchain, least privilege,
  timeout/concurrency, canonical local documentation links, and PR evidence.
- [ ] Add native macOS/Windows CI running `go run ./tools/harness verify`.
- [ ] Document native commands, fixture isolation, stage activation, named-test
  evidence limits, and explicit AI-versus-human review handling.
- [ ] Link Stage 000 as a prerequisite without marking product work complete.
- [ ] Run the complete local harness and independent final review, fix findings,
  and repeat affected tests.
- [ ] Commit, push, create the Stage 000 PR, request GitHub review, and inspect
  native CI. Record actual outcomes rather than requests as completed reviews.

## Completion Gate

```text
go run ./tools/harness verify
go run ./tools/harness gate 000
go run ./tools/harness status
git diff --check
```

Both native CI jobs must pass; all Stage 000 tests must actually run. Running
`go run ./tools/harness gate 001` must currently fail with unverified safety
requirements. Product stages remain planned. A PR is not merged automatically.
