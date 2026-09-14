# Treeclear Adapter Lifecycle Implementation Plan

- Status: Planned
- Sequence: 003 of 004
- Source architecture: [Treeclear Architecture](../architecture/2026-09-12-treeclear.md)
- Depends on: [002 Treeclear Agent Adapters](002-agent-adapters.md)

> Execute this plan task-by-task using an isolated Git worktree, test-driven development, and a review checkpoint after every task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow Treeclear's evidence adapters to be securely updated, pinned, rolled back, and customized without rebuilding the native safety core.

**Architecture:** The core embeds a first-party Ed25519 trust root and consumes signed canonical adapter metadata. Updates install immutable version directories, verify fixtures and health before atomically switching an active pointer, retain the previous version, and write an adapter lock digest that plans pin. Custom read-only bundles use the same validator and fixture harness; unsigned command-backed bundles remain developer-only and cannot influence scheduled apply.

**Tech Stack:** Existing Go stack, standard `crypto/ed25519`, `crypto/sha256`, `net/http`, atomic filesystem operations, embedded JSON schemas, and platform-private state directories.

## Global Constraints

- Deliver lifecycle acceptance for the macOS-first release. Windows-specific
  qualification is deferred to [#15](https://github.com/hellices/treeclear/issues/15);
  preserve portable safety contracts, Windows regression tests, and native CI.
- Cleanup, plan apply, and adapter update never run in the same process or hold the adapter state lock simultaneously.
- Apply never performs network access and uses exactly the adapter lock recorded by its plan.
- First-party external bundles require a valid Ed25519 signature, exact SHA-256 digest, size match, compatible SPI, and non-expired metadata.
- Update activation is atomic and retains one previous known-good version.
- Health-probe failure restores the previous version.
- No private signing key is committed to the repository.
- Built-in adapters remain available when no override is installed.
- Unsigned custom file/SQLite/PID adapters are developer-mode only until explicitly pinned.
- Custom command or JSON-RPC adapters require an exact executable digest and are never eligible for scheduled apply.
- Adapter trust never grants a Treeclear mutation capability.
- Preserve all Plan 001 and Plan 002 tests and public schemas.

---

## File Structure

| Path | Responsibility |
|---|---|
| `schemas/adapter-index/v1.json` | Signed update index contract |
| `trust/first-party-ed25519.pub` | Committed public verification key only |
| `trust/embed.go` | Embeds the first-party public key into release binaries |
| `internal/signing/canonical.go` | Canonical signed payload encoding |
| `internal/signing/verify.go` | Ed25519 signature and digest verification |
| `internal/signing/verify_test.go` | Test-key and tamper cases |
| `internal/adapterstore/layout.go` | Versioned path and active/previous layout |
| `internal/adapterstore/lock.go` | Deterministic adapter lock document and digest |
| `internal/adapterstore/store.go` | Immutable install and atomic pointer updates |
| `internal/adapterstore/store_test.go` | Crash-safe store tests |
| `internal/adapterupdate/index.go` | Remote signed index types |
| `internal/adapterupdate/client.go` | Bounded HTTPS fetch and artifact download |
| `internal/adapterupdate/update.go` | Verify, install, probe, activate, and rollback |
| `internal/adapterupdate/update_test.go` | httptest update scenarios |
| `internal/adaptertrust/store.go` | Local exact-digest trust records |
| `internal/adaptertrust/policy.go` | Planning, interactive apply, and schedule scopes |
| `internal/adapterdev/init.go` | New custom bundle scaffold |
| `internal/adapterdev/harness.go` | Fixture-based custom adapter testing |
| `internal/cli/adapter_update.go` | Update and rollback commands |
| `internal/cli/adapter_trust.go` | Init, validate, test, and trust commands |
| `tests/e2e/adapter_lifecycle_test.go` | Patch-without-rebuild and rollback proof |

## Task 1: Define canonical signed metadata and the first trust root

**Files:**
- Create: `schemas/adapter-index/v1.json`
- Create: `internal/signing/canonical.go`
- Create: `internal/signing/verify.go`
- Create: `internal/signing/verify_test.go`
- Create: `trust/first-party-ed25519.pub`
- Create: `trust/embed.go`

**Interfaces:**
- Produces: `signing.CanonicalJSON(value any) ([]byte, error)`
- Produces: `signing.Verify(publicKey ed25519.PublicKey, payload, signature []byte) error`
- Produces: `signing.VerifyArtifact(target signing.Target, artifact []byte) error`

- [ ] **Step 1: Write canonicalization, tamper, expiry, and key tests**

```go
func TestCanonicalJSONIgnoresMapInsertionOrder(t *testing.T) {
	left := map[string]any{"b": 2, "a": 1}
	right := map[string]any{"a": 1, "b": 2}
	leftJSON, err := CanonicalJSON(left)
	if err != nil {
		t.Fatal(err)
	}
	rightJSON, err := CanonicalJSON(right)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(leftJSON, rightJSON); diff != "" {
		t.Fatalf("canonical bytes mismatch (-left +right):\n%s", diff)
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"adapterId":"codex","version":"1.0.0"}`)
	signature := ed25519.Sign(privateKey, payload)
	payload[10] ^= 1
	if err := Verify(publicKey, payload, signature); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Verify() error = %v", err)
	}
}
```

Add tests for wrong key, malformed base64, artifact size mismatch, digest
mismatch, unknown key ID, duplicate target paths, expired metadata, and
rollback to a lower monotonically increasing index version.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/signing
```

Expected: FAIL because signing code and fixtures do not exist.

- [ ] **Step 3: Implement signed metadata and create keys safely**

Canonical JSON rules:

- UTF-8;
- lexical object keys;
- no insignificant whitespace;
- JSON number representation accepted only when round-trippable;
- arrays preserve order;
- duplicate keys are rejected before canonicalization.

Use this signed target:

```go
type Target struct {
	AdapterID        string `json:"adapterId"`
	AdapterVersion   string `json:"adapterVersion"`
	SPIVersion       int    `json:"spiVersion"`
	CoreMin          int    `json:"coreMin"`
	CoreMax          int    `json:"coreMax"`
	Path             string `json:"path"`
	Size             int64  `json:"size"`
	SHA256           string `json:"sha256"`
	BundleDigest     string `json:"bundleDigest"`
	AgentVersionMin  string `json:"agentVersionMin,omitempty"`
}
```

Generate test keys in memory with `ed25519.GenerateKey(rand.Reader)`.

Generate a separate production public key outside the repository:

```bash
mkdir -p trust
umask 077
openssl genpkey -algorithm ED25519 -out "${HOME}/.treeclear-release-ed25519.pem"
openssl pkey -in "${HOME}/.treeclear-release-ed25519.pem" -pubout -out trust/first-party-ed25519.pub
```

Commit only the production public key. Unit tests generate ephemeral keys and
must not write private key material to disk.

Create `trust/embed.go`:

```go
package trust

import _ "embed"

//go:embed first-party-ed25519.pub
var FirstPartyEd25519PEM []byte
```

Production verification parses only `trust.FirstPartyEd25519PEM`; it does not
look for the repository file at runtime. Add a test that parses the embedded
bytes and verifies their SHA-256 against the committed trust-root fixture.

Before the first release, a repository administrator stores that exact private
key in the protected release environment as
`TREECLEAR_ADAPTER_SIGNING_KEY_PEM`. The private key is never passed to a
development or pull-request workflow.

- [ ] **Step 4: Run signing and secret checks**

Run:

```bash
gofmt -w internal/signing
go test ./internal/signing
git grep -n "BEGIN PRIVATE KEY" && exit 1 || true
git status --short
```

Expected: tests PASS; no production private key appears in Git status or grep.

- [ ] **Step 5: Commit signing foundations**

```bash
git add schemas/adapter-index internal/signing trust
git commit -m "feat: verify signed adapter metadata"
```

## Task 2: Store immutable adapter versions and a deterministic lock

**Files:**
- Create: `internal/adapterstore/layout.go`
- Create: `internal/adapterstore/lock.go`
- Create: `internal/adapterstore/store.go`
- Create: `internal/adapterstore/store_test.go`
- Modify: `internal/adapter/registry.go`
- Test: `internal/adapter/registry_test.go`

**Interfaces:**
- Produces: `adapterstore.Store.Install(ctx context.Context, bundle adapter.Bundle) error`
- Produces: `adapterstore.Store.Activate(ctx context.Context, next adapterstore.ActiveState) error`
- Produces: `adapterstore.Store.Rollback(ctx context.Context, adapterID string) error`
- Produces: `adapterstore.Store.Lock(ctx context.Context) (adapterstore.Lock, error)`
- Produces: `adapterstore.Lock.Digest() (string, error)`
- Extends registry resolution to active external override, then embedded baseline.

- [ ] **Step 1: Write crash-safe layout and lock tests**

```go
func TestActivateRetainsPreviousAndIsAtomic(t *testing.T) {
	store := newStore(t)
	installFixture(t, store, "copilot", "1.0.0")
	installFixture(t, store, "copilot", "1.1.0")

	first := ActiveState{
		SchemaVersion: 1,
		Generation:    1,
		Adapters: map[string]AdapterActivation{
			"copilot": {Current: "1.0.0"},
		},
		Lock: lockFor("copilot", "1.0.0"),
	}
	if err := store.Activate(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := ActiveState{
		SchemaVersion: 1,
		Generation:    2,
		Adapters: map[string]AdapterActivation{
			"copilot": {Current: "1.1.0", Previous: "1.0.0"},
		},
		Lock: lockFor("copilot", "1.1.0"),
	}
	if err := store.Activate(context.Background(), second); err != nil {
		t.Fatal(err)
	}

	state := readState(t, store)
	active := state.Adapters["copilot"]
	if active.Current != "1.1.0" || active.Previous != "1.0.0" {
		t.Fatalf("state = %#v", state)
	}
}
```

Add tests for path traversal in adapter IDs/versions, immutable reinstall with
different bytes, interrupted temporary directory, missing current version,
deterministic lock ordering, and lock digest changes.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/adapterstore ./internal/adapter
```

Expected: FAIL because the versioned store does not exist.

- [ ] **Step 3: Implement versioned install, activation, and lock**

Use layout:

```text
<data>/adapters/<adapter-id>/versions/<version>/
<data>/adapters/active-state.json
```

Do not use symlinks or per-adapter pointer files. Windows support and
cross-adapter transactions are safer with one atomically replaced
`active-state.json`:

```go
type AdapterActivation struct {
	Current  string `json:"current"`
	Previous string `json:"previous,omitempty"`
}

type LockEntry struct {
	AdapterID      string `json:"adapterId"`
	AdapterVersion string `json:"adapterVersion"`
	BundleDigest   string `json:"bundleDigest"`
	TrustRecordDigest string `json:"trustRecordDigest,omitempty"`
	ExecutableDigests []string `json:"executableDigests,omitempty"`
	InvocationDigests []string `json:"invocationDigests,omitempty"`
	Source         string `json:"source"`
}

type Lock struct {
	SchemaVersion int         `json:"schemaVersion"`
	IndexVersion  uint64      `json:"indexVersion"`
	IndexDigest   string      `json:"indexDigest"`
	Entries       []LockEntry `json:"entries"`
}

type ActiveState struct {
	SchemaVersion int                          `json:"schemaVersion"`
	Generation    uint64                       `json:"generation"`
	Adapters      map[string]AdapterActivation `json:"adapters"`
	Lock          Lock                         `json:"lock"`
}
```

Install into `<version>.tmp-<random>`, verify the complete bundle digest, sync,
and rename. Existing immutable versions are accepted only when their digest
matches exactly.

`active-state.json` persists active/previous versions, the complete adapter
lock, and the highest accepted signed index version/digest. Update rejects a
lower version after restart and rejects the same version with a different
digest.

`Activate` writes one fully verified next state to a sibling temporary file,
syncs it, and atomically renames it over `active-state.json`. A crash before
rename leaves the complete previous generation; a crash after rename leaves
the complete next generation. `Rollback` constructs and atomically writes a
new generation that swaps one adapter's current/previous selection.

- [ ] **Step 4: Run store and registry tests**

Run:

```bash
gofmt -w internal/adapterstore internal/adapter
go test ./internal/adapterstore ./internal/adapter
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit adapter storage**

```bash
git add internal/adapterstore internal/adapter
git commit -m "feat: store versioned adapter overrides"
```

## Task 3: Download and verify signed adapter updates

**Files:**
- Create: `internal/adapterupdate/index.go`
- Create: `internal/adapterupdate/client.go`
- Create: `internal/adapterupdate/client_test.go`
- Create: `internal/adapterupdate/update.go`
- Create: `internal/adapterupdate/update_test.go`

**Interfaces:**
- Produces: `adapterupdate.Client.FetchIndex(ctx context.Context, url string) (adapterupdate.SignedIndex, error)`
- Produces: `adapterupdate.Client.FetchTarget(ctx context.Context, baseURL string, target signing.Target) ([]byte, error)`
- Produces: `adapterupdate.Updater.Update(ctx context.Context, request adapterupdate.Request) (adapterupdate.Result, error)`

- [ ] **Step 1: Write HTTPS, size, signature, and compatibility tests**

Use `httptest.NewTLSServer` and inject its client. Cover:

- valid index and bundle;
- plain HTTP rejected except an injected test server;
- redirect to a different host rejected;
- response larger than declared size rejected;
- wrong digest or signature rejected;
- expired index rejected;
- core/SPI incompatibility rejected;
- lower index version rejected;
- one invalid target prevents any activation.

Example:

```go
func TestUpdateRejectsDigestMismatchBeforeInstall(t *testing.T) {
	server := signedServer(t, withCorruptArtifact())
	updater, store := newUpdater(t, server)

	_, err := updater.Update(context.Background(), Request{IndexURL: server.URL + "/index.json"})

	if !errors.Is(err, signing.ErrDigestMismatch) {
		t.Fatalf("Update() error = %v", err)
	}
	if got := installedVersions(t, store, "codex"); len(got) != 0 {
		t.Fatalf("installed versions = %v", got)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/adapterupdate
```

Expected: FAIL because update code does not exist.

- [ ] **Step 3: Implement bounded update download and verification**

Signed index:

```go
type Index struct {
	Format      string           `json:"format"`
	Version     uint64           `json:"version"`
	GeneratedAt time.Time        `json:"generatedAt"`
	ExpiresAt   time.Time        `json:"expiresAt"`
	Targets     []signing.Target `json:"targets"`
}

type SignedIndex struct {
	Signed    json.RawMessage `json:"signed"`
	KeyID     string          `json:"keyId"`
	Signature string          `json:"signature"`
}
```

HTTP client rules:

- HTTPS only in production;
- 10-second total timeout;
- maximum 2 MiB index;
- target bytes limited to exact declared size plus one byte;
- no cross-host redirects;
- no credentials or ambient cookies;
- `User-Agent: treeclear/<version>`;
- no automatic retries during one update transaction.

Default URL:

```text
https://github.com/hellices/treeclear/releases/latest/download/adapters-index.json
```

Verify every target before installing any target. Update remains an explicit
foreground command.

- [ ] **Step 4: Run update and full tests**

Run:

```bash
gofmt -w internal/adapterupdate
go test ./internal/adapterupdate
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit verified updates**

```bash
git add internal/adapterupdate
git commit -m "feat: fetch verified adapter updates"
```

## Task 4: Probe, activate, and automatically roll back adapters

**Files:**
- Modify: `internal/adapterupdate/update.go`
- Create: `internal/adapterupdate/health.go`
- Create: `internal/adapterupdate/health_test.go`
- Modify: `internal/adapterstore/store.go`
- Test: `internal/adapterstore/store_test.go`

**Interfaces:**
- Produces: `adapterupdate.HealthChecker.Check(ctx context.Context, bundle adapter.Bundle) adapterupdate.Health`
- Produces: atomic update transaction with automatic rollback.

- [ ] **Step 1: Write probe failure and rollback tests**

```go
func TestUpdateRollsBackFailedHealthProbe(t *testing.T) {
	updater, store := updaterWithActiveVersion(t, "opencode", "1.0.0")
	updater.Health = fakeHealth{ByVersion: map[string]error{"1.1.0": errors.New("fixture mismatch")}}

	result, err := updater.Update(context.Background(), requestFor("opencode", "1.1.0"))

	if err == nil {
		t.Fatal("Update() succeeded")
	}
	if got := activeVersion(t, store, "opencode"); got != "1.0.0" {
		t.Fatalf("active version = %q", got)
	}
	if !result.RolledBack {
		t.Fatalf("result = %#v", result)
	}
}
```

Also test failed activation before pointer switch, crash after pointer switch,
unhealthy first install, and rollback with missing previous version.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/adapterupdate ./internal/adapterstore
```

Expected: FAIL because health activation is incomplete.

- [ ] **Step 3: Implement health checks and rollback transaction**

A health check must:

1. validate manifest and schemas;
2. run every bundled synthetic fixture through its declared source decoder and
   mapping;
3. compare normalized evidence to the bundled golden evidence;
4. run installation probes without reading user sessions;
5. reject warnings classified as fatal.

Update order:

```text
download -> verify all -> install all immutable versions -> health all ->
construct complete ActiveState -> one atomic Activate -> report success
```

If activation fails before atomic rename, the previous `active-state.json`
remains authoritative. No compensating multi-file rollback is required.

- [ ] **Step 4: Run lifecycle and race tests**

Run:

```bash
gofmt -w internal/adapterupdate internal/adapterstore
go test ./internal/adapterupdate ./internal/adapterstore
go test -race ./internal/adapterupdate ./internal/adapterstore
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit health rollback**

```bash
git add internal/adapterupdate internal/adapterstore
git commit -m "feat: roll back unhealthy adapters"
```

## Task 5: Add scoped local trust records

**Files:**
- Create: `internal/adaptertrust/store.go`
- Create: `internal/adaptertrust/policy.go`
- Create: `internal/adaptertrust/store_test.go`
- Modify: `internal/adapter/registry.go`
- Modify: `internal/plan/build.go`
- Test: `internal/plan/build_test.go`

**Interfaces:**
- Produces: `adaptertrust.Store.Trust(ctx context.Context, record adaptertrust.Record) error`
- Produces: `adaptertrust.Store.Resolve(ctx context.Context, adapterID, digest string) (adaptertrust.Record, bool)`
- Produces: `adaptertrust.Policy.Allows(mode adaptertrust.Mode, record adaptertrust.Record) bool`

- [ ] **Step 1: Write trust-scope tests**

Trust scopes:

```go
const (
	ScopePlan             Scope = "plan"
	ScopeInteractiveApply Scope = "interactive-apply"
)
```

Tests prove:

- plan-only trust cannot influence apply classification;
- interactive apply trust cannot influence scheduled apply;
- digest mismatch invalidates trust;
- command adapters require every executable digest;
- trust records cannot set a mutation capability;
- corrupted trust store blocks custom evidence rather than resetting to empty
  success.
- a `ScopePlan` record cannot produce `AdapterStatus.Trusted=true` for
  interactive or scheduled apply;
- `ScopeInteractiveApply` works only when
  `CollectionRequest.ApplyMode=ApplyInteractive`;
- no local scope is accepted when `ApplyMode=ApplyScheduled`.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/adaptertrust ./internal/plan
```

Expected: FAIL because trust policy does not exist.

- [ ] **Step 3: Implement exact-digest trust**

```go
type Record struct {
	SchemaVersion     int      `json:"schemaVersion"`
	AdapterID         string   `json:"adapterId"`
	AdapterDigest     string   `json:"adapterDigest"`
	ExecutableDigests []string `json:"executableDigests,omitempty"`
	Scopes            []Scope  `json:"scopes"`
	TrustedAt         time.Time `json:"trustedAt"`
}
```

Store records in a private atomic JSON file. Sort by adapter ID and digest.

Implement `Record.Digest() (string, error)` over canonical record JSON. The
registry copies that value to `adapterstore.LockEntry.TrustRecordDigest` and
every normalized `domain.AgentEvidence.TrustRecordDigest`. The plan adapter
lock and candidate evidence fingerprint therefore both change when a trust
record changes.

The plan must record the exact trust record digest used for each custom
adapter. Apply reloads the record and compares its digest before evidence
collection. Scheduled mode accepts no local custom trust record.

Plan creation sets `Plan.IntendedApplyMode` before adapter collection. The
trust policy evaluates the requested mode while producing
`AdapterStatus.Trusted`; changing the mode invalidates the HMAC-signed plan and
is rejected by apply.

- [ ] **Step 4: Run trust and full tests**

Run:

```bash
gofmt -w internal/adaptertrust internal/adapter internal/plan
go test ./internal/adaptertrust ./internal/adapter ./internal/plan
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit trust policy**

```bash
git add internal/adaptertrust internal/adapter internal/plan
git commit -m "feat: add scoped adapter trust"
```

## Task 6: Add the custom adapter authoring harness

**Files:**
- Create: `internal/adapterdev/init.go`
- Create: `internal/adapterdev/init_test.go`
- Create: `internal/adapterdev/harness.go`
- Create: `internal/adapterdev/harness_test.go`
- Create: `internal/adapterdev/templates/adapter.json`
- Create: `internal/adapterdev/templates/session-v1.json`
- Create: `internal/cli/adapter_trust.go`
- Modify: `internal/cli/adapters.go`
- Test: `internal/cli/adapters_test.go`

**Interfaces:**
- Produces commands: `adapters init`, `validate`, `test`, and `trust`
- Produces a fixture report with exact normalized evidence diffs.

- [ ] **Step 1: Write authoring workflow tests**

Test that:

```bash
treeclear adapters init ./my-adapter
treeclear adapters validate ./my-adapter
treeclear adapters test ./my-adapter
```

creates a valid file-json adapter, schema, input fixture, and expected evidence
fixture without external network access.

Test path-exists refusal, invalid fixture, unknown field, schema mismatch,
command-backed warning, exact digest display, and trust-scope enforcement.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/adapterdev ./internal/cli -run Adapter
```

Expected: FAIL because authoring commands do not exist.

- [ ] **Step 3: Implement scaffold, validation, test, and trust commands**

Generated layout:

```text
my-adapter/
  adapter.json
  schemas/session-v1.json
  fixtures/input.json
  fixtures/expected-evidence.json
  README.md
```

`validate` performs schema and semantic checks only.

`test` runs only bundled fixture files by default. Command-backed fixture
execution requires `--allow-command` and prints the exact executable path and
digest before prompting.

`trust` syntax:

```text
treeclear adapters trust ./my-adapter --digest sha256:<digest> --scope plan
treeclear adapters trust ./my-adapter --digest sha256:<digest> --scope interactive-apply --executable sha256:<digest>
```

Reject `scheduled-apply` as a local trust scope.

- [ ] **Step 4: Run authoring and full tests**

Run:

```bash
gofmt -w internal/adapterdev internal/cli
go test ./internal/adapterdev ./internal/cli
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit the authoring kit**

```bash
git add internal/adapterdev internal/cli
git commit -m "feat: add custom adapter authoring"
```

## Task 7: Expose update and rollback commands and prove patchability

**Files:**
- Create: `internal/cli/adapter_update.go`
- Create: `internal/cli/adapter_update_test.go`
- Modify: `internal/cli/root.go`
- Create: `tests/e2e/adapter_lifecycle_test.go`
- Modify: `README.md`

**Interfaces:**
- Produces: `treeclear adapters update`
- Produces: `treeclear adapters rollback <adapter-id>`
- Proves a provider format patch works without rebuilding Treeclear.

- [ ] **Step 1: Write the patch-without-rebuild e2e test**

The test:

1. builds Treeclear once;
2. creates a signed adapter index with fixture adapter 1.0.0;
3. runs update and verifies activation;
4. changes provider fixture shape;
5. confirms 1.0.0 becomes unknown;
6. publishes signed adapter 1.1.0 with only manifest/schema/mapping changes;
7. runs update without rebuilding Treeclear;
8. confirms normalized evidence succeeds;
9. publishes corrupt 1.2.0;
10. confirms automatic rollback to 1.1.0;
11. runs an offline plan using the pinned 1.1.0 lock;
12. replaces the updater HTTP transport with one that fails the test if called;
13. applies the plan and confirms the adapter store and lock digest are
    unchanged.

- [ ] **Step 2: Run the e2e test and verify it fails**

Run:

```bash
go test ./tests/e2e -run TestAdapterPatchWithoutCoreRebuild -v
```

Expected: FAIL because CLI update wiring is absent.

- [ ] **Step 3: Implement foreground update and rollback CLI**

Commands:

```text
treeclear adapters update [--index-url <https-url>] [--format json]
treeclear adapters rollback <adapter-id> [--format json]
```

Both commands acquire the shared `internal/statelock` exclusive lock with
operation name `adapter-update`. They fail immediately when an apply lock
exists and hold the lock through verification, health checks, activation,
lockfile replacement, or rollback.

Human output includes installed, activated, retained previous, rolled-back,
and rejected versions. JSON includes typed error categories without raw
session content.

README documents:

- built-in fallback;
- explicit updates;
- no updates during cleanup;
- custom adapter developer mode;
- rollback;
- offline apply.

- [ ] **Step 4: Run complete adapter lifecycle verification**

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

Expected: every command succeeds; corrupt updates never become active.

- [ ] **Step 5: Commit adapter lifecycle**

```bash
git add internal/cli tests/e2e README.md
git commit -m "feat: manage adapter updates safely"
```

## Plan 3 Completion Gate

Run:

```bash
go test ./...
go test -race ./...
go test ./tests/e2e -v
go build ./cmd/treeclear
GOOS=windows GOARCH=amd64 go build ./cmd/treeclear
rm -f treeclear.exe
treeclear adapters list --format json
git status --short
```

Expected:

- first-party updates require a valid signature and digest;
- incompatible, expired, corrupt, or unhealthy updates remain inactive;
- rollback restores the previous known-good adapter;
- a fixture format change is patched without rebuilding the core;
- plans pin the exact adapter and executable identities;
- unsigned custom adapters cannot influence scheduled apply;
- private signing keys are absent from the repository;
- the working tree is clean.
