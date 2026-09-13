# Treeclear Contributor Instructions

Read `docs/plans/README.md` and the active plan before editing. Architecture
and product safety requirements in `docs/architecture/` remain canonical.

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
- Never remove primary, current, dirty, locked, or active worktrees.
- Never force-remove ordinary cleanup targets; preserve local branches.
- Apply stays offline, revalidates the full plan, and verifies all required
  snapshots before any removal. Adapters provide evidence, not mutation APIs.
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
