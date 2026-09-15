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

The selected approach is an explicitly requested, one-shot process reader.
No existing command escalates automatically and installation remains
unprivileged. A genuinely non-administrator account without separately
authorized inspection still gets a protective incomplete result.

## Permission boundary

The proposed `scan` and `plan` flag is `--process-source=native|sudo`, defaulting to `native`.
The `sudo` mode is macOS-only and never comes from repository configuration
or an environment variable. It invokes the installed sibling
`treeclear-process` through the absolute system `sudo`, with `-n`, fixed argv,
system-only environment, `/` as cwd, a deadline and bounded output. It never
prompts, installs anything, modifies sudo policy, or retries without `-n`.
The operator must separately authorize that command with the operating system.

The helper accepts only a versioned protocol request and a fresh random
challenge. It refuses non-root collection. It reads native process metadata;
it does not load Treeclear configuration, traverse repositories, execute Git
or adapters, create state, install jobs, or remove files. It returns the
challenge, protocol/build/platform identity, actual effective UID, process
records and collection error over its private stdout pipe. Both sides bound
time and data. Unexpected, truncated, non-canonical, stale, wrong-version,
non-root or failed responses become global unknown evidence, never a native
fallback or a successful empty list. Owner relations in received records are
recomputed against the original caller's UID, not the helper's root UID.

The helper is installed next to the CLI by ordinary `go install`; there is no
privileged installation. The user must trust both binaries before authorizing
their execution. This is not a sandbox for untrusted executables and does not
protect against a malicious process already controlling the user's installed
binaries or private Treeclear state. Signed release distribution remains
Plan 004 work.

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

## Verification and completion

Use test-first ordinary Go tests for native identity changes, verified kernel
and zombie handling, unknown owner/access failures, enumeration retries and
exhaustion, cancellation, limits, strict helper replies, no implicit sudo,
and non-macOS refusal. No global cwd/config/environment changes in helpers.

Exercise actual installed sibling executables, not only in-process CLI calls.
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
https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/proc_info.c
https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/bsd_init.c
https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/kern_exit.c
https://github.com/shirou/gopsutil/blob/v4.26.8/process/process_darwin.go
```
