# Treeclear Safety Core Implementation Plan

- Status: In progress — Task 7A fingerprint foundation
- Sequence: 001 of 004
- Source architecture: [Treeclear Architecture](../architecture/2026-09-12-treeclear.md)
- Depends on: [000 Minimal Development Baseline](000-development-harness.md)

Stage 000 supplies the module, development commands, and shared test fixtures.
Reuse those files; Task 1 still owns the product CLI and dependencies, and
Task 4 still owns inventory integration coverage. No product task is complete
merely because the harness is available.

> Execute this plan task-by-task using an isolated Git worktree, test-driven development, and a review checkpoint after every task. Steps use checkbox (`- [ ]`) syntax for tracking.

PRs #1 and #2 are merged: the standard development baseline, Tasks 1–6, and
the read-only `scan` command brought forward from Task 7 are delivered.
The current Task 7A slice adds pure candidate fingerprints and policy digests.
Task 7 plan building, private persistence, integrity, and `plan`/`explain`
commands, plus Tasks 8–11 cleanup and recovery, remain pending. This split
keeps the identity contract independently reviewable before introducing
private storage or signed plans. Agent adapters and later plans remain
unimplemented; no mutation command is exposed.

**Goal:** Build a working macOS and Windows Treeclear CLI that discovers Git worktrees, correlates process activity, classifies candidates, writes expiring plans, safely removes approved worktrees, and restores them from verified local snapshots.

**Architecture:** A Go CLI delegates all operating-system and Git reads to narrow collectors, converts them into immutable domain values, and evaluates a pure fail-closed policy. Apply reloads the exact plan, re-collects every precondition, snapshots every pending target, and only then performs serial `git worktree remove` operations with a durable journal.

**Tech Stack:** Go 1.26.0 with toolchain 1.26.5, Cobra 1.10.2, go-toml/v2 2.4.3, gopsutil/v4 4.26.8, x/sys 0.48.0, go-cmp 0.7.0, Git 2.36 or newer, standard-library tar/gzip and crypto packages.

## Global Constraints

- Module path: `github.com/hellices/treeclear`.
- License: Apache-2.0 using the canonical Apache Software Foundation license text.
- Supported MVP operating systems: macOS and Windows; keep platform interfaces open for Linux.
- Default inactivity threshold: exactly 7 days.
- Default plan expiry: exactly 15 minutes.
- Running `treeclear` without arguments is read-only.
- Unknown, inaccessible, malformed, or conflicting evidence must block the affected candidate.
- Never invoke `git worktree remove --force` in the default cleanup path.
- Never remove the primary worktree.
- Preserve local branches during ordinary worktree cleanup.
- Agent-session archive and deletion are outside this MVP.
- Apply is offline and must not download dependencies, adapters, or updates.
- Apply uses the exact policy, adapter bundle, executable, argv, canonical cwd,
  and allowed environment identities recorded by the plan.
- All pending candidates must pass preflight and snapshot verification before the first removal.
- Snapshots remain local and private; Treeclear has no telemetry.

---

## File Structure

| Path | Responsibility |
|---|---|
| `go.mod`, `go.sum` | Reproducible Go module and pinned dependencies |
| `Makefile` | Existing-tool build and test entry points |
| `LICENSE` | Canonical Apache-2.0 license |
| `cmd/treeclear/main.go` | Process entry point only |
| `internal/cli/root.go` | Cobra root wiring and exit-code translation |
| `internal/cli/scan.go` | Read-only inventory output |
| `internal/cli/plan.go` | Plan creation and output |
| `internal/cli/apply.go` | Apply command wiring |
| `internal/cli/restore.go` | Restore and trash command wiring |
| `internal/config/config.go` | Typed defaults and merged configuration |
| `internal/config/duration.go` | TOML duration parsing |
| `internal/domain/worktree.go` | Git worktree value types |
| `internal/domain/evidence.go` | Process and future agent evidence types |
| `internal/domain/decision.go` | Classification, reason, and policy types |
| `internal/domain/plan.go` | Stable plan, candidate, snapshot, and journal schemas |
| `internal/execx/runner.go` | Shell-free bounded command execution |
| `internal/git/client.go` | Git command facade |
| `internal/git/worktree_porcelain.go` | NUL-safe worktree parser |
| `internal/git/status_porcelain.go` | NUL-safe status parser |
| `internal/discovery/repositories.go` | Root scanning and common-dir deduplication |
| `internal/inventory/load.go` | Fully enriched worktree inventory |
| `internal/pathutil/path.go` | Canonical containment API |
| `internal/pathutil/path_unix.go` | macOS path identity |
| `internal/pathutil/path_windows.go` | Windows path, drive, UNC, and case identity |
| `internal/process/source.go` | Injectable process-source contract |
| `internal/process/gopsutil.go` | gopsutil-backed process source |
| `internal/process/collector.go` | Process snapshot and worktree association |
| `internal/correlate/evidence.go` | Evidence grouping by canonical worktree |
| `internal/policy/evaluate.go` | Pure classification decision table |
| `internal/plan/build.go` | Plan construction |
| `internal/plan/fingerprint.go` | Candidate and policy hashes |
| `internal/plan/integrity.go` | Local HMAC key and plan integrity |
| `internal/plan/store.go` | Atomic private plan persistence |
| `internal/fssecure/private_unix.go` | Private Unix directory and file permissions |
| `internal/fssecure/private_windows.go` | Protected Windows ACLs |
| `internal/snapshot/manifest.go` | Snapshot schema and integrity hashes |
| `internal/snapshot/create.go` | Git metadata, patches, and untracked archive |
| `internal/snapshot/restore.go` | Safe worktree restoration |
| `internal/apply/engine.go` | Whole-plan preflight, snapshot, removal, and resume |
| `internal/apply/environment.go` | Current policy, adapter lock, and executable verifier |
| `internal/apply/journal.go` | Durable apply state machine |
| `internal/statelock/lock.go` | Cross-process lock API and shared metadata |
| `internal/statelock/lock_unix.go` | macOS `flock` implementation |
| `internal/statelock/lock_windows.go` | Windows `LockFileEx` implementation |
| `internal/trash/store.go` | Snapshot discovery and explicit pruning |
| `internal/testutil/repo.go` | Real temporary Git repository fixtures |
| `tests/e2e/core_test.go` | End-to-end CLI safety and restore behavior |
| `README.md` | Product summary and core-only quick start |

## Task 1: Bootstrap the Go CLI and repository contract

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `LICENSE`
- Create: `cmd/treeclear/main.go`
- Create: `internal/cli/root.go`
- Create: `internal/cli/root_test.go`
- Create: `internal/version/version.go`

**Interfaces:**
- Produces: `cli.Execute(ctx context.Context, args []string, stdout, stderr io.Writer, buildVersion string) int`
- Produces: `cli.NewRootCommand(deps cli.Dependencies) *cobra.Command`
- Produces: `version.Value string`

- [ ] **Step 1: Write the failing root-command tests**

```go
package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestExecuteWithoutArgumentsIsReadOnly(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := Execute(context.Background(), nil, &stdout, &stderr, "test")

	if code != 0 {
		t.Fatalf("Execute() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Safely clear stale agent worktrees") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "removed") {
		t.Fatalf("default command reported a mutation: %q", stdout.String())
	}
}

func TestExecuteVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := Execute(context.Background(), []string{"version"}, &stdout, &stderr, "v0.0.0-test")

	if code != 0 {
		t.Fatalf("Execute() code = %d, stderr = %q", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "v0.0.0-test" {
		t.Fatalf("version output = %q", got)
	}
}
```

- [ ] **Step 2: Run the tests and verify they fail**

Run:

```bash
go test ./internal/cli
```

Expected: FAIL because the Go module and `Execute` do not exist.

- [ ] **Step 3: Create the module, license, CLI, and build targets**

Create `go.mod`:

```go
module github.com/hellices/treeclear

go 1.26.0

toolchain go1.26.5

require (
	github.com/google/go-cmp v0.7.0
	github.com/pelletier/go-toml/v2 v2.4.3
	github.com/shirou/gopsutil/v4 v4.26.8
	github.com/spf13/cobra v1.10.2
	golang.org/x/sys v0.48.0
)
```

Create `internal/version/version.go`:

```go
package version

var Value = "dev"
```

Create `internal/cli/root.go`:

```go
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

type Dependencies struct {
	Stdout      io.Writer
	Stderr      io.Writer
	BuildVersion string
}

func NewRootCommand(deps Dependencies) *cobra.Command {
	root := &cobra.Command{
		Use:           "treeclear",
		Short:         "Safely clear stale agent worktrees",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Safely clear stale agent worktrees. Run `treeclear scan` to inspect candidates.")
			return err
		},
	}
	root.SetOut(deps.Stdout)
	root.SetErr(deps.Stderr)
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the Treeclear version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), deps.BuildVersion)
			return err
		},
	})
	return root
}

func Execute(ctx context.Context, args []string, stdout, stderr io.Writer, buildVersion string) int {
	cmd := NewRootCommand(Dependencies{
		Stdout:       stdout,
		Stderr:       stderr,
		BuildVersion: buildVersion,
	})
	cmd.SetArgs(args)
	if err := cmd.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
```

Create `cmd/treeclear/main.go`:

```go
package main

import (
	"context"
	"os"

	"github.com/hellices/treeclear/internal/cli"
	"github.com/hellices/treeclear/internal/version"
)

func main() {
	os.Exit(cli.Execute(context.Background(), os.Args[1:], os.Stdout, os.Stderr, version.Value))
}
```

Create `Makefile`:

```make
.PHONY: build test test-race

build:
	go build ./cmd/treeclear

test:
	go test ./...

test-race:
	go test -race ./...
```

Create the canonical license:

```bash
curl --fail --location https://www.apache.org/licenses/LICENSE-2.0.txt -o LICENSE
go mod tidy
```

- [ ] **Step 4: Run the targeted and repository tests**

Run:

```bash
go test ./internal/cli
go test ./...
go build ./cmd/treeclear
```

Expected: all tests PASS and the binary builds.

- [ ] **Step 5: Commit the bootstrap**

```bash
git add go.mod go.sum Makefile LICENSE cmd/treeclear internal/cli internal/version
git commit -m "chore: bootstrap Treeclear CLI"
```

## Task 2: Define stable domain and configuration types

**Files:**
- Create: `internal/config/duration.go`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `internal/domain/worktree.go`
- Create: `internal/domain/evidence.go`
- Create: `internal/domain/decision.go`
- Create: `internal/domain/plan.go`
- Test: `internal/domain/types_test.go`

**Interfaces:**
- Produces: `config.Default() config.Config`
- Produces: `config.Load(userPath, repositoryPath string, overrides config.Overrides) (config.Config, error)`
- Produces: `domain.Worktree`, `domain.EvidenceSet`, `domain.Decision`, `domain.Plan`, and `domain.ApplyJournal`
- Constraint: JSON field names introduced here are stable schema fields used by all later plans.

- [ ] **Step 1: Write tests for defaults, duration parsing, and stable JSON names**

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultSafetyValues(t *testing.T) {
	got := Default()
	if got.InactivityThreshold != 7*24*time.Hour {
		t.Fatalf("InactivityThreshold = %s", got.InactivityThreshold)
	}
	if got.PlanExpiry != 15*time.Minute {
		t.Fatalf("PlanExpiry = %s", got.PlanExpiry)
	}
	if got.SnapshotMaxBytes != 64<<20 {
		t.Fatalf("SnapshotMaxBytes = %d", got.SnapshotMaxBytes)
	}
}

func TestLoadOverridesRepositoryAndUserConfig(t *testing.T) {
	user := writeConfig(t, `inactivity_threshold = "30d"`)
	repo := writeConfig(t, `inactivity_threshold = "14d"`)

	got, err := Load(user, repo, Overrides{InactivityThreshold: durationPtr(7 * 24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if got.InactivityThreshold != 7*24*time.Hour {
		t.Fatalf("InactivityThreshold = %s", got.InactivityThreshold)
	}
}

func durationPtr(value time.Duration) *time.Duration {
	return &value
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "treeclear.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
```

Add `internal/domain/types_test.go`:

```go
package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPlanJSONUsesStableSchemaNames(t *testing.T) {
	data, err := json.Marshal(Plan{SchemaVersion: 1, ID: "plan_test"})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, name := range []string{`"schemaVersion":1`, `"planId":"plan_test"`} {
		if !strings.Contains(got, name) {
			t.Fatalf("plan JSON %s does not contain %s", got, name)
		}
	}
}
```

- [ ] **Step 2: Run the tests and verify they fail**

Run:

```bash
go test ./internal/config ./internal/domain
```

Expected: FAIL because the packages and types do not exist.

- [ ] **Step 3: Implement duration, configuration, and domain schemas**

Use this exact public shape in `internal/config/config.go`:

```go
package config

import (
	"errors"
	"os"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Roots               []string
	InactivityThreshold time.Duration
	PlanExpiry           time.Duration
	BaseBranches        []string
	MinimumTrustGrade   string
	SnapshotMaxBytes     int64
}

type Overrides struct {
	Roots               []string
	InactivityThreshold *time.Duration
	PlanExpiry           *time.Duration
	BaseBranches        []string
	MinimumTrustGrade   *string
	SnapshotMaxBytes     *int64
}

type fileConfig struct {
	Roots               []string  `toml:"roots"`
	InactivityThreshold *Duration `toml:"inactivity_threshold"`
	PlanExpiry           *Duration `toml:"plan_expiry"`
	BaseBranches        []string  `toml:"base_branches"`
	MinimumTrustGrade   *string   `toml:"minimum_trust_grade"`
	SnapshotMaxBytes     *int64    `toml:"snapshot_max_bytes"`
}

func Default() Config {
	return Config{
		InactivityThreshold: 7 * 24 * time.Hour,
		PlanExpiry:           15 * time.Minute,
		BaseBranches:        []string{"main", "master"},
		MinimumTrustGrade:   "versioned-private",
		SnapshotMaxBytes:     64 << 20,
	}
}

func Load(userPath, repositoryPath string, overrides Overrides) (Config, error) {
	cfg := Default()
	for _, path := range []string{userPath, repositoryPath} {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Config{}, err
		}
		var file fileConfig
		if err := toml.Unmarshal(data, &file); err != nil {
			return Config{}, err
		}
		applyFile(&cfg, file)
	}
	if len(overrides.Roots) > 0 {
		cfg.Roots = append([]string(nil), overrides.Roots...)
	}
	if overrides.InactivityThreshold != nil {
		cfg.InactivityThreshold = *overrides.InactivityThreshold
	}
	if overrides.PlanExpiry != nil {
		cfg.PlanExpiry = *overrides.PlanExpiry
	}
	if overrides.BaseBranches != nil {
		cfg.BaseBranches = append([]string(nil), overrides.BaseBranches...)
	}
	if overrides.MinimumTrustGrade != nil {
		cfg.MinimumTrustGrade = *overrides.MinimumTrustGrade
	}
	if overrides.SnapshotMaxBytes != nil {
		cfg.SnapshotMaxBytes = *overrides.SnapshotMaxBytes
	}
	if cfg.InactivityThreshold <= 0 || cfg.PlanExpiry <= 0 || cfg.SnapshotMaxBytes <= 0 {
		return Config{}, errors.New("durations and snapshot_max_bytes must be positive")
	}
	return cfg, nil
}

func applyFile(cfg *Config, file fileConfig) {
	if file.Roots != nil {
		cfg.Roots = append([]string(nil), file.Roots...)
	}
	if file.InactivityThreshold != nil {
		cfg.InactivityThreshold = file.InactivityThreshold.Duration
	}
	if file.PlanExpiry != nil {
		cfg.PlanExpiry = file.PlanExpiry.Duration
	}
	if file.BaseBranches != nil {
		cfg.BaseBranches = append([]string(nil), file.BaseBranches...)
	}
	if file.MinimumTrustGrade != nil {
		cfg.MinimumTrustGrade = *file.MinimumTrustGrade
	}
	if file.SnapshotMaxBytes != nil {
		cfg.SnapshotMaxBytes = *file.SnapshotMaxBytes
	}
}
```

Implement `Duration.UnmarshalText` so `30m`, `24h`, and whole-day suffixes such as
`7d` parse deterministically:

```go
package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalText(text []byte) error {
	value := string(text)
	if strings.HasSuffix(value, "d") {
		days, err := strconv.ParseInt(strings.TrimSuffix(value, "d"), 10, 64)
		if err != nil || days <= 0 {
			return fmt.Errorf("invalid day duration %q", value)
		}
		d.Duration = time.Duration(days) * 24 * time.Hour
		return nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value, err)
	}
	d.Duration = parsed
	return nil
}
```

Define these domain contracts without adding provider-specific fields:

```go
package domain

import "time"

type GitStatus struct {
	Staged    int `json:"staged"`
	Unstaged  int `json:"unstaged"`
	Unmerged  int `json:"unmerged"`
	Untracked int `json:"untracked"`
}

func (s GitStatus) Clean() bool {
	return s.Staged+s.Unstaged+s.Unmerged+s.Untracked == 0
}

type Worktree struct {
	Path               string    `json:"path"`
	RepositoryRoot     string    `json:"repositoryRoot"`
	CommonGitDir       string    `json:"commonGitDir"`
	AdminDir           string    `json:"adminDir"`
	Head               string    `json:"head"`
	Branch             string    `json:"branch,omitempty"`
	Upstream           string    `json:"upstream,omitempty"`
	Primary            bool      `json:"primary"`
	Current            bool      `json:"current"`
	PathSafe           bool      `json:"pathSafe"`
	Detached           bool      `json:"detached"`
	Locked             bool      `json:"locked"`
	LockReason         string    `json:"lockReason,omitempty"`
	Prunable           bool      `json:"prunable"`
	GitStateKnown      bool      `json:"gitStateKnown"`
	CollectionErrors   []string  `json:"collectionErrors,omitempty"`
	Status             GitStatus `json:"status"`
	Recoverable        bool      `json:"recoverable"`
	LastCommitAt       time.Time `json:"lastCommitAt"`
	MetadataModifiedAt time.Time `json:"metadataModifiedAt"`
	EstimatedBytes     int64     `json:"estimatedBytes"`
	IndexHash          string    `json:"indexHash"`
	AdminHash          string    `json:"adminHash"`
}
```

```go
package domain

import "time"

type EvidenceState string
type TrustGrade string

const (
	EvidenceActive        EvidenceState = "active"
	EvidenceIdle          EvidenceState = "idle"
	EvidenceCompleted     EvidenceState = "completed"
	EvidenceArchived      EvidenceState = "archived"
	EvidenceInactive      EvidenceState = "inactive"
	EvidenceUnknown       EvidenceState = "unknown"
	EvidenceNotApplicable EvidenceState = "not-applicable"
)

const (
	TrustSupportedAPI       TrustGrade = "supported-api"
	TrustSupportedAppServer TrustGrade = "supported-app-server"
	TrustSupportedCLI       TrustGrade = "supported-cli"
	TrustExperimentalAPI    TrustGrade = "experimental-api"
	TrustVersionedPrivate   TrustGrade = "versioned-private"
	TrustUnversionedPrivate TrustGrade = "unversioned-private"
	TrustProcessOnly        TrustGrade = "process-only"
)

type ProcessEvidence struct {
	PID         int32     `json:"pid"`
	CreatedAt   time.Time `json:"createdAt"`
	Executable string    `json:"executable"`
	CWD         string    `json:"cwd,omitempty"`
	State       EvidenceState `json:"state"`
	Error       string    `json:"error,omitempty"`
	Fingerprint string    `json:"fingerprint"`
}

type ProcessReference struct {
	PID         int32     `json:"pid"`
	CreatedAt   time.Time `json:"createdAt"`
	Executable string    `json:"executable"`
	Fingerprint string    `json:"fingerprint"`
}

type ExecutableIdentity struct {
	Path              string   `json:"path"`
	Version           string   `json:"version"`
	SHA256            string   `json:"sha256"`
	Arguments         []string `json:"arguments"`
	WorkingDirectory  string   `json:"workingDirectory"`
	EnvironmentDigest string   `json:"environmentDigest"`
	InvocationDigest  string   `json:"invocationDigest"`
}

type WorktreeBinding struct {
	Kind       string `json:"kind"`
	Identifier string `json:"identifier"`
	Version    string `json:"version,omitempty"`
}

type AgentEvidence struct {
	AdapterID        string             `json:"adapterId"`
	AdapterVersion   string             `json:"adapterVersion"`
	BundleDigest     string             `json:"bundleDigest"`
	Provider         string             `json:"provider"`
	SourceID         string             `json:"sourceId"`
	SessionID        string             `json:"sessionId,omitempty"`
	ThreadID         string             `json:"threadId,omitempty"`
	ProjectID        string             `json:"projectId,omitempty"`
	CWD              string             `json:"cwd,omitempty"`
	RepositoryRoot   string             `json:"repositoryRoot,omitempty"`
	WorktreePath     string             `json:"worktreePath,omitempty"`
	State            EvidenceState      `json:"state"`
	CreatedAt        time.Time          `json:"createdAt,omitempty"`
	UpdatedAt        time.Time          `json:"updatedAt,omitempty"`
	ObservedAt       time.Time          `json:"observedAt"`
	ProcessRefs      []ProcessReference `json:"processRefs,omitempty"`
	Binding          *WorktreeBinding   `json:"binding,omitempty"`
	SourceKind       string             `json:"sourceKind"`
	SupportGrade     TrustGrade         `json:"supportGrade"`
	Confidence       string             `json:"confidence"`
	SchemaVersion    string             `json:"schemaVersion,omitempty"`
	RawFingerprint   string             `json:"rawFingerprint"`
	TrustRecordDigest string            `json:"trustRecordDigest,omitempty"`
	Executable        *ExecutableIdentity `json:"executable,omitempty"`
	RevalidationMode  string             `json:"revalidationMode"`
	Warnings         []string           `json:"warnings,omitempty"`
}

type EvidenceSet struct {
	Processes []ProcessEvidence `json:"processes"`
	Agents    []AgentEvidence   `json:"agents"`
	Adapters  []AdapterStatus   `json:"adapters,omitempty"`
	Warnings  []string          `json:"warnings,omitempty"`
}

type AdapterStatus struct {
	AdapterID   string     `json:"adapterId"`
	Applicable  bool       `json:"applicable"`
	Healthy     bool       `json:"healthy"`
	Trusted     bool       `json:"trusted"`
	BestGrade   TrustGrade `json:"bestGrade,omitempty"`
	OfflineRevalidatable bool `json:"offlineRevalidatable"`
	Error       string     `json:"error,omitempty"`
}
```

Define classification and plan structs with the stable fields tested above:

```go
package domain

import "time"

type Classification string

const (
	Protected Classification = "protected"
	Review    Classification = "review"
	Safe      Classification = "safe"
)

type Reason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Decision struct {
	Classification Classification `json:"classification"`
	Reasons        []Reason       `json:"reasons"`
	InactiveFor    time.Duration  `json:"inactiveFor"`
}

type Policy struct {
	Now      time.Time      `json:"now"`
	Settings PolicySettings `json:"settings"`
}

type PolicySettings struct {
	InactivityThreshold time.Duration `json:"inactivityThreshold"`
	PlanExpiry          time.Duration `json:"planExpiry"`
	BaseBranches        []string      `json:"baseBranches"`
	MinimumTrustGrade   TrustGrade    `json:"minimumTrustGrade"`
	SnapshotMaxBytes    int64         `json:"snapshotMaxBytes"`
}
```

```go
package domain

import "time"

type Plan struct {
	SchemaVersion     int         `json:"schemaVersion"`
	ID                string      `json:"planId"`
	GeneratedAt       time.Time   `json:"generatedAt"`
	ExpiresAt         time.Time   `json:"expiresAt"`
	ToolVersion       string      `json:"toolVersion"`
	IntendedApplyMode ApplyMode   `json:"intendedApplyMode"`
	PolicyDigest      string      `json:"policyDigest"`
	AdapterLockDigest string      `json:"adapterLockDigest"`
	ExecutableIdentities []ExecutableIdentity `json:"executableIdentities,omitempty"`
	Scope             PlanScope   `json:"scope"`
	Candidates        []Candidate `json:"candidates"`
	Summary           PlanSummary `json:"summary"`
	Warnings          []string    `json:"warnings,omitempty"`
	Integrity         PlanIntegrity `json:"integrity"`
}

type ApplyMode string

const (
	ApplyInteractive ApplyMode = "interactive"
	ApplyScheduled   ApplyMode = "scheduled"
)

type PlanIntegrity struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	MAC       string `json:"mac"`
}

type PlanScope struct {
	Roots []string `json:"roots"`
}

type Candidate struct {
	ID            string       `json:"candidateId"`
	Worktree      Worktree     `json:"worktree"`
	Evidence      EvidenceSet  `json:"evidence"`
	Decision      Decision     `json:"decision"`
	Action        string       `json:"action"`
	Fingerprint   string       `json:"fingerprint"`
	Snapshot      SnapshotPlan `json:"snapshot"`
}

type SnapshotPlan struct {
	Required         bool  `json:"required"`
	MaximumBytes     int64 `json:"maximumBytes"`
	UntrackedFiles   int   `json:"untrackedFiles"`
	UntrackedBytes   int64 `json:"untrackedBytes"`
	SensitiveBlocked bool  `json:"sensitiveBlocked"`
}

type PlanSummary struct {
	Safe             int   `json:"safe"`
	Review           int   `json:"review"`
	Protected        int   `json:"protected"`
	ReclaimableBytes int64 `json:"reclaimableBytes"`
}

type ApplyJournal struct {
	SchemaVersion int            `json:"schemaVersion"`
	PlanID        string         `json:"planId"`
	StartedAt     time.Time      `json:"startedAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
	State         string         `json:"state"`
	Entries       []JournalEntry `json:"entries"`
}

type JournalEntry struct {
	CandidateID string `json:"candidateId"`
	State       string `json:"state"`
	SnapshotID  string `json:"snapshotId,omitempty"`
	Error       string `json:"error,omitempty"`
}
```

- [ ] **Step 4: Run format and tests**

Run:

```bash
gofmt -w internal/config internal/domain
go test ./internal/config ./internal/domain
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit the stable schemas**

```bash
git add internal/config internal/domain
git commit -m "feat: define core safety schemas"
```

## Task 3: Add bounded command execution and NUL-safe Git parsing

**Files:**
- Create: `internal/execx/runner.go`
- Create: `internal/execx/runner_test.go`
- Create: `internal/git/worktree_porcelain.go`
- Create: `internal/git/worktree_porcelain_test.go`
- Create: `internal/git/status_porcelain.go`
- Create: `internal/git/status_porcelain_test.go`
- Create: `internal/git/client.go`
- Create: `internal/git/client_test.go`

**Interfaces:**
- Produces: `execx.Runner.Run(ctx context.Context, request execx.Request) (execx.Result, error)`
- Produces: `git.Client.ListWorktrees(ctx, repository string) ([]domain.Worktree, error)`
- Produces: `git.Client.Status(ctx, worktree string) (domain.GitStatus, error)`
- Produces: `git.Client.RemoveWorktree(ctx, repository, path string) error`

- [ ] **Step 1: Write parser and no-shell tests**

Use a NUL-delimited fixture that includes spaces and a locked reason:

```go
func TestParseWorktreePorcelainZ(t *testing.T) {
	input := []byte("worktree /tmp/main repo\x00HEAD abc123\x00branch refs/heads/main\x00\x00" +
		"worktree /tmp/feature\nname\x00HEAD def456\x00branch refs/heads/feature\x00locked agent active\x00\x00")

	got, err := parseWorktreePorcelainZ(input)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]rawWorktree{
		{Path: "/tmp/main repo", Head: "abc123", Branch: "main"},
		{Path: "/tmp/feature\nname", Head: "def456", Branch: "feature", Locked: true, LockReason: "agent active"},
	}, got); diff != "" {
		t.Fatalf("worktrees mismatch (-want +got):\n%s", diff)
	}
}
```

Test status record prefixes `1`, `2`, `u`, `?`, and `!`, and test that
`execx.OSRunner` receives an executable plus argv rather than a shell string.
Configure a fixture repository with a `diff.external` command and a textconv
driver that create marker files. Assert snapshot diff commands include
`--no-ext-diff --no-textconv` and neither marker is created.

- [ ] **Step 2: Run the tests and verify they fail**

Run:

```bash
go test ./internal/execx ./internal/git
```

Expected: FAIL because the runner and parsers do not exist.

- [ ] **Step 3: Implement the runner and Git facade**

Use these runner contracts:

```go
package execx

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

type Request struct {
	Directory string
	Name      string
	Args      []string
	Env       []string
	Timeout   time.Duration
	MaxBytes  int
}

type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

type Runner interface {
	Run(context.Context, Request) (Result, error)
}

type OSRunner struct{}

func (OSRunner) Run(ctx context.Context, req Request) (Result, error) {
	if req.Name == "" || req.MaxBytes <= 0 || req.Timeout <= 0 {
		return Result{}, errors.New("name, timeout, and max bytes are required")
	}
	runCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, req.Name, req.Args...)
	cmd.Dir = req.Directory
	if req.Env != nil {
		cmd.Env = req.Env
	}
	var stdout, stderr limitedBuffer
	stdout.limit = req.MaxBytes
	stderr.limit = req.MaxBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return result, runCtx.Err()
	}
	if stdout.exceeded || stderr.exceeded {
		return result, errors.New("command output limit exceeded")
	}
	return result, err
}

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		b.exceeded = true
		remaining := b.limit - b.Len()
		if remaining > 0 {
			_, _ = b.Buffer.Write(p[:remaining])
		}
		return len(p), nil
	}
	return b.Buffer.Write(p)
}
```

`git.Client` must always use a sanitized environment that preserves the
minimum platform variables required to launch Git:

```go
execx.Request{
	Directory: repository,
	Name:      "git",
	Args:      args,
	Env: execx.SanitizedEnvironment(os.Environ(), map[string]string{
		"LC_ALL": "C",
		"LANG":   "C",
	}),
	Timeout:   30 * time.Second,
	MaxBytes:  16 << 20,
}
```

`SanitizedEnvironment` keeps only `PATH`, `HOME`, `USERPROFILE`,
`SYSTEMROOT`, `TMPDIR`, `TEMP`, `TMP`, and the explicit overrides. It sorts
keys before returning `KEY=value` entries.

`RemoveWorktree` must construct exactly:

```go
[]string{"worktree", "remove", "--", path}
```

It must not accept or append `--force`.

At startup, reject Git versions older than 2.36 because Treeclear requires the
stable NUL-delimited `git worktree list --porcelain -z` format.

- [ ] **Step 4: Run parser, runner, and full tests**

Run:

```bash
gofmt -w internal/execx internal/git
go test ./internal/execx ./internal/git
go test ./...
```

Expected: all tests PASS, including paths containing spaces and newlines.

- [ ] **Step 5: Commit the Git boundary**

```bash
git add internal/execx internal/git
git commit -m "feat: add bounded Git collection"
```

## Task 4: Discover repositories and build enriched inventory

**Files:**
- Create: `internal/discovery/repositories.go`
- Create: `internal/discovery/repositories_test.go`
- Create: `internal/inventory/load.go`
- Create: `internal/inventory/load_test.go`
- Create: `internal/testutil/repo.go`

**Interfaces:**
- Consumes: `git.Client`
- Produces: `discovery.Find(ctx context.Context, roots []string) ([]discovery.Repository, []error)`
- Produces: `inventory.Loader.Load(ctx context.Context, roots []string) ([]domain.Worktree, []error)`
- Produces: real Git fixtures in `testutil.Repository`

- [ ] **Step 1: Write real-repository integration tests**

Create a fixture helper that initializes a primary repository, configures a
test identity, creates one commit, and adds linked worktrees. Write tests that
assert:

```go
func TestLoaderDeduplicatesCommonGitDirectories(t *testing.T) {
	repo := testutil.NewRepository(t)
	feature := repo.AddWorktree(t, "feature", "feature/one")

	got, errs := newTestLoader(t).Load(context.Background(), []string{repo.Root, feature})

	if len(errs) != 0 {
		t.Fatalf("Load() errors = %v", errs)
	}
	if len(got) != 2 {
		t.Fatalf("worktree count = %d, want 2", len(got))
	}
	if !got[0].Primary && !got[1].Primary {
		t.Fatal("inventory has no primary worktree")
	}
}
```

Add cases for a nested repository, a `.git` file, `node_modules`, `.cache`,
`target`, an unreadable directory, a locked worktree, and a detached worktree.
Add a case where Treeclear cwd is `<linked-worktree>/subdirectory` and assert
the linked worktree has `Current=true`.

- [ ] **Step 2: Run the tests and verify they fail**

Run:

```bash
go test ./internal/discovery ./internal/inventory
```

Expected: FAIL because discovery and inventory do not exist.

- [ ] **Step 3: Implement discovery and enrichment**

Define:

```go
type Repository struct {
	Root         string
	CommonGitDir string
}

type GitClient interface {
	CommonGitDir(context.Context, string) (string, error)
	ListWorktrees(context.Context, string) ([]domain.Worktree, error)
	InspectWorktree(context.Context, string, domain.Worktree) (domain.Worktree, error)
}
```

Discovery must:

- canonicalize roots before walking;
- skip `.git`, `node_modules`, `.cache`, `target`, and Treeclear's data
  directory;
- recognize only primary repositories as discovery anchors;
- deduplicate by canonical common Git directory;
- return typed per-path errors rather than dropping them.

Inventory enrichment must calculate:

- primary status by comparing canonical paths;
- current status by calling `pathutil.Contains(worktree.Path, treeclearCWD)`,
  so both the worktree root and every descendant cwd set `Current=true`;
- path safety, which is false when canonicalization, symlink, junction, or
  repository-overlap validation fails;
- Git status;
- upstream and remote recoverability;
- HEAD, index, and admin SHA-256 hashes;
- last commit time;
- administrative metadata modification time;
- total reclaimable bytes by walking the worktree without following symlinks.

`git.Client.InspectWorktree` owns the Git commands for status, upstream,
recoverability, HEAD, index/admin hashes, commit time, and metadata time. The
inventory loader never fabricates those fields independently.

An enrichment failure never leaves a zero-value worktree looking clean.
`GitStateKnown` starts false and becomes true only after status, HEAD, index,
admin metadata, recoverability, and timestamps are collected successfully.
Every failed field is recorded in `CollectionErrors`. Policy protects any
worktree where `GitStateKnown` is false.

Limit concurrent repository enrichment with a worker pool of
`min(runtime.GOMAXPROCS(0), 8)`.

- [ ] **Step 4: Run integration and race tests**

Run:

```bash
gofmt -w internal/discovery internal/inventory internal/testutil
go test ./internal/discovery ./internal/inventory
go test -race ./internal/discovery ./internal/inventory
go test ./...
```

Expected: all tests PASS with no race reports.

- [ ] **Step 5: Commit inventory**

```bash
git add internal/discovery internal/inventory internal/testutil
git commit -m "feat: discover and inventory worktrees"
```

## Task 5: Collect cross-platform process evidence

**Files:**
- Create: `internal/pathutil/path.go`
- Create: `internal/pathutil/path_unix.go`
- Create: `internal/pathutil/path_windows.go`
- Create: `internal/pathutil/path_test.go`
- Create: `internal/process/source.go`
- Create: `internal/process/gopsutil.go`
- Create: `internal/process/collector.go`
- Create: `internal/process/collector_test.go`
- Create: `internal/process/smoke_test.go`

**Interfaces:**
- Produces: `pathutil.Canonical(path string) (string, error)`
- Produces: `pathutil.Contains(parent, child string) bool`
- Produces: `process.Source.List(ctx context.Context) ([]process.Info, error)`
- Produces: `process.Collector.Collect(ctx context.Context, worktrees []domain.Worktree) (process.Collection, []error)`

- [ ] **Step 1: Write fake-source and containment tests**

Use an injected source:

```go
type fakeSource struct {
	processes []Info
	err       error
}

func (f fakeSource) List(context.Context) ([]Info, error) {
	return f.processes, f.err
}

func TestCollectorAssociatesOnlyContainedCWD(t *testing.T) {
	created := time.Unix(100, 0).UTC()
	collector := Collector{Source: fakeSource{processes: []Info{
		{PID: 10, CreatedAt: created, Executable: "/bin/test", CWD: "/repo/wt/subdir", Inspectable: true},
		{PID: 11, CreatedAt: created, Executable: "/bin/test", CWD: "/repo/wt-other", Inspectable: true},
	}}}
	worktrees := []domain.Worktree{{Path: "/repo/wt"}}

	got, errs := collector.Collect(context.Background(), worktrees)

	if len(errs) != 0 {
		t.Fatalf("Collect() errors = %v", errs)
	}
	if len(got.ByWorktree["/repo/wt"]) != 1 || got.ByWorktree["/repo/wt"][0].PID != 10 {
		t.Fatalf("evidence = %#v", got)
	}
}
```

Add path tests for sibling-prefix attacks, symlinks, macOS case-sensitive
paths, Windows drive-letter case, `\\?\` prefixes, UNC paths, and separators.
Add a process test proving an inaccessible cwd is retained in
`Collection.Uninspectable` with `State=unknown` rather than dropped.
Add cases proving both `OwnerSame` and `OwnerUnknown` inaccessible processes
enter `GlobalUnknown`. A proven `OwnerOther` process with inaccessible cwd and
no path hint must also enter `GlobalUnknown`; owner is diagnostic provenance,
not proof that the process cannot access the worktree.
Add a source-enumeration failure case asserting `Complete=false` and a
synthetic global unknown record are copied into every candidate.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/pathutil ./internal/process
```

Expected: FAIL because the packages do not exist.

- [ ] **Step 3: Implement process and path collectors**

Use this process-source contract:

```go
package process

import (
	"context"
	"time"
)

type Info struct {
	PID         int32
	CreatedAt   time.Time
	Executable string
	CommandLine []string
	CWD         string
	Owner       string
	OwnerRelation OwnerRelation
	Inspectable bool
	Error       string
}

type OwnerRelation string

const (
	OwnerSame    OwnerRelation = "same-user"
	OwnerOther   OwnerRelation = "other-user"
	OwnerUnknown OwnerRelation = "unknown"
)

type Source interface {
	List(context.Context) ([]Info, error)
}

type Collection struct {
	ByWorktree   map[string][]domain.ProcessEvidence
	Uninspectable map[int32]domain.ProcessEvidence
	GlobalUnknown []domain.ProcessEvidence
	Complete      bool
	Errors        []string
}
```

`GopsutilSource.List` must enumerate `process.ProcessesWithContext`, then read
`CreateTimeWithContext`, `ExeWithContext`, `CwdWithContext`,
`CmdlineSliceWithContext`, `UsernameWithContext`, and `NameWithContext`. It
must retain PID, creation time, executable, owner, and available command-line
paths when cwd is inaccessible, with `Inspectable=false` and the error text.
It must not turn access denial into an empty successful process.

The collector maps inaccessible process records to `ProcessEvidence` with
`State=unknown` and stores them by PID in `Collection.Uninspectable`. Owner
lookup is tri-state; lookup failure is `OwnerUnknown`, never `OwnerOther`. It
associates that evidence with a worktree when the executable or an absolute
command-line path is inside the worktree. If a same-user process has no
inspectable cwd and no usable path hint, it is added to
`Collection.GlobalUnknown` and copied into every candidate's evidence set.
Unknown-owner and other-user inaccessible processes follow the same
fail-closed path because ownership does not prove inability to access a
worktree. Plan 002 PID-lock and provider bindings consume the PID map and can
make additional evidence relevant.

If `Source.List` fails or returns an incomplete enumeration,
`Collection.Complete=false`, the error is retained, and the collector appends
a synthetic `ProcessEvidence{PID: 0, State: unknown}` to `GlobalUnknown`.
`correlate.Group` copies it to every candidate, so process collection failure
cannot produce a safe decision.

The collector hashes:

```text
pid || creation timestamp || executable || canonical cwd
```

with SHA-256 and records the result in `ProcessEvidence.Fingerprint`.

`pathutil.Contains` must use component-aware relative paths, never string
prefix matching.

- [ ] **Step 4: Run unit, race, and platform smoke tests**

Run:

```bash
gofmt -w internal/pathutil internal/process
go test ./internal/pathutil ./internal/process
go test -race ./internal/process
GOOS=windows GOARCH=amd64 go test -c ./internal/pathutil
GOOS=windows GOARCH=amd64 go test -c ./internal/process
rm -f pathutil.test.exe process.test.exe
```

Expected: unit and race tests PASS and Windows test binaries cross-compile.

- [ ] **Step 5: Commit process evidence**

```bash
git add internal/pathutil internal/process
git commit -m "feat: collect process worktree evidence"
```

## Task 6: Implement evidence correlation and the fail-closed policy

**Files:**
- Create: `internal/correlate/evidence.go`
- Create: `internal/correlate/evidence_test.go`
- Create: `internal/policy/evaluate.go`
- Create: `internal/policy/evaluate_test.go`

**Interfaces:**
- Consumes: `domain.Worktree`, `domain.ProcessEvidence`, and future `domain.AgentEvidence`
- Produces: `correlate.Group(worktrees []domain.Worktree, processes process.Collection, agents []domain.AgentEvidence) map[string]domain.EvidenceSet`
- Produces: `policy.Evaluate(worktree domain.Worktree, evidence domain.EvidenceSet, policy domain.Policy) domain.Decision`

- [ ] **Step 1: Write the complete decision table as tests**

Table cases must include:

```go
tests := []struct {
	name string
	wt   domain.Worktree
	ev   domain.EvidenceSet
	want domain.Classification
	code string
}{
	{"primary", worktree(primary(), clean(), stale(), recoverable()), noEvidence(), domain.Protected, "primary_worktree"},
	{"locked", worktree(locked(), clean(), stale(), recoverable()), noEvidence(), domain.Protected, "locked"},
	{"unknown git", worktree(linked(), unknownGit(), stale(), recoverable()), noEvidence(), domain.Protected, "unknown_git_state"},
	{"dirty", worktree(linked(), dirty(), stale(), recoverable()), noEvidence(), domain.Protected, "dirty"},
	{"active process", worktree(linked(), clean(), stale(), recoverable()), activeProcess(), domain.Protected, "active_process"},
	{"active agent", worktree(linked(), clean(), stale(), recoverable()), activeAgent(), domain.Protected, "active_agent"},
	{"unhealthy adapter", worktree(linked(), clean(), stale(), recoverable()), unhealthyAdapter(), domain.Protected, "adapter_unhealthy"},
	{"insufficient trust", worktree(linked(), clean(), stale(), recoverable()), lowTrustAdapter(), domain.Review, "adapter_trust"},
	{"unknown relevant agent", worktree(linked(), clean(), stale(), recoverable()), unknownAgent(), domain.Protected, "unknown_evidence"},
	{"recent", worktree(linked(), clean(), recent(), recoverable()), noEvidence(), domain.Protected, "recent"},
	{"unrecoverable", worktree(linked(), clean(), stale(), unrecoverable()), noEvidence(), domain.Review, "unrecoverable_commits"},
	{"detached", worktree(detached(), clean(), stale(), recoverable()), noEvidence(), domain.Review, "detached"},
	{"safe", worktree(linked(), clean(), stale(), recoverable()), noEvidence(), domain.Safe, "safe"},
}
```

Assert reason ordering is stable and independent of map iteration.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/correlate ./internal/policy
```

Expected: FAIL because correlation and evaluation do not exist.

- [ ] **Step 3: Implement pure correlation and evaluation**

Policy precedence is exact:

1. primary/current/path-unsafe;
2. locked/prunable;
3. dirty;
4. active process;
5. active or recent agent;
6. unknown or conflicting relevant evidence;
7. recent worktree;
8. detached or unrecoverable;
9. safe.

Trust grades rank from strongest to weakest:

```text
supported-api
supported-app-server
supported-cli
experimental-api
versioned-private
unversioned-private
process-only
```

`hasInsufficientTrust` considers only applicable adapters and rejects any
healthy evidence whose rank is weaker than
`PolicySettings.MinimumTrustGrade`. Unknown grade strings are weaker than
every known grade.

`correlate.Group` starts each candidate with its
`processes.ByWorktree[worktree.Path]` records and then appends every
`processes.GlobalUnknown` record to every candidate before policy evaluation.
It never drops `Uninspectable`; Plan 002 may bind those records by PID through
agent lock evidence.

Use helpers that append stable `domain.Reason` values:

```go
func Evaluate(wt domain.Worktree, ev domain.EvidenceSet, p domain.Policy) domain.Decision {
	inactiveSince := wt.LastCommitAt
	if wt.MetadataModifiedAt.After(inactiveSince) {
		inactiveSince = wt.MetadataModifiedAt
	}
	inactiveFor := p.Now.Sub(inactiveSince)

	switch {
	case wt.Primary:
		return decision(domain.Protected, inactiveFor, "primary_worktree", "primary worktrees are never removable")
	case wt.Current:
		return decision(domain.Protected, inactiveFor, "current_worktree", "the Treeclear process is running inside the worktree")
	case !wt.PathSafe:
		return decision(domain.Protected, inactiveFor, "unsafe_path", "canonical path identity is not safe")
	case wt.Locked:
		return decision(domain.Protected, inactiveFor, "locked", "Git reports the worktree as locked")
	case wt.Prunable:
		return decision(domain.Protected, inactiveFor, "prunable_registration", "missing worktree registration requires explicit metadata cleanup")
	case !wt.GitStateKnown:
		return decision(domain.Protected, inactiveFor, "unknown_git_state", "required Git state could not be collected")
	case !wt.Status.Clean():
		return decision(domain.Protected, inactiveFor, "dirty", "worktree has staged, unstaged, unmerged, or untracked changes")
	case hasActiveProcess(ev):
		return decision(domain.Protected, inactiveFor, "active_process", "a process cwd is inside the worktree")
	case hasActiveAgent(ev):
		return decision(domain.Protected, inactiveFor, "active_agent", "an agent session is active or recent")
	case hasUnhealthyApplicableAdapter(ev):
		return decision(domain.Protected, inactiveFor, "adapter_unhealthy", "a relevant adapter is unhealthy")
	case hasUnknownRelevantEvidence(ev):
		return decision(domain.Protected, inactiveFor, "unknown_evidence", "relevant evidence is unknown or conflicting")
	case inactiveFor < p.Settings.InactivityThreshold:
		return decision(domain.Protected, inactiveFor, "recent", "worktree has not exceeded the inactivity threshold")
	case wt.Detached:
		return decision(domain.Review, inactiveFor, "detached", "detached HEAD requires review")
	case !wt.Recoverable:
		return decision(domain.Review, inactiveFor, "unrecoverable_commits", "branch state has no proven recovery path")
	case hasNonOfflineRevalidatableAdapter(ev):
		return decision(domain.Review, inactiveFor, "offline_revalidation", "relevant adapter evidence cannot be fully revalidated offline")
	case hasInsufficientTrust(ev, p.Settings.MinimumTrustGrade):
		return decision(domain.Review, inactiveFor, "adapter_trust", "relevant adapter evidence does not meet the trust policy")
	default:
		return decision(domain.Safe, inactiveFor, "safe", "clean, inactive, and recoverable")
	}
}
```

Do not read the clock or filesystem inside this package.

- [ ] **Step 4: Run policy and race tests**

Run:

```bash
gofmt -w internal/correlate internal/policy
go test ./internal/correlate ./internal/policy
go test -race ./internal/correlate ./internal/policy
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit the policy**

```bash
git add internal/correlate internal/policy
git commit -m "feat: classify cleanup candidates safely"
```

## Task 7: Build, fingerprint, persist, and inspect plans

Reviewable delivery slices:

- **7A (current):** `CandidateFingerprint` and `PolicyDigest`, deterministic
  precondition encoding, and focused mutation/canonicalization tests.
- **7B (pending):** private filesystem storage, local HMAC integrity, expiry,
  and tamper rejection on native macOS and Windows.
- **7C (pending):** builder integration and the `plan`/`explain` CLI commands.

The combined Task 7 checkboxes below remain open until all slices are delivered.
7A does not create, authenticate, persist, load, or apply a plan.

**Files:**
- Create: `internal/plan/fingerprint.go`
- Create: `internal/plan/fingerprint_test.go`
- Create: `internal/plan/integrity.go`
- Create: `internal/plan/integrity_test.go`
- Create: `internal/plan/build.go`
- Create: `internal/plan/build_test.go`
- Create: `internal/plan/store.go`
- Create: `internal/plan/store_test.go`
- Create: `internal/fssecure/private_unix.go`
- Create: `internal/fssecure/private_windows.go`
- Create: `internal/fssecure/private_test.go`
- Create: `internal/cli/scan.go`
- Create: `internal/cli/plan.go`
- Modify: `internal/cli/root.go`
- Test: `internal/cli/plan_test.go`

**Interfaces:**
- Produces: `plan.Builder.Build(ctx context.Context, request plan.Request) (domain.Plan, error)`
- Produces: `plan.Store.Save(ctx context.Context, value domain.Plan) (string, error)`
- Produces: `plan.Store.Load(ctx context.Context, idOrPath string) (domain.Plan, error)`
- Produces CLI commands: `scan`, `plan`, and `explain`

- [ ] **Step 1: Write deterministic fingerprint and expiry tests**

```go
func TestCandidateFingerprintChangesWithAnyPrecondition(t *testing.T) {
	base := candidateFixture()
	original, err := CandidateFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	changed := base
	changed.Worktree.Head = "different"
	next, err := CandidateFingerprint(changed)
	if err != nil {
		t.Fatal(err)
	}
	if original == next {
		t.Fatal("fingerprint did not change with HEAD")
	}
}

func TestStoreRejectsExpiredPlan(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := NewStore(t.TempDir(), func() time.Time { return now }, bytes.Repeat([]byte{0x42}, 32))
	path, err := store.Save(context.Background(), domain.Plan{
		SchemaVersion: 1,
		ID:            "plan_expired",
		ExpiresAt:     time.Unix(199, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	now = time.Unix(200, 0).UTC()
	_, err := store.Load(context.Background(), path)
	if !errors.Is(err, ErrPlanExpired) {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestStoreRejectsTamperedAction(t *testing.T) {
	store := NewStore(t.TempDir(), time.Now, bytes.Repeat([]byte{0x42}, 32))
	value := domain.Plan{
		SchemaVersion: 1,
		ID:            "plan_tampered",
		ExpiresAt:     time.Now().Add(time.Minute),
		Candidates: []domain.Candidate{{
			ID:       "protected-candidate",
			Action:   "none",
			Decision: domain.Decision{Classification: domain.Protected},
		}},
	}
	path, err := store.Save(context.Background(), value)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["candidates"].([]any)[0].(map[string]any)["action"] = "remove"
	tampered, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = store.Load(context.Background(), path)
	if !errors.Is(err, ErrPlanIntegrity) {
		t.Fatalf("Load() error = %v", err)
	}
}
```

The test file imports `bytes`, `context`, `encoding/json`, `errors`, `os`,
`testing`, `time`, and `github.com/hellices/treeclear/internal/domain`.
Implement `NewStore(root string, now func() time.Time, integrityKey []byte)
Store` with the exact constructor used above.

Add a test that changing process creation time or agent fingerprint changes the
candidate fingerprint.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/plan ./internal/cli
```

Expected: FAIL because plan building and commands do not exist.

- [ ] **Step 3: Implement canonical fingerprints, atomic storage, and commands**

Define the builder request:

```go
type Request struct {
	Roots             []string
	Settings          domain.PolicySettings
	IntendedApplyMode domain.ApplyMode
	AdapterLockDigest string
}
```

Interactive `scan` and `plan` use `ApplyInteractive`. Plan 004
`schedule run` uses `ApplyScheduled`. `Builder.Build` copies the mode into
`Plan.IntendedApplyMode` before adapter trust evaluation and HMAC signing.

`CandidateFingerprint` must hash a dedicated struct rather than the mutable
candidate JSON:

```go
type candidatePreconditions struct {
	Path         string                   `json:"path"`
	RepositoryRoot string                 `json:"repositoryRoot"`
	CommonGitDir string                   `json:"commonGitDir"`
	AdminDir     string                   `json:"adminDir"`
	Head         string                   `json:"head"`
	Branch       string                   `json:"branch"`
	Upstream     string                   `json:"upstream"`
	IndexHash    string                   `json:"indexHash"`
	AdminHash    string                   `json:"adminHash"`
	Primary      bool                     `json:"primary"`
	Current      bool                     `json:"current"`
	PathSafe     bool                     `json:"pathSafe"`
	Detached     bool                     `json:"detached"`
	Locked       bool                     `json:"locked"`
	Prunable     bool                     `json:"prunable"`
	GitStateKnown bool                    `json:"gitStateKnown"`
	CollectionErrors []string             `json:"collectionErrors"`
	Recoverable  bool                     `json:"recoverable"`
	Status       domain.GitStatus         `json:"status"`
	LastCommitAt time.Time                `json:"lastCommitAt"`
	MetadataModifiedAt time.Time          `json:"metadataModifiedAt"`
	Snapshot     domain.SnapshotPlan      `json:"snapshot"`
	Action       string                   `json:"action"`
	Decision     decisionPrecondition     `json:"decision"`
	Processes    []processPrecondition    `json:"processes"`
	Agents       []agentPrecondition      `json:"agents"`
	Adapters     []domain.AdapterStatus   `json:"adapters"`
}

type processPrecondition struct {
	PID         int32                `json:"pid"`
	CreatedAt   time.Time            `json:"createdAt"`
	Executable string               `json:"executable"`
	CWD         string               `json:"cwd"`
	State       domain.EvidenceState `json:"state"`
	Fingerprint string               `json:"fingerprint"`
}

type agentPrecondition struct {
	AdapterID        string                    `json:"adapterId"`
	AdapterVersion   string                    `json:"adapterVersion"`
	BundleDigest     string                    `json:"bundleDigest"`
	Provider         string                    `json:"provider"`
	SourceID         string                    `json:"sourceId"`
	SessionID        string                    `json:"sessionId"`
	ThreadID         string                    `json:"threadId"`
	ProjectID        string                    `json:"projectId"`
	CWD              string                    `json:"cwd"`
	RepositoryRoot   string                    `json:"repositoryRoot"`
	WorktreePath     string                    `json:"worktreePath"`
	State            domain.EvidenceState      `json:"state"`
	CreatedAt        time.Time                 `json:"createdAt"`
	UpdatedAt        time.Time                 `json:"updatedAt"`
	ProcessRefs      []domain.ProcessReference `json:"processRefs"`
	Binding          *domain.WorktreeBinding   `json:"binding"`
	SourceKind       string                    `json:"sourceKind"`
	SupportGrade     domain.TrustGrade         `json:"supportGrade"`
	Confidence       string                    `json:"confidence"`
	SchemaVersion    string                    `json:"schemaVersion"`
	RawFingerprint   string                    `json:"rawFingerprint"`
	TrustRecordDigest string                   `json:"trustRecordDigest"`
	Executable       *domain.ExecutableIdentity `json:"executable"`
	RevalidationMode string                    `json:"revalidationMode"`
}

type decisionPrecondition struct {
	Classification domain.Classification `json:"classification"`
	ReasonCodes     []string              `json:"reasonCodes"`
}
```

Sort process, offline-revalidatable agent, and adapter preconditions by stable identity before
marshaling and SHA-256 hashing. Deliberately exclude observation timestamps
and human warning text; include source content fingerprints, session update
times, process creation times, action, classification, reason codes, and every
policy-relevant value. Planning-only agent evidence remains in the HMAC-signed
plan for explanation but is excluded from the removal fingerprint because
apply does not execute that source; `AdapterStatus.OfflineRevalidatable` must
still be true for a Safe decision.

The implementation extends the sketch with warning/error-presence bits because
the existing policy treats those as unknown even when state/health fields
otherwise look valid. Human warning text, process/adapter diagnostic text,
reason messages, cached fingerprints, display IDs, lock reason text, byte
estimates, and elapsed inactivity are not removal preconditions. Collection
errors retain their exact text as specified above. Only explicit
`planning-only` records are omitted; unknown revalidation modes fail closed.
Inputs must already contain canonical path identities. Instants are encoded
in UTC; unordered lists use deterministic full-value tie breakers and retain
duplicates. Nil and empty lists normalize alike, while executable argument
order is preserved. Invalid UTF-8 and unencodable timestamps in hashed fields
return an error instead of a lossy fingerprint. Digests use `sha256:` followed
by 64 lowercase hexadecimal digits. These digests are not authentication or
permission to remove anything; those checks belong to the remaining tasks.

`PolicyDigest` is SHA-256 over canonical `domain.PolicySettings`. Before
hashing, copy and sort `BaseBranches`. The digest inputs are exactly:

- inactivity threshold;
- plan expiry;
- base branches;
- minimum adapter trust grade;
- snapshot maximum bytes.

`Store.Save` must write mode `0600` to a temporary file under the private plan
directory, `Sync`, close, and rename atomically.

`internal/plan/integrity.go` creates one random 32-byte HMAC key in the private
Treeclear state directory on first use. It stores the key through
`fssecure.EnsurePrivateDirectory`, derives `KeyID` as SHA-256 of the key, and
computes HMAC-SHA-256 over canonical plan JSON with `Integrity.MAC` empty.
`Store.Load` verifies the MAC with `hmac.Equal` before trusting any action,
decision, path, or fingerprint. User-supplied plan files use the same local
key and fail with `ErrPlanIntegrity` when edited or copied from another
installation.

Task 7 also implements `fssecure.EnsurePrivateDirectory` and
`fssecure.WritePrivateFile`. Unix uses `0700` directories and `0600` files.
Windows uses a protected DACL granting full control only to the current user
and `SYSTEM`, through `windows.GetCurrentProcessToken`,
`windows.ACLFromEntries`, and `windows.SetNamedSecurityInfo`.

The builder dependencies are explicit:

```go
type InventoryLoader interface {
	Load(context.Context, []string) ([]domain.Worktree, []error)
}

type ProcessCollector interface {
	Collect(context.Context, []domain.Worktree) (process.Collection, []error)
}

type Builder struct {
	Inventory InventoryLoader
	Processes ProcessCollector
	Now       func() time.Time
	Version   string
}
```

`treeclear scan` prints all decisions without saving a plan.

`treeclear plan` saves a plan and defaults actions to `remove` only for safe
candidates. Review and protected actions are `none`.

`treeclear explain <candidate-id>` loads the newest non-expired plan and prints
all reasons and evidence for exactly one candidate.

Task 7 owns these public flags:

```text
treeclear scan [--root <path> ...] [--inactivity-threshold <duration>] [--format human|json]
treeclear plan [--root <path> ...] [--inactivity-threshold <duration>] [--format human|json] [--output <path>]
treeclear explain <candidate-id> [--plan <id-or-path>] [--format human|json]
```

`--root` is repeatable. `--inactivity-threshold` uses `config.Duration`.
`--output` writes the same canonical plan bytes saved by `plan.Store`; it does
not bypass private canonical storage.

- [ ] **Step 4: Run targeted, full, and command smoke tests**

Run:

```bash
gofmt -w internal/plan internal/cli
go test ./internal/plan ./internal/cli
go test ./...
GOOS=windows GOARCH=amd64 go test -c ./internal/fssecure
rm -f fssecure.test.exe
go run ./cmd/treeclear plan --root .
```

Expected: tests PASS. The command writes a plan, classifies the primary
worktree as protected, and performs no removal.

- [ ] **Step 5: Commit plans and read-only CLI**

```bash
git add internal/plan internal/fssecure internal/cli
git commit -m "feat: create expiring cleanup plans"
```

## Task 8: Create private, verified recovery snapshots

**Files:**
- Create: `internal/snapshot/manifest.go`
- Create: `internal/snapshot/create.go`
- Create: `internal/snapshot/create_test.go`
- Create: `internal/snapshot/restore.go`
- Create: `internal/snapshot/restore_test.go`

**Interfaces:**
- Produces: `fssecure.EnsurePrivateDirectory(path string) error`
- Produces: `snapshot.Manager.Create(ctx context.Context, plan domain.Plan, candidate domain.Candidate) (snapshot.Receipt, error)`
- Produces: `snapshot.Manager.Restore(ctx context.Context, snapshotID string) error`

- [ ] **Step 1: Write snapshot integrity and privacy tests**

Create a real worktree with:

- one staged text modification;
- one unstaged binary modification;
- one non-ignored untracked file;
- one ignored `node_modules` file;
- one symlink on macOS.

Assert:

```go
receipt, err := manager.Create(ctx, safePlan(), candidate)
if err != nil {
	t.Fatal(err)
}
manifest := readManifest(t, receipt.Path)
if manifest.UntrackedFiles != 1 {
	t.Fatalf("UntrackedFiles = %d", manifest.UntrackedFiles)
}
if _, err := os.Stat(filepath.Join(receipt.Path, "untracked.tar.gz")); err != nil {
	t.Fatal(err)
}
if strings.Contains(string(readArchiveNames(t, receipt.Path)), "node_modules") {
	t.Fatal("ignored dependency content was archived")
}
```

Add a test that corrupting one archive byte causes `Verify` and `Restore` to
fail before creating a worktree.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/fssecure ./internal/snapshot
```

Expected: FAIL because private storage and snapshots do not exist.

- [ ] **Step 3: Implement private storage, manifest, create, verify, and restore**

Snapshot layout:

```text
<data>/trash/<snapshot-id>/
  manifest.json
  worktree-list.bin
  status.bin
  staged.patch
  unstaged.patch
  untracked.tar.gz
```

Capture tracked changes only with:

```text
git diff --binary --no-ext-diff --no-textconv
git diff --cached --binary --no-ext-diff --no-textconv
```

The snapshot manager must not honor `diff.external` or textconv drivers.

Manifest:

```go
type Manifest struct {
	SchemaVersion   int               `json:"schemaVersion"`
	SnapshotID      string            `json:"snapshotId"`
	PlanID          string            `json:"planId"`
	CandidateID     string            `json:"candidateId"`
	CandidateFingerprint string       `json:"candidateFingerprint"`
	PolicyDigest    string            `json:"policyDigest"`
	AdapterLockDigest string          `json:"adapterLockDigest"`
	EvidenceDigest  string            `json:"evidenceDigest"`
	CreatedAt       time.Time         `json:"createdAt"`
	RepositoryRoot  string            `json:"repositoryRoot"`
	CommonGitDir    string            `json:"commonGitDir"`
	WorktreePath    string            `json:"worktreePath"`
	Head            string            `json:"head"`
	Branch          string            `json:"branch,omitempty"`
	RecoveryRef     string            `json:"recoveryRef,omitempty"`
	Files           map[string]string `json:"files"`
	UntrackedFiles  int               `json:"untrackedFiles"`
	UntrackedBytes  int64             `json:"untrackedBytes"`
}
```

Create snapshots in a sibling temporary directory. Hash every payload, write
the manifest last, verify every hash, fsync, then rename atomically.

`Create` copies `plan.PolicyDigest`, `plan.AdapterLockDigest`,
`candidate.Fingerprint`, and a canonical SHA-256 digest of
`candidate.Evidence` into the manifest. It rejects an empty digest.

On Unix, enforce directory mode `0700` and file mode `0600`.

Reuse Task 7's `fssecure` package for every snapshot directory and file.

For detached HEAD, create
`refs/treeclear/recovery/<snapshot-id>` before removal. For a branch, record the
branch without moving it.

Restore must refuse existing paths and moved branches, add the worktree at the
recorded commit, apply the staged patch with `git apply --index --binary`, apply
the unstaged patch with `git apply --binary`, safely extract untracked entries
without path traversal, and verify restored SHA-256 values.

- [ ] **Step 4: Run snapshot and Windows compile tests**

Run:

```bash
gofmt -w internal/snapshot
go test ./internal/fssecure ./internal/snapshot
go test ./...
GOOS=windows GOARCH=amd64 go test -c ./internal/fssecure
rm -f fssecure.test.exe
```

Expected: all tests PASS and Windows ACL code cross-compiles.

- [ ] **Step 5: Commit snapshot and restore primitives**

```bash
git add internal/snapshot
git commit -m "feat: add verified recovery snapshots"
```

## Task 9: Apply plans with whole-batch preflight and a durable journal

**Files:**
- Create: `internal/apply/journal.go`
- Create: `internal/apply/journal_test.go`
- Create: `internal/apply/engine.go`
- Create: `internal/apply/engine_test.go`
- Create: `internal/apply/environment.go`
- Create: `internal/apply/environment_test.go`
- Create: `internal/cli/apply.go`
- Create: `internal/statelock/lock.go`
- Create: `internal/statelock/lock_test.go`
- Modify: `internal/cli/root.go`
- Test: `internal/cli/apply_test.go`

**Interfaces:**
- Consumes: `plan.Store`, inventory/process collectors, `snapshot.Manager`, and `git.Client`
- Produces: `apply.Engine.Apply(ctx context.Context, plan domain.Plan, mode domain.ApplyMode) (domain.ApplyJournal, error)`
- Produces: `treeclear apply --plan <id-or-file>`

- [ ] **Step 1: Write race, preflight, and partial-apply tests**

Use fakes that record method order. Required tests:

```go
func TestApplyRevalidatesAllCandidatesBeforeSnapshotOrRemoval(t *testing.T) {
	events := []string{}
	engine := fixtureEngine(&events)
	engine.Revalidator = changedOnSecondCandidate()

	_, err := engine.Apply(context.Background(), twoCandidatePlan(), domain.ApplyInteractive)

	if !errors.Is(err, ErrPreconditionChanged) {
		t.Fatalf("Apply() error = %v", err)
	}
	if slices.Contains(events, "snapshot") || slices.Contains(events, "remove") {
		t.Fatalf("mutation occurred before full preflight: %v", events)
	}
}

func TestApplyCreatesAllSnapshotsBeforeFirstRemoval(t *testing.T) {
	events := []string{}
	engine := fixtureEngine(&events)

	_, err := engine.Apply(context.Background(), twoCandidatePlan(), domain.ApplyInteractive)

	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{
		"preflight:a", "preflight:b",
		"snapshot:a", "snapshot:b",
		"post-snapshot:a", "post-snapshot:b",
		"pre-remove:a", "remove:a",
		"pre-remove:b", "remove:b",
	}, events); diff != "" {
		t.Fatalf("event order mismatch (-want +got):\n%s", diff)
	}
}
```

Add tests for expired plan, non-safe action, changed process creation time,
changed index hash, plan action tampering, removal failure after one success,
and idempotent resume.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/apply ./internal/cli
```

Expected: FAIL because apply and journal packages do not exist.

- [ ] **Step 3: Implement the state machine and CLI**

Use explicit dependencies:

```go
type Revalidator interface {
	Revalidate(context.Context, domain.Plan) ([]domain.Candidate, error)
}

type Snapshotter interface {
	Create(context.Context, domain.Plan, domain.Candidate) (snapshot.Receipt, error)
}

type Remover interface {
	RemoveWorktree(context.Context, string, string) error
}

type JournalStore interface {
	LoadOrCreate(context.Context, domain.Plan) (domain.ApplyJournal, error)
	Save(context.Context, domain.ApplyJournal) error
}

type EnvironmentVerifier interface {
	VerifyPolicy(context.Context, string) error
	VerifyAdapterLock(context.Context, string) error
	VerifyExecutables(context.Context, []domain.ExecutableIdentity) error
}

type OperationLock interface {
	Acquire(context.Context, string) (release func() error, err error)
}
```

`internal/statelock` has platform files:

```text
lock.go
lock_unix.go
lock_windows.go
```

On macOS, open one lock file and use `unix.Flock` with
`LOCK_EX|LOCK_NB`, retrying with context-aware bounded backoff. On Windows,
open the same state file and use `windows.LockFileEx` with
`LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY`, then
`windows.UnlockFileEx` on release. Both implementations retain the open file
handle for the full critical section and write the operation name and PID only
after acquiring the OS lock.

Add two-handle tests proving a second acquire blocks/fails until release, plus
Windows cross-compilation.

`Apply(ctx, plan, mode)` first requires `mode == plan.IntendedApplyMode`.
Interactive CLI apply passes `ApplyInteractive`; `schedule run --mode
apply-safe` passes `ApplyScheduled`.

`Apply` order is exact:

1. reject expired or unsupported plan;
2. acquire the shared exclusive operation lock as `apply`;
3. verify current policy digest, adapter lock digest, and resolved executable
   identities against the plan;
4. load or create the journal;
5. preflight every pending candidate and compare fingerprints;
   independently recompute its policy decision and require `Safe` plus action
   `remove`;
6. snapshot every pending candidate and persist receipts;
7. revalidate the complete pending set again after snapshots;
8. immediately revalidate each candidate once more before its removal;
9. remove that candidate serially;
10. save after every state transition;
11. mark `completed`, `partial`, or `failed`;
12. release the lock.

Any post-snapshot or pre-remove difference aborts all remaining removals.
Snapshots already created remain available and are recorded in the journal.

`internal/statelock` stores one OS-level exclusive lock in Treeclear's private
state directory. Plan 003 adapter update must acquire this same lock with
operation name `adapter-update`, so update and apply cannot overlap.

`internal/apply/environment.go` provides the production verifier. It receives
a `CurrentPolicy func(context.Context) (domain.PolicySettings, error)`,
canonicalizes it through the same Task 7 digest function, and compares the
result with `Plan.PolicyDigest`. In Plan 001, the adapter lock is the digest of
an empty adapter set and the executable list is empty. Plan 002 replaces those
inputs through injected registry and executable resolvers without changing the
apply engine.

Already completed candidates are valid only when the registered worktree and
path are absent. If either reappears, return `ErrPreconditionChanged`.

The CLI must require `--plan`; JSON output returns the entire journal and a
typed error category. It must not accept `--force`.

- [ ] **Step 4: Run targeted, race, and full tests**

Run:

```bash
gofmt -w internal/apply internal/cli
go test ./internal/apply ./internal/cli
go test -race ./internal/apply
go test ./...
```

Expected: all tests PASS with event ordering proven.

- [ ] **Step 5: Commit apply**

```bash
git add internal/apply internal/cli
git commit -m "feat: apply plans with full revalidation"
```

## Task 10: Expose restore and trash management

**Files:**
- Create: `internal/trash/store.go`
- Create: `internal/trash/store_test.go`
- Create: `internal/cli/restore.go`
- Create: `internal/cli/restore_test.go`
- Modify: `internal/cli/root.go`

**Interfaces:**
- Produces: `trash.Store.List(ctx context.Context) ([]snapshot.Manifest, error)`
- Produces: `trash.Store.Prune(ctx context.Context, olderThan time.Duration, now time.Time) ([]string, error)`
- Produces commands: `restore`, `trash list`, and `trash prune`

- [ ] **Step 1: Write explicit-prune and restore CLI tests**

```go
func TestTrashDoesNotExpireAutomatically(t *testing.T) {
	store := newTrashStore(t)
	old := storeFixture(t, time.Unix(1, 0))

	got, err := store.List(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SnapshotID != old {
		t.Fatalf("List() = %#v", got)
	}
}

func TestPruneRequiresPositiveAge(t *testing.T) {
	_, err := newTrashStore(t).Prune(context.Background(), 0, time.Now())
	if !errors.Is(err, ErrInvalidRetention) {
		t.Fatalf("Prune() error = %v", err)
	}
}
```

Test that `treeclear restore` refuses an occupied path and that
`treeclear trash prune` prints every deleted snapshot ID.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/trash ./internal/cli
```

Expected: FAIL because trash and restore CLI wiring do not exist.

- [ ] **Step 3: Implement list, explicit prune, and restore commands**

`trash.Store.List` verifies manifests before returning them and sorts newest
first.

`trash.Store.Prune` deletes only verified snapshot directories whose
`CreatedAt` is strictly older than `now.Add(-olderThan)`. It never follows
symlinks and rejects paths outside the configured trash root.

CLI forms:

```text
treeclear restore <snapshot-id>
treeclear trash list [--format json]
treeclear trash prune --older-than 30d [--format json]
```

Reuse `config.Duration` parsing for day suffixes.

- [ ] **Step 4: Run command and full tests**

Run:

```bash
gofmt -w internal/trash internal/cli
go test ./internal/trash ./internal/cli
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit restore and trash**

```bash
git add internal/trash internal/cli
git commit -m "feat: expose worktree recovery"
```

## Task 11: Prove the core end to end and document the core-only workflow

**Files:**
- Create: `tests/e2e/core_test.go`
- Create: `README.md`
- Modify: `Makefile`

**Interfaces:**
- Verifies all Plan 1 public commands and safety invariants.
- Produces documented quick-start commands that remain valid when later plans add adapters.

- [ ] **Step 1: Write an end-to-end remove-and-restore test**

The test must:

1. build `./cmd/treeclear` into a temp directory;
2. create a primary repository and clean linked worktree;
3. pass `--inactivity-threshold 1ms` and wait until the worktree exceeds that
   threshold;
4. create a JSON plan;
5. apply the plan;
6. assert the linked path is gone and the branch remains;
7. locate the snapshot;
8. restore it;
9. assert HEAD and file bytes match;
10. assert the primary worktree was never offered for removal.

Add a second test that starts a child process with cwd inside the linked
worktree and confirms classification is protected.

- [ ] **Step 2: Run the end-to-end test and verify it fails**

Run:

```bash
go test ./tests/e2e -run TestCoreRemoveAndRestore -v
```

Expected: FAIL until test-only clock injection and final dependency wiring are
complete.

- [ ] **Step 3: Wire production dependencies and write the README**

Replace temporary CLI factories with one production composition root in
`internal/cli/root.go`. Production and e2e binaries both use `time.Now`; tests
control inactivity only through the public threshold option.

README quick start:

````markdown
# Treeclear

Safely clear stale coding-agent worktrees.

## Core preview

```bash
treeclear scan --root ~/workspace
treeclear plan --root ~/workspace --format json
treeclear apply --plan <plan-id>
treeclear trash list
treeclear restore <snapshot-id>
```

Treeclear defaults to a 7-day inactivity threshold, never force-removes a
worktree, preserves branches, and revalidates the full plan before removal.
Agent-aware adapters are added in the next implementation phase.
````

Add `e2e` to `Makefile`:

```make
.PHONY: e2e
e2e:
	go test ./tests/e2e -v
```

- [ ] **Step 4: Run the complete core verification**

Run:

```bash
gofmt -w tests/e2e
go test ./...
go test -race ./...
go test ./tests/e2e -v
go build ./cmd/treeclear
GOOS=windows GOARCH=amd64 go build ./cmd/treeclear
rm -f treeclear.exe
git diff --check
```

Expected: every command succeeds with no race reports or diff errors.

- [ ] **Step 5: Commit the working safety core**

```bash
git add README.md Makefile tests/e2e internal/cli
git commit -m "test: verify Treeclear core workflow"
```

## Plan 1 Completion Gate

Before starting the adapter plan:

```bash
go test ./...
go test -race ./...
go test ./tests/e2e -v
go build ./cmd/treeclear
GOOS=windows GOARCH=amd64 go build ./cmd/treeclear
rm -f treeclear.exe
git status --short
```

Expected:

- every command exits 0;
- `git status --short` is empty;
- a clean stale linked worktree can be removed and restored;
- dirty, current, locked, active-process, detached, and unknown candidates are
  not removed;
- all snapshots verify before removal;
- branches remain after worktree removal;
- no adapter code is required for the core-only workflow.
