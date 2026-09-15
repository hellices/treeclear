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
code, run children, read file contents, or write files. It returns only the
challenge, protocol identity, effective UID, error/unknown counts, fixed error
categories, and an exact active-child identity-match boolean. Raw process
records, command lines, paths, owners and error text never enter CI output or
artifacts. Malformed or mismatched replies, excess output, denied elevation,
timeouts and partial collection must fail the opt-in test, not skip or fall
back to ordinary collection. Fixture digests and ordinary-user ownership must
remain unchanged.

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
https://github.com/shirou/gopsutil/blob/v4.26.8/process/process_darwin.go
```
