# Explicit macOS process visibility

This is the Plan 001 Task 5/7C follow-up for issue #18. It does not advance
snapshot work, approve PR #14, add mutation commands, or qualify a release.

Status: Slice A corrects native Darwin enumeration. The permission boundary
in Slice B is a proposal, not available functionality. Independent AI design
review requires an end-to-end proof before that boundary is implemented:
ordinary-user path validation may still fail even when a helper can read
native process fields. No path validation may be bypassed to claim success.

## Problem and alternatives

The installed ordinary-user preview cannot inspect other users' working
directories. Darwin's `proc_security_policy` checks the caller's effective
UID for `PROC_PIDVNODEPATHINFO`; root normally has `PRIV_GLOBAL_PROC_INFO`,
but MAC policy can still deny inspection. Full Disk Access is not a substitute
for this UID check. Ownership is not evidence of worktree irrelevance.

Ignoring inaccessible processes is unsafe. Running the whole CLI as root is
also rejected: Git, configuration, repository discovery, and plan/key writes
must retain the invoking user's privileges. A permanently privileged service,
setuid executable, or sudoers rule is unnecessary for the read-only preview.

The immediate approach is an explicitly authorized, test-only one-shot
process reader. No existing command escalates automatically and installation
remains unprivileged. A genuinely non-administrator account without separately
authorized inspection still gets a protective incomplete result.

## Installation permission and persistent privilege

An administrator authorizing file installation does not give future CLI
processes root privileges. Keeping that ability requires a separate privileged
mechanism and its own authentication, authorization, update and removal
contract. A root-owned executable without setuid is not a root service.

For ongoing privilege on macOS 13 and later, Apple's Service Management APIs
are the candidate for a separately reviewed, signed on-demand helper. This
does not require keeping the helper process continuously running, but it does
leave a registered privileged capability. Client code identity and operation
authorization must both be checked; an authenticated connection alone is not
authorization. Registration approval, denial/revocation, updates, removal,
protocol limits and root-owned executable integrity need native tests.

A helper intended to be read-only still executes as root: that intention is
not an OS sandbox. A parser, dependency or request-validation flaw can have
system-wide consequences. Do not install a daemon, setuid binary, sudoers
exception or whole-CLI root launcher as a shortcut. Root also does not bypass
every Mandatory Access Control or privacy restriction; Full Disk Access and
administrator authorization are separate concerns.

The earlier `--process-source=native|sudo` product proposal is withdrawn.
Apple discourages programmatic sudo in a shipping app. The explicit sudo
invocation below is only a disposable development experiment, not a new CLI
option, supported installer, runtime permission solution or release design.

## Test-only permission feasibility boundary

Before implementing any product privilege transport, test the complete
existing `process.Collector`, not just `Source.List`, as root. Executable and
cwd canonicalization, file-type checks, and checked containment must still
run. A probe failure does not justify skipping these checks or excluding
processes based on their owner. The test driver and Git fixtures stay
unprivileged throughout both the root-side expiry check and collection.

The probe is a separate test executable under `tests/processprobe`; normal
`make install` still installs only `treeclear`. Ordinary Go unit tests exercise
the probe contract without elevation. Only an explicit workflow-dispatch
opt-in on a disposable native macOS runner invokes the installed probe through
absolute `/usr/bin/sudo -n --`. This test does not support local or self-hosted
elevated execution. Before building, creating fixtures or invoking sudo, it
requires all of `TREECLEAR_TEST_PROCESS_PROBE=1`, `GITHUB_ACTIONS=true`,
`GITHUB_EVENT_NAME=workflow_dispatch`, `RUNNER_ENVIRONMENT=github-hosted`,
`RUNNER_OS=macOS`, and `GITHUB_JOB=process-visibility-probe`. Absent opt-in skips;
present opt-in in any other context fails. These spoofable environment checks
prevent accidental execution; they are not authentication. Sudo still controls
OS authorization, and the exact reviewed binary must be trusted. Never prompt
or retry without `-n`, read a password, or change system authorization policy.

The ordinary-user test creates temporary `internal/testutil` Git fixtures and
an owned sleep child. It installs the test executable into a private temporary
`GOBIN`, records its SHA-256, and sends one bounded canonical versioned JSON
request through private stdin. The request binds a fresh random challenge,
the original non-root UID, exact fixture paths, and the owned child's PID and
microsecond creation time. The probe rejects non-root execution and a caller
UID that differs from sudo's original-user identity. Both sides use fixed
argv/environment, bounded I/O and deadlines; the root process must terminate
itself on expiry because an unprivileged parent cannot reliably kill it.

The probe only inspects process metadata and performs the collector's path
checks. It does not import Git, CLI, configuration, state, adapters or mutation
code, run children, read file contents, or write files. By default it returns the
challenge, protocol identity, effective UID, error/unknown counts, fixed error
categories, and an exact active-child identity-match boolean. Raw process
records, command lines, paths, owners and error text never enter CI output or
artifacts. Malformed or mismatched replies, excess output, denied elevation,
timeouts and partial collection must fail the opt-in test, not skip or fall
back to ordinary collection. Fixture digests and ordinary-user ownership must
remain unchanged.

Diagnostic reports count the first collector/source field prefix for each
returned error using a fixed vocabulary, plus fixed permission, missing-path,
invalid-argument, native-read and other error-wording categories. These are
advisory string classifications, not typed OS errors or proof of the cause;
later failures in a combined message are not enumerated. Both category totals
must equal the returned error count, and the parent rejects unknown labels,
invalid counts and malformed replies before logging. No diagnostic changes
completeness, the set of processes inspected, or any path validation. Raw
messages and arbitrary labels remain excluded from reports and CI artifacts.
Executable-read errors additionally have a fixed errno-label histogram. Its
total cannot exceed the executable-first-error count. `unavailable` means the
native call supplied no errno; it must not be misreported as OS-supplied EIO.
Unrecognized codes use `other`, never an arbitrary raw value or label.

Three additional fixed booleans report whether the probe's PID, its current
parent PID or the test-driver PID from the private request occurs in the
existing uninspectable map. The driver PID must be positive, but it is only a
diagnostic hint, not caller authentication or authorization. These flags use
PID membership rather than creation identity; they prove neither ownership
nor relevance. Parent and driver can coincide. Any true flag with a zero
uninspectable count invalidates the report before logging. Raw PIDs remain
private, and the flags never change completeness or the processes inspected.

For a retained uninspectable entry, the test-only probe may additionally read
fixed-size native process metadata to produce target/parent role histograms.
It samples at most 16 entries in deterministic PID order, with at most three
metadata reads per entry and a context check between reads. Inputs exceeding
65,536 entries are not sampled or allocated into another PID list. A missing
metadata reader also marks every entry as not sampled, not unavailable. Two target
reads bracket a parent read; the target PID/start-time must match the collection
and both target records must agree before attribution. A parent hint also
requires the requested parent PID and an earlier positive creation time.
This is a bounded observation, not an atomic ancestry snapshot.

By default, only fixed labels for CI listener/worker, Go/Node tool, shell, system init,
other, unavailable, changed identity and unsampled entries leave the private
protocol. Both histograms retain all entries as counts; the receiver rejects
unknown labels, non-positive/oversized values or totals differing from the
uninspectable count before logging. Both maps must have identical unsampled
and identity-changed counts; the parent unavailable count must be at least the
target unavailable count. The shared sampled count cannot exceed 16 and must
be zero for inputs above 65,536 entries. Raw names remain private except for
the explicitly approved one-time diagnostic below. PIDs, parent PIDs, owners,
paths, arguments and metadata errors always remain private. Process names are
spoofable hints, not image attestation, caller authentication, ownership or
proof of worktree irrelevance. These labels never filter a process, clear an
error, substitute an executable path or qualify incomplete collection.

On September 16, 2026, the user explicitly approved one disposable hosted-macOS
diagnostic run that may disclose the retained failing target's and parent's
kernel display names in public CI logs. A separate workflow-dispatch boolean,
`process_visibility_names`, defaults to false and warns about public disclosure.
Only explicit true supplies `TREECLEAR_TEST_PROCESS_PROBE_NAMES=1`; the existing
probe opt-in and all hosted/manual interlocks still apply. Other nonempty name
flag values fail before installation or elevation. The request carries optional
`include_process_names`, and the parent independently binds reply acceptance to
its own opt-in. Probe authorization alone never authorizes names.

The optional `uninspectable_names` list contains at most 16 process/parent
pairs from the existing identity-validated sampling, without extra native
lookups. Each emitted name is 1-16 bytes of ASCII letters, digits, dot, hyphen
or underscore. Unsafe or overlong names are withheld, never truncated into a
different hint. A process name is required for each pair; its parent is optional
and requires the existing parent-identity checks plus the same name validation.
Changed, unavailable, canceled and unsampled target identities have no pair.
The receiver rejects unapproved names, invalid characters/lengths, extra fields,
and pair totals exceeding the attributable sampled targets or parents, before
logging any report. Complete/no-unknown reports cannot contain names. Errors
never echo rejected contents. No PID, full path, owner, argv, environment,
timestamp, raw error or other process record is added. These spoofable display
names are advisory hints only, not executable identity, authorization or a
reason to filter a process. Completeness and all safety checks are unchanged.
This consent covers one reviewed run, not recurring disclosure or a local
privileged path; additional name-disclosing runs need renewed user approval.

The operator must trust the exact test executable before authorizing it. The
private pipe and challenge prevent accidental response mixups; they are not a
sandbox or protection against malicious code already controlling the caller's
binary or test state. The probe installs no privileged file, service or
authorization exception. Normal operating-system audit logging is not disabled.

Passing the probe establishes native process/path feasibility only on that
runner. It does not establish complete installed `treeclear scan`/`plan`, a
normal desktop permission experience, helper IPC security, Intel macOS
qualification or production readiness. A product boundary and complete
installed-binary acceptance remain separate prerequisites for issue #18.

## Native enumeration

Darwin gets a dedicated source; Windows retains the existing gopsutil source.
Read complete native PID/start-time snapshots around inspection, compare
creation identity as well as PID, and retry a bounded number of entire
collections if the observations conflict. Never silently drop a missing PID,
reuse cached creation identity across observations, or clear a residual error.
Empty, malformed, duplicated or unbounded snapshots and exhausted retries
remain incomplete. All inaccessible live processes are retained, regardless
of owner, and existing local/global unknown propagation is unchanged.

Native table sizing/read operations have their own three-attempt growth bound
and check the collection context between calls. Reject excessive or malformed
sizes before allocation (at most 65,536 kernel records). Do not call a native
library helper with an unbounded internal retry loop. This is a bound on the
software retries, not a claim that Go can preempt an individual kernel call.

The kernel's PID 0 record is not a user-space process. Recognize it only from
the native combination of PID/parent PID 0, root UID, `P_SYSTEM`, `SRUN`,
`kernel_task` name and valid start time. Unexpected PID 0 remains an error.
Likewise, only positively identified native zombies, with matching creation
identity at both boundaries, can be treated as exited; mere inspection
failure or `P_WEXIT` is not sufficient. Record these exclusions in source
tests and documentation rather than adding an owner/PID ignore list to policy.

Process fields use native start time and effective UID rather than network
username lookup. Executable, cwd and argv still come from native Darwin
inspection. This cannot make a sequence of OS queries atomic or prevent a
process starting immediately afterwards. The evidence is a bounded current
observation, not a lease. Future apply must invoke fresh collection and retain
the full-plan revalidation requirement; saved helper output is not an input.

The PR #22 CI investigation also corrects executable-error provenance in the
Darwin source: the pinned library discarded `proc_pidpath` errno. The native
wrapper uses the same system API with a fixed 4,096-byte buffer, pins the OS
thread, resolves the errno pointer before the call, clears it, and captures a
failure's errno before unlocking. Successful reads ignore stale errno and
require an absolute, exactly terminated path within the allocation bound.
Missing errno, malformed results and cancellation still fail; no fallback path,
process exclusion, extra privilege or collector-validation bypass is added.
This is a source-error correction, not a product permission mechanism.

A live owned process can return native ENOENT after its temporary executable
is unlinked. The isolated regression checks that the same PID/start-time is
still alive and that the collector retains unknown evidence for its active
worktree. ENOENT is not proof of process exit or irrelevance, and this controlled
reproduction does not identify the cause of the hosted feasibility failure.

## Future product integration acceptance

Use test-first ordinary Go tests for native identity changes, verified kernel
and zombie handling, unknown owner/access failures, enumeration retries and
exhaustion, cancellation, limits, strict helper replies, no implicit sudo,
and non-macOS refusal. No global cwd/config/environment changes in helpers.

After separately reviewing a product transport, exercise the actual installed
CLI and helper executables, not only in-process CLI calls.
Use `internal/testutil` to create primary/current/dirty/locked/active/clean
worktrees, keep a known child alive in the active worktree, and verify complete
scan/plan in the explicitly authorized mode. Revalidate plan export,
explanation, tamper rejection, fixture non-mutation and ordinary-user state
ownership. Native tests must fail, not pass on partial output, when they are
run as elevated-mode qualification. Ordinary default-mode protective coverage
remains separate. Local tests never request elevation automatically; only an
explicit opt-in test in a disposable CI runner may exercise the sudo mode.

Run uncached tests, race tests, vet, build and formatting, preserve native
Windows CI and independently review the change as AI review, not human
approval. Record the OS/architecture and exact installed binary provenance
for qualified runs. An untested permission or desktop remains a limitation;
passing protective tests alone does not close #18 or imply production readiness.

## Primary references

The locally inspected macOS SDK `sys/proc.h` defines `P_SYSTEM`, `SRUN` and
`SZOMB`. Apple's XNU source provides the access check, kernel identity and
exit-state semantics; the pinned gopsutil Darwin source implements the native
cwd call. Source locations:

```text
https://developer.apple.com/forums/thread/708765
https://developer.apple.com/documentation/servicemanagement/smappservice
https://developer.apple.com/documentation/servicemanagement/updating-helper-executables-from-earlier-versions-of-macos
https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/proc_info.c
https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/bsd_init.c
https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/kern_exit.c
https://github.com/apple-oss-distributions/xnu/blob/main/libsyscall/wrappers/libproc/libproc.c
https://github.com/shirou/gopsutil/blob/v4.26.8/process/process_darwin.go
```
