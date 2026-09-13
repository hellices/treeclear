# Treeclear

Safely clear stale coding-agent worktrees.

**Core preview:** help, `version`, `scan`, `plan`, and `explain` are implemented.
Cleanup, recovery snapshots, restore, and agent-provider adapters are not
implemented yet. A `safe` classification or a signed plan is not deletion
authorization. None of these commands removes a worktree.

## Try the CLI

Install Go 1.26.5 and Git 2.36 or newer:

```text
go run ./cmd/treeclear version
go run ./cmd/treeclear scan --root /path/to/workspace
go run ./cmd/treeclear scan --root /path/to/workspace --inactivity-threshold 14d --format json
go run ./cmd/treeclear plan --root /path/to/workspace --format json --output /path/to/plan.json
go run ./cmd/treeclear explain <candidate-id> --plan /path/to/plan.json
```

`--root` is repeatable and preserves commas and spaces in paths. Without a
root, scan and plan use configured roots or the containing Git repository. Outside
a repository with no configured roots, it shows guidance and does not scan;
use `--root .` for intentional recursive discovery. `treeclear` without
arguments shows help and performs no collection.

Scan discovers repositories and linked worktrees, inspects Git state, gathers
process evidence, and prints `protected`, `review`, or `safe` with reasons.
Primary/current, dirty, locked, unsafe, active, and unknown states remain
protected. Uninspectable processes or failed collection are not inactivity.
Scan does not write a plan. Branches, indexes, and worktree registrations remain
unchanged by scan, plan, and explain.

JSON output includes `schemaVersion`, `toolVersion`, `collectedAt`, `complete`,
`worktrees` (each with `worktree`, `evidence`, and `decision`), and `warnings`.
Incomplete collection still prints available results, protects candidates,
sets `complete: false`, and exits with status 1. Human output escapes control
characters in paths; JSON retains the complete warning list.

## Plans and explanations

`plan` saves a private, authenticated plan under the OS user-configuration
directory at `treeclear/plans/<plan-id>.json`. The default expiry is exactly
15 minutes and the default inactivity threshold is 7 days. Candidate IDs are
stable for a worktree identity, while each plan gets a fresh ID. Only safe
candidates receive a proposed `remove` action, always requiring a recovery
snapshot; review and protected candidates receive `none`. Actual snapshots
and apply are not available in this preview.

Plan JSON on stdout contains the signed, versioned plan; diagnostics go to
stderr. Partial collection failures still save and print an inspectable plan,
block potentially affected candidates, and exit unsuccessfully. Core-only
adapter warnings remain visible in both plan and explain.

`--output` writes an independent private copy of the exact saved bytes. It
never overwrites an existing destination or changes an existing parent
directory's permissions. The destination must be on the same filesystem as
Treeclear state; cross-filesystem export fails safely and retains the saved
plan. Use `--output`, rather than shell redirection, for an automatically
protected file. Do not edit or pretty-print a saved plan: changes fail HMAC
verification. Plans can contain local paths and evidence; keep them local.

Without `--plan`, `explain` selects the newest authenticated, unexpired plan
by generation time. It does not silently skip malformed or inaccessible plan
documents, or search older plans to find a missing candidate. Explicit
`--plan` accepts an ID or private file path and does not re-read current policy
or collect new Git/process evidence. A filename that is also a valid plan ID
needs `./` or an absolute path to disambiguate it. JSON explanation output is
one complete candidate; human output includes reasons, inactivity, Git state,
process evidence, and snapshot requirements.

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
