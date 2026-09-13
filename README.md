# Treeclear

Safely clear stale coding-agent worktrees.

**Read-only core preview:** help, `version`, and `scan` are implemented.
Plans, cleanup, recovery snapshots, restore, and agent-provider adapters are
not implemented yet. A `safe` scan classification is not deletion authorization.

## Try the CLI

Install Go 1.26.5 and Git 2.36 or newer:

```text
go run ./cmd/treeclear version
go run ./cmd/treeclear scan --root /path/to/workspace
go run ./cmd/treeclear scan --root /path/to/workspace --inactivity-threshold 14d --format json
```

`--root` is repeatable and preserves commas and spaces in paths. Without a
root, scan uses configured roots or the containing Git repository. Outside
a repository with no configured roots, it shows guidance and does not scan;
use `--root .` for intentional recursive discovery. `treeclear` without
arguments shows help and performs no collection.

Scan discovers repositories and linked worktrees, inspects Git state, gathers
process evidence, and prints `protected`, `review`, or `safe` with reasons.
Primary/current, dirty, locked, unsafe, active, and unknown states remain
protected. Uninspectable processes or failed collection are not inactivity.
No command writes a plan or removes a worktree; branches and indexes remain
unchanged by scan.

JSON output includes `schemaVersion`, `toolVersion`, `collectedAt`, `complete`,
`worktrees` (each with `worktree`, `evidence`, and `decision`), and `warnings`.
Incomplete collection still prints available results, protects candidates,
sets `complete: false`, and exits with status 1. Human output escapes control
characters in paths; JSON retains the complete warning list.

## Configuration

Optional TOML files load from the OS user-configuration directory at
`treeclear/config.toml`, then `treeclear.toml` in the invocation directory.
Explicit flags override both files. Unknown settings and invalid safety
values are errors. For example:

```toml
inactivity_threshold = "14d"
base_branches = ["main", "master"]
```

Repository-local executable Git filters, submodule index entries, and hidden
index flags (`assume-unchanged` or `skip-worktree`) prevent read-only
collection and produce an unknown/protected result. Submodule inspection is
not yet supported; scan never clears index flags to inspect hidden changes.
A Git worktree root that differs from its registered path is unsafe. Missing
process permissions or unsupported OS inspection also remain unknown.
Agent-provider evidence is deliberately absent in this preview.

## Development

Run `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`,
and `go build ./...`; `gofmt -l .` must print nothing. `make verify` is an
optional shortcut. `make build` writes the native CLI into `bin/` and accepts
`VERSION=<version>`. Windows contributors can use Go commands in PowerShell.

Tests use temporary repositories and synthetic process records. Native
process smoke tests inspect only the test process. CI runs on macOS and
Windows; neither baseline tests nor this preview certify future cleanup.

- [Implementation sequence](docs/plans/README.md)
- [Development and review workflow](docs/development.md)
- [Contributor instructions](AGENTS.md)
- [Architecture](docs/architecture/2026-09-12-treeclear.md)
- [Apache-2.0 license](LICENSE)
