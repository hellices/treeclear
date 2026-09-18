# Explicit whole-worktree removal

Date: 2026-09-18

Status: The maintainer approved the product direction in conversation. This
written contract is awaiting maintainer review; implementation is pending.
This documentation slice does not enable deletion, change existing plans,
complete a technical review, or qualify a release.

## Decision and scope

The default for an explicitly selected linked worktree is to discard its
entire contents without a Treeclear backup. This includes staged, unstaged,
unmerged, untracked, and ignored files, including dependency directories and
environment files. Local branches are not deleted. There is no Treeclear
undo for a no-backup removal; preserving a branch does not preserve local
changes or ignored files.

"All contents" does not mean "all discovered worktrees." Discovery never
selects deletion targets. No-argument commands remain non-mutating. This
contract authorizes a product capability, not removal of any real user
worktree during development.

Mandatory snapshots were rejected for this default. Silently disregarding
snapshot requirements in existing plans was also rejected. Optional backup
is a separate, explicitly requested capability and cannot fall back to
unbacked deletion.

This amends only content-disposal and backup policy. Primary/current,
locked/active, unsafe-path, unknown-evidence, inactivity, adapter-trust,
offline execution, private-state, and branch-preservation protections remain.
Windows product support remains deferred to #15; native Windows compatibility
CI remains required alongside both native macOS architectures.

## Proposed command contract

The following commands and flags describe future behavior, not functionality
available in the current read-only preview:

```text
treeclear plan --root <repository-scope> --worktree <exact-linked-worktree-path>
treeclear plan --root <repository-scope> --worktree <path> --skip-dirty
treeclear apply --plan <plan-id-or-file>
treeclear apply --plan <plan-id-or-file> --yes --format json
```

- `--worktree` is repeatable and selects exact registered linked-worktree
  roots, not globs, repository roots, path prefixes, or recursive descendants.
  Relative paths are resolved against the invocation directory without
  changing process-wide cwd. Spaces and commas are literal path characters.
- Every requested target must resolve unambiguously within the collected
  scope. Duplicate identities, overlapping targets, aliases that cannot be
  safely verified, missing registrations, and unsupported state produce no
  executable removal plan; they are not silently discarded from selection.
- Without explicit targets, planning remains a read-only inventory preview;
  it must not produce executable removals under the new contract. Unselected
  candidates always have action `none`.
- `--skip-dirty` defaults to false. When true, staged, unstaged, unmerged, or
  non-ignored untracked changes exclude that target. Ignored files alone do
  not make a target dirty for this option and do not protect it from deletion.
  These exclusions are recorded and cannot become removals during apply.
- `--backup` is an opt-in planning choice. Until the optional recovery
  contract is implemented and independently reviewed, a request fails
  explicitly; no actionable backup plan or silent downgrade is allowed.
- `--yes` only supplies confirmation for the exact authenticated plan. It
  cannot add targets, change disposal/backup policy, bypass a lock, or make
  incomplete evidence acceptable. Non-interactive or JSON apply requires
  `--yes`; it must not hang waiting for input.
- No repository configuration, environment variable, or adapter supplies
  target selection or confirmation. Apply has no content-policy override
  flags; a policy change requires a newly generated plan.

Human plan/explain/confirmation output must distinguish selection from
eligibility and state that all contents, including ignored files, will be
deleted without backup. Escape paths and control characters. A missing,
negative, or unreadable confirmation means no worktree mutation.

## Eligibility is separate from disposal

Explicit selection permits discarding known dirty contents; it does not
turn an existing `protected` decision into permission by deleting one reason
code. A dedicated, tested eligibility evaluation must run every retained
protection before assigning any removal action. In particular, the existing
policy returns on the first reason: bypassing its dirty result could hide a
later active-process or unknown-evidence result.

All selected targets require complete fresh Git, process, and applicable
agent evidence. An explicitly selected primary/current, locked, active,
recent, inaccessible, conflicting, or unsafe target blocks the executable
batch. The same is true for unproven branch/HEAD retention, unsupported
submodule or nested-repository state, and unverified filesystem boundaries.
An intentional `--skip-dirty` exclusion is different: it remains visible with
action `none`, while otherwise eligible selected targets may proceed.

Detached HEAD without a separately proven retained reference is not made
safe by claiming that local branches are preserved. The no-backup mode does
not create hidden recovery refs. Existing scan recommendations and scheduled
safe-only cleanup are not broadened to include dirty targets. A scheduler
must not infer explicit selection from a repository root or use this manual
discard-all contract for unattended deletion.

## Authenticated plan migration

Use a new plan schema version; do not reinterpret version 1. The new schema
must authenticate the exact selection, explicit-removal intent, discard-all
disposition, backup mode, `skipDirty` choice, final actions, identities,
evidence, expiry, policy/adapter digests, and candidate fingerprints.
Missing or unknown disposal fields are errors, not default permission.

Continue to inspect/explain historical version-1 plans without mutation.
Apply rejects them even when their signatures are valid. A stored plan that
requires a snapshot never becomes an unbacked plan through a new executable,
flag, configuration change, or automatic migration. The plan schema change
does not change the independent snapshot-manifest schema.

Deletion eligibility, user confirmation, and plan authentication are separate
requirements. A signature, selection flag, classification, or `--yes` alone
is insufficient. The exact serialized fields and compatibility tests belong
to the first read-only implementation slice after this contract is reviewed.

## Removal boundary and journal

Apply remains macOS-only and offline. It acquires the existing planned shared
operation lock, verifies the complete plan/environment, and revalidates every
pending target before the first removal. It revalidates each target again
immediately before its own removal, including branch, HEAD, registered root,
native path identity, and the content state authorized by the plan. Ignored
and untracked state cannot be omitted merely because ordinary Git status
does not describe it fully. Unsupported or incomplete observations block.

Use Git's worktree-removal operation, not a recursive-filesystem fallback.
Git requires force for an unclean worktree; at most one `--force` is permitted
only by the authenticated explicit discard-all contract after all retained
checks pass. Never escalate on failure, pass double force to defeat a lock,
unlock a target, remove branches, prune unrelated registrations, or change
Git safety configuration. The ordinary safe-only cleanup route does not use
force. Submodules and nested independent repositories are not silently
removed by treating force as authorization for additional repositories.

Record a private durable intent before each removal and its result afterward.
The journal records backup mode and exact completed, skipped, failed, or
uncertain actions, not file contents. Stop remaining removals on failure or
changed evidence; report partial completion and do not promise rollback.
A retry must never delete a recreated path or infer success merely from an
absent path without a trustworthy corresponding journal outcome. Ambiguous
interrupted outcomes require inspection rather than blind repetition.

In no-backup mode, do not create a content archive, recovery ref, or recovery
receipt. Plans and journals are still required local control records, not
backups. Per-target checks and the Treeclear lock do not make external
filesystem changes atomic or provide immunity to arbitrary concurrent
replacement. The reviewed implementation and acceptance evidence must state
that boundary accurately.

## Optional backup and existing review debt

Backups, restore, and trash management are not prerequisites for the base
no-backup feature. Existing snapshot codecs and tests are retained. The
old recovery design excludes ignored files; it is therefore not a complete
backup of the new discard-all scope. Do not expose `--backup` as working
until its reviewed coverage preserves the otherwise-lost state, including
ignored files, or rejects unsupported/sensitive/over-limit targets before
any deletion. Failure to create or verify any requested backup aborts the
whole batch before its first removal. Restore must be qualified before
advertising recovery.

Changing product scope does not close
[source-identity review #23](https://github.com/hellices/treeclear/issues/23).
Snapshot-specific lifecycle work can be deferred, but shared identity,
inventory, and source-read dependencies still need an authorized independent
assessment before dependent mutation implementation or qualification. Do not
route the previously unavailable assessment through a substitute reviewer,
claim an unrelated review as clearance, or replace readers to bypass that
outcome. This design review is not that missing source-code verdict.

[Process visibility #18](https://github.com/hellices/treeclear/issues/18)
also remains a release blocker: inaccessible processes are not inactivity.
No privilege elevation, new helper experiment, TCC/SIP change, or actual user
worktree removal is authorized by this amendment. Signing/notarization #25
and the other macOS release requirements are unchanged.

## Delivery and acceptance

Deliver scoped PRs in this order, with independent review and required native
CI before dependent stages:

1. Review this contract and align the architecture and contributor rules.
2. Implement versioned explicit selection, disposal policy, read-only
   plan/explain rendering, and legacy-plan rejection tests. Expose no deletion.
3. After applicable source-identity and process-evidence prerequisites are
   cleared, implement retained eligibility checks, the shared lock, journal,
   Git removal boundary, and whole-batch/per-target revalidation.
4. Wire confirmed apply, unsupported-platform refusal, and installed-binary
   acceptance against disposable `internal/testutil` fixtures only.
5. Develop optional backup/restore in separately reviewed slices; do not
   reintroduce it as a prerequisite for unbacked deletion.

Write failing tests before each behavior change. Acceptance must cover:

- Literal and ambiguous target selection, unselected worktree preservation,
  version-1 rejection, tampered/missing disposal fields, and no apply-time
  policy override.
- Default dirty/untracked/ignored disposal, `--skip-dirty`, an ignored-only
  target, explicit no-backup confirmation, non-interactive refusal without
  `--yes`, and unavailable/requested-backup failure without deletion.
- Each retained protection, especially dirty plus active/unknown evidence;
  no short-circuit bypass of later protections.
- Whole-batch failure before the first removal, post-plan content/identity
  changes including ignored files, per-target changes, cancellation, partial
  Git failures, interrupted journals, and recreated-path refusal.
- Actual installed macOS binary removal of selected disposable fixtures,
  including ignored `.env`-named synthetic content and dependency directories;
  retained branches and unselected/protected sentinel fixtures; and no backup
  artifacts in no-backup mode. Mock-only success is not native acceptance.
- Required uncached normal/race suites, vet, build, formatting, and native
  macOS arm64/amd64 plus Windows compatibility CI. Preserve the macOS
  user-temporary-directory semantics in local isolated validation.

No implementation, successful product deletion, independent source-review
clearance, or production readiness is claimed by this documentation stage.

## Reference

[Git worktree documentation](https://git-scm.com/docs/git-worktree) defines
ordinary removal, dirty-worktree force, locked-worktree double force, and
the prohibition on removing a main worktree. These Git capabilities are not
substitutes for Treeclear's eligibility and explicit-selection checks.
