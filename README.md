# Treeclear

Safely clear stale coding-agent worktrees.

**Core preview:** help, `version`, `scan`, `plan`, and `explain` are implemented.
Cleanup, recovery snapshots, restore, and agent-provider adapters are not
implemented yet. A `safe` classification or a signed plan is not deletion
authorization. None of these commands removes a worktree.

**Planned removal behavior:** explicitly selected worktrees will be removed
with all local contents, including dirty and ignored files, without backup
by default. Branches and non-optional target/evidence protections remain.
Opt-in backup and `--skip-dirty` are separate choices. This is the
[new removal contract](docs/specs/2026-09-18-explicit-worktree-removal.md),
not available deletion functionality. The read-only version-2 preview now
records literal `--worktree` selection and `--skip-dirty`; requesting
`--backup` fails explicitly because backup is not implemented.

**Platform scope:** the first supported release targets macOS. Windows support
is deferred to [follow-up #15](https://github.com/hellices/treeclear/issues/15).
Existing Windows code and native CI are retained as compatibility evidence,
not a production-support claim. Linux is also outside the first release.
This scope decision does not make the current preview production-ready.

## Install the macOS source preview

From a reviewed checkout on macOS with Go 1.26.5, Git 2.36 or newer, and Make:

```sh
GOBIN="$HOME/.local/bin" make install VERSION="preview-$(git rev-parse --short HEAD)"
"$HOME/.local/bin/treeclear" version
"$HOME/.local/bin/treeclear" --help
```

This builds the current checkout for the Go toolchain's native macOS target.
It does not install a service, change your shell profile, or enable cleanup.
It is a local source build, **not a signed/notarized production distribution**.
See [installation, PATH, upgrade, and removal instructions](docs/installation.md)
for details. Installation tests use temporary destinations, not your home.

## Try the CLI

Install Go 1.26.5 and Git 2.36 or newer:

```text
go run ./cmd/treeclear version
go run ./cmd/treeclear scan --root /path/to/workspace
go run ./cmd/treeclear scan --root /path/to/workspace --inactivity-threshold 14d --format json
go run ./cmd/treeclear plan --root /path/to/workspace --format json --output /path/to/plan.json
go run ./cmd/treeclear plan --root /path/to/workspace --worktree "/path/to/linked worktree" --skip-dirty
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

Scan JSON output includes `schemaVersion`, `toolVersion`, `collectedAt`, `complete`,
`worktrees` (each with `worktree`, `evidence`, and `decision`), and `warnings`.
Incomplete collection still prints available results, protects candidates,
sets `complete: false`, and exits with status 1. Human output escapes control
characters in paths. Warning previews show at most five warnings, each limited
to 512 bytes of escaped, quoted text, with omission/truncation notices and
directions to full JSON. JSON retains the complete warning list.

## Plans and explanations

`plan` saves a private, authenticated plan under the OS user-configuration
directory at `treeclear/plans/<plan-id>.json`. The default expiry is exactly
15 minutes and the default inactivity threshold is 7 days. Candidate IDs are
stable for a worktree identity, while each plan gets a fresh ID. Newly
generated plans use schema version 2 with `execution: "preview-only"`.
**Every candidate has action `none`, no required snapshot, and no scheduled
reclaimable bytes**, even when selected or classified `safe`. Ordinary
inventory classification is not explicit-removal eligibility. Actual
snapshots and apply are not available in this preview.

Repeat `--worktree <path>` to record exact registered linked-worktree roots.
Spaces and commas are literal; relative paths resolve against the invocation
directory. Globs and prefixes do not expand selection. Duplicate, overlapping,
unmatched, and primary-root selections fail without saving a plan. Without
targets, the plan is an inventory preview with every candidate unselected.
Configuration and environment variables cannot supply these selections.

The authenticated `removal` record binds the requested `discard-all` contents,
`none` backup mode, exact `selectedPaths`, `skipDirty`, intent, and preview-only
execution. Default `--skip-dirty=false` records the all-content choice,
including dirty, untracked, and ignored files, without backup. With explicit
targets, `--skip-dirty` records a `dirty` exclusion for known staged, unstaged,
unmerged, or non-ignored untracked changes; ignored files alone do not trigger
that exclusion. Selection and exclusions are visible separately from ordinary
classification. Neither permits deletion, overrides unknown evidence, or
provides an undo. Local branches remain untouched.

`--backup` fails before collection or saving a plan; it never silently falls
back to no backup. `--backup=false` makes no backup request. `--yes`, force,
and apply are not exposed. Authenticated version-1 plans remain inspectable
with their original snapshot intent; v1 and v2 previews are rejected by the
apply-load boundary and never become executable after an upgrade.

Plan JSON on stdout contains the signed, versioned plan; diagnostics go to
stderr. Partial collection failures still save and print an inspectable plan,
block potentially affected candidates, and exit unsuccessfully. Core-only
adapter warnings remain visible in both plan and explain.
Their stderr warning previews use the same bounds as scan. After successfully
saving and printing a partial plan, the terminal error names its saved ID and
incomplete status instead of repeating every collection error. Full diagnostics
remain in the authenticated saved plan JSON and JSON stdout.

Actual installed-binary testing on an ordinary-user macOS desktop produces
incomplete scan/plan results because native process inspection is partially
inaccessible. [Issue #18](https://github.com/hellices/treeclear/issues/18) tracks
this unresolved release-qualification blocker. Bounded diagnostics do not
restore visibility, turn unknown evidence into inactivity, or authorize cleanup.

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
needs `./` or an absolute path to disambiguate it. Version-2 JSON explanations
contain `schemaVersion`, `planId`, `removal`, and the complete `candidate`.
Version-1 explanations retain their historical candidate-only JSON shape.
Human output includes selection/disposal intent, reasons, inactivity, Git
state, process evidence, and the preview's limitations.

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

Split-index backing entries (`sharedindex.*`, including retained remnants)
also block collection before index-reading Git commands. Treeclear does not
rewrite indexes or reset timestamps to bypass this restriction. The target's
effective common Git directory must identify the same native directory as the
repository's common store; a physically distinct copied store is not equivalent.

## Development

Run `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`,
and `go build ./...`; `gofmt -l .` must print nothing. `make verify` is an
optional shortcut. `make build` writes the native CLI into `bin/` and accepts
`VERSION=<version>`. Windows contributors can use Go commands in PowerShell.

Tests use temporary repositories and synthetic process records. Small native
process smoke tests inspect only the test process; installed-binary runtime
tests use real native collection against temporary worktrees and an owned
process. CI runs on macOS and Windows; neither baseline tests nor this preview
certify future cleanup.

- [Implementation sequence](docs/plans/README.md)
- [Development and review workflow](docs/development.md)
- [Contributor instructions](AGENTS.md)
- [Architecture](docs/architecture/2026-09-12-treeclear.md)
- [Apache-2.0 license](LICENSE)
