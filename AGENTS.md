# Treeclear Contributor Instructions

Read `docs/plans/README.md` and the active plan before editing. Architecture
and product safety requirements in `docs/architecture/` remain canonical.

The first supported release targets macOS. Windows support is deferred to
[follow-up #15](https://github.com/hellices/treeclear/issues/15); preserve
existing Windows code, tests, and required native CI. Deferral does not resolve
review findings in shared or macOS code or waive any safety invariant.

## Development

- Use Go 1.26.5 (language 1.26.0) and Git 2.36 or newer.
- Run `go test -count=1 ./...`, `go test -race -count=1 ./...`,
  `go vet ./...`, and `go build ./...`. `gofmt -l .` must print nothing.
- `make verify` is an optional shortcut; use Go commands on Windows.
- Write a failing test before implementing behavior or fixing a bug.
- Use `internal/testutil` and temporary directories for Git fixtures.
- Never test against real workspaces, agent databases, or scheduler jobs.
- Do not mutate global Git config or process-wide cwd/environment in helpers.
- Add dependencies only when used; keep generated files and secrets out of Git.
- Use the existing isolated worktree; do not create nested worktrees.

## Safety

- Unknown, inaccessible, malformed, or conflicting evidence blocks cleanup.
- Never remove primary, current, locked, or active worktrees. Dirty worktrees
  remain protected in ordinary/scheduled cleanup. Only a reviewed explicit
  whole-worktree selection may authorize discarding dirty and ignored contents;
  see [the removal contract](docs/specs/2026-09-18-explicit-worktree-removal.md).
- Explicit whole-worktree removal defaults to no backup; `--skip-dirty` and
  requested backup are separate plan-bound choices. Do not reinterpret old
  plans or expose unimplemented options as working functionality.
- Never force-remove ordinary cleanup targets. An explicit discard-all plan
  may authorize a single Git force only after all retained checks pass;
  never defeat locks, add targets, or use recursive-delete fallbacks.
  Preserve local branches in every removal mode.
- Apply stays offline, revalidates the full plan, and verifies all required
  opt-in backups before any removal; backup failure never falls back to
  unbacked deletion. Adapters provide evidence, not mutation APIs.
- This scope change does not clear source-identity review #23 or process
  visibility #18. Shared safety findings still gate dependent mutation work.
- Keep snapshots and evidence local; never upload user data or credentials.

## PRs and reviews

- Keep PRs scoped to an implementation stage or a clearly labeled slice.
- Include exact test commands, results, limitations, and the plan reference.
- Obtain independent review for each PR and fix blocking findings before
  advancing to dependent stages. Label AI reviews; they are not human approval.
- Native macOS and Windows CI must pass; cross-builds are not native tests.
- Do not merge, force-push, publish releases, or change repository protections
  without user authorization.
- Use ordinary Go tooling. Do not add a custom stage/acceptance gate framework.
