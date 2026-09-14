# Treeclear Operations and Release Implementation Plan

- Status: Planned
- Sequence: 004 of 004
- Source architecture: [Treeclear Architecture](../architecture/2026-09-12-treeclear.md)
- Depends on: [003 Treeclear Adapter Lifecycle](003-adapter-lifecycle.md)

Stage 000 establishes baseline contributor rules and native CI before feature
development. Tasks 1 and 7 extend that foundation with the completed product's
documentation, e2e, platform, and release requirements; they do not replace it.

> Execute this plan task-by-task using an isolated Git worktree, test-driven development, and a review checkpoint after every task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver Treeclear as a documented, agent-friendly, scheduled, tested, signed macOS product with reproducible release artifacts and SBOMs. Windows product qualification and publication are deferred to [follow-up #15](https://github.com/hellices/treeclear/issues/15).

**Architecture:** Repository Markdown remains canonical, `AGENTS.md` provides tool-neutral contributor instructions, and one standard Agent Skill is packaged through thin host wrappers. A scheduler service generates plan-only launchd jobs. Existing native macOS/Windows CI remains in place, while first-release jobs sign macOS binaries, generate checksums and SBOMs, and publish immutable GitHub Release assets. Windows Task Scheduler and release jobs belong to #15.

**Tech Stack:** Existing Go stack, Agent Skills open standard, launchd property lists, Windows `schtasks.exe`, GitHub Actions, actions/checkout 7.0.1, setup-go 7.0.0, upload-artifact 7.0.1, download-artifact 8.0.1, Azure Artifact Signing Action 2.0.0, Anchore SBOM Action 0.24.2, Cosign Installer 4.1.2, Syft 1.51.1.

Windows-only tools in this stack are retained as follow-up design references,
not first-release dependencies. Task 5 and Windows-only portions of Tasks 6,
8, and 9 are deferred to #15. They are not marked complete and do not block
the macOS product milestone. Existing required CI and shared-code review
findings are not waived.

## Global Constraints

- Git-tracked Markdown in `docs/` is canonical.
- External or local LLM Wiki instances are supplemental indexes only.
- `AGENTS.md` is the canonical cross-agent operating instruction file.
- `CLAUDE.md` imports `AGENTS.md` and contains only Claude-specific additions.
- Skills live under `.agents/skills`, not under product documentation.
- Default scheduled cleanup is weekly, offline, and plan-only.
- Safe-only scheduled apply requires explicit opt-in.
- Adapter updates use a separate opt-in schedule and never run in a cleanup apply process.
- Scheduling uses no resident daemon.
- First-release artifacts target macOS amd64/arm64, with native runtime
  acceptance for each advertised architecture. Windows artifacts are deferred.
- Release binaries are signed; releases include SHA-256 checksums and SPDX JSON SBOMs.
- GitHub Actions are pinned to full commit SHAs.
- Treeclear remains telemetry-free.

---

## File Structure

| Path | Responsibility |
|---|---|
| `AGENTS.md` | Canonical repository build, test, safety, and documentation rules |
| `CLAUDE.md` | Portable Claude import shim |
| `CONTRIBUTING.md` | Human contribution workflow |
| `SECURITY.md` | Vulnerability reporting and security boundaries |
| `docs/specs/cli-behavior-v1.md` | Stable CLI and exit-code contract |
| `docs/specs/plan-format-v1.md` | Public plan and journal contract |
| `docs/specs/adapter-contract-v1.md` | Adapter manifest and evidence contract |
| `docs/adr/0001-fail-closed-unknown-evidence.md` | Unknown-evidence decision |
| `docs/adr/0002-plan-before-mutation.md` | Plan/apply decision |
| `docs/adr/0003-local-recovery-snapshots.md` | Recovery decision |
| `docs/adr/0004-adapter-trust-and-versioning.md` | Adapter lifecycle decision |
| `.agents/skills/treeclear/SKILL.md` | Portable Treeclear Agent Skill |
| `.agents/skills/treeclear/references/safety.md` | Skill safety reference |
| `integrations/claude/.claude-plugin/plugin.json` | Claude thin package |
| `integrations/codex/agents/openai.yaml` | Codex thin metadata |
| `internal/integration/install.go` | Skill installation into supported user paths |
| `internal/schedule/service.go` | Scheduler-neutral plan/report contract |
| `internal/schedule/report.go` | Private scheduled report persistence |
| `internal/schedule/launchd_darwin.go` | launchd installation |
| `internal/schedule/schtasks_windows.go` | Deferred Windows Task Scheduler installation (#15) |
| `internal/schedule/unsupported.go` | Explicit unsupported-platform error |
| `internal/cli/integration.go` | Skill install/status/remove commands |
| `internal/cli/schedule.go` | Schedule install/status/remove commands |
| `.github/workflows/ci.yml` | Native tests and cross-build verification |
| `.github/workflows/release.yml` | Native signing and release publication |
| `scripts/package.sh` | Deterministic macOS packaging |
| `scripts/package.ps1` | Deferred deterministic Windows packaging (#15) |
| `docs/release.md` | Signing variables and release runbook |
| `tests/e2e/schedule_test.go` | Schedule lifecycle tests |
| `tests/e2e/skill_test.go` | Skill policy tests |

## Task 1: Establish canonical documentation and contributor instructions

**Files:**
- Create: `AGENTS.md`
- Create: `CLAUDE.md`
- Create: `CONTRIBUTING.md`
- Create: `SECURITY.md`
- Create: `docs/specs/cli-behavior-v1.md`
- Create: `docs/specs/plan-format-v1.md`
- Create: `docs/specs/adapter-contract-v1.md`
- Create: `docs/adr/0001-fail-closed-unknown-evidence.md`
- Create: `docs/adr/0002-plan-before-mutation.md`
- Create: `docs/adr/0003-local-recovery-snapshots.md`
- Create: `docs/adr/0004-adapter-trust-and-versioning.md`
- Modify: `docs/README.md`
- Modify: `README.md`

**Interfaces:**
- Documents the commands and schemas implemented by Plans 001-003.
- Establishes one canonical instruction source for Copilot, Claude Code, Cursor, Codex, and OpenCode.

- [ ] **Step 1: Write a documentation consistency test**

Create `internal/docs/docs_test.go`:

```go
package docs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCanonicalDocumentsExistAndDoNotReferenceToolSpecificPlanPaths(t *testing.T) {
	root := repositoryRoot(t)
	required := []string{
		"AGENTS.md",
		"CLAUDE.md",
		"CONTRIBUTING.md",
		"SECURITY.md",
		"docs/architecture/2026-09-12-treeclear.md",
		"docs/specs/cli-behavior-v1.md",
		"docs/specs/plan-format-v1.md",
		"docs/specs/adapter-contract-v1.md",
	}
	for _, name := range required {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(strings.ToLower(string(data)), "required sub-skill") {
			t.Fatalf("%s contains workflow-engine instructions", name)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
```

- [ ] **Step 2: Run the test and verify it fails**

Run:

```bash
go test ./internal/docs
```

Expected: FAIL because the required documents do not exist.

- [ ] **Step 3: Write concise canonical documents**

`AGENTS.md` must stay below 200 lines and contain:

```markdown
# Treeclear Contributor Instructions

## Build and test

- Format: `gofmt -w <changed-go-files>`
- Unit and integration tests: `go test ./...`
- Race tests: `go test -race ./...`
- macOS build: `go build ./cmd/treeclear`
- Windows build check: `GOOS=windows GOARCH=amd64 go build ./cmd/treeclear`

## Safety invariants

- Unknown evidence blocks cleanup.
- Adapters return evidence and never receive mutation APIs.
- Apply is offline and revalidates the complete plan before mutation.
- Never add a force-removal path for ordinary cleanup.
- Preserve local branches by default.

## Documentation

- Architecture: `docs/architecture/`
- External contracts: `docs/specs/`
- Implementation sequence: `docs/plans/`
- Durable decisions: `docs/adr/`
- Agent skills: `.agents/skills/`
```

`CLAUDE.md` is exactly:

```markdown
@AGENTS.md

## Claude Code

Use the same repository rules as `AGENTS.md`.
```

Each ADR contains `Status`, `Date`, `Context`, `Decision`, `Alternatives`, and
`Consequences`. Specs copy externally observable behavior from code and link
back to the accepted architecture; they do not duplicate implementation-task
checklists.

`SECURITY.md` explicitly states:

- private vulnerability reporting contact through GitHub Security Advisories;
- adapter trust boundary;
- no telemetry or upload;
- snapshots may contain sensitive untracked files;
- supported release branches and response expectations.

`docs/README.md` states that an LLM Wiki may index the repository but is never
authoritative.

- [ ] **Step 4: Run documentation and full tests**

Run:

```bash
gofmt -w internal/docs
go test ./internal/docs
go test ./...
git diff --check
```

Expected: all tests PASS and canonical documents contain no workflow-engine
instructions.

- [ ] **Step 5: Commit documentation governance**

```bash
git add AGENTS.md CLAUDE.md CONTRIBUTING.md SECURITY.md README.md docs internal/docs
git commit -m "docs: establish Treeclear contributor guides"
```

## Task 2: Package one portable Agent Skill with thin host integrations

**Files:**
- Create: `.agents/skills/treeclear/SKILL.md`
- Create: `.agents/skills/treeclear/references/safety.md`
- Create: `integrations/claude/.claude-plugin/plugin.json`
- Create: `integrations/codex/agents/openai.yaml`
- Create: `internal/integration/install.go`
- Create: `internal/integration/install_test.go`
- Create: `internal/cli/integration.go`
- Modify: `internal/cli/root.go`
- Create: `tests/e2e/skill_test.go`

**Interfaces:**
- Produces: `integration.Install(ctx context.Context, target integration.Target, source fs.FS) (integration.Result, error)`
- Produces commands: `treeclear integration install|status|remove`
- Produces one tool-neutral skill body.

- [ ] **Step 1: Write skill policy and installer tests**

Tests assert the skill:

- runs `treeclear plan --format json`;
- explains safe, review, protected, and adapter results;
- requires approval;
- applies the exact plan;
- never mentions `git worktree remove`, `--force`, direct session-database
  parsing, or session deletion.

Installer tests cover exact destinations:

```go
var targets = map[Target]string{
	TargetCopilot:  "~/.copilot/skills/treeclear",
	TargetClaude:   "~/.claude/skills/treeclear",
	TargetCodex:    "~/.agents/skills/treeclear",
	TargetCursor:   "~/.cursor/skills/treeclear",
	TargetOpenCode: "~/.config/opencode/skills/treeclear",
}
```

Test existing-file refusal, identical reinstall, hash mismatch, remove, and
Windows path behavior.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/integration ./tests/e2e -run Skill
```

Expected: FAIL because skill packaging and installation do not exist.

- [ ] **Step 3: Write the standard skill and installer**

Skill frontmatter:

```yaml
---
name: treeclear
description: Safely inspect, plan, approve, apply, and restore stale coding-agent Git worktrees with Treeclear.
license: Apache-2.0
compatibility: Requires the treeclear CLI on PATH.
---
```

The skill workflow is exactly:

1. Run `treeclear adapters doctor --format json`.
2. Run `treeclear plan --format json`.
3. Summarize classifications, evidence trust, blocked reasons, expected
   reclaimed bytes, and snapshot bytes.
4. Ask the user to approve the exact plan ID.
5. Run `treeclear apply --plan <exact-plan-id> --format json`.
6. Report completed, skipped, failed, snapshot, and restore IDs.

`integration.Install` copies embedded skill files atomically and records a
manifest containing file hashes. It never overwrites modified files without
`--replace` and an interactive confirmation.

CLI:

```text
treeclear integration install --target copilot
treeclear integration install --target claude
treeclear integration install --target codex
treeclear integration install --target cursor
treeclear integration install --target opencode
treeclear integration status
treeclear integration remove --target <target>
```

- [ ] **Step 4: Run skill and full tests**

Run:

```bash
gofmt -w internal/integration internal/cli tests/e2e
go test ./internal/integration ./internal/cli
go test ./tests/e2e -run Skill -v
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit agent integrations**

```bash
git add .agents integrations internal/integration internal/cli tests/e2e
git commit -m "feat: package the Treeclear agent skill"
```

## Task 3: Add scheduler-neutral reports and policy

**Files:**
- Create: `internal/schedule/service.go`
- Create: `internal/schedule/report.go`
- Create: `internal/schedule/report_test.go`
- Create: `internal/schedule/policy.go`
- Create: `internal/schedule/policy_test.go`

**Interfaces:**
- Produces: `schedule.Service.Install(ctx context.Context, request schedule.InstallRequest) error`
- Produces: `schedule.Service.Status(ctx context.Context) (schedule.Status, error)`
- Produces: `schedule.Service.Remove(ctx context.Context) error`
- Produces: `schedule.WriteReport(ctx context.Context, report schedule.Report) (string, error)`

- [ ] **Step 1: Write default and safe-only policy tests**

```go
func TestDefaultScheduleIsWeeklyPlanOnly(t *testing.T) {
	got := DefaultPolicy()
	if got.Frequency != Weekly || got.Weekday != time.Sunday || got.Hour != 3 {
		t.Fatalf("policy = %#v", got)
	}
	if got.Job != CleanupPlanJob || got.Mode != PlanOnly {
		t.Fatalf("default schedule mutates: %#v", got)
	}
}

func TestScheduledApplyRejectsLocalCustomTrust(t *testing.T) {
	err := ValidatePolicy(Policy{Job: CleanupPlanJob, Mode: ApplySafe, TrustMode: "local-custom"})
	if !errors.Is(err, ErrUnattendedTrust) {
		t.Fatalf("ValidatePolicy() error = %v", err)
	}
}
```

Test report private permissions, atomic writes, retention count, deterministic
JSON, and omission of transcript text.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/schedule
```

Expected: FAIL because schedule types do not exist.

- [ ] **Step 3: Implement the common scheduler contract**

```go
type Mode string
type JobKind string

const (
	PlanOnly  Mode = "plan-only"
	ApplySafe Mode = "apply-safe"

	CleanupPlanJob   JobKind = "cleanup-plan"
	AdapterUpdateJob JobKind = "adapter-update"
)

type Policy struct {
	Job             JobKind  `json:"job"`
	Frequency      Frequency `json:"frequency"`
	Weekday        time.Weekday `json:"weekday"`
	Hour           int       `json:"hour"`
	Minute         int       `json:"minute"`
	Mode           Mode      `json:"mode"`
	TrustMode      string    `json:"trustMode"`
	ReportRetention int      `json:"reportRetention"`
}

type InstallRequest struct {
	Executable string
	Roots      []string
	Policy     Policy
}
```

Default is Sunday at 03:00 local time, plan-only, no adapter update, and 12
reports retained.

Scheduled cleanup command:

```text
treeclear schedule run --mode plan-only --format json --output <private-report-path>
```

Task 3 defines scheduler policy and Task 6 owns `schedule run`. Scheduled
execution:

- stdin is never read;
- confirmation prompts are disabled;
- output must be an absolute path inside the private reports directory;
- custom local trust is rejected;
- plan never implies apply;
- adapter update accepts only the configured first-party HTTPS index.

Safe-only mode may call apply only when the generated plan contains selected
safe candidates and all evidence relevant to those selected candidates is
embedded or first-party signed. Protected and review candidates remain in the
plan with action `none` and do not block unrelated safe candidates. Collection
errors or unknown evidence affecting any selected candidate block the complete
apply transaction.

Adapter updates are a separate `AdapterUpdateJob` with a separate platform
label/task and command:

```text
treeclear schedule run --job adapter-update --format json --output <private-report-path>
```

It cannot set `Mode=ApplySafe`, cannot share a process invocation with a
cleanup job, and is not installed unless the user opts in.

- [ ] **Step 4: Run schedule and full tests**

Run:

```bash
gofmt -w internal/schedule
go test ./internal/schedule
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit schedule policy**

```bash
git add internal/schedule
git commit -m "feat: define scheduled cleanup policy"
```

## Task 4: Implement macOS launchd installation

**Files:**
- Create: `internal/schedule/launchd_darwin.go`
- Create: `internal/schedule/launchd_darwin_test.go`
- Create: `internal/schedule/unsupported.go`

**Interfaces:**
- Implements `schedule.Service` on macOS.
- Uses label `io.github.hellices.treeclear.weekly`.
- Uses separate label `io.github.hellices.treeclear.adapter-update` for the
  opt-in update job.

- [ ] **Step 1: Write plist and command-runner tests**

Golden plist assertions:

```xml
<key>Label</key>
<string>io.github.hellices.treeclear.weekly</string>
<key>ProgramArguments</key>
<array>
  <string>/absolute/path/treeclear</string>
  <string>schedule</string>
  <string>run</string>
  <string>--mode</string>
  <string>plan-only</string>
  <string>--format</string>
  <string>json</string>
  <string>--output</string>
  <string>/Users/example/Library/Application Support/Treeclear/reports</string>
  <string>--root</string>
  <string>/Users/example/workspace</string>
</array>
<key>StartCalendarInterval</key>
<dict>
  <key>Weekday</key><integer>0</integer>
  <key>Hour</key><integer>3</integer>
  <key>Minute</key><integer>0</integer>
</dict>
```

Test XML escaping, absolute binary requirement, private plist write,
idempotent status, bootout-before-replace, remove, and two configured roots.
The generated `ProgramArguments` must append one `--root`, `<canonical-path>`
pair for every configured root in its original order; no root may be omitted
or shell-joined.

- [ ] **Step 2: Run Darwin tests and verify they fail**

Run:

```bash
go test ./internal/schedule -run Launchd
```

Expected: FAIL because launchd support does not exist.

- [ ] **Step 3: Implement launchd through an injected command runner**

Path:

```text
~/Library/LaunchAgents/io.github.hellices.treeclear.weekly.plist
```

Use fixed argv:

```text
launchctl bootstrap gui/<uid> <plist>
launchctl print gui/<uid>/io.github.hellices.treeclear.weekly
launchctl bootout gui/<uid>/io.github.hellices.treeclear.weekly
```

Never use `launchctl load` or a shell string. Write the plist atomically and
remove it only after successful bootout or a verified not-loaded result.

- [ ] **Step 4: Run macOS schedule tests**

Run:

```bash
gofmt -w internal/schedule
go test ./internal/schedule -run Launchd
go test ./...
```

Expected: all tests PASS on macOS.

- [ ] **Step 5: Commit launchd support**

```bash
git add internal/schedule
git commit -m "feat: install launchd cleanup plans"
```

## Task 5: Implement Windows Task Scheduler installation (deferred to #15)

Retain these steps for the Windows follow-up. This task is not a dependency
of the macOS implementation of Task 6, and its unchecked steps do not count
as completed first-release work. Cross-build checks here remain supplementary
to the native Windows runtime acceptance required by #15.

**Files:**
- Create: `internal/schedule/schtasks_windows.go`
- Create: `internal/schedule/schtasks_windows_test.go`

**Interfaces:**
- Implements `schedule.Service` on Windows.
- Uses task name `Treeclear Weekly Plan`.
- Uses separate task name `Treeclear Adapter Update` for the opt-in update
  job.

- [ ] **Step 1: Write exact argv and quoting tests**

Expected creation argv:

```go
[]string{
	"/Create", "/F",
	"/TN", "Treeclear Weekly Plan",
	"/SC", "WEEKLY",
	"/D", "SUN",
	"/ST", "03:00",
	"/RL", "LIMITED",
	"/TR", `"C:\Program Files\Treeclear\treeclear.exe" schedule run --mode plan-only --format json --output "C:\Users\Example\AppData\Local\Treeclear\reports" --root "C:\Users\Example\workspace"`,
}
```

Test spaces, quotes, UNC executable rejection, every configured root in
original order, status query, not-found status, idempotent replacement, and
delete.

- [ ] **Step 2: Cross-compile tests and verify they fail**

Run:

```bash
GOOS=windows GOARCH=amd64 go test -c ./internal/schedule
```

Expected: FAIL because Windows scheduler code does not exist.

- [ ] **Step 3: Implement Task Scheduler with fixed `schtasks.exe` argv**

Commands:

```text
schtasks.exe /Create ...
schtasks.exe /Query /TN "Treeclear Weekly Plan" /FO LIST /V
schtasks.exe /Delete /F /TN "Treeclear Weekly Plan"
```

Reject executable paths containing a quote or control character. Resolve the
binary to an absolute local path before composing `/TR`. Do not invoke
PowerShell or `cmd.exe`.

- [ ] **Step 4: Run Windows cross-build and native-independent tests**

Run:

```bash
gofmt -w internal/schedule
GOOS=windows GOARCH=amd64 go test -c ./internal/schedule
rm -f schedule.test.exe
go test ./...
```

Expected: Windows tests compile and the full local suite passes.

- [ ] **Step 5: Commit Task Scheduler support**

```bash
git add internal/schedule
git commit -m "feat: install Windows cleanup plans"
```

## Task 6: Expose schedule lifecycle commands

**Files:**
- Create: `internal/cli/schedule.go`
- Create: `internal/cli/schedule_test.go`
- Modify: `internal/cli/root.go`
- Create: `tests/e2e/schedule_test.go`
- Modify: `README.md`

**Interfaces:**
- Produces: `treeclear schedule install|status|remove|run`
- Preserves plan-only defaults on macOS; Windows integration is deferred.
- Rejects unsupported-platform scheduling without installing, changing, or
  removing jobs. Test the explicit refusal before implementing the commands.

- [ ] **Step 1: Write command and e2e tests**

Tests cover:

```text
treeclear schedule install
treeclear schedule install --apply-safe
treeclear schedule install --job adapter-update
treeclear schedule status --format json
treeclear schedule run --mode plan-only --format json --output <path>
treeclear schedule run --mode apply-safe --format json --output <path>
treeclear schedule run --job adapter-update --format json --output <path>
treeclear schedule remove --job cleanup-plan
treeclear schedule remove --job adapter-update
```

Assert default install is plan-only, adapter update cannot be combined with
cleanup install or `--apply-safe`, unsafe local adapter trust rejects
`--apply-safe`, and reinstall is idempotent.

First-release E2E tests use injected fake launchctl runners; native macOS
smoke jobs exercise test-owned status commands without leaving a job installed.
Windows schtasks integration tests remain with deferred Task 5. Existing
Windows CI instead retains compatibility and unsupported-platform controls.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/cli -run Schedule
go test ./tests/e2e -run Schedule -v
```

Expected: FAIL because CLI schedule wiring does not exist.

- [ ] **Step 3: Implement schedule commands and docs**

`install` prints the exact executable, roots, local schedule time, mode,
report directory, adapter trust policy, and command before confirmation.

`schedule run --mode plan-only` creates and writes one plan and report.
`schedule run --mode apply-safe` creates a plan, selects only candidates
already classified safe, applies that exact plan through the normal apply
engine, and writes plan plus journal to the report. It never treats protected
or review candidates as errors unless their evidence affects a selected safe
candidate.

`schedule run --job adapter-update` invokes the normal signed updater in a
separate process and writes only update results to its report.

For `schedule run`, `--output` is an existing or creatable private report
directory. The report service creates a timestamped atomic JSON file inside
that directory; it never treats the value as a shell-expanded path.

JSON status:

```json
{
  "installed": true,
  "platform": "darwin",
  "job": "cleanup-plan",
  "mode": "plan-only",
  "frequency": "weekly",
  "weekday": "Sunday",
  "time": "03:00",
  "adapterUpdates": false
}
```

README documents how to opt in to safe-only apply and how to remove the
cleanup schedule. It separately documents the opt-in adapter-update job.

- [ ] **Step 4: Run schedule and full tests**

Run:

```bash
gofmt -w internal/cli tests/e2e
go test ./internal/cli -run Schedule
go test ./tests/e2e -run Schedule -v
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit schedule CLI**

```bash
git add internal/cli tests/e2e README.md
git commit -m "feat: manage scheduled cleanup reports"
```

## Task 7: Add native macOS and Windows continuous integration

Keep both existing native jobs and required checks. macOS product acceptance
and Windows compatibility coverage have different support claims; Windows-only
product acceptance is deferred to #15, not reported as passing by this task.
This scope change does not change repository protections or disable a check.

**Files:**
- Create: `.github/workflows/ci.yml`
- Create: `scripts/verify-docs.sh`
- Create: `scripts/verify-docs.ps1`

**Interfaces:**
- Runs format, unit, race, e2e, documentation, and build checks on pull requests.
- Uses full-SHA-pinned GitHub Actions.

- [ ] **Step 1: Add a failing local workflow contract test**

Create `internal/ci/workflow_test.go` that parses `.github/workflows/ci.yml`
as text and asserts:

- `macos-15` and `windows-2025`;
- Go `1.26.5`;
- `go test ./...`;
- `go test -race ./...`;
- `go test ./tests/e2e -v`;
- every `uses:` value ends in a 40-character commit SHA.

- [ ] **Step 2: Run the test and verify it fails**

Run:

```bash
go test ./internal/ci
```

Expected: FAIL because CI does not exist.

- [ ] **Step 3: Implement the CI workflow**

Pin:

```yaml
actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
```

Matrix:

```yaml
strategy:
  fail-fast: false
  matrix:
    os: [macos-15, windows-2025]
```

Steps:

```text
go version
git version
go mod download
go test ./...
go test -race ./...
go test ./tests/e2e -v
go build -trimpath ./cmd/treeclear
```

The documentation scripts verify canonical links, reject workflow-engine
instructions in product plans, reject incomplete-document markers, and check
Agent Skill forbidden commands.

- [ ] **Step 4: Run local workflow contract and repository tests**

Run:

```bash
chmod +x scripts/verify-docs.sh
./scripts/verify-docs.sh
go test ./internal/ci
go test ./...
git diff --check
```

Expected: all commands PASS.

- [ ] **Step 5: Commit CI**

```bash
git add .github/workflows/ci.yml scripts internal/ci
git commit -m "ci: test Treeclear on macOS and Windows"
```

## Task 8: Build, sign, attest, and publish release artifacts

**Files:**
- Create: `.github/workflows/release.yml`
- Create: `scripts/package.sh`
- Deferred to #15: `scripts/package.ps1`
- Create: `docs/release.md`
- Create: `internal/ci/release_test.go`
- Modify: `.gitignore`
- Modify: `README.md`

**Interfaces:**
- Publishes signed macOS amd64/arm64 archives after native acceptance.
- Does not publish Windows archives or advertise Windows production support.
- Publishes `SHA256SUMS`, Cosign bundle, and SPDX JSON SBOM per archive.
- Publishes the signed adapter index from Plan 003.

- [ ] **Step 1: Write release workflow contract tests**

Assert `.github/workflows/release.yml`:

- triggers only on `v*` tags and manual dispatch;
- grants `contents: write`, `id-token: write`, and no broader permissions;
- uses protected `release` environment;
- builds first-release artifacts on macOS runners with only `GOOS=darwin`;
- requires native runtime acceptance for each advertised macOS architecture;
- rejects Windows targets and unexpected archives in first-release publication;
- signs before packaging;
- generates SBOMs and checksums after signing;
- signs checksums with Cosign;
- uploads the signed adapter index;
- pins every action to a full SHA.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/ci -run Release
```

Expected: FAIL because the release workflow does not exist.

- [ ] **Step 3: Implement deterministic native packaging and signing**

Pin these actions:

```yaml
actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1
actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8.0.1
anchore/sbom-action@3ad7283483fc7af8ff2b4ea19663c2d5ca935e26 # v0.24.2
sigstore/cosign-installer@6f9f17788090df1f26f669e9d70d6ae9567deba6 # v4.1.2
```

First-release build jobs use an explicit macOS architecture matrix:

```yaml
jobs:
  macos:
    strategy:
      matrix:
        goarch: [amd64, arm64]
    runs-on: macos-15
    env:
      GOOS: darwin
      GOARCH: ${{ matrix.goarch }}

```

Each matrix entry builds, signs, packages, and uploads exactly one macOS
artifact named `treeclear_<version>_<os>_<arch>`. The final release job refuses
publication unless both expected macOS archives are present and there are no
unsupported-platform archives. This build matrix is not proof of native
execution on both architectures: record native runtime acceptance separately
before publication. Do not claim an unvalidated architecture is supported.

macOS secrets:

```text
MACOS_CERTIFICATE_P12_BASE64
MACOS_CERTIFICATE_PASSWORD
MACOS_SIGNING_IDENTITY
APPLE_NOTARY_KEY_ID
APPLE_NOTARY_ISSUER_ID
APPLE_NOTARY_PRIVATE_KEY
APPLE_TEAM_ID
```

#### Deferred Windows packaging and signing (#15)

Do not add these jobs, actions, or credentials to the first-release workflow.
They are retained for the independently qualified Windows follow-up:

```yaml
azure/login@a641126d1b8aa4d1fa005f4f92df94a3a4c4c906 # v3.1.0
azure/artifact-signing-action@c7ab2a863ab5f9a846ddb8265964877ef296ee82 # v2.0.0
```

```yaml
jobs:
  windows:
    strategy:
      matrix:
        goarch: [amd64, arm64]
    runs-on: windows-2025
    env:
      GOOS: windows
      GOARCH: ${{ matrix.goarch }}
```

Windows uses GitHub OIDC plus:

```text
AZURE_CLIENT_ID
AZURE_TENANT_ID
AZURE_SUBSCRIPTION_ID
```

The workflow logs in with:

```yaml
- uses: azure/login@a641126d1b8aa4d1fa005f4f92df94a3a4c4c906 # v3.1.0
  with:
    client-id: ${{ secrets.AZURE_CLIENT_ID }}
    tenant-id: ${{ secrets.AZURE_TENANT_ID }}
    subscription-id: ${{ secrets.AZURE_SUBSCRIPTION_ID }}
```

and repository variables:

```text
AZURE_ARTIFACT_SIGNING_ENDPOINT
AZURE_ARTIFACT_SIGNING_ACCOUNT
AZURE_ARTIFACT_SIGNING_PROFILE
```

Windows signs `.exe` files with Azure Artifact Signing using SHA-256 and the
Microsoft RFC 3161 timestamp service, then packages with `Compress-Archive`.
The Windows follow-up must verify authorized signing access and native runtime
acceptance for each published architecture; neither is assumed available.

#### First-release adapter metadata and publication

The signed adapter index uses the Plan 003 private key from the protected
secret `TREECLEAR_ADAPTER_SIGNING_KEY_PEM`. The workflow writes it to the
runner's temporary directory with user-only permissions, signs the canonical
index, and removes it in an `always()` cleanup step. The key is never stored in
an artifact or repository checkout.

Before signing, the release job derives a public key and compares it byte for
byte with the committed trust root:

```bash
umask 077
printf '%s' "$TREECLEAR_ADAPTER_SIGNING_KEY_PEM" > "$RUNNER_TEMP/adapter-signing.pem"
openssl pkey -in "$RUNNER_TEMP/adapter-signing.pem" -pubout -out "$RUNNER_TEMP/derived.pub"
cmp "$RUNNER_TEMP/derived.pub" trust/first-party-ed25519.pub
```

Any mismatch fails the release before an adapter index or release asset is
published.

Build with:

```text
go build -trimpath -buildvcs=true -ldflags "-s -w -X github.com/hellices/treeclear/internal/version.Value=<tag>" ./cmd/treeclear
```

macOS signs with hardened runtime and timestamp, packages each architecture
with `ditto`, and submits the zip with `xcrun notarytool --wait`.

After signed artifacts are downloaded, generate SPDX JSON SBOMs with Syft,
write sorted `SHA256SUMS`, sign it keylessly with Cosign, and publish all files
to one GitHub Release.

Add `/dist/` to `.gitignore`; release and snapshot packaging must not leave
generated archives tracked by Git.

- [ ] **Step 4: Run workflow contract and snapshot packaging tests**

Run:

```bash
chmod +x scripts/package.sh
go test ./internal/ci -run Release
go test ./...
./scripts/package.sh snapshot
find dist -maxdepth 1 -type f -print | sort
git diff --check
```

Expected: release contract tests PASS and local unsigned snapshot archives,
checksums, and SBOMs are generated without publishing.

- [ ] **Step 5: Commit the release pipeline**

```bash
git add .github/workflows/release.yml .gitignore scripts docs/release.md internal/ci README.md
git commit -m "ci: publish signed Treeclear releases"
```

## Task 9: Run final acceptance and document the release candidate

**Files:**
- Create: `docs/acceptance-v1.md`
- Modify: `README.md`
- Modify: `docs/README.md`

**Interfaces:**
- Verifies all architecture MVP completion criteria.
- Produces one auditable release-candidate checklist.

- [ ] **Step 1: Write the acceptance matrix**

The matrix has one row for each architecture completion criterion and columns:

```text
Criterion | Automated test | macOS evidence | Windows follow-up (#15) | Status
```

Windows entries are explicitly deferred, not passed or used as first-release
acceptance. The macOS columns must contain actual native evidence, including
unsupported-platform mutation refusal and each published architecture.

It includes:

- protected/unknown never removed;
- TOCTOU invalidation;
- five-provider capability report;
- safe stale removal;
- byte-verified restore;
- launchd lifecycle;
- Task Scheduler lifecycle (deferred to #15);
- adapter patch without core rebuild;
- corrupt adapter rollback;
- custom adapter validation and pinning;
- no adapter mutation capability;
- offline apply;
- signed artifacts, checksums, and SBOM.

- [ ] **Step 2: Run the complete verification suite**

Run:

```bash
go test ./...
go test -race ./...
go test ./tests/e2e -v
go build -trimpath ./cmd/treeclear
./scripts/verify-docs.sh
./scripts/package.sh snapshot
git diff --check
git status --short
```

Expected: every command exits 0; only intentional acceptance-document changes
remain before the commit.

- [ ] **Step 3: Complete acceptance evidence and documentation links**

Record command versions and summarized outputs in `docs/acceptance-v1.md`.
Update README with links to:

- architecture;
- CLI, plan, and adapter specs;
- security policy;
- contributing guide;
- Agent Skill;
- installation and scheduling;
- release verification.

Do not copy architecture or plan content into README.

- [ ] **Step 4: Review the release candidate diff**

Run:

```bash
git diff --check
git diff --stat
git status --short
```

Expected: only README, documentation index, and acceptance evidence are
changed.

- [ ] **Step 5: Commit release acceptance**

```bash
git add README.md docs/README.md docs/acceptance-v1.md
git commit -m "docs: record Treeclear v1 acceptance"
```

## Plan 4 Completion Gate

Keep the existing native macOS and Windows CI checks passing. Windows results
are compatibility evidence; macOS product acceptance and supported mutation
E2E results must come from native macOS execution. Run the applicable suite:

```text
go test ./...
go test -race ./...
go test ./tests/e2e -v
go build -trimpath ./cmd/treeclear
```

Run from a protected release environment:

```text
tag v0.1.0
verify signed macOS archives
verify SHA256SUMS and Cosign bundle
verify SPDX JSON SBOMs
verify signed adapters-index.json
```

Expected:

- canonical documentation is tool-neutral and committed under standard paths;
- the Agent Skill installs for all five supported hosts;
- launchd defaults to weekly plan-only operation; Task Scheduler remains
  deferred to #15;
- update and cleanup schedules remain separate;
- all actions are SHA-pinned;
- release binaries are signed;
- only qualified macOS artifacts are published; no Windows release claim is made;
- checksums, Cosign evidence, SBOMs, and adapter metadata are published;
- the repository and release satisfy every accepted MVP criterion.
