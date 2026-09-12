# Treeclear

Safely clear stale coding-agent worktrees.

**Current state: development harness only.** The safety core and product CLI
are not implemented. Passing Stage 000 does not mean worktree removal is safe.

## Development

Install Go 1.26.5 and Git 2.36 or newer, then run from the repository root:

```text
go run ./tools/harness doctor
go run ./tools/harness verify
go run ./tools/harness status
```

These commands also work in Windows PowerShell. `make verify` is an optional
shortcut. The harness performs formatting, local documentation-link checks,
static analysis, uncached tests, acceptance checks, race tests, native build,
and macOS/Windows cross-builds. Native CI runs on both supported platforms.

Tests use isolated temporary Git repositories and synthetic data. They do not
scan or remove worktrees from your workspace or read real agent sessions.

## Implementation sequence

Begin with the [development harness plan](docs/plans/000-development-harness.md),
then follow the [ordered product plans](docs/plans/README.md). Each stage has
its own PR, validation evidence, and independent review; PRs are not merged
automatically.

- [Contributor instructions](AGENTS.md)
- [Development and review workflow](docs/development.md)
- [Architecture](docs/architecture/2026-09-12-treeclear.md)
- [Documentation index](docs/README.md)
- [Apache-2.0 license](LICENSE)
