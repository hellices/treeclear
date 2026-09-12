# Development Harness

## Decision

Build a small, repository-owned development harness before the safety core.
The harness is development tooling, not the Treeclear product, an agent
runtime, or proof that any cleanup operation is safe.

The initial audit found only seven documentation files. Go 1.26.5, Git, and
authenticated GitHub access are available locally; there are no executable
tests, CI workflows, contributor instructions, or protected-branch checks.

## Alternatives

- CI alone is cheap but cannot establish isolation or meaningful safety
  evidence when there is no implementation or test suite.
- A complete agent platform, provider emulator, and release harness would
  duplicate future work and prematurely freeze product interfaces.
- The selected approach supplies shared fixtures, executable evidence gates,
  native CI, and review records. Product-specific tests arrive with their
  owning implementation stage.

## Boundaries

- Use the planned module path, Go 1.26.0 language version, and Go 1.26.5
  toolchain. Start with the standard library; add product dependencies only
  when used. There is no placeholder product CLI.
- Tests create synthetic data and real Git repositories only below temporary
  directories. They must not inspect or mutate the user's real worktrees,
  provider databases, scheduler jobs, credentials, or global Git configuration.
- Git fixtures isolate HOME, USERPROFILE, XDG directories, Git configuration,
  hooks, templates, signing, credential prompts, and inherited Git overrides.
- Tests control time explicitly and can record named operations with injected
  errors. These helpers expose no production deletion or process APIs.
- No benchmark, coverage percentage, mocked success, skipped test, or empty
  test selection is sufficient evidence that a safety requirement passes.

## Components

### Verification entry point

`go run ./tools/harness` is the native cross-platform entry point. `Makefile`
targets are optional shortcuts. The tool locates the repository root and
provides `doctor`, `fmt`, `docs`, `test`, `race`, `build`, `cross`, `gate`,
`status`, and `verify` commands. `fmt` checks rather than rewrites files.

`verify` runs toolchain checks, formatting, documentation links, static
analysis, uncached tests, acceptance evidence, race tests, native build, and
macOS/Windows cross-builds. Commands run without a shell, have deadlines, and
stop on failure. Cross-builds are not reported as native platform tests.
Development commands may fetch Go modules; the product's offline-apply
invariant is a separate, initially unverified requirement.

### Acceptance evidence

`tests/acceptance/requirements.json` is a versioned, cumulative list of
stage-owned requirements with exact Go package and top-level test names.
`activeStage` starts at `000`. Future safety requirements remain planned.

The gate reads uncached `go test -json` results and requires an actual `pass`
event for every required test in every stage through the selected stage.
Missing, skipped, failed, malformed, or empty evidence fails closed. The
manifest rejects unknown fields, duplicate IDs, invalid selectors, and
unknown stages. Selecting a future stage must fail until its tests exist and
pass. Activating product directories also requires activating their owning
stage; merely leaving `activeStage` at `000` must not bless product code.

This proves that named tests ran, not that their assertions are adequate.
Independent review still evaluates assertions and safety coverage. `status`
describes the configured scope; it does not claim tests have passed.

### Native CI and review

Every pull request and push to `main` runs on `macos-15` and `windows-2025`
with Go 1.26.5. Actions use verified full commit SHAs, a read-only token,
explicit timeouts, and cancellation of superseded runs. No privileged
`pull_request_target`, secrets, release upload, or auto-merge is introduced.

Each stage has a separate PR with scope, test commands, results, limitations,
and independent review evidence. Resolve blocking findings and rerun the
affected tests before progressing. AI review is labeled as AI review, never
as human approval. A review request alone is not a completed review.

Required CI checks can protect `main` after their real check names are
observed. Do not invent a human reviewer or require an impossible self-
approval in a single-maintainer repository. Do not merge automatically.

## Adoption

Stage 000 brings forward only the reusable module/build setup from Plan 001,
temporary repository fixtures from Plan 001 Task 4, and baseline contributor
instructions/CI from Plan 004. Those plans retain their feature and product
acceptance work. All product safety invariants remain unverified until their
stage tests pass and their implementation is reviewed.
