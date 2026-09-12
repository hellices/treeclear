# Treeclear Implementation Plans

These plans turn the accepted
[Treeclear Architecture](../architecture/2026-09-12-treeclear.md) into
independently testable delivery stages.

Execute them in sequence:

0. [Minimal Development Baseline](000-development-harness.md)
1. [Safety Core](001-treeclear-core.md)
2. [Agent Adapters](002-agent-adapters.md)
3. [Adapter Lifecycle](003-adapter-lifecycle.md)
4. [Operations and Release](004-operations-and-release.md)

Each plan must leave the repository buildable and tested. Plans are execution
records, while `docs/architecture/` and `docs/specs/` remain the durable source
of product and protocol behavior.

## Cross-plan CLI ownership

| Command or flag | Owning plan |
|---|---|
| `treeclear`, `version` | 001 |
| `scan --root --inactivity-threshold --format` | 001 |
| `plan --root --inactivity-threshold --format --output` | 001 |
| `explain --plan --format` | 001 |
| `apply --plan --format` | 001 |
| `restore`, `trash list`, `trash prune` | 001 |
| `adapters list`, `adapters doctor` | 002 |
| `adapters init`, `validate`, `test`, `trust` | 003 |
| `adapters update`, `adapters rollback` | 003 |
| `integration install`, `status`, `remove` | 004 |
| `schedule install`, `status`, `remove`, `run` | 004 |

## Safety traceability

| Architecture invariant | Persisted field or mechanism | Revalidated by | Primary test owner |
|---|---|---|---|
| Unknown never means inactive | `EvidenceState=unknown`, `GitStateKnown`, and reason codes | Policy engine | 001 Tasks 4-6, 002 Tasks 4-10 |
| Process enumeration must be complete | `process.Collection.Complete` plus global unknown sentinel | Correlator and policy | 001 Tasks 5-6 |
| No primary/current/dirty/locked removal | Full `candidatePreconditions` | Apply revalidator | 001 Tasks 6 and 9 |
| Exact plan and policy | Canonical `PolicySettings`, `PolicyDigest`, candidate fingerprint, expiry | `EnvironmentVerifier` | 001 Tasks 7 and 9 |
| Exact adapters and commands | Adapter/source/bundle provenance, `AdapterLockDigest`, trust digest, executable, argv, cwd, and environment identities | `EnvironmentVerifier` | 002 Tasks 3-6, 003 Tasks 2 and 5 |
| Custom trust scope matches apply mode | `Plan.IntendedApplyMode` and scoped trust record | Collector plus apply engine | 002 Task 6, 003 Task 5 |
| Update and apply never overlap | Shared `internal/statelock` lock | Apply and updater | 001 Task 9, 003 Task 7 |
| Snapshot before mutation | Snapshot receipt plus policy/evidence digests | Apply engine | 001 Tasks 8 and 9 |
| Branch preserved | Removal argv omits branch deletion | Git client | 001 Tasks 3, 9, and 11 |
| Apply stays offline | No updater dependency in apply and network-failing e2e transport | Apply e2e | 003 Task 7 |
| Apply never executes provider commands | Local-readonly collection plus file-only executable verification | Apply e2e | 002 Tasks 5-6, 003 Task 7 |
| Adapters have no core mutation API | Evidence-only SPI | Compile-time interfaces and security tests | 002 Tasks 1-6 |
| Scheduled cleanup defaults plan-only | Scheduler `JobKind` and `Mode` | Schedule service | 004 Tasks 3-6 |
| Signed update rollback | Version store state and signed index version | Updater transaction | 003 Tasks 1-4 |
