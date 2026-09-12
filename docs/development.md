# Development and Review Workflow

## Toolchain and entry points

Use Go 1.26.5 and Git 2.36 or newer. The module uses Go language version
1.26.0. The native Go commands below work from the repository root in macOS
shells and Windows PowerShell; installing Make is not required.

| Command | Purpose |
|---|---|
| `go run ./tools/harness doctor` | Check the exact Go toolchain and Git minimum |
| `go run ./tools/harness fmt` | Read-only Go formatting check |
| `go run ./tools/harness docs` | Check local Markdown file links |
| `go run ./tools/harness vet` | Run `go vet ./...` |
| `go run ./tools/harness test` | Run uncached tests and active-stage evidence gate |
| `go run ./tools/harness race` | Run uncached native race tests |
| `go run ./tools/harness build` | Compile all existing packages natively |
| `go run ./tools/harness cross` | Cross-build darwin/windows, amd64/arm64 |
| `go run ./tools/harness verify` | Run every preceding check, stopping on failure |
| `go run ./tools/harness gate 000` | Verify requirements through an explicit stage |
| `go run ./tools/harness status` | Print configured scope, not a passing-test claim |

The harness rejects a mismatched Go version during `doctor`/`verify`. Its
children use `GOENV=off`, `GOTOOLCHAIN=local`, `GOWORK=off`, and `GOFLAGS=-mod=readonly`,
and discard inherited cross-build overrides. Go bootstrap itself still uses
the caller's Go installation: install the pinned version first. Development
dependency downloads are allowed; this does not test the future product's
offline apply path. No unused dependency or empty `go.sum` is committed.

Each child command has a ten-minute deadline (shortened by cancellation);
each `go test` package has a five-minute timeout. Captured test evidence has a
32 MiB limit. An overflow, timeout, or malformed event stream fails the gate.
Use raw targeted `go test -count=1 ./internal/<package>` commands during the
RED/GREEN cycle, and the complete gate before publishing a PR.

There is no `cmd/treeclear` yet. Build checks compile development tooling, not
a placeholder product. Cross-builds are compilation evidence only; macOS and
Windows CI must execute tests natively.

## Isolated fixtures

`testutil.NewRepository(test)` creates a real committed primary repository
under a test-owned temporary root. `repo.AddWorktree(test, name, branch)` adds
a linked sibling worktree. `repo.Root` and `repo.Git(test, args...)` are the
shared interface used by the core plan. Temporary cleanup never calls the
product or discovers outside worktrees.

Git subprocesses use private HOME/USERPROFILE/XDG directories and a sanitized
Git environment. They disable global/system config, hooks, templates,
signing, prompts, and inherited repository overrides. Fixture changes must
preserve this isolation on both platforms; never use a real provider profile.

`testutil.NewClock(now)` provides explicit `Now` and `Advance` methods.
`testutil.NewRecorder(failures)` records named operations and returns injected
errors; `Operations` returns a copy. These helpers support later process,
snapshot, apply, and update fakes without creating premature product APIs.

## Acceptance requirements and honest readiness

[The acceptance manifest](../tests/acceptance/requirements.json) starts with
`activeStage: "000"`. Every requirement names its owning stage, a concrete Go
package, and an exact top-level test. Test names already present in a product
plan are reused; additional names reserve explicit safety coverage for the
stage implementing it. A rename requires an equivalent assertion and review.

The gate reads real `go test -json -count=1` events. A requirement passes only
when its exact test runs and passes, its package completes successfully, and
no subtest in that requirement is skipped. Missing packages, no tests,
skips, failures, and malformed results are failures, not exemptions. Ordinary
platform-specific tests may be outside the shared acceptance list, but their
native results and limitations must still be recorded in the PR.

The manifest is cumulative: Stage 002 includes 000, 001, and 002. Its schema
and selectors are validated. Known product directories enforce their owning
stage; other new Go code outside the harness defaults to Stage 001. This is a
repository-layout check, not semantic analysis or a security sandbox. Review
must catch hidden product logic, weakened assertions, or missing invariants.

The initial manifest is a minimum regression gate, not an exhaustive test
specification. Keep every acceptance criterion in the owning product plan,
and extend the manifest with additional important invariants as implemented.
Do not satisfy future requirements with no-op tests or mock-only success.
Right now `go run ./tools/harness gate 001` must fail: the safety core is
unimplemented. Status output never claims stored or historical test success.

The Markdown checker validates inline local file links outside fenced code.
It does not fetch external URLs or validate Markdown anchors, reference-style
links, or rendered output. CI contract tests check required configuration
patterns; they are not a general YAML parser. Actual GitHub Actions execution
is the native workflow acceptance test.

## Stage PR procedure

1. Read the active plan and predecessor review. Keep a coherent stage on its
   own branch/PR; state the prerequisite PR when stacking is necessary.
2. Write and observe failing tests, implement the smallest change, and run
   targeted tests. Review coherent units independently while maintaining
   non-overlapping write scopes for delegated work.
3. Run the full harness. Update the plan checkboxes only for work actually
   completed, and retain future stages as unverified.
4. Obtain independent specification and code-quality review of the exact diff.
   Fix blocking findings, rerun affected tests, and request re-review.
5. Commit and open the stage PR. Record test commands/results, both native CI
   results, reviewed commit range, reviewer identity/type, fixes, and limits.
6. Request GitHub review. Distinguish `requested`, `received`, `findings
   resolved`, and actual human approval. AI review is not human approval and
   the PR author's self-review is not independent review.
7. Do not merge automatically or start a dependent stage with unresolved
   blocking findings. A human decides whether to merge after reviewing the PR.

CI runs on pull requests and pushes to `main`, with full-SHA-pinned actions,
read-only token permissions, no persisted checkout credentials, no secrets,
25-minute jobs, and cancellation of superseded runs. Go caching is disabled
until the repository has a real dependency lockfile.

After the native jobs have actually run, repository administrators can make
`verify (macos-15)` and `verify (windows-2025)` required status checks and
require resolution of conversations on `main`. Do not weaken existing rules
or invent human approvals. A single-maintainer repository needs a real
additional reviewer before a required human-approval count can be enforced.
Branch policy changes require user authorization; this document does not
silently change GitHub settings.

## Ownership of later work

Stage 000 brings module/build scaffolding and temporary repository helpers
forward from Plan 001, and baseline instructions/CI from Plan 004. CLI
commands, safety policy, process collectors, snapshots, adapters, scheduling,
product e2e/fuzz suites, signing, and release workflows remain with their
original plans. Native CI becoming green does not mark those plans complete.
