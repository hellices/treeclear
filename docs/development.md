# Development and Review Workflow

Use Go 1.26.5 and Git 2.36 or newer. From the repository root:

```text
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build ./...
gofmt -l .
```

`gofmt -l .` must print nothing; fix formatting with `gofmt -w <files>`.
`make verify` runs the same checks. Windows contributors can use the Go
commands directly in PowerShell. CI runs both native macOS and Windows tests.
For a quick iteration, use `go test -count=1 ./internal/<package>`.

The first supported release targets macOS. Windows support is deferred to
[follow-up #15](https://github.com/hellices/treeclear/issues/15); the existing
Windows implementation and native CI remain compatibility coverage, not a
production-support claim. Keep both current CI jobs and their required checks.
Deferral does not resolve outstanding reviews or waive shared/macOS safety
findings. Existing dual-platform execution records remain historical evidence.

`make build` writes the CLI into `bin/`; `make build VERSION=v0.0.0-test`
sets the version string. Without Make, use `go build -o bin/ ./cmd/treeclear`.

`make install` uses standard `go install` with the same version setting, only
for the Go toolchain's native macOS target. Use an explicit absolute `GOBIN`
for a predictable destination; see [source-preview installation](installation.md).
It does not package, sign, publish, or qualify a production release.

## Test isolation

`internal/testutil` provides real temporary Git repositories, a controllable
clock, and an operation recorder. Repository helpers isolate child Git
configuration, hooks, credentials, and home directories from the developer.
`Repository.Git` accepts trusted test code, not untrusted commands; keep all
path operands inside temporary fixtures. It is not a Git command sandbox.
Use synthetic provider records and injected failures; never scan or remove
real user worktrees or sessions in tests.

The read-only CLI integration tests use those repositories plus synthetic
process sources. Binary help/version tests run without Git on the child PATH.
Small native process smoke tests inspect only the current test process. A test
requiring distinct case-sensitive names reports a skip on filesystems that
cannot create them; cross-compilation does not replace native Windows tests.

`go test -count=1 ./tests/e2e -run TestMakeInstall -v` exercises the real Make
target on macOS, using temporary homes, Go paths, and install destinations.
It covers default/explicit Go destinations, spaces and Unicode, native build
metadata, version overrides, upgrades, cross-target refusal, invalid
destinations, and compiler failures that retain the previous binary. Runtime
smoke commands run in an empty temporary home without Git or Go on PATH and
check that no configuration/state is created. Only build/module caches are
reused; helpers never change the parent process's cwd, environment, or Git
configuration. These Make tests skip on other hosts; all existing native
Windows tests and required CI checks remain unchanged.

`go test -count=1 ./tests/e2e -run '^TestInstalledPreviewRunsNativeScanPlanExplain$' -v`
installs the real macOS executable and runs scan, plan, and explain as separate
OS processes, without injected CLI dependencies or a Go toolchain on the
runtime PATH. It uses temporary primary/current/dirty/locked/active/clean
worktrees and an owned sleep process. It checks native process correlation,
protected decisions, partial-result failure status, private canonical export,
saved-ID lookup, tamper rejection, bounded diagnostics, and unchanged fixture
files/indexes/branches/registrations. No real workspace or agent state is used.
Only command names, exit statuses, byte counts, and warning counts are logged;
raw native process evidence and authenticated plans are not CI artifacts.

This regression allows a protective partial collection as a passing test, not
as complete native runtime acceptance. Ordinary-user macOS process visibility
remains the release blocker tracked by
[issue #18](https://github.com/hellices/treeclear/issues/18). Complete permission
and architecture qualification require separate design and review; the test
does not ignore inaccessible processes, change ownership rules, or elevate
privileges. The installed runtime test skips outside macOS, without replacing
any required native Windows checks.

## Evidence boundaries

Correlation and policy remain pure. Nonempty worktree and agent paths supplied
to `correlate.Group` must already be canonical absolute identities. Inventory
and process collection own filesystem normalization. Future adapter mapping
must resolve aliases with `pathutil.Canonical` before correlation and retain
unresolvable bindings as unknown evidence, not silently omit them. The current
CLI supplies no agent-provider evidence.

This preview conservatively protects every returned worktree when a scan is
incomplete, even if an individual inspection failure can be localized.
Scan-wide failures are retained as evidence warnings without changing the
collector's process-enumeration completeness. Correlation preserves the
collector's global unknown failure record, adding a fallback only when missing.

Byte estimates exclude the root Git marker and its metadata tree. Marker aliases
are matched by filesystem identity, not case spelling alone. Nested Git markers
still make the affected worktree unknown and unsafe.

Discovery also excludes filesystem-identical case aliases of `.git`,
`node_modules`, `.cache`, and `target`, including explicit roots beneath them.
Distinct case-sensitive directory names are not aliases.

Missing or unresolvable administrative identities and administrative directories
outside the repository's common Git directory invalidate path safety.
Administrative hashing accepts at most 4,096 enumerated entries (including the
root and skipped directories) and 16 MiB of selected file contents. Index hashing
has its own 16 MiB limit. Both hashes share a 30-second context deadline;
cancellation is checked between bounded directory batches and file reads.
Synchronous operating-system filesystem calls cannot be forcibly interrupted.
Any limit, cancellation, or collection error leaves Git state unknown.

Agent evidence cannot establish inactivity when both `createdAt` and `updatedAt`
are absent; observation time is not activity time.

## Plan fingerprint foundation

`internal/plan` exposes pure `CandidateFingerprint` and `PolicyDigest`
functions (Plan 001 Task 7A, merged in PR #3). Collectors still own canonical
filesystem identities; hashing does not resolve paths or read user data.

Fingerprints cover the dedicated safety preconditions, action, reason codes,
snapshot requirements, process identity/creation time, offline agent source
identity and content, and adapter health/trust/offline status. They exclude
observation times and human presentation text but preserve uncertainty via
warning/error-presence bits. Planning-only agent records are excluded from
the removal fingerprint, not from the signed explanatory plan.
Unrecognized revalidation modes are errors, not omitted evidence.

Both functions return `sha256:` plus lowercase hex. Tests cover field changes,
stable ordering (including conflicting identities), immutable inputs, equal
UTC instants, empty-list normalization, and malformed hashed inputs. The
candidate fingerprint is not a substitute for HMAC verification,
whole-plan revalidation, snapshot verification, or explicit apply approval.

## Private plan storage

Plan 001 Task 7B adds `NewStore(root, now, integrityKey)`, `Store.Save`, and
`Store.Load`. Task 7C integrates the builder and `plan`/`explain` CLI with this
storage contract. Snapshots and apply remain unimplemented. Authentication
alone does not establish that an action is safe or authorize removal.

With a nil key, the first save creates a random 32-byte `integrity.key` under
the private state root. Injected keys are copied and never persisted. Plans
live at `plans/<planId>.json`; both plans and keys are immutable. Concurrent
creators use exclusive atomic hard-link publication of a flushed private
temporary file, and an existing destination returns `fs.ErrExist` rather
than being overwritten. Unsupported filesystems fail closed. Tests use only
temporary directories, injected clocks, and synthetic plans.

Unix storage uses `0700` directories and `0600` files. Windows creates a
protected DACL granting full control only to the current user and `SYSTEM`.
Reads verify private ownership/security without changing permissions and
reject links, reparse points, nonregular files, and oversized data. Only the
requested directory and newly created components are hardened; existing
ancestors are not changed. External private plan files may reside in public
parent directories. Administrator and same-user malicious processes are
outside the privacy boundary; HMAC is authentication, not encryption.

On macOS, use local APFS/HFS with ownership enabled and without nonempty
extended ACLs. On Windows, the volume must support persistent ACLs. Both
require hard-link publication support. Linux compilation is supplementary,
not a substitute for the required native macOS/Windows jobs. An interrupted
process may leave private staging files; automatic recovery is not yet added.

The store signs the complete canonical version-1 JSON with HMAC-SHA-256,
including explanations, planning-only evidence, observation timestamps, and
list ordering. UTC timestamp normalization does not mutate caller values.
`Load` requires the exact canonical bytes, apart from surrounding whitespace:
do not pretty-print or reorder a saved/exported file. Duplicate/unknown/case-
aliased fields and noncanonical times are rejected rather than interpreted
ambiguously. The MAC is checked before schema, expiry, or action metadata is
trusted. Another installation's key cannot authenticate an exported plan.

The version-1 integrity object is the final JSON field. Loading first checks
its fixed canonical trailer and authenticates the raw bytes with the MAC
value elided, before decoding candidates or evidence. A canonical typed
round-trip is checked afterward. Compact unauthenticated arrays therefore
cannot expand into large typed plans before rejection.

Plans are limited to 16 MiB. IDs start with `plan_`, contain only ASCII
letters/digits/underscores/hyphens in their nonempty suffix, and are at most
128 bytes. Other nonempty, non-NUL inputs to `Load` are paths, including bare
relative filenames without an extension. A valid ID takes precedence; use
`./plan_example` or an absolute path to load a file whose name is also an ID.
Expiry must be strictly later than the clock. The builder
owns populating generation time; this low-level store rejects nonzero future
generation times but accepts zero for minimal plan construction. Missing or
corrupt key reads fail without generating replacement state; existing corrupt
keys are never silently replaced by saves. Invalid saves are rejected before
state creation, and loading never creates state or repairs ACLs.

Already-canceled operations, and cancellation detected during preflight, do
not create state. Once filesystem work begins, cancellation is checked
between phases but cannot interrupt synchronous OS calls. Private directories
or a key can remain if cancellation arrives during initialization; publication
already in progress can complete successfully. Cancellation does not roll
back shared initialization or delete immutable files used by other callers.

## Plan building and inspection

Task 7C's `Builder.Build` uses explicit inventory/process interfaces, an
injected clock, and `Request` policy settings and intended apply mode. It
collects inventory before bounded process inspection, correlates evidence,
then evaluates policy before fingerprinting the final action and snapshot
requirements. Only safe candidates propose `remove`, with a required snapshot.
Generation/expiry are recorded, policy settings are digested, and plan IDs
combine canonical content with cryptographic randomness. No mutation API or
adapter collection is wired into this stage.

Invalid requests and parent-context cancellation return no usable plan.
Collection failures instead return an inspectable plan plus an error, with
diagnostics blocking potentially affected candidates. Missing or conflicting
worktree identity and incomplete process enumeration never establish safety.
The CLI persists a returned partial plan but exits unsuccessfully. Tests cover
collector ordering, policy/action/fingerprint coupling, immutability, timeout
handling, duplicate identities, and summary overflow using synthetic data.

Proven-local process failures carry a direct `*process.WorktreeError` with
affected input worktree paths. The collector's diagnostic strings preserve
returned-error order. Builder validates this correspondence and the scope
before restricting a warning to those candidates; it never infers scope merely
from matching message text. Enumeration, containment, unbound uncertainty,
inconsistent diagnostics and otherwise unscoped errors still block globally.

`Store.Latest` authenticates each `.json` plan before considering its generation
time. It skips only otherwise-valid authenticated expired plans; filename/ID
mismatches and invalid generation windows block selection even after expiry.
Other load errors also stop selection. Equal generation times use descending
plan ID order. The selected plan is checked again for expiry and cancellation.
Unrelated non-JSON files and in-flight private staging files are not plans.
Missing state stays missing.

CLI JSON output retains the canonical signed bytes, with one trailing stdout
newline; `--output` omits that newline to match storage exactly. Export creates
an independent private staging file under private state, then uses exclusive
hard-link publication and removes staging. It never links the canonical plan
itself. Existing export-parent permissions are preserved, newly created parents
are private, and other users cannot alter the private staging entry. State and
destination must be on the same filesystem; cross-filesystem publication fails
without an unsafe fallback. Saved plans survive export or output failures.

Explain loads authenticated state without collecting fresh evidence or parsing
current repository policy. It prints one candidate's full recorded evidence,
with plan-level warnings on stderr. Discovery roots and private file/state/export
paths preserve `..` until ancestor resolution, so a symlink cannot silently
change the requested scope or select another authenticated plan through
premature lexical cleaning. Relative inputs use the physical working directory,
not a logical `PWD` alias. Unresolvable traversal fails closed, and final
file/directory symlinks remain rejected. Final CLI errors also escape control
characters, including those inside wrapped filesystem errors.
Temporary Git end-to-end fixtures verify that planning, export, explanation,
and expiry rejection leave indexes, branches and registrations unchanged.
No real-workspace smoke test is used.

## Delivery

Follow the [implementation plans](plans/README.md) in order. Each stage, or
clearly labeled reviewable slice, gets a PR with its tests and limitations.
Request independent review, fix blocking findings, and keep human approval
separate from AI review. Do not merge automatically. A dependent PR may be
stacked on its prerequisite; name the base PR and do not claim it is merged.

The baseline deliberately uses standard Go tools, not a custom verifier,
acceptance manifest, or stage activation system. Add product tests alongside
the behavior they protect; passing baseline tests is not evidence that
unimplemented cleanup behavior is safe.
