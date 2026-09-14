# Treeclear Agent Adapters Implementation Plan

- Status: Planned
- Sequence: 002 of 004
- Source architecture: [Treeclear Architecture](../architecture/2026-09-12-treeclear.md)
- Depends on: [001 Treeclear Safety Core](001-treeclear-core.md)

> Execute this plan task-by-task using an isolated Git worktree, test-driven development, and a review checkpoint after every task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a read-only, capability-graded adapter system that correlates worktree activity from GitHub Copilot, Codex, OpenCode, Claude Code, and Cursor without granting provider code any Treeclear mutation capability.

**Architecture:** Signed-update support is deferred to Plan 003, so this plan loads audited built-in bundles through `go:embed`. A generic runner handles schema-validated file, SQLite, PID-lock, command JSON, and stdio JSON-RPC sources; the Copilot source uses the official read-only Go SDK bridge first. Every source normalizes into `domain.AgentEvidence`, and failures become typed unknown evidence for relevant worktrees.

**Tech Stack:** Existing Plan 001 stack plus GitHub Copilot Go SDK 1.0.13, modernc SQLite 1.58.0, go-yaml/v3 3.0.5, jsonschema/v6 6.0.3, Go `embed`, `database/sql`, and standard JSON-RPC framing utilities.

## Global Constraints

- Qualify adapters for the macOS-first release. Windows-specific product
  acceptance is deferred to [#15](https://github.com/hellices/treeclear/issues/15);
  preserve existing Windows fixtures, interfaces, and native CI.
- Preserve every invariant and public schema introduced by Plan 001.
- Adapters return evidence only; they never receive Git, branch, file, worktree, or session mutation APIs.
- Prefer supported SDK, app-server, or machine-readable CLI interfaces.
- Private files and databases are read-only, version-guarded fallbacks.
- Adapter or schema failure yields unknown evidence for relevant candidates, never inactive evidence.
- Provider absence with no path, marker, process, or session binding yields `not-applicable`.
- Command sources use fixed argv, a sanitized environment, time limits, output limits, and executable identity hashes.
- Unsigned external adapters and updates remain out of scope until Plan 003.
- Transcript and prompt content must not enter plans, logs, fixtures derived from real users, or test snapshots.
- Agent-session archive and deletion remain out of scope.
- Every task ends with targeted tests, the full existing suite, and a focused commit.

---

## File Structure

| Path | Responsibility |
|---|---|
| `schemas/adapter/v1.json` | Declarative adapter manifest contract |
| `schemas/evidence/v1.json` | Normalized public evidence contract |
| `internal/adapter/manifest.go` | Typed manifest and capability values |
| `internal/adapter/validate.go` | Embedded JSON Schema validation and semantic checks |
| `internal/adapter/bundle.go` | Immutable loaded adapter bundle |
| `internal/adapter/registry.go` | Built-in registry and deterministic ordering |
| `internal/adapter/mapping/pointer.go` | Restricted RFC 6901 JSON pointer evaluation |
| `internal/adapter/mapping/mapper.go` | Record-to-evidence mapping |
| `internal/adapter/source/source.go` | Source interface and result contract |
| `internal/adapter/source/files.go` | JSON, JSONL, and YAML readers |
| `internal/adapter/source/sqlite.go` | Query-only SQLite reader |
| `internal/adapter/source/pidlock.go` | PID lock filename/record reader |
| `internal/adapter/source/command.go` | Fixed-argv JSON/JSONL command source |
| `internal/adapter/source/jsonrpc.go` | Bounded stdio JSON-RPC exchange |
| `internal/adapter/source/executable.go` | Absolute path, version, and SHA-256 identity |
| `internal/adapter/native/copilot.go` | Official Copilot SDK read-only bridge |
| `internal/adapter/applicability.go` | Provider relevance decision |
| `internal/adapter/collector.go` | Source priority, fallback, and normalized evidence |
| `adapters/embed.go` | `go:embed` first-party bundles |
| `adapters/builtin/*/adapter.json` | Provider bundle manifests |
| `adapters/builtin/*/schemas/` | Provider output schemas |
| `adapters/builtin/*/fixtures/` | Synthetic provider version fixtures |
| `internal/cli/adapters.go` | `adapters list` and `adapters doctor` |
| `tests/e2e/adapters_test.go` | Five-provider capability and fail-closed tests |

## Task 1: Define and validate the adapter and evidence contracts

**Files:**
- Create: `schemas/adapter/v1.json`
- Create: `schemas/evidence/v1.json`
- Create: `internal/adapter/manifest.go`
- Create: `internal/adapter/validate.go`
- Create: `internal/adapter/validate_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Produces: `adapter.Manifest`, `adapter.Source`, `adapter.Capability`, and `adapter.SupportGrade`
- Produces: `adapter.ValidateManifest(data []byte) (adapter.Manifest, error)`
- Produces: `adapter.ValidateEvidence(data []byte) error`

- [ ] **Step 1: Write manifest validation tests**

```go
func TestValidateManifestRejectsMutationAndNetwork(t *testing.T) {
	tests := []string{
		`{"format":"treeclear-agent-adapter","formatVersion":1,"adapterId":"bad","adapterVersion":"1.0.0","permissions":{"mutating":true,"network":false}}`,
		`{"format":"treeclear-agent-adapter","formatVersion":1,"adapterId":"bad","adapterVersion":"1.0.0","permissions":{"mutating":false,"network":true}}`,
	}
	for _, input := range tests {
		if _, err := ValidateManifest([]byte(input)); err == nil {
			t.Fatalf("ValidateManifest(%s) succeeded", input)
		}
	}
}

func TestValidateManifestAcceptsEvidenceOnlySource(t *testing.T) {
	input := []byte(`{
	  "format":"treeclear-agent-adapter",
	  "formatVersion":1,
	  "adapterId":"fixture",
	  "adapterVersion":"1.0.0",
	  "coreCompatibility":{"min":1,"max":1},
	  "capabilities":["session.list","session.cwd"],
	  "sources":[{"id":"sessions","kind":"file-json","supportGrade":"versioned-private","revalidationMode":"local-readonly","schema":"session-v1","priority":10}],
	  "schemas":{"session-v1":"schemas/session-v1.json"},
	  "permissions":{"mutating":false,"network":false}
	}`)
	got, err := ValidateManifest(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.AdapterID != "fixture" || len(got.Sources) != 1 {
		t.Fatalf("manifest = %#v", got)
	}
}
```

Add tests for duplicate source IDs, unknown source kinds, unknown support grades,
invalid semantic versions, unsupported core ranges, undeclared schemas, and
capabilities containing a Treeclear mutation operation.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/adapter
```

Expected: FAIL because adapter contracts do not exist.

- [ ] **Step 3: Add dependencies and implement schemas and semantic checks**

Run:

```bash
go get github.com/github/copilot-sdk/go@v1.0.13
go get github.com/santhosh-tekuri/jsonschema/v6@v6.0.3
go get go.yaml.in/yaml/v3@v3.0.5
go get modernc.org/sqlite@v1.58.0
```

Use these manifest types:

```go
type Manifest struct {
	Format             string            `json:"format"`
	FormatVersion      int               `json:"formatVersion"`
	AdapterID          string            `json:"adapterId"`
	AdapterVersion     string            `json:"adapterVersion"`
	CoreCompatibility CoreVersionRange  `json:"coreCompatibility"`
	AgentCompatibility AgentVersionRange `json:"agentCompatibility,omitempty"`
	Capabilities      []Capability      `json:"capabilities"`
	Sources           []Source          `json:"sources"`
	Schemas           map[string]string `json:"schemas,omitempty"`
	Permissions       Permissions       `json:"permissions"`
}

type CoreVersionRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type AgentVersionRange struct {
	Min string `json:"min,omitempty"`
	Max string `json:"max,omitempty"`
}

type Permissions struct {
	Mutating bool `json:"mutating"`
	Network  bool `json:"network"`
}

type Source struct {
	ID           string          `json:"id"`
	Kind         SourceKind      `json:"kind"`
	SupportGrade SupportGrade    `json:"supportGrade"`
	RevalidationMode string      `json:"revalidationMode"`
	Schema       string          `json:"schema"`
	Priority     int             `json:"priority"`
	Config       json.RawMessage `json:"config,omitempty"`
}
```

Allowed source kinds are exactly:

```go
const (
	SourceFileJSON     SourceKind = "file-json"
	SourceFileJSONL    SourceKind = "file-jsonl"
	SourceFileYAML     SourceKind = "file-yaml"
	SourceSQLite       SourceKind = "sqlite-readonly"
	SourcePIDLock      SourceKind = "pid-lock"
	SourceExecJSON     SourceKind = "exec-json"
	SourceExecJSONL    SourceKind = "exec-jsonl"
	SourceStdioJSONRPC SourceKind = "stdio-jsonrpc"
	SourceNativeSDK    SourceKind = "native-sdk"
)
```

`ValidateManifest` validates only manifest bytes and semantics. It must run
JSON Schema first, then reject:

- `permissions.mutating=true`;
- `permissions.network=true`;
- unknown fields or kinds;
- duplicate IDs;
- missing schema declarations for referenced schema IDs;
- a core range that excludes SPI version 1;
- capabilities outside the documented capability enum.

Allowed revalidation modes are `local-readonly` and `planning-only`.
`file-*`, `sqlite-readonly`, and `pid-lock` must be `local-readonly`.
`exec-*`, `stdio-jsonrpc`, and `native-sdk` must be `planning-only`; manifests
that declare them local are rejected.

Task 2 adds bundle-level validation after the `Bundle` type exists.

- [ ] **Step 4: Run validation and full tests**

Run:

```bash
gofmt -w internal/adapter
go test ./internal/adapter
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit contracts**

```bash
git add go.mod go.sum schemas internal/adapter
git commit -m "feat: define agent adapter contracts"
```

## Task 2: Load immutable built-in bundles

**Files:**
- Create: `internal/adapter/bundle.go`
- Create: `internal/adapter/registry.go`
- Create: `internal/adapter/registry_test.go`
- Create: `internal/adapter/builtin.go`
- Create: `adapters/builtin/fixture/adapter.json`
- Create: `adapters/builtin/fixture/schemas/session-v1.json`

**Interfaces:**
- Produces: `adapter.Bundle`
- Produces: `adapter.Registry.List() []adapter.Bundle`
- Produces: `adapter.Registry.Get(id string) (adapter.Bundle, bool)`
- Produces: `adapter.LoadEmbedded(fs fs.FS) (adapter.Registry, error)`
- Produces: `adapter.ValidateBundle(bundle adapter.Bundle) error`

- [ ] **Step 1: Write deterministic registry tests**

```go
func TestLoadEmbeddedSortsAndCopiesBundles(t *testing.T) {
	registry, err := LoadEmbedded(fixtureFS())
	if err != nil {
		t.Fatal(err)
	}
	first := registry.List()
	second := registry.List()
	if diff := cmp.Diff(first, second); diff != "" {
		t.Fatalf("registry is nondeterministic (-first +second):\n%s", diff)
	}
	first[0].Manifest.AdapterID = "mutated"
	if got, _ := registry.Get("fixture"); got.Manifest.AdapterID != "fixture" {
		t.Fatal("registry exposed mutable internal state")
	}
}
```

Test duplicate adapter IDs, missing schema assets, invalid embedded manifests,
and lexical adapter ordering.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/adapter -run Registry
```

Expected: FAIL because bundle loading does not exist.

- [ ] **Step 3: Implement embedded bundle loading**

Use:

```go
type Bundle struct {
	Manifest Manifest
	Schemas  map[string][]byte
	Files    map[string][]byte
	Digest   string
	BuiltIn  bool
}

type Registry struct {
	byID map[string]Bundle
}
```

`Bundle.Digest` is SHA-256 over sorted relative path, NUL, length, NUL, and
file bytes for every bundle file. Return deep copies from registry methods.

`ValidateBundle` rejects declared schema paths that are missing, escape the
bundle root, fail to parse, or do not match their declared SHA-256 digest.
`LoadEmbedded` calls it after loading every file and before registering the
bundle.

Create `adapters/embed.go` and embed:

```go
package adapters

import (
	"embed"
	"io/fs"
)

//go:embed builtin
var builtins embed.FS

func FS() fs.FS {
	sub, err := fs.Sub(builtins, "builtin")
	if err != nil {
		panic(err)
	}
	return sub
}
```

`internal/adapter` imports `github.com/hellices/treeclear/adapters` only to
obtain this read-only filesystem and keeps all parsing in the internal package.

- [ ] **Step 4: Run registry and full tests**

Run:

```bash
gofmt -w internal/adapter
go test ./internal/adapter
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit built-in registry**

```bash
git add internal/adapter adapters/builtin
git commit -m "feat: embed immutable adapter bundles"
```

## Task 3: Implement restricted mapping into normalized evidence

**Files:**
- Create: `internal/adapter/mapping/pointer.go`
- Create: `internal/adapter/mapping/pointer_test.go`
- Create: `internal/adapter/mapping/mapper.go`
- Create: `internal/adapter/mapping/mapper_test.go`

**Interfaces:**
- Produces: `mapping.Resolve(document any, pointer string) (any, bool)`
- Produces: `mapping.Mapper.Map(document any, spec mapping.Spec, context mapping.Context) ([]domain.AgentEvidence, error)`

- [ ] **Step 1: Write JSON pointer and evidence mapping tests**

```go
func TestResolveRFC6901Escapes(t *testing.T) {
	document := map[string]any{"a/b": map[string]any{"~key": "value"}}
	got, ok := Resolve(document, "/a~1b/~0key")
	if !ok || got != "value" {
		t.Fatalf("Resolve() = %#v, %v", got, ok)
	}
}

func TestMapperNeverCopiesSummaryOrPrompt(t *testing.T) {
	document := map[string]any{"sessions": []any{map[string]any{
		"id": "s1", "cwd": "/repo/wt", "updated": "2026-09-01T00:00:00Z",
		"summary": "secret prompt",
	}}}
	got, err := New().Map(document, fixtureSpec(), Context{AdapterID: "fixture", AdapterVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(got)
	if bytes.Contains(data, []byte("secret prompt")) {
		t.Fatalf("mapped evidence leaked summary: %s", data)
	}
}
```

Test missing required fields, invalid timestamps, status enum mapping, path
normalization, stable evidence ordering, and fingerprint changes.

Before enabling provider evidence, add mapping and Task 6 collector integration
regressions for active and unknown records using symlink aliases, native Windows
junctions and case variants, and unresolvable or conflicting path bindings.
Resolve nonempty paths with Plan 001 `pathutil.Canonical` before `correlate.Group`;
failed bindings must retain unknown evidence for every candidate whose relevance
cannot be excluded, not silently discard records. Keep filesystem access in
mapping/collection, not in the pure correlator. These remain pending Plan 002
requirements; the read-only Plan 001 slice supplies no agent-provider evidence.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/adapter/mapping
```

Expected: FAIL because mapping does not exist.

- [ ] **Step 3: Implement a deliberately small mapping language**

Use:

```go
type Spec struct {
	RecordsPointer string                  `json:"recordsPointer"`
	Fields         map[string]FieldMapping `json:"fields"`
	StatusMap      map[string]domain.EvidenceState `json:"statusMap,omitempty"`
}

type FieldMapping struct {
	Pointer  string `json:"pointer"`
	Required bool   `json:"required"`
	Kind     string `json:"kind"`
}

type Context struct {
	AdapterID      string
	AdapterVersion string
	BundleDigest   string
	Provider       string
	SourceID       string
	SourceKind     string
	RevalidationMode string
	SupportGrade   domain.TrustGrade
	SchemaVersion  string
	TrustRecordDigest string
	ObservedAt     time.Time
}
```

Supported field kinds are exactly `string`, `path`, `time-rfc3339`,
`time-unix-seconds`, and `state`. Supported target field names are exactly
`sessionId`, `threadId`, `projectId`, `cwd`, `repositoryRoot`,
`worktreePath`, `binding.kind`, `binding.identifier`, `binding.version`,
`state`, `createdAt`, and `updatedAt`.

Unknown target fields fail validation. Do not add arbitrary expressions,
templates, JavaScript, regex replacement, or transcript-content fields.
The mapper fills provider, source ID, source kind, revalidation mode, bundle digest, support
grade, schema version, observed time, trust-record digest, and SHA-256
`RawFingerprint` from `Context`; PID-lock correlation adds normalized
`ProcessRefs`.

- [ ] **Step 4: Run mapper tests and fuzz seed cases**

Run:

```bash
gofmt -w internal/adapter/mapping
go test ./internal/adapter/mapping
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit mapping**

```bash
git add internal/adapter/mapping
git commit -m "feat: normalize adapter evidence"
```

## Task 4: Add core-mediated file, SQLite, and PID-lock sources

**Files:**
- Create: `internal/adapter/source/source.go`
- Create: `internal/adapter/source/files.go`
- Create: `internal/adapter/source/files_test.go`
- Create: `internal/adapter/source/sqlite.go`
- Create: `internal/adapter/source/sqlite_test.go`
- Create: `internal/adapter/source/pidlock.go`
- Create: `internal/adapter/source/pidlock_test.go`

**Interfaces:**
- Produces: `source.Reader.Read(ctx context.Context, request source.Request) (source.Result, error)`
- Produces source kinds `file-json`, `file-jsonl`, `file-yaml`, `sqlite-readonly`, and `pid-lock`

- [ ] **Step 1: Write read-boundary and schema-failure tests**

Tests must prove:

- a path outside `AllowedRoots` is rejected;
- symlink escape is rejected;
- YAML aliases cannot expand beyond configured node and byte limits;
- JSONL stops on a malformed record and returns typed unknown, not partial
  inactive evidence;
- SQLite opens with `mode=ro`, `PRAGMA query_only=ON`, and a 250 ms busy
  timeout;
- non-`SELECT` and multi-statement SQL are rejected;
- missing tables or columns yield `ErrSchemaMismatch`;
- a stale PID lock remains stale evidence until OS process identity confirms
  it.

Example:

```go
func TestSQLiteRejectsMutationQuery(t *testing.T) {
	reader := SQLiteReader{}
	_, err := reader.Read(context.Background(), Request{
		AllowedRoots: []string{t.TempDir()},
		Config: SQLiteConfig{Query: "DELETE FROM sessions"},
	})
	if !errors.Is(err, ErrReadOnlyViolation) {
		t.Fatalf("Read() error = %v", err)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/adapter/source
```

Expected: FAIL because source readers do not exist.

- [ ] **Step 3: Implement guarded readers**

Use:

```go
type Request struct {
	AdapterID      string
	SourceID       string
	AllowedRoots   []string
	Config         any
	MaximumBytes   int64
	MaximumRecords int
}

type Result struct {
	Document    any
	ObservedAt  time.Time
	Fingerprint string
	Executable  *domain.ExecutableIdentity
	Warnings    []string
}

type Reader interface {
	Read(context.Context, Request) (Result, error)
}
```

File readers use `os.Open`, `io.LimitReader`, `json.Decoder.DisallowUnknownFields`
when a typed schema is expected, and YAML decoder node limits.

SQLite uses:

```text
file:<escaped-path>?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(250)
```

Verify `PRAGMA user_version`, required table names, required column names, and
declared SQLite type affinities before running the single SELECT query.

PID-lock parsing returns PID, lock path, lock modification time, and recognized
format version. It never declares the process active or stale without the
Plan 001 process collector.

- [ ] **Step 4: Run source and full tests**

Run:

```bash
gofmt -w internal/adapter/source
go test ./internal/adapter/source
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit read-only sources**

```bash
git add internal/adapter/source
git commit -m "feat: add guarded local evidence readers"
```

## Task 5: Add command and stdio JSON-RPC evidence sources

**Files:**
- Create: `internal/adapter/source/executable.go`
- Create: `internal/adapter/source/executable_test.go`
- Create: `internal/adapter/source/command.go`
- Create: `internal/adapter/source/command_test.go`
- Create: `internal/adapter/source/jsonrpc.go`
- Create: `internal/adapter/source/jsonrpc_test.go`
- Create: `internal/adapter/source/testdata/fake_jsonrpc/main.go`

**Interfaces:**
- Produces: `source.ResolveInvocation(ctx context.Context, config source.CommandConfig, expansion source.ExpansionContext) (domain.ExecutableIdentity, error)`
- Produces: `source.VerifyInvocation(ctx context.Context, recorded domain.ExecutableIdentity, config source.CommandConfig, expansion source.ExpansionContext) error`
- Produces: fixed-argv `exec-json`, `exec-jsonl`, and `stdio-jsonrpc` readers
- Produces: executable identity included in source fingerprint

- [ ] **Step 1: Write executable replacement and protocol-limit tests**

Required tests:

```go
func TestExecutableFingerprintChangesWhenBinaryChanges(t *testing.T) {
	path := writeExecutable(t, []byte("first"))
	first, err := HashExecutable(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("second"), 0o700); err != nil {
		t.Fatal(err)
	}
	second, err := HashExecutable(path)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("executable digest did not change")
	}
}
```

Also test timeout, oversized stdout/stderr, malformed JSON, unexpected
JSON-RPC IDs, protocol error responses, extra responses, command argv with
shell metacharacters, and developer-mode rejection for custom commands.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/adapter/source -run 'Executable|Command|JSONRPC'
```

Expected: FAIL because command sources do not exist.

- [ ] **Step 3: Implement fixed-argv execution and bounded JSON-RPC**

Define:

```go
type ExecutableIdentity struct {
	Path              string   `json:"path"`
	Version           string   `json:"version"`
	SHA256            string   `json:"sha256"`
	Arguments         []string `json:"arguments"`
	WorkingDirectory  string   `json:"workingDirectory"`
	EnvironmentDigest string   `json:"environmentDigest"`
	InvocationDigest  string   `json:"invocationDigest"`
}

type CommandConfig struct {
	Executable string            `json:"executable"`
	Args       []string          `json:"args"`
	VersionArgs []string         `json:"versionArgs"`
	WorkingDirectoryTemplate string `json:"workingDirectory"`
	Environment map[string]string `json:"environment,omitempty"`
	Timeout    time.Duration     `json:"timeout"`
	MaxBytes   int               `json:"maxBytes"`
}

type ExpansionContext struct {
	RepositoryRoot string
	WorktreePath   string
	UserHome       string
}

type RPCStep struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	ResultPointer string   `json:"resultPointer"`
}
```

Resolve executables with `exec.LookPath`, convert to an absolute canonical
path, hash file bytes, and capture bounded version output. Canonicalize the
working directory, preserve the exact argv array, sort the sanitized
`KEY=value` environment, and compute `EnvironmentDigest` and
`InvocationDigest` over all fields. Execute directly with
`exec.CommandContext`; never use `sh`, `bash`, `cmd.exe`, or PowerShell.

Version command execution is planning-only. `VerifyInvocation`, used by
apply, never starts the provider binary and never runs `versionArgs`; it only:

1. resolves the executable path;
2. hashes file bytes;
3. reconstructs argv, canonical cwd, and sanitized environment from the
   currently active signed bundle;
4. compares binary and invocation digests with the plan.

`CommandConfig.WorkingDirectoryTemplate` is mandatory. Its complete grammar is
one root token with an optional slash-separated relative suffix:

```text
${repositoryRoot}
${worktreePath}
${userHome}
${repositoryRoot}/relative/subdirectory
```

No other variable, empty value, absolute suffix, drive prefix, UNC prefix,
`.` component, or `..` component is valid. Expansion uses `filepath.Join`,
canonicalizes the result with Plan 001 `pathutil`, and verifies it remains
inside the selected root. It never inherits the Treeclear process cwd.
Planning and apply receive the same explicit repository, worktree, and home
context and must produce byte-identical canonical paths.

Add validation tests for missing, empty, escaping, unknown-token, Windows
drive/UNC, and separator cases, plus a golden test proving plan/apply expansion
matches on macOS and Windows path fixtures.

The recorded human-readable version string is informational and
HMAC-protected; the unchanged binary SHA-256 is the non-executing proof that
its version-bearing bytes did not change.

The JSON-RPC reader permits only the declared ordered steps. It rejects
notifications and methods not declared by the signed bundle.

- [ ] **Step 4: Run source, race, and full tests**

Run:

```bash
gofmt -w internal/adapter/source
go test ./internal/adapter/source
go test -race ./internal/adapter/source
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit command sources**

```bash
git add internal/adapter/source
git commit -m "feat: add bounded command evidence"
```

## Task 6: Implement adapter applicability, priority, and plan integration

**Files:**
- Create: `internal/adapter/applicability.go`
- Create: `internal/adapter/applicability_test.go`
- Create: `internal/adapter/collector.go`
- Create: `internal/adapter/collector_test.go`
- Modify: `internal/apply/environment.go`
- Test: `internal/apply/environment_test.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/plan/build.go`
- Modify: `internal/plan/fingerprint.go`
- Test: `internal/plan/build_test.go`

**Interfaces:**
- Produces: `adapter.Applicability(worktree domain.Worktree, probe adapter.ProbeResult) domain.EvidenceState`
- Produces: `adapter.Collector.Collect(ctx context.Context, worktrees []domain.Worktree, request adapter.CollectionRequest) (adapter.Collection, []error)`
- Extends plan fingerprints with adapter bundle and executable identities.

- [ ] **Step 1: Write relevance and fallback tests**

Tests cover:

- absent provider and no marker -> `not-applicable`;
- installed provider plus managed-root worktree and discovery failure ->
  `unknown`;
- installed provider failure for an unrelated worktree -> warning only;
- supported source succeeds -> lower-priority private fallback is not used;
- supported source returns incomplete state -> fallback adds evidence without
  replacing provenance;
- conflicting sources -> both evidence records remain and policy protects.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/adapter ./internal/plan
```

Expected: FAIL because collection and plan integration do not exist.

- [ ] **Step 3: Implement deterministic source evaluation**

Source order is ascending `priority`, then lexical source ID.

Collector return type:

```go
type Collection struct {
	Evidence         []domain.AgentEvidence
	StatusByWorktree map[string][]domain.AdapterStatus
}

type CollectionMode string

const (
	CollectionPlan  CollectionMode = "plan"
	CollectionApply CollectionMode = "apply"
)

type CollectionRequest struct {
	Mode      CollectionMode
	ApplyMode domain.ApplyMode
}
```

Each source result records:

- adapter and source ID;
- adapter version and bundle digest;
- support grade;
- schema version;
- observed time;
- source fingerprint;
- executable identity when applicable;
- warnings.

The collector emits one `domain.AdapterStatus` per adapter and relevant
worktree through `Collection.StatusByWorktree`. `Healthy` requires successful
manifest, schema, and source processing. `Trusted` is true for embedded
bundles in this plan. `BestGrade` is the strongest evidence grade produced for
that candidate.

`CollectionPlan` runs all sources. `CollectionApply` runs only
`local-readonly` sources and never starts an external provider command,
JSON-RPC server, or SDK runtime. `OfflineRevalidatable` is true only when the
adapter declares and successfully collects complete local evidence for that
candidate.

For custom adapters, `AdapterStatus.Trusted` is evaluated for the request's
`ApplyMode`. A plan intended for interactive apply requires the
`interactive-apply` trust scope before a custom adapter can contribute to a
Safe decision. Scheduled mode rejects every locally trusted custom adapter.

Collector errors must implement:

```go
type SourceError struct {
	AdapterID string
	SourceID  string
	Category  string
	Err       error
}
```

Plan builder includes adapters after process collection, recomputes evidence
correlation, and adds the registry digest to `AdapterLockDigest`.
It also copies each unique command identity into
`Plan.ExecutableIdentities`, copies the full invocation identity into
`AgentEvidence.Executable`, and includes both in candidate fingerprints.

Update the production `apply.EnvironmentVerifier` wiring so
`VerifyAdapterLock` recomputes the current registry lock digest and
`VerifyExecutables` uses `VerifyInvocation` with the candidate's explicit
`ExpansionContext` to resolve and hash every
recorded binary and reconstruct exact argv, canonical cwd, and sanitized
environment without executing the binary. The
composition root must inject the same registry and executable resolver into
plan creation and apply. Add tests proving a bundle switch or executable
replacement, argv change, cwd change, or allowed environment change returns
`ErrPreconditionChanged` before snapshot creation.

Apply revalidation calls `Collector.Collect(..., CollectionRequest{Mode:
CollectionApply, ApplyMode: plan.IntendedApplyMode})`. Add a test
runner that fails immediately if any command, JSON-RPC, SDK, HTTP, or other
network-capable source is invoked in that mode. The same test injects a version
runner that fails if called, proving executable verification does not execute
`versionArgs`.

- [ ] **Step 4: Run adapter, plan, and race tests**

Run:

```bash
gofmt -w internal/adapter internal/plan
go test ./internal/adapter ./internal/plan
go test -race ./internal/adapter
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit adapter correlation**

```bash
git add internal/adapter internal/plan
git commit -m "feat: correlate agent evidence"
```

## Task 7: Add the GitHub Copilot adapter

**Files:**
- Create: `internal/adapter/native/copilot.go`
- Create: `internal/adapter/native/copilot_test.go`
- Create: `adapters/builtin/copilot/adapter.json`
- Create: `adapters/builtin/copilot/schemas/workspace-v1.json`
- Create: `adapters/builtin/copilot/schemas/sqlite-session-v1.json`
- Create: `adapters/builtin/copilot/fixtures/sdk-sessions-v1.json`
- Create: `adapters/builtin/copilot/fixtures/workspace-v1.yaml`
- Create: `adapters/builtin/copilot/fixtures/unknown-workspace.yaml`

**Interfaces:**
- Consumes: official `github.com/github/copilot-sdk/go` v1.0.13
- Produces: `native.CopilotSource.List(ctx context.Context) ([]domain.AgentEvidence, error)`
- Fallback order: SDK, guarded workspace YAML, guarded SQLite, PID lock.

- [ ] **Step 1: Write SDK conversion and fallback tests**

Use an SDK interface, not a real runtime:

```go
type CopilotClient interface {
	ListSessions(context.Context, *copilot.SessionListFilter) ([]copilot.SessionMetadata, error)
	Stop() error
}
```

Test:

- `SessionID`, `ModifiedTime`, and `Context.WorkingDirectory` map without
  copying `Summary`;
- SDK success prevents SQLite from becoming authoritative;
- SDK unavailable permits guarded fallback;
- unknown workspace YAML yields unknown evidence;
- a lock PID is correlated only after PID and creation-time validation;
- remote sessions without a local cwd do not bind to worktrees.

- [ ] **Step 2: Run Copilot tests and verify they fail**

Run:

```bash
go test ./internal/adapter/native -run Copilot
```

Expected: FAIL because the source does not exist.

- [ ] **Step 3: Implement SDK-first discovery and the bundle**

Use:

```go
client := copilot.NewClient(&copilot.ClientOptions{
	Connection:       copilot.StdioConnection{Path: resolvedCopilotPath},
	WorkingDirectory: home,
})
sessions, err := client.ListSessions(ctx, nil)
```

Always call `Stop` and join stop errors with collection errors. Do not call
`DeleteSession`.

The fallback manifest permits only:

- `~/.copilot/session-state/*/workspace.yaml`;
- `~/.copilot/session-state/*/inuse.<pid>.lock`;
- `~/.copilot/session-store.db` in query-only mode.

Validate required SQLite columns and omit session summaries from selected
columns.

- [ ] **Step 4: Run Copilot and full tests**

Run:

```bash
gofmt -w internal/adapter/native
go test ./internal/adapter/native
go test ./internal/adapter
go test ./...
```

Expected: all tests PASS without requiring login or a live Copilot runtime.

- [ ] **Step 5: Commit Copilot evidence**

```bash
git add internal/adapter/native adapters/builtin/copilot go.mod go.sum
git commit -m "feat: add Copilot session evidence"
```

## Task 8: Add the Codex and OpenCode adapters

**Files:**
- Create: `adapters/builtin/codex/adapter.json`
- Create: `adapters/builtin/codex/schemas/thread-list-v2.json`
- Create: `adapters/builtin/codex/schemas/worktree-binding-v1.json`
- Create: `adapters/builtin/codex/fixtures/thread-list-v2.json`
- Create: `adapters/builtin/codex/fixtures/worktree-binding-v1.json`
- Create: `adapters/builtin/opencode/adapter.json`
- Create: `adapters/builtin/opencode/schemas/session-list-v1.json`
- Create: `adapters/builtin/opencode/fixtures/session-list-v1.json`
- Test: `internal/adapter/providers_test.go`

**Interfaces:**
- Produces Codex `supported-app-server` session and versioned binding evidence.
- Produces OpenCode `supported-cli` session evidence.

- [ ] **Step 1: Write provider fixture tests**

Codex fixture:

```json
{
  "result": {
    "data": [{
      "id": "thread-1",
      "cwd": "/repo/.worktrees/feature",
      "status": "idle",
      "createdAt": 1780000000,
      "updatedAt": 1780000100
    }],
    "nextCursor": null
  }
}
```

OpenCode fixture:

```json
[{
  "id": "session-1",
  "title": "must not be copied",
  "updated": 1780000100,
  "created": 1780000000,
  "projectId": "project-1",
  "directory": "/repo/.worktrees/feature"
}]
```

Assert correct states, paths, support grades, timestamps, and absence of
`title` in normalized evidence.

- [ ] **Step 2: Run fixture tests and verify they fail**

Run:

```bash
go test ./internal/adapter -run 'Codex|OpenCode'
```

Expected: FAIL because bundles do not exist.

- [ ] **Step 3: Add exact provider manifests**

Codex uses fixed argv `codex app-server`, an ordered JSON-RPC `thread/list`
request, and the declared result pointer `/result/data`. It also reads
versioned `codex-thread.json` only from Git administrative metadata.

OpenCode uses exactly:

```text
opencode session list --format json
```

Its executable version command is `opencode --version`. No delete command is
declared.

- [ ] **Step 4: Run provider and full tests**

Run:

```bash
go test ./internal/adapter -run 'Codex|OpenCode'
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit Codex and OpenCode adapters**

```bash
git add adapters/builtin/codex adapters/builtin/opencode internal/adapter/providers_test.go
git commit -m "feat: add Codex and OpenCode evidence"
```

## Task 9: Add conservative Claude Code and Cursor adapters

**Files:**
- Create: `adapters/builtin/claude-code/adapter.json`
- Create: `adapters/builtin/claude-code/schemas/transcript-metadata-v1.json`
- Create: `adapters/builtin/claude-code/fixtures/transcript-v1.jsonl`
- Create: `adapters/builtin/claude-code/fixtures/transcript-unknown.jsonl`
- Create: `adapters/builtin/cursor/adapter.json`
- Create: `adapters/builtin/cursor/schemas/worktree-config-v1.json`
- Create: `adapters/builtin/cursor/fixtures/worktrees-v1.json`
- Test: `internal/adapter/providers_test.go`

**Interfaces:**
- Produces Claude `versioned-private` or unknown transcript evidence.
- Produces Cursor documented-worktree evidence and an explicit unavailable
  session-list capability when no stable machine-readable interface exists.

- [ ] **Step 1: Write conservative fixture tests**

Assert:

- known Claude JSONL extracts only session ID, cwd, and timestamp;
- an unknown Claude record shape creates unknown evidence;
- a Claude transcript parse error never yields inactive;
- `.claude/worktrees/<name>` binds through Git plus documented path convention;
- Cursor `~/.cursor/worktrees/<repo>/<name>` binds as provider-managed;
- `.cursor/worktrees.json` is treated as setup metadata, not proof of an active
  session;
- unavailable Cursor persisted-session status protects only relevant
  Cursor-managed worktrees.

- [ ] **Step 2: Run fixture tests and verify they fail**

Run:

```bash
go test ./internal/adapter -run 'Claude|Cursor'
```

Expected: FAIL because bundles do not exist.

- [ ] **Step 3: Add manifests with explicit limitations**

Claude allowed roots:

```text
~/.claude/projects
<repository>/.claude/worktrees
```

Cursor allowed roots:

```text
~/.cursor/worktrees
<repository>/.cursor/worktrees.json
```

Do not declare session deletion, do not read prompt bodies, and do not parse
unknown JSONL records as if their fields were stable.

- [ ] **Step 4: Run provider and full tests**

Run:

```bash
go test ./internal/adapter -run 'Claude|Cursor'
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit Claude and Cursor adapters**

```bash
git add adapters/builtin/claude-code adapters/builtin/cursor internal/adapter/providers_test.go
git commit -m "feat: add Claude and Cursor evidence"
```

## Task 10: Expose adapter diagnostics and prove five-provider behavior

**Files:**
- Create: `internal/cli/adapters.go`
- Create: `internal/cli/adapters_test.go`
- Modify: `internal/cli/root.go`
- Create: `tests/e2e/adapters_test.go`
- Modify: `README.md`

**Interfaces:**
- Produces: `treeclear adapters list [--format json]`
- Produces: `treeclear adapters doctor [--format json]`
- Verifies all five adapters can report evidence or an explicit unavailable
  reason.

- [ ] **Step 1: Write CLI and e2e diagnostics tests**

`adapters list` JSON must include:

```json
{
  "adapterId": "cursor",
  "adapterVersion": "1.0.0",
  "builtIn": true,
  "capabilities": [],
  "sources": [],
  "health": "limited",
  "warnings": ["persisted session status is unavailable"]
}
```

The e2e test runs Treeclear with isolated synthetic home directories for all
five providers and verifies:

- deterministic adapter ordering;
- no prompt or summary leakage;
- relevant unknown evidence protects;
- unrelated unavailable adapters do not globally block;
- candidate fingerprints include bundle and executable identity;
- changing fixture shape changes only the external bundle fixture, not core
  policy code.

- [ ] **Step 2: Run diagnostics tests and verify they fail**

Run:

```bash
go test ./internal/cli -run Adapter
go test ./tests/e2e -run Adapter -v
```

Expected: FAIL because diagnostics commands and production wiring are absent.

- [ ] **Step 3: Implement diagnostics and update documentation**

`adapters doctor` probes every source but performs no mutation. Human output
shows:

- adapter/version;
- interface used;
- support grade;
- executable path/version/digest prefix;
- schema version;
- health;
- warnings and affected roots.

README adds:

```bash
treeclear adapters list
treeclear adapters doctor
treeclear plan --root ~/workspace --format json
```

Document Copilot, Codex, OpenCode as structured providers and Claude/Cursor as
conservative providers.

- [ ] **Step 4: Run complete adapter verification**

Run:

```bash
gofmt -w internal/cli tests/e2e
go test ./...
go test -race ./...
go test ./tests/e2e -run Adapter -v
go build ./cmd/treeclear
GOOS=windows GOARCH=amd64 go build ./cmd/treeclear
rm -f treeclear.exe
git diff --check
```

Expected: every command exits 0 and every provider reports evidence or an
explicit unavailable reason.

- [ ] **Step 5: Commit five-provider support**

```bash
git add internal/cli tests/e2e README.md
git commit -m "feat: expose multi-agent diagnostics"
```

## Plan 2 Completion Gate

Run:

```bash
go test ./...
go test -race ./...
go test ./tests/e2e -v
go build ./cmd/treeclear
GOOS=windows GOARCH=amd64 go build ./cmd/treeclear
rm -f treeclear.exe
treeclear adapters doctor --format json
git status --short
```

Expected:

- all tests and builds pass;
- Copilot, Codex, OpenCode, Claude Code, and Cursor have deterministic
  capability records;
- relevant adapter failures become protected unknown evidence;
- unrelated unavailable adapters do not globally block;
- no adapter or provider command receives a Treeclear mutation API;
- plans omit prompts, titles, and summaries;
- the repository is clean.
