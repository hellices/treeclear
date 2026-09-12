# Treeclear

Safely clear stale coding-agent worktrees.

The repository currently contains development scaffolding and isolated Git
test fixtures. The product CLI is being built in [Plan 001](docs/plans/001-treeclear-core.md).

## Development

Install Go 1.26.5 and Git 2.36 or newer, then run:

```text
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build ./...
gofmt -l .
```

Formatting output must be empty. These commands also work in PowerShell;
`make verify` is an optional shortcut. CI runs natively on macOS and Windows.
Tests use temporary repositories, not your workspaces or real agent sessions.

- [Implementation sequence](docs/plans/README.md)
- [Development and review workflow](docs/development.md)
- [Contributor instructions](AGENTS.md)
- [Architecture](docs/architecture/2026-09-12-treeclear.md)
- [Documentation index](docs/README.md)
- [Apache-2.0 license](LICENSE)
