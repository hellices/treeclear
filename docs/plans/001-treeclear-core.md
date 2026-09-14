# Treeclear Safety Core Implementation Plan

- Status: In progress — Task 8H bounded read-only source capture
- Sequence: 001 of 004
- Source architecture: [Treeclear Architecture](../architecture/2026-09-12-treeclear.md)
- Depends on: [000 Minimal Development Baseline](000-development-harness.md)

Stage 000 supplies the module, development commands, and shared test fixtures.
Reuse those files; Task 1 still owns the product CLI and dependencies, and
Task 4 still owns inventory integration coverage. No product task is complete
merely because the harness is available.

> Execute this plan task-by-task using an isolated Git worktree, test-driven development, and a review checkpoint after every task. Steps use checkbox (`- [ ]`) syntax for tracking.

PRs #1, #2, #3, #4, #5, #6, #7, #8, #9, #10, #11, #12, and #13 are merged: the standard development baseline, Tasks 1–6,
the read-only `scan` command brought forward from Task 7, and Task 7A's pure
candidate fingerprints and policy digests, and Task 7B's private authenticated
plan storage and Task 7C's plan builder and `plan`/`explain` commands are
delivered. Task 7D's ancestor-creation race correction also passed independent
review and native macOS/Windows CI, including its merge commit. Task 8A's pure
snapshot-integrity foundation also passed independent review and native CI on
both the final head and merge commit. Task 8B's guarded raw Git reads also
passed independent review and native CI on the final head and merge commit.
Task 8C's bounded administrative diagnostics also passed independent review
and native macOS/Windows CI on the final head and merge commit. Task 8D's
bounded in-memory untracked tar/gzip codec also passed independent review and
native macOS/Windows CI on the final head and merge commit. Task 8E's bounded
read-only collection of explicitly supplied source leaves and their parents
also passed independent review and native CI on the final head and merge
commit. Task 8F's exact guarded status-derived untracked paths also passed
independent review and native macOS/Windows CI on the final head and actual
merge. Task 8G's bounded whole-bundle verification passed independent review
and native macOS/Windows CI on its final head and actual merge, without changing
the legacy hash-only payload contract. Task 8H now composes the guarded readers
into bounded, revalidated source capture.
Task 8 source capture, publication and restore, and Tasks 9–11 cleanup and
recovery remain pending.
Each slice keeps its safety contract independently reviewable.
Agent adapters and later plans remain unimplemented; no mutation command is
exposed.

**Goal:** Build a working macOS and Windows Treeclear CLI that discovers Git worktrees, correlates process activity, classifies candidates, writes expiring plans, safely removes approved worktrees, and restores them from verified local snapshots.

**Architecture:** A Go CLI delegates all operating-system and Git reads to narrow collectors, converts them into immutable domain values, and evaluates a pure fail-closed policy. Apply reloads the exact plan, re-collects every precondition, snapshots every pending target, and only then performs serial `git worktree remove` operations with a durable journal.

**Tech Stack:** Go 1.26.0 with toolchain 1.26.5, Cobra 1.10.2, go-toml/v2 2.4.3, gopsutil/v4 4.26.8, x/sys 0.41.0, go-cmp 0.7.0, Git 2.36 or newer, standard-library tar/gzip and crypto packages.

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
	golang.org/x/sys v0.41.0
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
func TestParseWorktreePorcelainZ(test *testing.T) {
	head := strings.Repeat("a", 40)
	otherHead := strings.Repeat("b", 40)
	input := []byte("worktree /tmp/main repo\x00HEAD " + head + "\x00branch refs/heads/main\x00\x00" +
		"worktree /tmp/feature\nname\x00HEAD " + otherHead + "\x00branch refs/heads/feature\x00locked agent active\x00\x00")
	actual, err := parseWorktreePorcelainZ(input)
	want := []rawWorktree{
		{Path: "/tmp/main repo", Head: head, Branch: "main"},
		{Path: "/tmp/feature\nname", Head: otherHead, Branch: "feature", Locked: true, LockReason: "agent active"},
	}
	if err != nil || !reflect.DeepEqual(actual, want) {
		test.Fatalf("parsed = %#v, error = %v", actual, err)
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

- **7A (merged, PR #3):** `CandidateFingerprint` and `PolicyDigest`, deterministic
  precondition encoding, and focused mutation/canonicalization tests.
- **7B (merged, PR #4):** private filesystem storage, local HMAC integrity, expiry,
  and tamper rejection on native macOS and Windows.
- **7C (current):** builder integration and the `plan`/`explain` CLI commands.

The combined Task 7 checkboxes below remain open until all slices are delivered.
7B accepts already constructed plans; it does not build a plan, expose a new
CLI command, authorize an action, or apply cleanup.

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

- [x] **Step 1: Write deterministic fingerprint and expiry tests**

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

- [x] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/plan ./internal/cli
```

Expected: FAIL because plan building and commands do not exist.

- [x] **Step 3: Implement canonical fingerprints, atomic storage, and commands**

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

`Store.Save` validates the schema, identifier, expiry, encoding, and 16 MiB
document limit before creating state. Schema 1 identifiers start with `plan_`
and have an ASCII letter/digit/underscore/hyphen suffix, at most 128 bytes in
total. Expiry must be strictly after the injected clock. A nonzero generation
time must not be in the future and must precede expiry; the Task 7C builder
owns supplying generation time and deriving unique content/randomness IDs.

The 7B publication contract refines the original overwrite-capable rename
sketch: plans and the integrity key are immutable, exclusively published
files. `fssecure.WritePrivateFile` writes a private temporary file in the
destination directory, calls `Sync`, closes it, and atomically links it into
the final name without replacement. An existing name returns `fs.ErrExist`;
the temporary name is removed on success or failure. Filesystems without
exclusive hard-link publication fail closed rather than falling back to
truncation or overwrite. This prevents concurrent same-ID saves or first-use
key creation from replacing a complete winner.

`internal/plan/integrity.go` creates one random 32-byte HMAC key in the private
Treeclear state directory on first save as `integrity.key`. A supplied 32-byte
test key is copied and is not persisted. It stores the local key through
`fssecure.WritePrivateFile`, derives `KeyID` as `sha256:` plus lowercase hex, and
computes HMAC-SHA-256 over canonical plan JSON with `Integrity.MAC` empty.
`Store.Load` verifies the MAC with `hmac.Equal` before trusting any action,
decision, path, or fingerprint. User-supplied plan files use the same local
key and fail with `ErrPlanIntegrity` when edited or copied from another
installation. Missing, malformed, or inaccessible keys are never repaired by
`Load`; malformed or inaccessible keys also block `Save` rather than being
replaced.

The signed JSON includes the complete typed plan, including planning-only
records, observation times, explanatory messages, and list ordering. All
timestamps are normalized to UTC without modifying caller values. Loads
accept the canonical encoding emitted by the store, with optional surrounding
whitespace, not pretty-printed/reordered documents, duplicate or unknown
fields, case-aliased names, or alternate time spellings. This strict version-1
encoding prevents ambiguous JSON representations. Exports in Task 7C must
preserve the saved bytes. Stored plans use `plans/<planId>.json`; loading by ID
also checks that the authenticated identifier matches the requested ID.
Other nonempty, non-NUL load arguments are file paths, with no extension
requirement; relative paths resolve against the caller's working directory.
Use `./` or an absolute path when a filename itself is also a valid plan ID.

The canonical version-1 integrity object is the final root field. `Load`
authenticates the bounded raw document, excluding only the MAC value in this
fixed trailer, before materializing the typed plan. It then checks a canonical
typed round-trip. This ordering avoids memory amplification from compact
unauthenticated arrays of empty candidate/evidence objects.

Cancellation detected before filesystem work creates no state. After that
boundary, checks occur between phases, not inside synchronous OS calls:
private initialization can remain and in-flight publication can complete.
Do not roll back shared keys/directories or immutable files on cancellation.

Task 7 also implements `fssecure.EnsurePrivateDirectory`,
`fssecure.WritePrivateFile`, and bounded read-only
`fssecure.ReadPrivateFile(path string, maximumBytes int64) ([]byte, error)`.
Unix uses `0700` directories and `0600` files.
Windows uses a protected DACL granting full control only to the current user
and `SYSTEM`, established at creation rather than after exposing plaintext.
The implementation reuses the existing `golang.org/x/sys` v0.41.0 dependency;
no version upgrade is required for this slice. Reads verify ownership and
privacy without repairing permissions and reject symlinks/reparse points,
nonregular files, and unverifiable security. External plan files do not
require private parent directories. Directory hardening applies only to the
requested directory and newly created components, not unrelated existing
ancestors. These controls protect against other ordinary local users, not
administrators or malicious processes running as the same user.

macOS storage is restricted to local APFS/HFS with ownership enabled and no
nonempty extended ACLs; unsafe ACLs are rejected, not silently removed.
Windows requires persistent ACL support. Publication also requires hard-link
support on either system. Abrupt process termination can leave private
staging files; automatic staging recovery is outside this slice.

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

Task 7C execution details:

- Builder requests contain explicit policy settings and apply mode. Invalid
  requests and cancellation return no usable plan. Collection failures instead
  produce an inspectable, blocked plan and an error; the CLI saves and prints
  that plan but exits unsuccessfully. Collection diagnostics never enable a
  removal action. The core-only adapter limitation remains visible.
- The process collector marks proven-local failures with a direct
  `*process.WorktreeError` and the affected input worktree paths. Its string
  diagnostics remain an ordered projection of returned errors. The builder
  checks that correspondence and scope, not message similarity, before keeping
  a failure local. Enumeration, containment, unbound uncertainty, inconsistent
  diagnostics, and errors without proven scope remain globally blocking.
  Binding keys outside the inventory, invalid process PIDs, and conflicting
  records for a positive PID block globally even without an `Uninspectable`
  entry. Independent per-worktree path-failure sentinels remain local.
- Candidate IDs are stable for a worktree identity; plan IDs combine canonical
  content with fresh cryptographic randomness. Fingerprints include the final
  decision, action, and snapshot requirements. Only safe candidates default to
  `remove`, with a required snapshot; all other actions are `none`.
- Default `explain` selects by authenticated generation time, not filesystem
  timestamps, with plan ID as a deterministic tie breaker. Only otherwise-valid
  authenticated expired plans are skipped. Filename/ID mismatches and invalid
  generation windows block selection even after expiry. Malformed,
  inaccessible, or unauthenticated documents stop selection rather than
  silently falling back. It never falls back to an older plan merely to find
  the requested candidate.
- Explicit `explain --plan` does not collect Git/process state or re-evaluate
  current configuration. It displays the authenticated plan's recorded reasons
  and evidence. Missing state is an error, not a reason to initialize storage.
- Explicit file, state, and export paths resolve existing ancestors before
  cleaning parent traversal; relative inputs start at the physical working
  directory rather than a logical `PWD` alias. Unresolvable traversal fails
  closed; final-object symlinks remain rejected. Final CLI errors are escaped
  at the stderr output boundary, including wrapped filesystem errors.
- CLI inputs share one physical working-directory anchor, including explicit
  configuration files. Windows rooted paths use that directory's volume;
  same-drive relative paths use its directory, without cleaning their suffix.
  Other-drive relative paths fail closed and require an absolute path rather
  than guessing another drive's current directory.
- Human explanation sections escape non-printing Unicode, including bidi and
  C1 controls, while retaining readable JSON layout. Their decoded data and
  machine JSON output preserve the complete recorded values.
- Exports are independent private copies with exclusive publication, not hard
  links to the canonical plan and not overwrite operations. Existing export
  parent permissions are preserved; only newly created parent directories are
  private. An export/output failure does not delete an already saved plan.
  Export staging stays inside private state rather than in a potentially
  shared destination directory. State and export must be on the same
  filesystem for exclusive hard-link publication; cross-filesystem exports
  fail closed without falling back to an unsafe copy.
- Verification uses the existing temporary Git fixtures, synthetic process
  sources, injected clocks and native CI; no new harness framework or real
  workspace smoke test is added.

- [x] **Step 4: Run targeted, full, and command smoke tests**

Run:

```bash
gofmt -w internal/plan internal/cli
go test ./internal/plan ./internal/cli
go test ./...
GOOS=windows GOARCH=amd64 go test -c ./internal/fssecure
rm -f fssecure.test.exe
go test -count=1 ./tests/e2e
```

Expected: tests PASS. Isolated command fixtures write a plan, classify the
primary worktree as protected, and perform no removal. Never run the smoke
test against a real developer workspace. Native macOS/Windows CI, not the
supplementary cross-build, supplies platform verification.

- [x] **Step 5: Commit plans and read-only CLI**

```bash
git add internal/plan internal/fssecure internal/cli
git commit -m "feat: create expiring cleanup plans"
```

### Task 7D: Concurrent private-state creation correction

PR #5 was reviewed with no remaining actionable findings and both native CI
jobs passed at its final head. Its identical-tree merge commit then exposed
a timing-dependent Windows failure in `TestEnsurePrivateDirectoryConcurrentCreators`.
The same failure reproduces locally under repetition: an ancestor is missing
during symlink resolution but another creator publishes the real directory
before the follow-up `Lstat`. Returning the stale not-found error is incorrect.

Revalidate that newly visible real directory once through the normal ancestor
resolver. This is a bounded retry, not a blanket retry of inaccessible paths,
dangling links or conflicting objects. Preserve physical path resolution,
private ownership/modes/ACLs, ancestor security and exclusive publication.

The existing ordinary concurrent-creator test runs 32 bounded rounds; no
custom filesystem harness or acceptance framework is introduced. Record the
native failure, local RED/GREEN stress checks and final native CI in the
corrective PR, then obtain independent review and merge before Task 8.

Delivered in PR #6, merge commit `15a70d15632373bcd34719094015378d9d081b56`.
Both final-head native CI `34768564097` and post-merge native CI `34769218044`
passed. Independent AI review and Copilot review had no remaining actionable
findings; these are not human approval.

## Task 8: Create private, verified recovery snapshots

### Task 8A execution slice: pure integrity foundation

This slice provides `snapshot.ValidateManifest`, `EncodeManifest`,
`DecodeManifest`, `VerifyPayloads`, and `plan.EvidenceDigest`. It introduces no
capture, filesystem/Git mutation, receipt, apply or restore API. Keep the full
Task 8 steps below pending until the remaining snapshot lifecycle is delivered.

The manifest schema below includes `ToolVersion`, `PlanSchemaVersion`,
`AdminDir`, and `AdministrativeEntries`, matching `internal/snapshot/manifest.go`.
The snapshot and plan schemas are independently fixed at version 1.
Administrative entries record relative slash paths, file/directory kinds, original modes and raw
diagnostic bytes, including an explicit root and nonempty `HEAD`, `commondir`
and `gitdir` files. An index, when present, is recorded as raw file bytes.
Administrative records are diagnostic only and must never be replayed into
live Git metadata. Original permission bits include setuid, setgid and sticky;
these do not authorize using those permissions for private snapshot storage.
In particular, shared Git repositories legitimately generate setgid
administrative directories.

Encoding normalizes time to UTC and sorts entries without mutating the caller.
Decoding requires the exact canonical encoding and rejects unknown, duplicate,
case-aliased, malformed UTF-8 and noncanonical JSON. Bounds are 32 MiB per encoded
document, 4,096 administrative entries and 16 MiB aggregate administrative
bytes. A token preflight bounds collections before typed slice/map expansion
(32 fields per object, 4,096 entries per array, eight collection levels and
32,768 total JSON values, including containers). Base64 diagnostic byte counts
are also bounded before typed expansion, including case-aliased data fields.
Identifiers and printable ASCII tool versions are bounded to 128 bytes,
branches to 1,024 bytes, absolute identity paths to 32 KiB and administrative
relative paths to 4,096 bytes. These serialization bounds do not replace the
configured payload-size or sensitivity policy.

`pathutil.ValidateAbsoluteForm` checks only the current platform's canonical
absolute syntax using the existing path preparation rules, with no filesystem
access. It cannot prove repository identity, existence or symlink resolution.
Administrative paths reject traversal, nonportable names, duplicate and
case-folded conflicting identities, missing parents and incompatible modes.
Physical identity and coherent Git/admin capture remain later responsibilities.
Branches are short branch names, including a legitimate `@` branch. Future
Git consumers must use its qualified `refs/heads/@` identity, not confuse the
short name with revision shorthand for the current HEAD.

The full evidence digest covers every stored evidence field, including
planning-only agents, observation times and complete diagnostics. It reuses
the signed plan's UTC-normalized JSON representation, preserving collection
order, duplicates and existing nil/empty semantics, rather than the narrower
removal fingerprint. Existing signed-plan bytes remain unchanged.

Payload verification requires exactly `worktree-list.bin`, `status.bin`,
`staged.patch`, `unstaged.patch` and `untracked.tar.gz`, with matching SHA-256
hashes. It does not parse Git output or archives. Canonical decoding and payload
hashes do not authenticate a manifest or establish a complete usable snapshot;
a later receipt must commit to the complete manifest bytes, including metadata.
Missing policy, adapter-lock, candidate or evidence digests fail closed. Do not
invent adapter provenance: current core-only plans remain inspection-only.

The existing ordinary Go tests, isolated `internal/testutil` fixtures and native
macOS/Windows CI suffice. This pure slice uses in-memory synthetic values,
isolated Git compatibility fixtures and ordinary regression/fuzz tests, not a
new acceptance or harness framework.
Independent review and final-head native CI are required before merge.

Still required before Task 9: administrative reads, coherent Git/admin before/after
revalidation, safe untracked archives with modes and symlinks, sensitive and
oversize preflight, detached recovery refs, private fsync/atomic publication,
receipt commitments, repository/branch/target checks and byte-for-byte restore.

### Task 8B execution slice: narrow raw Git reads

This prerequisite extends the existing guarded `git.Client`; it does not add a
generic command API, snapshot capture, receipt, publication, restore or apply.

```go
func (client *Client) ListWorktreesRaw(ctx context.Context, repository string) ([]domain.Worktree, []byte, error)
func (client *Client) StatusRaw(ctx context.Context, worktree string) (domain.GitStatus, []byte, error)
```

Each method returns parsed values and exact porcelain bytes from the same
single collection. Existing `ListWorktrees` and `Status` retain their signatures
and delegate to that path without duplicate Git commands. Preserve NULs,
whitespace and raw pathname bytes without trimming, normalization or re-encoding
of the raw payload. Parsed worktree paths retain the existing native-path
conversion. Any guard, command, parser or common-directory lookup failure
returns no raw bytes or partial parsed result. `Diff` similarly discards partial
stdout on failure while preserving successful staged/unstaged binary patches.

Keep shell-free argv, sanitized environment, bounded output and timeout,
offline reads, executable-filter and unsafe-index rejection, safe diff flags,
and primary/bare inventory rules. An expected quiet exit-one from the filter
lookup or detached-HEAD lookup must have empty output, including diagnostic
stderr carried by a native exit error, and a matching native process exit or
explicit status reported without a runner error. Transport,
wait-delay, joined, unknown or conflicting failures remain errors; an exit
code alone must not authorize continuing collection.
Validate status mode/object-ID metadata and
worktree HEAD object-ID and supported short-branch syntax before exposing
parsed/raw results. Unknown or malformed record forms fail closed. Preserve
legitimate SHA-1/SHA-256 widths,
zero IDs for absent/unborn state, rename/copy records, and unmerged stage
metadata; the snapshot manifest separately requires a known nonzero HEAD.
These reads do not prove physical repository identity or capture coherence.
Mode fields accept canonical Git encodings; object IDs accept ASCII hex with a
consistent width within each status record. Rename scores use canonical decimal
0–100 and must agree with the record's rename/copy status. Known ordinary `DA`
and `DD`, worktree-side renames/copies, and absent-stage modes remain supported.
Only the final worktree-side mode field may contain `040000`; stored HEAD,
index and unmerged-stage mode fields reject it. Native macOS Git can emit it for
an accessible embedded repository replacing an indexed regular file when an ACL
grants access despite zero POSIX permission bits. It appears in both ordinary
and unmerged records without a sparse index. This syntactic compatibility does
not establish that an embedded repository is a safe cleanup target.
The porcelain parser and snapshot manifest share `gitref.ValidBranchName`:
bounded (1,024-byte), UTF-8, literal short branch names, including `@`, with no
checkout-expression expansion. This is the existing snapshot-supported profile,
not a claim to accept every lower-level Git reference spelling. In particular,
short-branch rules are stricter than validating an arbitrary full ref.

Harness assessment: existing `runnerFunc`, `internal/testutil` temporary Git
fixtures, standard Go unit/fuzz tests and native macOS/Windows CI suffice.
No new framework, dependency, real workspace, agent database or scheduler job is
needed. The fixture `Repository.Git` trims surrounding whitespace, so exact-byte
tests capture the real runner's output rather than treating that helper's return
value as a universally exact raw oracle.
Native temporary fixtures cover SHA-1 and SHA-256 repositories, staged/unstaged
binary patches, rename source paths, unmerged stage metadata, unborn/intent-to-add
state and unchanged index bytes. Pure fuzz tests exercise both porcelain parsers.
The unmerged fixture table names each conflict code and its stage-presence mask
explicitly. This clarifies the already-correct mapping; it does not change the
effective stage combinations previously tested.

- [x] Write failing raw-byte, failure-output and malformed-protocol regressions.
- [x] Implement shared guarded reads and the relevant parser checks.
- [x] Verify binary patches, rename and unmerged metadata in isolated Git fixtures.
- [x] Run full normal/race/vet/build/format checks and native macOS/Windows CI on
  implementation head `64a4f84` (CI run `34776255641`).
- [x] Close the review follow-up on unsupported porcelain branch names using
  the shared snapshot-supported profile and raw-boundary regressions.
- [x] Reproduce and correct exit-one execution-error handling and misplaced
  directory-mode acceptance, preserving native quiet exits and worktree modes.
- [x] Repeat independent review until clean, verify the final revised head on
  native macOS/Windows CI, then merge and verify the merge commit.

Delivered in PR #8, merge commit `34c6d8a11c1b2341aa44627f03ab3831bfec6ef0`.
Final-head native CI `34782004567` and post-merge native CI `34782409173` passed.
Independent AI review `5192229370` passed spec and quality with no remaining
actionable findings. The final external AI review had no concrete code findings
but retained a general recommendation for human review; no human approval is
claimed. The merged tree is identical to reviewed head `dd8e670`.

Remaining Task 8 lifecycle work listed above stays pending; Task 9 is not ready.

### Task 8C execution slice: bounded administrative diagnostic reads

This slice consumes the existing `AdminEntry` schema and adds only:

```go
func ReadAdministrative(ctx context.Context, directory string) ([]AdminEntry, error)
```

The reader accepts an existing absolute administrative directory, returns
deterministically ordered caller-owned diagnostic entries, and performs no
Git command, filesystem mutation, permission repair or publication. Preserve
the existing Git fingerprint serialization, manifest encoding and schema.
Absolute root inputs retain the manifest's 32 KiB byte budget and canonical
form; relative entry paths retain their 4,096-byte budget. Native filesystem
path/component limits can still reject a lexically supported path. On Windows,
canonical drive/UNC input is converted to extended spelling only for the native
metadata open; raw extended spelling is not a new public input format.
A separate reader avoids changing persisted plan fingerprints; implementing
the full snapshot lifecycle here would make the safety boundary too broad.

Retain exact file bytes and original permission bits, including setuid, setgid
and sticky. Include an explicit root and nonempty regular `HEAD`, `commondir`
and `gitdir`; an index is optional and may be empty. Directories have nil data.
Use the existing portable path, mode and required-entry validators, 4,096-entry
limit including root, and 16 MiB aggregate raw-byte limit. Reserve entry capacity
when names are enumerated, not just visited; bound directory batches, pending
names and growing files before collecting excessive content. Reject aliases,
nonportable names, traversal and malformed single-component names before
opening them, rather than normalizing conflicting evidence away.

Anchor descendant access to opened roots/directories. Reject symlinks, Windows
reparse records and unsupported object types or modes. Compare observed and
opened identities before reading; check each file before and after reading and
revalidate every observed entry and the original root path before returning.
Capture Windows root identity through a no-follow metadata handle: path-based
`os.Lstat` can defer file-ID lookup until after a replacement. A native Windows
same-mode/size/time replacement test must therefore exercise pinned identity.
Check native reparse and device attributes even if `FileMode` looks regular.

Prevent Unix file/directory-to-FIFO substitution from blocking open. Plain
`os.OpenRoot` is not sufficient on the selected toolchain; verified directory
acquisition and nonblocking file opens, or equally strong native primitives,
must cover this boundary. Propagate all open/stat/readdir/read/close failures
exposed by the Go APIs, cancellation and changed observations with nil partial
output. In Go 1.26.5 a trailing slash in `Root.OpenRoot` causes a precheck but
does not protect its subsequent Unix open against FIFO replacement. A terminal
dot makes the mutable directory an intermediate `O_DIRECTORY` open and opens
the final dot relative to that acquired directory. A native Darwin atomic
directory/FIFO exchange regression covers this late race, beyond the earlier
before-call substitution tests. Require both successful exchanges and directory
opens in each stress test. A separate absolute-root race regression covers the
direct `os.OpenRoot` path: unlike `Root.OpenRoot`, it forwards its trailing slash
unchanged to the Unix open syscall, which requires a directory atomically.
Keep successful administrative-read integration tests limited to the supported
reader platforms; unsupported targets retain their explicit error contract.
Check context around filesystem operations
and bounded reads; synchronous OS calls are not
claimed preemptibly cancellable. Rejecting file growth can read one extra
detection byte beyond the aggregate allowance, but cannot return partial
output. Close every acquired handle through its Go API on all paths; errors
hidden by the standard library cannot be surfaced. In particular, Go 1.26.5's
Unix `os.Root.Close` discards its underlying native close return value.

These object checks do not authenticate the supplied Git relationship, match a
candidate fingerprint, prove cross-payload coherence, forbid mounts/hard links,
or defeat same-user ABA/content changes with restored metadata. Administrative
records remain diagnostic only and must never be replayed into live Git
metadata. Complete capture coordination, archives, sensitivity/size policy,
recovery refs, private publication, receipts and restore remain later work.

Harness assessment: ordinary temporary filesystem fixtures, narrow per-call
injection around real handles, and `internal/testutil` Git fixtures suffice.
No new framework, dependency, real workspace/database/scheduler test, global
cwd/environment/config mutation or process enumeration is needed. The existing
native macOS/Windows matrix must exercise the final head and merge commit.

- [x] Write failing exact-byte/ownership Git integration and focused unsafe-read,
  replacement, I/O-failure, cancellation, entry/byte-limit and growth tests.
- [x] Implement the bounded read-only collector and platform identity checks.
- [x] Verify genuine Git index/unmerged metadata and shared original modes
  without altering fixture bytes or permissions.
- [x] Run full local normal/race/vet/build/format/diff checks.
- [x] Repeat independent review until clean, verify final-head native macOS/
  Windows CI, merge, and verify native CI on the merge commit.

### Task 8D execution slice: bounded untracked archive codec

**Goal:** Give later capture and restore a tested `untracked.tar.gz` format
without adding filesystem collection, publication, extraction or cleanup.
Use standard `archive/tar` and `compress/gzip`; preserve the existing manifest,
payload names and Git fingerprint formats.

**Files:** New `internal/snapshot/untracked*.go` production/focused test files
and `internal/snapshot/archive_*_test.go` integration fixtures; this plan remains the
execution record. No dependency or development acceptance framework is needed.

**Interfaces:**

```go
type UntrackedEntry struct {
	Path       string
	Kind       string
	Mode       fs.FileMode
	Data       []byte
	LinkTarget string
}

func EncodeUntracked(entries []UntrackedEntry, maximumBytes int64) ([]byte, error)
func DecodeUntracked(contents []byte, maximumBytes int64) ([]UntrackedEntry, error)
```

The positive, representable byte budget bounds both compressed bytes and the
entire expanded tar stream, including headers and padding. Bound work during
allocation and decompression, not after materializing hostile declared sizes.
An expansion rejection may consume one extra detection byte. Limit entries to
4,096, including directories and symlinks, and paths/link text to 4,096 bytes.
Return nil output on any failure; use `ErrUntrackedInvalid` for invalid input/
format/profile and `ErrUntrackedLimit` for capacity breaches. Empty entries
encode a real empty archive; empty encoded bytes are invalid.

The required Go 1.26.5 tar reader also caps special metadata at 1 MiB. That
standard-library capacity breach is `ErrUntrackedLimit`, retaining
`tar.ErrFieldTooLong` as a wrapped cause, even below the caller's larger byte
budget. Malformed numeric fields and unsupported header formats instead return
`ErrUntrackedInvalid`; they are not conflated with that metadata capacity.

Entry paths use the existing portable lexical profile and case-fold collision
checks. Reject root/absolute/traversal paths, backslashes, reserved/control
names, `.git` components and duplicates. Explicit file/symlink ancestors of
other entries are invalid; missing directories may be implicit parents.
Retain original paths and permission bits, including setuid/setgid/sticky.
Kinds are regular `file`, `directory` and `symlink`, with matching modes.
Directories have nil data; files have no link target; symlinks have nil data
and nonempty relative link text resolving lexically within the archive root,
excluding `.git`. Preserve that original link text. Raw `.`/`..` components
in link text are accepted only when the complete lexical target remains in
the root. Reject unsupported types, mode bits and conflicting fields.

Encoding is deterministic across input permutations, uses fixed owner/time
metadata without machine/user names, and supports standard USTAR/PAX long and
Unicode names. Decode standard-library-defined effective entries, rejecting
hardlinks, devices, FIFOs, sparse/global metadata and unsupported extensions.
This is not a generic archive extractor. Return sorted caller-owned entries
without modifying input slices or retaining aliases into encoded input.

Require one complete gzip member, verified checksum/trailer and no appended
members or garbage; closing a gzip reader alone is insufficient. Require an
aligned tar stream and its two zero end blocks, not a parseable prefix.
Reject nonzero final-entry alignment padding, hidden second archives and
nonzero trailing data; ordinary all-zero tar padding can be accepted within
the same budget. Bounded decompression before
`tar.Reader` is sufficient; do not introduce a custom tar parser.

These are structural checks, not Git-ignore/sensitivity decisions, native
filesystem identity/containment proof or authenticated capture. The codec does
not silently filter `.env` files or select paths; later collection/policy must
do that before apply. The future manager must enforce the plan's whole-snapshot
budget as well. No publication, recovery ref, receipt, restoration or mutation
API is added. Task9 remains blocked on the remaining snapshot lifecycle.

Harness assessment: ordinary Go table tests, standard tar/gzip fixture writers,
bounded fuzz seeds and an `internal/testutil` temporary Git fixture suffice.
The integration fixture composes the codec with the existing manifest/hash
validators and checks original bytes/modes remain unchanged; it does not claim
production path selection or cross-payload coherence. No actual workspace,
database, scheduler, process enumeration, global environment/cwd/config change
or generated fixture is needed. The existing optional Task8A fuzz timeout is
unresolved and is not attributed to or claimed fixed by this new codec.

- [x] Assess scope, existing harness and the clean merged baseline.
- [x] Write and observe failing codec and real-Git composition tests.

```go
func TestUntrackedCodecRoundTrip(test *testing.T) {
	entries := []UntrackedEntry{
		{Path: "binary.bin", Kind: "file", Mode: 0o600, Data: []byte{0, 1, 0xff}},
		{Path: "folder", Kind: "directory", Mode: fs.ModeDir | 0o750},
		{Path: "folder/link", Kind: "symlink", Mode: fs.ModeSymlink | 0o777, LinkTarget: "../binary.bin"},
	}
	encoded, err := EncodeUntracked(entries, 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	decoded, err := DecodeUntracked(encoded, 1<<20)
	if err != nil || !reflect.DeepEqual(decoded, entries) {
		test.Fatalf("archive round trip: %#v, %v", decoded, err)
	}
}
```

- [x] Implement the bounded codec and cover deterministic ordering, ownership,
  special modes, long/Unicode names, unsafe paths/links/types, collisions,
  truncation, checksum/trailer errors, appended data and entry/byte boundaries.
- [x] Run `go test -count=1 ./...`, `go test -race -count=1 ./...`,
  `go vet ./...`, `go build ./...`, `gofmt -l .` and `git diff --check`.

Local implementation evidence: missing-API failures were followed by intended
assertion failures against explicit unimplemented placeholders, then passing
codec and real-Git integration tests. A first implementation edit's syntax
failure was recorded separately, not as behavioral RED. Initial head `1e829e2`
passed 19 focused tests and 24 ordinary bounded fuzz seeds in normal/race
execution. Additional seed
and limiter coverage was added after the first GREEN, not claimed as fresh RED.
Both Git composition and native Unix special-mode/link fixtures pass; their
three-repeat race run also passes. On local Go 1.26.5 darwin/arm64, all required
full-repository commands pass, with empty formatting and diff-check output.
These are local results, not native Windows evidence or independent approval.
No timed fuzz run was performed, and the earlier Task8A timeout is unresolved.

PR #10 review coverage follow-up: a global-tar-header concern was checked
against the unchanged implementation and Go 1.26.5's standard reader. Twelve
fixtures with empty, owner or path global metadata before, between, after or
repeated among valid files are rejected with nil output. A permanent regression
test also asserts that the standard reader exposes each global header. This
adds coverage for an already-rejected input, not a new behavioral fix or RED
claim, and does not introduce a raw tar parser. That follow-up head `1ad668f`
has 20 focused codec tests, not 19, plus the same 24 bounded fuzz seeds.

A subsequent review's suppressed findings exposed a final-file alignment
padding gap: bytes skipped by `tar.Reader` preceded the codec's zero-tail
check. Sixteen USTAR/PAX mutations failed their intended rejection assertions
because the decoder returned entries with no error. After the one-line framing
correction, all are rejected with a classified error and nil entries. Valid
zero padding still passes. This is a strict trailing-framing fix, not
authenticated capture or a custom tar parser.

The parser-error finding was independently checked against the required Go
implementation and bounded fixtures. Malformed numeric fields use
`tar.ErrHeader` or an unsupported format and already return
`ErrUntrackedInvalid`. Valid PAX owner metadata at the 1 MiB boundary succeeds;
one byte beyond that standard-library cap returns `tar.ErrFieldTooLong` and
`ErrUntrackedLimit` below the caller's larger budget. The existing mapping is
retained, with permanent positive/capacity/malformed regression coverage and
no new behavioral RED claim. That follow-up head `5490f4f` contains 22 focused
codec test functions plus 24 bounded fuzz seeds.

The next review identified repeated `path.Dir`/`path.Clean` scans in ancestor
validation. A fixed corpus of 4,096 entries with 4,095-byte paths exceeded a
five-second diagnostic timeout, with the stack in that ancestor walk. This is
a reproduced performance failure, not an assertion-level functional RED or
the unrelated unresolved Task8A fuzz timeout. Ancestor validation now searches
the sorted folded paths for each explicit entry's first descendant, retaining
the directory and original-spelling checks without rescanning every implicit
parent. The same diagnostic command passes in 2.050 seconds locally; the
permanent test has no machine-specific timing assertion. Ordinary one-iteration
Go benchmarks for 64 entries at depth 2,043 measured 384.4 ms before and 10.9 ms
after; these are local observations, not statistical or cross-platform claims.
The suite now contains 23 focused codec tests plus 24 bounded fuzz seeds. Every
update still requires full local verification, independent re-review and native
CI before merge.

- [x] Create a Task8D PR, repeat independent review until clean, and verify
  native macOS/Windows CI on its final head before the authorized merge.
- [x] Verify native macOS/Windows CI on the merge commit before the next slice.

PR #10's final independent AI full-range review at `6140789` passed the spec
and code-quality reviews with zero actionable findings. Copilot's final round
also raised no concrete code finding, while recommending human review; neither
automated review is human approval. Native macOS/Windows CI passed on the final
head (run `34793061385`) and the authorized merge `74a9b65` (run `34794514333`).
Reviewed-head ancestry and exact merged-tree equality were verified. No force
push, protection bypass, release, branch/worktree removal or separate main
checkout mutation was performed.

### Task 8E execution slice: bounded untracked source reads

This slice composes Task8D with a narrow native read-only source boundary. The
caller supplies an already approved list of relative leaf paths; this primitive
does not discover Git-untracked paths, decide ignore/sensitivity policy, or
authenticate the selection. No snapshot publication, extraction or mutation
command is exposed. The remaining Task8 lifecycle and Task9 stay pending.

**Files:**
- Create: `internal/snapshot/read_untracked.go`
- Create: `internal/snapshot/read_untracked_selection.go`
- Create: `internal/snapshot/read_untracked_data.go`
- Create: `internal/snapshot/read_untracked_observation.go`
- Create: focused `internal/snapshot/read_untracked*_test.go` fixtures
- Create: `internal/snapshot/untracked_read_integration_test.go`
- Create: `internal/snapshot/untracked_read_native_unix_test.go`
- Create: `internal/snapshot/untracked_read_contract_test.go`

**Interface:**

```go
func ReadUntracked(ctx context.Context, directory string, paths []string, maximumBytes int64) ([]UntrackedEntry, error)
```

Validate the entire request before source operations. Require an absolute root
in the existing canonical lexical profile, at most 32 KiB of root text, and
the codec's portable relative paths, 4,096-byte text cap and positive safely
representable byte budget. Reject root/absolute/traversal names, `.git`
components, aliases, duplicates and requested leaf ancestors in any input
order. A requested directory is invalid; do not recursively enumerate it or
read unrequested siblings. Precompute and bound the union of requested leaves
and their required parent directories at 4,096 output entries before opening
the root. Do not repeatedly clean or hash every full implicit prefix; a bounded
component tree can reserve each distinct node before allocation. Copy input
slices rather than sorting or changing the caller's request.

Read regular-file bytes and supported link text, with original permission and
special bits. Add required parents with their actual modes; do not add the root
as an archive entry. Preserve supplied path spelling and original relative
link text, returning sorted caller-owned codec-compatible entries. Unix links
are read without dereferencing their targets and must satisfy the codec's
lexical link profile; dangling/self links can be structurally valid. Windows
reparse points, including symlinks/junctions, and native devices are explicitly
unsupported in this slice and fail closed. Supporting selected Windows link
tags requires a separately verified extension, not an assumed cross-build.

Reuse Task8C's verified native opening primitives without weakening them:
Unix nested roots use the terminal-dot guard, absolute roots retain the
trailing-slash guard, and Windows root identity uses no-follow metadata
handles. Hold rooted directory handles, compare opened objects with no-follow
observations before consuming bytes, check files/links around reads, then
revalidate every observed root/parent/leaf and the original root path before
success. An empty request still validates and revalidates its root. Reject
detected replacement, mode/time/size change, short/growing reads, unsupported
metadata and inaccessible/missing objects with nil output. Close every acquired
handle on every path; any exposed close failure invalidates the result.

Bound aggregate regular-file bytes before allocation and during reads, with
at most one extra growth-detection byte. Link text, names, entries and retained
handles have their own bounds. The raw-byte budget is distinct from the
codec's serialized tar/gzip budget and the future whole-snapshot budget; it is
not an exact heap quota. Native resource limits may reject a valid request;
do not raise process limits or fall back to unrooted reads. Check cancellation
around operations and bounded reads; synchronous OS calls are not claimed
preemptibly cancellable. Preserve native I/O/context causes. Use
`ErrUntrackedInvalid` for request/profile/observed-state failures and
`ErrUntrackedLimit` for capacity breaches. Go's hidden native close errors,
including the pinned Unix `os.Root.Close` behavior, cannot be surfaced.

These observations do not prove an atomic or coherent snapshot, native path-name
authentication, mount/hard-link exclusion or immunity to same-user ABA/content
changes with restored metadata. The reader does not silently filter `.env` or
infer Git eligibility. Selection/sensitivity policy and plan-bound capture must
be independently enforced before any future publication or cleanup.

Harness assessment: existing ordinary Go tooling, `internal/testutil` isolated
Git fixtures, rooted operation seams and handle-tracking patterns suffice.
Add deterministic I/O/cancellation/replacement/limit tests and native Unix and
Windows guards, not a custom gate framework, dependency or generated fixture.
Keep every test in owned temporary directories; never use real workspaces,
databases, scheduler jobs, process enumeration, or global cwd/environment/Git
configuration changes. The inherited optional Task8A timed-fuzz timeout remains
unexplained and unresolved.

Execution evidence on September 14, 2026: the clean merged baseline passed
before changes. The initial integration compilation failure was followed by
intended assertion failures against an explicit unimplemented placeholder:
two Git/codec/manifest integration tests, request/capacity/context contracts,
and five injected I/O boundaries. Production followed those observed failures.
The subsequent expanded fault matrix, Windows fixture-identity controls and
empty-input/file coverage first ran GREEN; they are additional coverage, not
additional RED evidence. Windows fixture identity now comes from an open
handle's `File.Stat`, with the close checked, rather than deferred pathname
identity lookup. This source-driven fixture correction is not a claimed
native Windows failure or fix verification.

The fresh local Go 1.26.5 darwin/arm64 runs passed `go test -count=1 ./...`,
`go test -race -count=1 ./...`, `go vet ./...` and `go build ./...`.
`gofmt -l .` and `git diff --check` printed nothing. A separate focused JSON
run passed 21 reader top-level tests and 109 subtests, with no failures or
skips; subtest counts include table containers. All artifacts remain local.
Native Windows and Linux execution, native CI, independent review and merge
verification remain pending at this execution checkpoint. The Windows
symlink fixtures explicitly skip only when the native runner lacks symlink
creation privilege; a cross-build would not establish native behavior.

The first native CI run (`34796863194`, implementation head `2ae302a`) failed
two Windows root-replacement fixture cases: `post-read` and `later-file`.
The fixture attempted to rename the root while the reader retained its opened
`nested` directory; native rename returned access denied before the intended
identity replacement. Windows root-replacement cases now select files directly
under the root, avoiding an opened descendant directory while retaining the
same pre/post-open, completed-read and later-file boundaries, mandatory actual
identity change, metadata equivalence, nil-result and handle-close assertions.
Unix still exercises the original nested-root layout. No case is skipped and
no production guard or existing helper is changed. Native verification of this
fixture correction was pending at that implementation checkpoint.

Corrected head `39292b3` passed native macOS/Windows CI run `34797241711`.
Independent review then identified a distinct test-oracle gap: the Windows
attribute-injection wrappers can be rejected by `os.SameFile` regardless of
their flags, masking omission of the new reader's native validator call.
Direct `validateUntrackedReadInfo` tests now use both native and wrapped safe
controls, plus reparse/device/combined/unavailable attributes while preserving
the same valid ordinary mode and size. No identity comparison can satisfy
these assertions. The original end-to-end no-byte/close checks are retained.
At that review checkpoint, native verification and independent re-review of
these controls were pending; no Windows guard-bypass experiment had run.

Subsequent native CI run `34798452057` on `ac06db5` passed both platforms,
including the direct Windows validator controls. A one-off Windows
`go test -overlay` diagnostic omitted only the native guard in a temporary
copy: all four unsafe-attribute cases failed, while the safe wrapped control
passed. The diagnostic required those exact outcomes and did not modify the
checked-out source. This is observed native counterfactual evidence, rather
than a source-only inference. Its temporary workflow step is removed before
merge; the ordinary CI workflow is restored, with no persistent custom gate.
The reader and test sources are unchanged by that removal. Native macOS/Windows
CI run `34798947080` passed on `b4690cd`. Independent AI task/spec and separate
whole-branch integration re-reviews of that revision also completed with no
actionable findings; these are not human approval. A later Copilot review
identified the stale verification-status wording corrected in this record.
Every subsequent revision must receive exact-head native macOS/Windows CI and
independent re-review, with all review bodies and threads checked, before the
authorized merge. Merge-commit native verification remains a separate
prerequisite for advancing to the next slice.

- [x] Verify the clean merged baseline and assess the existing harness.
- [x] Observe failing integration, request, capacity, context and sentinel-I/O
  assertions before implementation, then expand deterministic fault coverage.
- [x] Implement the bounded explicit-path reader and native fail-closed policy.
- [x] Verify codec/manifest composition, source preservation, native modes/links
  and no unrequested traversal; run all required local Go/hygiene commands.
- [x] Open a scoped PR, repeat independent review until clean, and verify native
  macOS/Windows CI on the exact final head before the authorized merge.
- [x] Verify native macOS/Windows CI on the merge commit before the next slice.

Task8E closure: PR #11 merged as `1586faab130efaf76f5f60bd6aacdabe8b76929e`
from reviewed head `98bb54c62458c2bf7a2bc56bd461907e2a9e82e7`. Both independent
AI task/spec and whole-branch reviews report no actionable findings at that
head; their separate CI addenda verify native run `34800086018`. Copilot's
final review produced no new inline comments; its human-review recommendation
is retained without claiming human approval. The stale plan-status thread
was fixed, answered with revision-bound evidence and resolved. The expected
merge parents, reviewed-head ancestry and complete tree equality were checked.
Native push/main CI `34800623095` passed macOS and Windows at the actual merge
commit, including normal/race tests, vet, build, formatting and unchanged-source
checks. Raw reports and logs remain local. All prior limitations remain,
including the unexplained optional Task8A timed-fuzz timeout.

### Task 8F: Exact status-derived untracked paths (scoped slice)

This slice supplies the explicit Git selection needed by Task8E. Prefer one
shared status observation over a second `ls-files --others` payload command or
ad-hoc record splitting in snapshot code. A rename/copy source is a separate
NUL-delimited path and may itself look like an untracked record; it must not
be mistaken for another entry. The existing strict parser remains authoritative.

**Files:**
- Create: `internal/git/status_snapshot.go`
- Modify: `internal/git/client.go`
- Modify: `internal/git/status_porcelain.go`
- Test: `internal/git/status_paths_test.go`
- Test: `internal/git/status_snapshot_test.go`
- Test: `internal/git/status_diagnostics_test.go`
- Test: `internal/git/status_diagnostics_integration_test.go`
- Test: `internal/git/status_snapshot_integration_test.go`
- Modify: `internal/git/porcelain_fuzz_test.go`
- Test: `internal/snapshot/status_selection_integration_test.go`

**Interface:**

```go
type StatusSnapshot struct {
	Status         domain.GitStatus
	Raw            []byte
	UntrackedPaths []string
}

func (client *Client) StatusSnapshot(ctx context.Context, worktree string) (StatusSnapshot, error)
```

The new API first reuses the existing effective-worktree-root check, rejecting
subdirectories and conflicting configured roots before collecting repository-
relative path evidence. This is one guarded read-only `rev-parse`, not another
status observation. Then reuse the same executable-filter and unsafe-index
guards and exactly one `status --porcelain=v2 -z --untracked-files=all
--ignore-submodules=none` invocation, with the existing environment, timeout
and 16 MiB per-command output budget. Root validation is a point-in-time
observation, not atomic source binding or pathname authentication.

Return the summary, unchanged raw bytes and only genuine `?` record paths from
that single payload. Preserve exact spelling, arbitrary path bytes and encounter
order; exclude tracked, ignored and rename/copy-source records. Do not sort,
deduplicate, normalize or silently apply sensitivity/portable-name filtering.
The new result owns its raw byte buffer and path slice; caller changes must
not affect another observation, retained runner output or another result field.

The retained-path mode accepts at most 4096 untracked records and fails closed
above that limit. Consume NUL records incrementally rather than allocating a
slice of every raw record before enforcing this bound. Legacy `Status`,
`StatusRaw` and the summary-only parser stay
source-compatible, retain their command guards and valid summary behavior,
and neither inherit the new root restriction/path-count limit nor allocate a
retained path slice. Share the strict parser and guarded status command path,
not a second parser. Any root/guard/command/framing/metadata/capacity error or
observed cancellation returns the complete zero result, preserving I/O and
context causes rather than exposing a partial summary, raw payload or selection.

An exit-zero status command with nonempty stderr is not a complete observation.
The shared status boundary must reject all such diagnostics, including unknown
or whitespace-only output, before exposing any stdout. Retain at most the first
4096 diagnostic bytes and preserve pre-existing command-error causes. Apply this
rule to `StatusSnapshot`, `StatusRaw` and `Status`, without changing the generic
runner's successful mutation behavior or warning-free legacy summaries.

Harness assessment: existing fake `execx.Runner` boundaries, strict parser
fixtures, `internal/testutil` temporary repositories and the normal Go/native
CI commands suffice. Add deterministic path/capacity/ownership/failure tests
and native Git-to-reader composition; no dependency, custom gate, new global
test state, real workspace/database/scheduler fixture or process enumeration.

- [x] Write and observe failing tests for exact path extraction, rename/copy
  source framing, late parse failures, zero-on-error/context, command guards,
  root mismatch, 4096/4097 boundaries, legacy compatibility and ownership.
- [x] Add isolated native Git fixtures for ignored/nested/untracked versus
  staged/unstaged/renamed paths, source/index preservation, root rejection,
  and composition with `ReadUntracked` and the existing archive codec.
- [x] Implement the shared parser/command path and narrow new result API.
- [x] Run `go test -count=1 ./...`, `go test -race -count=1 ./...`,
  `go vet ./...`, `go build ./...`, empty `gofmt -l .` and `git diff --check`.
- [x] Open a scoped PR, repeat independent reviews until no actionable
  findings, check complete remote review bodies/threads and require native
  macOS/Windows CI on the exact final head before the authorized merge.
- [x] Verify reviewed-head ancestry/tree identity and native macOS/Windows
  CI on the actual merge commit before advancing to another slice.

This API yields Git path evidence, not snapshot eligibility or a coherent
capture. Task8E still enforces its stricter portable paths, alias rules and
leaf-plus-parent capacity; 4096 leaves need not fit that component-tree limit.
Sensitivity policy, plan-bound revalidation, whole-snapshot budgeting,
publication, recovery refs, restore and all cleanup remain pending. Standard
fuzz seed tests do not resolve or diagnose Task8A's optional timed-fuzz timeout.

At the Task8F implementation checkpoint, an initial missing-API compile failure
was followed by assertion RED against explicit zero-result/selection stubs.
Path spelling/selection, capacity, command/guard failures, ownership, root I/O
and cancellation controls failed for the intended missing behavior; existing
legacy summaries and malformed-metadata rejection were already green. Separate
native macOS fixtures also reached six intended failing leaf invocations before
the new API was implemented. Their downstream reader/codec/source-preservation
assertions were subsequently exercised in the successful focused GREEN run.

Fresh Go 1.26.5 darwin/arm64 full tests passed (Git 13.166s, snapshot 10.502s),
as did the full race suite (Git 14.104s, snapshot 29.922s), vet and build.
Formatting and whitespace checks printed nothing. No native local Windows or
Linux execution or timed fuzz run is claimed. Keep subsequent CI/review results
bound to their actual revisions in the PR record; a historical checkpoint does
not waive exact-final-head or actual-merge-commit verification.

Independent whole-branch review of the initial Task8F head `4a18417` identified
one Important/P1 finding: successful status diagnostics were discarded, allowing
incomplete untracked enumeration to appear successful. Controller-run regression
tests reproduced that boundary failure for empty and partial stdout through all
three status APIs, including unrecognized/whitespace diagnostics and diagnostic
bounding. Warning-free empty observations, uncapped legacy summaries and existing
command-error cause preservation were already green. The initial passing native
CI did not resolve the finding; subsequent fixes, reviews and CI must remain
revision-bound in the PR record.

An independent tests-only worker also reproduced F1 with owned native macOS
repositories: denied directory and leaf access were positively established;
actual Git returned exit zero, diagnostics and either empty or partial stdout.
All three APIs reached the intended rejection assertions before the fix (six
assertion failures, no skips). The fixture restores permissions and checks source
bytes during cleanup. Windows explicitly skips this mode-based denial fixture;
deterministic runner diagnostics remain covered on all supported platforms.
No native Windows ACL-denial or Linux execution is claimed by this fixture.

The second task review of `be983eb` closed the runtime finding but identified
a Minor diagnostic-bound test-oracle gap. Exact error-byte comparisons now cover
4095, 4096, 4097 and 4101 diagnostic bytes. A temporary ordinary Go `-overlay`
probe with an intentionally wrong 4100-byte cap passed the old loose assertion
and failed the strengthened 4097/4101 cases; the real unchanged production code
passed all four boundaries and the native/runner diagnostic regressions. This
is test-oracle mutation evidence, not a newly discovered production cap defect.

PR #12 closed Task8F at reviewed head `6a5ac59de24bd070de0093377a755d1656bbf7dc`.
The final independent task and whole-branch reviews found no actionable
Critical, Important or Minor findings; both prior findings were closed. These
AI reviews and the bot reviews are not human approval. Native macOS/Windows
PR CI run `34805513224` passed with the expected base/head parents and complete
tree equality. The authorized merge `92f6523c5040a2f8b2df618050d6138be356c782`
then passed native macOS/Windows push CI `34806743917`; both native checkout
logs matched the actual merge SHA. Reviewed-head ancestry, merge parents and
tree identity were verified. Task8A's optional timed-fuzz timeout remains
unresolved and unwaived.

### Task 8G: Bounded whole-bundle verification (scoped slice)

Task8 requires verification and byte limits before publication or recovery.
The existing codecs and hash-only verifier deliberately expose separate
contracts: matching hashes alone do not establish a valid untracked archive.
Compose those boundaries now, rather than widening `VerifyPayloads` or coupling
source capture, sensitivity policy and filesystem publication prematurely.

**Files:**
- Create: `internal/snapshot/verify_bundle.go`
- Test: `internal/snapshot/verify_bundle_test.go`
- Test: `internal/snapshot/verify_bundle_integration_test.go`

**Interface:**

```go
func VerifyBundle(manifestContents []byte, payloads map[string][]byte, maximumBytes int64) error
```

Consume `DecodeManifest`, `VerifyPayloads` and `DecodeUntracked`; do not add a
second parser or tighten the legacy hash-only API. Require a positive safely
representable budget, then preflight the encoded manifest and all five required
payload byte lengths against that one budget before decoding, hashing or
decompressing. Use subtraction-based checks; reject missing and extra payloads.
Administrative bytes are counted through their encoded manifest representation,
including JSON/base64 overhead, not merely their decoded data lengths.

Preserve independent 32 MiB manifest, 16 MiB/4096-entry administrative and
4096-entry untracked codec limits, plus compressed/expanded tar and aggregate
file-data bounds. The whole serialized-byte budget is not a whole-heap quota.
Verify canonical manifest encoding, exact payload names and hashes, archive
validity, and manifest untracked accounting. Regular files and symlinks each
count as one leaf; directories are structural and do not increment
`UntrackedFiles`. `UntrackedBytes` counts regular-file data only. Symlink text
and tar headers still consume encoded/expanded archive budgets.

All failures match `ErrBundleInvalid`; invalid budgets and size/capacity failures
also match `ErrBundleLimit`. Preserve underlying manifest, payload and archive
errors for `errors.Is`. Return only an error, never mutate or retain inputs,
and perform no filesystem, Git, process or network operations. Success is not
a durable receipt, proof of source coherence, plan authentication, sensitivity
authorization, cleanup permission or protection from later caller mutation.

Harness assessment: existing ordinary Go tests, codec fixtures,
`internal/testutil` owned temporary repositories and native macOS/Windows CI
are sufficient. No framework, dependency, workflow or global-state changes.

- [x] Write unit tests and observe assertion RED for invalid budgets, the exact
  aggregate boundary and one-byte excess, each payload's contribution, encoded
  manifest/admin overhead, malformed/noncanonical manifests, missing/extra/
  corrupt payloads, invalid archives with recomputed hashes, leaf/data count
  mismatches, preserved underlying limits/errors and input immutability.
  Run `go test -count=1 ./internal/snapshot -run TestVerifyBundle -v`.
- [x] Implement the minimal pure wrapper and verify focused GREEN, including
  valid empty/file/directory/symlink bundles and independent expansion limits.
- [x] Add owned native Git/status/reader/archive/manifest composition and source
  preservation coverage. Prove malformed rehashed archive rejection separately
  from the unchanged successful hash-only control; exercise normal fuzz seeds.
- [x] Run `go test -count=1 ./...`, `go test -race -count=1 ./...`,
  `go vet ./...`, `go build ./...`, empty `gofmt -l .` and `git diff --check`.
- [x] Open a scoped PR, obtain independent task and whole-branch reviews, fix
  all actionable findings and repeat to clean; audit complete remote reviews
  and require exact-final-head native macOS/Windows CI before authorized merge.
- [x] Verify reviewed-head ancestry/tree identity and native macOS/Windows CI
  on the actual merge commit before advancing.

Plan-bound coherent capture, sensitivity preflight, private publication,
recovery refs, restore, receipts and cleanup remain pending. Standard fuzz seed
tests do not resolve or diagnose Task8A's optional timed-fuzz timeout.

At the implementation checkpoint, rejection assertions failed against a nil
stub before the wrapper was implemented. The encoded-administrative-overhead
fixture was corrected to retain required diagnostic files; a local ordinary
Go `-overlay` nil stub then reproduced that assertion failure without changing
production. Additional valid-gzip malformed/traversing tar and 4097-entry
rejections were also checked against the nil overlay. Valid and non-mutation
controls that already passed the stub are not claimed as behavioral RED.

The independent native integration worker's test arrived after implementation;
the same local overlay produced six intended rejection assertion failures,
while both valid controls and source-preservation cleanup passed. The actual
implementation subsequently passed all eight native leaf cases. Windows keeps
the portable composition and explicitly skips only the Unix symlink scenario.

Fresh controller Go 1.26.5 darwin/arm64 verification passed: focused bundle
tests (1.301s), `go test -count=1 ./...` (snapshot 10.861s),
`go test -race -count=1 ./...` (snapshot 31.917s), `go vet ./...` and
`go build ./...`. `gofmt -l .` and `git diff --check` printed nothing; source
hashes were unchanged. Ordinary suites exercised existing fuzz seeds, not a
timed fuzz campaign. Native Windows/Linux execution is not claimed locally;
final-head CI, independent reviews and actual-merge CI remain revision-bound
PR requirements, not satisfied by this historical checkpoint.

The first independent task review of `5c54990` found no production defect but
identified two Minor test-oracle gaps: an exact expanded-tar acceptance boundary
and present-but-empty opaque payloads. The controller reproduced both surviving
mutants against the original bundle suite. New standard tar/gzip boundary tests
reject a `maximumBytes-1` decoder mutation at the exact fit; ten positive
nil/empty-payload cases reject a length-as-presence mutation. Separate missing-key
and empty-gzip controls retain fail-closed behavior. The unchanged production
wrapper passes the strengthened focused suite (1.443s). These are regression
oracle corrections, not runtime defects; they still require exact-head re-review
and renewed native CI before merge.

### Remaining Task 8 lifecycle

Task8G closure: PR #13 merged reviewed head
`5fe8cb288898a31342b14d1053ef93e68c307f64` as
`d27094eddd453a8e083cf7278eaa43f06eac369f`. Both independent AI re-reviews
closed the two oracle findings with no further actionable findings. The
Copilot source-walk claim was disproved and its thread resolved; final Copilot
review had no new comments and retained its human-review recommendation.
These are not human approval. Native macOS/Windows runs `34809376066` and
`34810006969` passed on the reviewed PR checkout and actual merge respectively;
parents, reviewed-head ancestry and complete tree equality were verified.

A subsequent read-only AI boundary review of the identical merged tree found
no new actionable finding. Nine extra finite probes passed normally and under
race detection. A current-source, isolated-cache 20-second/four-worker manifest
fuzz run passed 1,111,337 executions on Go1.26.5 darwin/arm64. This is additional
bounded evidence, not a diagnosis or waiver of Task8A's original timed-fuzz
failure. The complete GitHub review/thread audit was clean. Raw inputs and
reports remain local. Source capture and the remaining lifecycle stay pending.

### Task 8H: Bounded read-only source capture (scoped slice)

Execute the existing Task8 capture requirement before coupling it to private
publication or restoration. Reuse the guarded Git, administrative and untracked
readers. Compare two bounded collections, including actual bytes and original
modes, rather than treating unchanged status counts as source stability.

**Files:**
- Create: `internal/snapshot/capture_source.go`
- Create: focused `internal/snapshot/capture_source_*.go` helpers only as needed
- Test: `internal/snapshot/capture_source_test.go`
- Test: `internal/snapshot/capture_source_integration_test.go`

Review-driven integration corrections also update `internal/git/client.go`,
add the bounded `internal/git/readonly_index.go` preflight and focused Git
regressions, and document the conservative unsupported-layout boundary in
`README.md`. Existing raw/status command-sequence fixtures retain their exact
guard assertions with the additional index-free lookups.

**Interface:**

```go
type SourceCapture struct {
	WorktreeList          []byte
	Status                git.StatusSnapshot
	StagedPatch           []byte
	UnstagedPatch         []byte
	AdministrativeEntries []AdminEntry
	UntrackedEntries      []UntrackedEntry
}

func CaptureSource(ctx context.Context, client *git.Client, expected domain.Worktree, maximumBytes int64) (SourceCapture, error)
```

The public boundary accepts the existing concrete Git client. Use a private
read-only interface and narrow injected file readers for deterministic tests;
do not introduce another Git parser, generic resource framework or mutation API.
The supplied worktree must be a known, path-safe, non-prunable linked worktree
with canonical physical identities and no collection errors. Require valid
HEAD/branch-or-detached identity, nonnegative status counts and valid index/admin
hashes. This capture API is not an eligibility decision: it can read a dirty,
locked or current linked worktree but never authorizes its removal.

Validate and observe repository, common-Git, worktree and administrative root
directory identities before collection. Require the exact supplied canonical
paths, a non-primary registered target, and an administrative directory strictly
inside its common Git directory. Retain native file identities across both
collections and recheck them and canonical paths before returning. A replaced,
aliased, inaccessible or conflicting root fails closed, even if the new path
contains identical bytes. Reuse guarded native opening primitives where needed.

Each collection performs the following bounded sequence:

1. Read `ListWorktreesRaw`; require a single exact matching target, matching
   primary repository/common identities and registered HEAD/branch/lock state.
2. Enrich that listed target with `InspectWorktree`; require known state,
   no errors and agreement with the supplied source identity, status, hashes
   and observed Git metadata. Do not trust a stale supplied record in place of
   fresh inspection. Resolve the target's effective common Git directory and
   verify its native identity against the primary common store before any
   index/content reads. Native aliases are not physically distinct stores.
   Bind primary administrative identity to that common store; a linked
   administrative directory must be an immediate worktree registration with
   reciprocal `.git`/`gitdir` evidence for the selected native worktree root.
   Read these pointers through bounded, checked native handles before hashing,
   and recheck routing after inspection's Git reads. Resolve relative pointer
   records against their owning directories in filesystem traversal order,
   before lexical normalization can remove a symlink/`..` component. Validate
   Git's literal backlink `/.git` suffix before resolving its parent; an alias
   file is not a substitute for that registration marker. Native-equivalent
   terminal/intermediate directory aliases are allowed, while pointer-file
   leaves remain nofollow. Missing, malformed or conflicting routing evidence
   does not authorize index/content reads.
3. Read `StatusSnapshot`; require agreement with the inspected status and its
   exact selected leaf count. Retain raw bytes and exact untracked paths.
4. Read guarded staged and unstaged binary patches, administrative diagnostics,
   and the explicitly selected untracked leaves/parents, in that order.
5. Preserve existing administrative and untracked validation/capacity limits;
   require the returned untracked leaves and structural parents to match the
   explicit Git selection. Recheck context and root identities.

Clone retained mutable values before invoking subsequent collectors so a
collector reusing its own buffers cannot erase evidence of a change. Compare
both complete raw worktree/status outputs, patches, selection, administrative
paths/kinds/modes/data, untracked paths/kinds/modes/data/link text and inspected
Git source fields. Differences fail closed; never retry until evidence happens
to agree. Return an owned first capture only after all comparisons succeed.
Cancellation or any error returns the complete zero `SourceCapture`, preserving
the original cause with `errors.Is`; no partial capture is usable.

Require a positive safely representable source-byte limit. Each collection
independently accounts, using subtraction, for all four raw Git byte sequences,
administrative file bytes, untracked regular-file bytes and symlink target text.
Check each result before advancing to the next collector. Fixed entry/path
limits still bound structural metadata. This source-content limit is not a
whole-heap quota or Task8G's encoded/expanded bundle limit; temporary collector
allocations and the second comparison copy are not described as fitting one
retained-copy budget. Existing per-command and per-codec bounds remain intact.

Expose `ErrSourceInvalid` for every failure, `ErrSourceChanged` for changed
observations, and `ErrSourceLimit` for invalid budgets or known byte/capacity
failures. Preserve context, filesystem, Git and existing codec error causes;
do not classify arbitrary Git errors by matching their text.

The shared Git boundary preflights each `ls-files`, `status` and `diff` dispatch
using an index-free administrative-directory lookup and bounded native name
enumeration. Any immediate `sharedindex.*` backing entry, including retained
remnants and case aliases, makes the layout unsupported until a non-mutating
split-index implementation exists. Do not read/rewrite the split index or
restore its timestamps. These preflights retain the existing non-atomic,
non-ABA-proof limits; they do not establish a filesystem transaction.

Matching bounded observations are not a filesystem transaction or proof against
unobserved change-and-revert (ABA) activity or a malicious same-user writer.
Later apply must still authenticate/revalidate the full plan and verify every
published snapshot before removal. Capture creates no archive, manifest,
recovery ref, file, receipt, process/provider invocation, network request or
cleanup action. Private publication, sensitivity preflight, restoration and
receipt/cleanup integration remain subsequent independently reviewed slices.

Harness assessment: reuse ordinary Go tests, injectable narrow readers/runners,
existing codecs, `internal/testutil` owned temporary repositories and native
macOS/Windows CI. No dependencies, global-state mutations, custom gates or
additional worktrees are needed.

- [x] Observe assertion RED for invalid identity/unknown state, changed roots,
  changed Git/raw/administrative/untracked evidence, aliased collector buffers,
  incomplete/extra selection, each byte contribution and exact limits,
  preserved errors/cancellation and zero results on every failure.
- [x] Implement the minimal read-only composition and verify focused GREEN.
- [x] Prove native Git composition, dirty binary/ignored/untracked inputs,
  supported native symlink behavior, deterministic mid-capture changes and
  unchanged source/index evidence using owned temporary repositories.
- [x] Run `go test -count=1 ./...`, `go test -race -count=1 ./...`,
  `go vet ./...`, `go build ./...`, empty `gofmt -l .` and `git diff --check`.
- [ ] Open a scoped PR, fix all actionable task/whole-branch review findings,
  repeat to clean, and audit all remote review bodies and threads.
- [ ] Require native macOS/Windows CI for the exact final head, use the
  authorized exact-head guarded merge, then verify actual-merge identity and
  native CI before advancing.

At the implementation checkpoint, the unit and native integration rejection
assertions failed against an explicit unimplemented stub before production
implementation. Missing-API compile failures are recorded separately and are
not behavioral RED. Two additional failing regressions exposed inconsistent
lock state and a duplicate primary path marked non-primary; both are fixed.
All 24 focused unit/native test groups subsequently passed normally (17.405s)
and under race detection (19.515s), including exact aggregate budget boundaries,
mutable-buffer ownership, all root open/stat/close failures, deterministic
mid-capture changes, preserved causes and complete zero failure results.

Controller Go1.26.5 darwin/arm64 verification passed `go test -count=1 ./...`
(snapshot 27.112s), `go test -race -count=1 ./...` (snapshot 52.656s),
`go vet ./...` and `go build ./...`. `gofmt -l .` and `git diff --check`
printed nothing; all Go source hashes were identical before and after the
matrix. Native Windows execution, independent reviews and final-head/actual-
merge CI remain pending revision-bound requirements. These tests neither
diagnose nor waive Task8A's historical optional timed-fuzz timeout. Source
capture remains read-only and does not establish an atomic filesystem snapshot,
sensitivity authorization, publication, a receipt or cleanup eligibility.

Initial-head CI `34833141007` passed on macOS but failed six Windows unit
fixtures: moving the repository/common root while descendant roots were pinned
returned access denied before a replacement occurred. Independent task review
identified this as a supported-platform harness blocker, not a production
acceptance of changed source data. Production pinning remains unchanged.

The revised fixtures prepare matching replacement directories outside all
observed roots before capture. They verify that an unpinned rename succeeds;
if Windows denies an ancestor move while capture handles are held, they require
an unchanged complete capture and native identity, no moved original, and a
successful rename after capture closes its handles. Other errors remain test
failures; successfully replaced roots must still produce `ErrSourceChanged`.
Separate substituted native observations exercise all four roots at all three
collection boundaries without depending on whether Windows allows the move.
All 12 rejection assertions fail when a local Go overlay disables the observed
identity comparison, despite equal size/mode/mtime; unchanged production passes.

The revised root tests passed locally (0.680s). The renewed full normal/race
matrices passed (snapshot 31.465s/56.267s), as did vet/build, empty formatting
output and diff checks, with identical Go/module hashes before and after.
These are local macOS results; the revised exact-head native Windows controls
and both independent re-reviews still require completion before merge.

The fixture-only head `c7491b8933a34061e7562bca3b44bc0eb8975eac` subsequently
passed native macOS/Windows CI `34834017678`; the task re-review found no new
fixture issue. Whole-branch AI integration review nevertheless found two
Important source-contract defects and one Minor error-category defect: the
effective common store could differ from the pinned primary store; native Git
split-index reads refreshed backing-file mtimes even on capture failure; and a
detected intra-inspection HEAD change lacked `ErrSourceChanged`. These findings
were reproduced on that exact head before fixes, independently of passing CI.
The controller's three original probes failed in 3.975s. Merge remains blocked
until all fixes receive revision-bound review and renewed native CI.

Durable regressions now cover common-directory conflicts before index reads,
HEAD/branch/attached-detached change causes, unchanged opaque read errors and
administrative mtime preservation, including split-index refusal. They failed
before the behavior changes. The identity correction compares native directory
identities and preserves legitimate case aliases; a self-review regression
first exposed and then corrected an over-strict lexical comparison. The shared
Git client emits a typed recognized-state-change cause, which capture maps to
`ErrSourceChanged` while preserving the original cause. The updated native
identity/cause/split-index regressions pass locally (Git 3.129s, snapshot 2.981s).
These focused results are not yet whole-matrix, independent re-review or native
Windows acceptance of the new Git-boundary changes.

The integrated R3 correction rejects immediate `sharedindex.*` backing entries
before every `ls-files`, `status` and `diff` dispatch. Native primary/linked
fixtures also introduce a split index after `ls-files` and prove that the next
dispatch refuses without further byte or mtime changes. Directory enumeration
uses bounded name-only batches and retained native identities. Explicit entry,
path and requested-read capacity failures expose `git.ErrIndexPreflightLimit`,
which capture maps to `ErrSourceLimit`; wrapped and canceled causes are retained,
and identical opaque error text is not reclassified. The source mapping's 24
selected typed-error assertions failed before the mapping and subsequently
passed, including the complete collector-error matrix (0.853s).

The first integrated normal suite exposed one remaining legacy status-selection
command-sequence expectation. Its focused assertion failed (0.586s), then passed
(0.695s) with the two required index-free preflights and the single-status-payload
assertion preserved. The renewed full `go test -count=1 ./...` passed (Git
17.884s, snapshot 33.450s); `go test -race -count=1 ./...` passed (Git 20.258s,
snapshot 58.702s). `go vet ./...` and `go build ./...` exited zero; `gofmt -l .`
and `git diff --check` printed nothing, with identical Go/module hashes before
and after. These are Go1.26.5 darwin/arm64 results. The retained-index-ID test
helper also now uses handle-based observations; its pre-fix lifetime regression
passed on Darwin, so no native Windows pre-fix RED is claimed. Independent R3
re-reviews and exact-head/actual-merge native CI remain required before advancing.

R3 head `aea68df2c0c80a5908bf9281f7262fcbaf16cb85` passed macOS CI, but run
`34838070056` failed 14 Windows fault-fixture leaf assertions: `File.Stat` on an
already closed handle reports `ERROR_INVALID_HANDLE`, not `fs.ErrClosed`.
The source snapshot suite passed on Windows (51.074s); its race/build steps
were not reached. The corrected test requires exactly one successful native
`Close` and an unusable post-close handle without assuming a platform-specific
`Stat` error. Production behavior is unchanged. A local overlay omitting the
close still fails the corrected assertion (0.478s); unmodified production
passes the focused fixture (0.491s). The renewed full normal/race matrices
pass (snapshot 33.995s/59.198s), as do vet/build, empty formatting/diff checks
and unchanged Go/module hashes. Final-revision independent review and native
macOS/Windows CI remain required; the failed run is not treated as acceptance.

R4 head `722631d` passed native macOS/Windows CI `34838794928`, bound to
tree `02f6399a8bce685257f2ca332e141e2249c5cfb5`. Independent AI review still
found actionable ordinary-index refresh, same-store administrative routing,
native Windows attribute, preflight-order and capacity-classification gaps.
These findings block merge regardless of the passing run. The R3 Windows
failure count is corrected to 14 leaf assertions (seven stages with two
cancellation variants); parent/package summaries are not additional cases.

The R5 test-first checkpoint disables `diff.autoRefreshIndex` in every guarded
Git invocation, including against repository configuration. Native
identical-content inode replacement was RED with default/true settings, then
GREEN for default/true/false settings with unchanged source/index bytes and
mtimes (5.719s). Inspection now preflights split-index names before hashing;
four primary/linked and present/missing-index cases were RED, then GREEN.
Legacy metadata byte/entry and strict untracked-selection limits now preserve
`git.ErrReadLimit`; native composition and typed/wrapped/canceled/opaque-error
controls pass (9.616s). Large legacy status summaries keep their complete
counts. Missing native evidence fails as invalid rather than changed; six
portable assertion failures now pass, including resource-lifetime controls.

Windows-specific native attribute tests are included before their production
fix so the missing REPARSE/DEVICE/shape checks can be observed on a native
Windows runner. Local cross-compilation is not native RED or acceptance.
This checkpoint is deliberately not merge-ready. Same-store administrative
routing, Windows correction, exact-final-tree independent re-review, both
native CI platforms, remote-thread disposition and actual-merge verification
remain pending. No source publication, cleanup or release is authorized by
this checkpoint's test results.

Checkpoint `43020de`, tree `94ab02ac00acffd3b11cc8cf849b3dd3a1644a43`, passed
the complete local Go matrix from an owned `git archive` export with identical
Go/module hashes. Native CI `34842184006` passed macOS and failed exactly ten
Windows attribute/shape leaf cases, on Go 1.26.5. Its checkout
`5ac85f385c36eb78acf8cdb8fe065e27b4f21efd` has that exact tree and parents
`d27094e` and `43020de`. This is actual Windows assertion RED, not inferred
from a cross-build, and the failed run is not acceptance.

The follow-up keeps the Windows regressions unchanged and validates a nonnil
native attribute structure, rejecting REPARSE/DEVICE bits before guarded
opens/enumeration and identity comparisons. Missing or wrong native evidence
is invalid, not an observed worktree change. Local scoped tests and race
controls pass (3.110s/4.325s); native Windows GREEN still requires new CI.

Administrative routing now checks primary/common and linked-registration
native identities and both pointer directions before hashes and after Git
reads. The pointer reader retains native handles, bounds content to 32 KiB,
checks native types and observations, and preserves read/close/cancellation
failures. Native tests reproduced same-store misrouting, six malformed or
unregistered layouts, and a persistent mid-inspection routing change before
the fix. Those eight rejection cases and three relative-pointer controls now
pass (5.118s); the broader routing/inspection controls pass (10.910s).
Additional source-capacity controls cover both initial metadata size and
streaming growth, exact/overflow directory counts and complete legacy status
summaries. These changes still require independent corrective review and
exact-final-tree native CI; no cleanup, publication or release is complete.

The integrated corrective candidate passed `go test -count=1 ./...`
(Git 24.088s, snapshot 47.070s), `go test -race -count=1 ./...`
(Git 30.803s, snapshot 77.055s), `go vet ./...` and `go build ./...`.
`gofmt -l .` and `git diff --check` printed nothing. Go/module source hashes
were identical before and after this local Darwin matrix. Independent review
and native Windows execution of the corrective implementation remain pending;
this passing local matrix is not merge or release clearance.

Corrective candidate `ca11055` passed both native platforms in CI
`34843816976`, but independent routing review found an Important traversal
error and a Minor alias regression. `filepath.Join`/`Clean` collapsed
symlink/`..` before native resolution, allowing both static wrong-admin source
captures and a persistent mid-read backlink change. A terminal directory alias
to the same administrative target was also rejected while an intermediate
alias worked. Native CI success did not override either finding.

R6 durable native regressions reproduced both full-capture bypasses (1,563
and 1,426 retained bytes in that local RED run), absolute/relative mid-read
changes, and three rejected valid alias/traversal layouts. The fix preserves
the raw relative base, resolves filesystem traversal with context and path
bounds, and validates backlink marker syntax before resolving its directory.
The nofollow pointer-file reader and its byte/type/identity/close safeguards
are unchanged. The complete focused routing/inspection controls now pass
(Git 16.950s); all three full-source refusal cases pass with zero output,
preserved change category and unchanged borrowed-index evidence (snapshot
5.314s). Fresh full-matrix, independent review and native CI evidence remain
required for this new revision.

The initial administrative-directory observation also retains the raw Lstat
metadata instead of silently replacing it with opened-handle metadata. Native
fixtures first reproduced eight lost-change assertions: mtime changes with
close/cancellation combinations, a replacement that reached enumeration, and
Unix identity/mode/size disagreements. The correction rejects observed
mode/size/mtime differences and, on Unix, retained native-ID differences before
returning the eager handle observation. Windows raw Lstat can load file IDs
lazily from a later pathname, so it is not treated as an eagerly pinned original
ID; its available metadata is checked and subsequent comparisons still use
eager File.Stat identities. Identical-metadata replacement before the first
Windows handle pin remains outside this finite-observation guarantee.

Common-store characterization controls accept native-equivalent terminal and
intermediate aliases and reject a persistent canonical-root substitution before
hashes or index-reading commands. They pass on both the routing correction and
unchanged `ca11055` production through a test-only overlay. Native Windows
junction controls are included, not inferred from Unix symlink traversal.
The requested raw-alias refusal still needs independent adjudication against
the canonical architecture's platform-aware alias support; it is not silently
accepted or dismissed because existing tests pass.

The independent R6 task reviewer stopped with provider `cyber_policy` HTTP 422
and produced no R6 verdict. No substitute approval, alternate-model retry or
policy bypass is used. Local correction and native CI may proceed, but final
independent task/whole-branch review and unresolved remote threads still block
merge and progression to a dependent stage.

The frozen R6 source passed `go test -count=1 ./...` (Git 34.731s, snapshot
63.005s), `go test -race -count=1 ./...` (Git 42.872s, snapshot 104.574s),
`go vet ./...`, and `go build ./...` on Go 1.26.5 darwin/arm64.
`gofmt -l .` and `git diff --check` printed nothing. All tracked/untracked
non-ignored source-file hashes were identical before and after the full matrix.
The initial-observation close controls also require one successful real Close
and an unusable handle, without assuming Windows reports `fs.ErrClosed`;
opaque-error and no-op-close assertion REDs preceded that portable oracle.
Native macOS/Windows CI on the published R6 tree remains pending, as does the
blocked independent review. No merge or production acceptance is claimed.

R6 candidate `7e21a4e`, tree `d45c26786bafab5727708288ad765192a5117f61`,
passed macOS CI `34848951210`; Windows failed exactly two administrative-alias
fixture preconditions before reaching their product assertions. The Windows
snapshot normal suite passed (88.919s), but race/build/source-unchanged steps
were not reached. Checkout `e34107e6abec2d2ca2dc429a9f6402175f15d190` has the
candidate tree and parents `d27094e`/`7e21a4e`.

Installed Go 1.26.5 Windows source confirms that ordinary junction mount points
are not classified as ModeSymlink for EvalSymlinks traversal. A terminal
junction can be retained rather than resolved, and an intermediate junction
can fail. The fixture's assumed EvalSymlinks canonical string was therefore not
a valid native identity oracle. The next test-first checkpoint uses eagerly
opened native identities and actual Git routing as fixture preconditions, and
directly exercises administrative-pointer resolution through both junction
positions. No toolchain, GODEBUG setting, fixture skip, or production relaxation
is used. Native product assertion RED must be observed before correcting the
Windows pointer resolver; this checkpoint is not merge-ready.

### Remaining Task 8 lifecycle

**Files:**
- Reuse: `internal/snapshot/manifest.go` and the delivered integrity codecs
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

Manifest and diagnostic entry schemas, matching `internal/snapshot/manifest.go`:

```go
type Manifest struct {
	SchemaVersion         int               `json:"schemaVersion"`
	ToolVersion           string            `json:"toolVersion"`
	PlanSchemaVersion     int               `json:"planSchemaVersion"`
	SnapshotID            string            `json:"snapshotId"`
	PlanID                string            `json:"planId"`
	CandidateID           string            `json:"candidateId"`
	CandidateFingerprint  string            `json:"candidateFingerprint"`
	PolicyDigest          string            `json:"policyDigest"`
	AdapterLockDigest     string            `json:"adapterLockDigest"`
	EvidenceDigest        string            `json:"evidenceDigest"`
	CreatedAt             time.Time         `json:"createdAt"`
	RepositoryRoot        string            `json:"repositoryRoot"`
	CommonGitDir          string            `json:"commonGitDir"`
	WorktreePath          string            `json:"worktreePath"`
	AdminDir              string            `json:"adminDir"`
	Head                  string            `json:"head"`
	Branch                string            `json:"branch,omitempty"`
	RecoveryRef           string            `json:"recoveryRef,omitempty"`
	Files                 map[string]string `json:"files"`
	UntrackedFiles        int               `json:"untrackedFiles"`
	UntrackedBytes        int64             `json:"untrackedBytes"`
	AdministrativeEntries []AdminEntry      `json:"administrativeEntries"`
}

type AdminEntry struct {
	Path string      `json:"path"`
	Kind string      `json:"kind"`
	Mode fs.FileMode `json:"mode"`
	Data []byte      `json:"data"`
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
