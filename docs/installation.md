# Install the macOS source preview

Treeclear can be installed from a reviewed source checkout for local
evaluation. This preview provides help, `version`, `scan`, `plan`, and
`explain`. It does not provide cleanup/apply, complete recovery snapshots,
restore, agent adapters, or scheduling. A `safe` result or an authenticated
plan is not permission to remove anything.

This is **not a production release** or a Developer ID-signed and notarized
download. Source installation does not complete the safety core or resolve
open reviews, including PR #14. Windows product support remains deferred to
[follow-up #15](https://github.com/hellices/treeclear/issues/15); its existing
code and required native CI remain in place.

## Prerequisites and source

- macOS and a native Go 1.26.5 toolchain (module language version 1.26.0).
- Git 2.36 or newer for building from Git and for repository operations.
- Make for the convenience target (`make --version` checks availability).
- Access to the module dependencies; the Go tool may download them while
  building. Installation is not an offline-distribution promise.

Clone the repository if you do not already have a checkout:

```sh
git clone https://github.com/hellices/treeclear.git
cd treeclear
```

Use a reviewed revision and a clean checkout. `make install` builds the
current checkout, including local changes; it does not fetch, select a release,
or certify the source. The version label below identifies the selected commit,
but is not a signature or proof of review.

## Install without administrator privileges

From the repository root:

```sh
GOBIN="$HOME/.local/bin" make install VERSION="preview-$(git rev-parse --short HEAD)"
"$HOME/.local/bin/treeclear" version
"$HOME/.local/bin/treeclear" --help
```

The standard Go installer creates the destination as needed. Keep `GOBIN` an
absolute path and quote it, including when it contains spaces. It must be a
directory, not the executable's filename. No `sudo` is needed for this
user-owned location. Installing again replaces that destination's `treeclear`
binary, so choose a different `GOBIN` if an existing installation must remain.

`VERSION` defaults to `dev` when omitted. Without an explicit `GOBIN`, Go uses
its configured `GOBIN`, or `GOPATH/bin` (normally `$HOME/go/bin`). The target
does not change persistent Go settings. The examples use an explicit `GOBIN`
so that your configured Go destination cannot silently select another location.

`make install` rejects non-macOS Go hosts and a `GOOS` or `GOARCH` that differs
from the Go toolchain's host target. It is not a cross-compilation entry point.
Check unexpected target settings with:

```sh
go env GOHOSTOS GOHOSTARCH GOOS GOARCH GOBIN GOPATH
```

A build or cross-build does not qualify an architecture for production.
Consult the PR's recorded native test evidence for architecture coverage;
Intel macOS release acceptance is not implied by Apple Silicon tests.

## Use the installed executable

The full path works without changing your shell. To use `treeclear` by name
in the current shell:

```sh
export PATH="$HOME/.local/bin:$PATH"
command -v treeclear
treeclear version
```

Persist the PATH setting yourself only if you want it. Installation does not
edit `.zshrc`, `.zprofile`, or another shell startup file. If an older executable
is selected, inspect `type -a treeclear` and use the full path.

Help and version do not need Git on PATH and do not collect workspace or
process evidence. For scan/plan/explain behavior, configuration, private state,
and incomplete-evidence handling, see the [README](../README.md). Planning
writes private local plans; "non-removing preview" does not mean that every
command is stateless. Keep plan and evidence files local.

## Known native process-visibility limitation

Installed-binary testing on an ordinary-user macOS desktop found that native
process inspection is partially inaccessible. On that machine, `scan` prints
`complete: false` JSON and exits with status 1; `plan` saves an authenticated,
inspectable partial plan but also exits with status 1. The saved plan can still
be inspected with `explain`. An inspectable plan is not complete collection or
authorization to remove anything.

[Issue #18](https://github.com/hellices/treeclear/issues/18) tracks the unresolved
visibility/permission design and native release qualification. The preview
does not elevate privileges, install a helper, or exclude inaccessible
processes based on their owner. Unknown evidence continues to block cleanup;
do not treat it as inactivity or use `sudo` as a blanket workaround.

Human warning previews show at most five warnings, each capped at 512 bytes of
escaped, quoted text. Notices disclose omitted warnings and truncated text.
Use scan's `--format json` or the authenticated saved plan JSON for the full
local evidence. Plan/explain stderr uses the same preview bounds; a saved
partial plan's terminal error keeps its plan ID and incomplete status without
repeating all nested collection errors. This diagnostics fix is separate from
the visibility blocker.

### Why installation does not request administrator access

The current user-local installation needs no persistent administrator
privileges. Authorizing an installer would not make later `scan` processes
privileged. A registered root helper would be a separate security-sensitive
component, even if it ran only on demand and intended to perform read-only
inspection. Do not run the whole CLI as root or add a setuid/sudoers shortcut.

The [permission feasibility design](specs/2026-09-16-macos-process-visibility.md)
first tests isolated, explicitly authorized process/path inspection on a
disposable macOS runner. That test tool is not installed by `make install` and
does not grant the CLI any privilege. Its elevated test driver refuses local
and self-hosted execution, even if its opt-in environment variable is set.
A future signed helper needs separate
review of caller authorization, updates, revocation and removal; a passing
root API experiment alone does not qualify that helper or resolve #18.

## Upgrade or return to a previous preview

Select the reviewed revision you intend to evaluate in a clean checkout,
without discarding local work. Re-run the same install command with the same
absolute `GOBIN`, then check the installed `version`. This also works for a
previous reviewed revision. There is no background update service.

Native installation tests cover replacement, compiler-failure preservation,
cross-target refusal, and invalid destinations using disposable directories.
They do not establish crash/power-loss recovery or concurrent-installation
guarantees beyond the standard Go installer.

## Remove the executable

For the destination used in this guide:

```sh
rm -- "$HOME/.local/bin/treeclear"
```

If you chose another `GOBIN`, remove only its exact `treeclear` executable.
This does not remove worktrees, local branches, configuration, plans, or future
recovery data. Do not delete those directories as part of uninstalling the CLI.
Remove a manually added PATH entry separately if desired. This source-install
target installs no scheduler job, service, shell hook, or agent integration.

## What remains before production distribution

The [implementation plans](plans/README.md) still own complete snapshot,
apply/restore, adapter, and operational acceptance. Signed macOS archives,
notarization, checksums, SBOMs, attestations, native acceptance for each
advertised architecture, and release publication remain in
[Plan 004 Task 8](plans/004-operations-and-release.md#task-8-build-sign-attest-and-publish-release-artifacts).
This slice adds no release workflow, downloadable archive, Homebrew formula,
signing credentials, or Windows publication.
