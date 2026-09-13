package plan

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
)

func storedPlanFixture() domain.Plan {
	stamp := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	return domain.Plan{
		SchemaVersion: 1, ID: "plan_fixture", GeneratedAt: stamp, ExpiresAt: stamp.Add(15 * time.Minute),
		ToolVersion: "test", IntendedApplyMode: domain.ApplyInteractive,
		PolicyDigest: "sha256:policy", AdapterLockDigest: "sha256:adapters",
		ExecutableIdentities: []domain.ExecutableIdentity{{Path: "/bin/provider", SHA256: "sha256:executable", Arguments: []string{"--read-only"}}},
		Scope:                domain.PlanScope{Roots: []string{"/repo"}}, Candidates: []domain.Candidate{candidateFixture()},
		Summary: domain.PlanSummary{Safe: 1, ReclaimableBytes: 100}, Warnings: []string{"explanation"},
	}
}

func TestPlanIntegrityRoundTrip(test *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	value := storedPlanFixture()
	before, err := json.Marshal(value)
	if err != nil {
		test.Fatal(err)
	}
	contents, err := encodeSignedPlan(value, key)
	if err != nil {
		test.Fatal(err)
	}
	if len(contents) == 0 {
		test.Fatal("signed plan is empty")
	}
	loaded, err := decodeAuthenticatedPlan(contents, key)
	if err != nil {
		test.Fatal(err)
	}
	keyDigest := sha256.Sum256(key)
	if loaded.Integrity.Algorithm != "hmac-sha256" || loaded.Integrity.KeyID != "sha256:"+hex.EncodeToString(keyDigest[:]) || len(loaded.Integrity.MAC) != 64 {
		test.Fatalf("integrity metadata = %+v", loaded.Integrity)
	}
	loaded.Integrity = domain.PlanIntegrity{}
	after, err := json.Marshal(loaded)
	if err != nil || !bytes.Equal(before, after) {
		test.Fatalf("round trip changed plan: %s, error = %v", after, err)
	}
	unchanged, err := json.Marshal(value)
	if err != nil || !bytes.Equal(before, unchanged) {
		test.Fatal("signing mutated the input")
	}
	if _, err := decodeAuthenticatedPlan(append(append([]byte("\n"), contents...), '\n'), key); err != nil {
		test.Fatalf("outer whitespace rejected: %v", err)
	}
}

func TestPlanIntegrityKnownCanonicalMAC(test *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	value := domain.Plan{SchemaVersion: 1, ID: "plan_vector"}
	keyDigest := sha256.Sum256(key)
	keyID := "sha256:" + hex.EncodeToString(keyDigest[:])
	payload := `{"schemaVersion":1,"planId":"plan_vector","generatedAt":"0001-01-01T00:00:00Z","expiresAt":"0001-01-01T00:00:00Z","toolVersion":"","intendedApplyMode":"","policyDigest":"","adapterLockDigest":"","scope":{"roots":null},"candidates":null,"summary":{"safe":0,"review":0,"protected":0,"reclaimableBytes":0},"integrity":{"algorithm":"hmac-sha256","keyId":"` + keyID + `","mac":""}}`
	authenticator := hmac.New(sha256.New, key)
	_, _ = authenticator.Write([]byte(payload))
	expected := strings.Replace(payload, `"mac":""`, `"mac":"`+hex.EncodeToString(authenticator.Sum(nil))+`"`, 1)
	actual, err := encodeSignedPlan(value, key)
	if err != nil || string(actual) != expected {
		test.Fatalf("canonical signed plan = %s, error = %v; want %s", actual, err, expected)
	}
}

func TestPlanIntegrityCoversCompleteDocument(test *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	contents, err := encodeSignedPlan(storedPlanFixture(), key)
	if err != nil {
		test.Fatal(err)
	}
	if len(contents) == 0 {
		test.Fatal("signing returned no document to authenticate")
	}
	cases := []struct {
		name   string
		change func(*domain.Plan)
	}{
		{"schema", func(value *domain.Plan) { value.SchemaVersion++ }},
		{"identifier", func(value *domain.Plan) { value.ID = "plan_other" }},
		{"generation", func(value *domain.Plan) { value.GeneratedAt = value.GeneratedAt.Add(time.Nanosecond) }},
		{"expiry", func(value *domain.Plan) { value.ExpiresAt = value.ExpiresAt.Add(time.Minute) }},
		{"tool", func(value *domain.Plan) { value.ToolVersion += "-changed" }},
		{"mode", func(value *domain.Plan) { value.IntendedApplyMode = domain.ApplyScheduled }},
		{"policy", func(value *domain.Plan) { value.PolicyDigest += "changed" }},
		{"adapter lock", func(value *domain.Plan) { value.AdapterLockDigest += "changed" }},
		{"executable", func(value *domain.Plan) { value.ExecutableIdentities[0].SHA256 += "changed" }},
		{"scope", func(value *domain.Plan) { value.Scope.Roots[0] = "/other" }},
		{"candidate ID", func(value *domain.Plan) { value.Candidates[0].ID += "changed" }},
		{"action", func(value *domain.Plan) { value.Candidates[0].Action = "none" }},
		{"path", func(value *domain.Plan) { value.Candidates[0].Worktree.Path = "/other" }},
		{"decision", func(value *domain.Plan) { value.Candidates[0].Decision.Classification = domain.Protected }},
		{"fingerprint", func(value *domain.Plan) { value.Candidates[0].Fingerprint += "changed" }},
		{"snapshot", func(value *domain.Plan) { value.Candidates[0].Snapshot.Required = false }},
		{"reason text", func(value *domain.Plan) { value.Candidates[0].Decision.Reasons[0].Message += "changed" }},
		{"observation", func(value *domain.Plan) { value.Candidates[0].Evidence.Agents[0].ObservedAt = value.ExpiresAt }},
		{"agent explanation", func(value *domain.Plan) { value.Candidates[0].Evidence.Agents[0].Warnings = []string{"changed"} }},
		{"process diagnostic", func(value *domain.Plan) { value.Candidates[0].Evidence.Processes[0].Error = "changed" }},
		{"summary", func(value *domain.Plan) { value.Summary.Safe++ }},
		{"warning", func(value *domain.Plan) { value.Warnings[0] += "changed" }},
		{"algorithm", func(value *domain.Plan) { value.Integrity.Algorithm = "sha256" }},
		{"key ID", func(value *domain.Plan) { value.Integrity.KeyID += "changed" }},
		{"MAC", func(value *domain.Plan) { value.Integrity.MAC = strings.Repeat("0", 64) }},
		{"missing MAC", func(value *domain.Plan) { value.Integrity.MAC = "" }},
		{"malformed MAC", func(value *domain.Plan) { value.Integrity.MAC = strings.Repeat("z", 64) }},
		{"noncanonical MAC", func(value *domain.Plan) { value.Integrity.MAC = strings.ToUpper(value.Integrity.MAC) }},
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			var changed domain.Plan
			if err := json.Unmarshal(contents, &changed); err != nil {
				test.Fatal(err)
			}
			scenario.change(&changed)
			tampered, err := json.Marshal(changed)
			if err != nil {
				test.Fatal(err)
			}
			loaded, err := decodeAuthenticatedPlan(tampered, key)
			if !errors.Is(err, ErrPlanIntegrity) || loaded.ID != "" || len(loaded.Candidates) != 0 {
				test.Fatalf("tampered document returned plan %q and error %v", loaded.ID, err)
			}
		})
	}
	if _, err := decodeAuthenticatedPlan(contents, bytes.Repeat([]byte{0x24}, 32)); !errors.Is(err, ErrPlanIntegrity) {
		test.Fatalf("foreign installation key error = %v", err)
	}
}

func TestPlanIntegrityRejectsAmbiguousEncoding(test *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	contents, err := encodeSignedPlan(storedPlanFixture(), key)
	if err != nil {
		test.Fatal(err)
	}
	if len(contents) == 0 {
		test.Fatal("signing returned no document to authenticate")
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, contents, "", "  "); err != nil {
		test.Fatal(err)
	}
	cases := map[string][]byte{
		"empty":                   {},
		"truncated":               contents[:len(contents)/2],
		"pretty printed":          formatted.Bytes(),
		"duplicate field":         append([]byte(`{"planId":"plan_fixture",`), contents[1:]...),
		"unknown field":           append([]byte(`{"unexpected":true,`), contents[1:]...),
		"case alias":              bytes.Replace(contents, []byte(`"planId"`), []byte(`"PlanId"`), 1),
		"trailing document":       append(append([]byte{}, contents...), []byte(`{}`)...),
		"alternate time spelling": bytes.Replace(contents, []byte(`12:00:00Z`), []byte(`12:00:00+00:00`), 1),
		"invalid UTF8":            bytes.Replace(contents, []byte(`explanation`), []byte{0xff}, 1),
	}
	for name, value := range cases {
		test.Run(name, func(test *testing.T) {
			if _, err := decodeAuthenticatedPlan(value, key); !errors.Is(err, ErrPlanIntegrity) {
				test.Fatalf("noncanonical document error = %v", err)
			}
		})
	}
}

func TestPlanIntegrityRejectsInvalidKeysAndValues(test *testing.T) {
	for _, size := range []int{0, 1, 31, 33, 64} {
		key := bytes.Repeat([]byte{0x42}, size)
		if _, err := encodeSignedPlan(storedPlanFixture(), key); !errors.Is(err, ErrIntegrityKey) {
			test.Errorf("signing with key size %d: %v", size, err)
		}
		if _, err := decodeAuthenticatedPlan([]byte(`{}`), key); !errors.Is(err, ErrIntegrityKey) {
			test.Errorf("verifying with key size %d: %v", size, err)
		}
	}
	for _, change := range []func(*domain.Plan){
		func(value *domain.Plan) { value.Warnings[0] = "\xff" },
		func(value *domain.Plan) { value.Candidates[0].Decision.Reasons[0].Message = "\xff" },
		func(value *domain.Plan) { value.Candidates[0].Evidence.Agents[0].Warnings = []string{"\xff"} },
		func(value *domain.Plan) { value.ExpiresAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) },
	} {
		value := storedPlanFixture()
		change(&value)
		if _, err := encodeSignedPlan(value, bytes.Repeat([]byte{0x42}, 32)); err == nil {
			test.Error("invalid signed value was accepted")
		}
	}
}

func TestPlanIntegrityNormalizesInstantsWithoutMutation(test *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	value := storedPlanFixture()
	stamp := value.GeneratedAt.Add(123 * time.Nanosecond).In(time.FixedZone("offset with seconds", 1817))
	value.GeneratedAt = stamp
	value.ExpiresAt = stamp.Add(time.Minute)
	value.Candidates[0].Worktree.LastCommitAt = stamp
	value.Candidates[0].Worktree.MetadataModifiedAt = stamp
	value.Candidates[0].Evidence.Processes[0].CreatedAt = stamp
	value.Candidates[0].Evidence.Agents[0].CreatedAt = stamp
	value.Candidates[0].Evidence.Agents[0].UpdatedAt = stamp
	value.Candidates[0].Evidence.Agents[0].ObservedAt = stamp
	value.Candidates[0].Evidence.Agents[0].ProcessRefs[0].CreatedAt = stamp
	before, err := json.Marshal(value)
	if err != nil {
		test.Fatal(err)
	}
	contents, err := encodeSignedPlan(value, key)
	if err != nil {
		test.Fatal(err)
	}
	loaded, err := decodeAuthenticatedPlan(contents, key)
	if err != nil {
		test.Fatal(err)
	}
	if len(loaded.Candidates) != 1 || len(loaded.Candidates[0].Evidence.Agents) == 0 || len(loaded.Candidates[0].Evidence.Processes) == 0 || len(loaded.Candidates[0].Evidence.Agents[0].ProcessRefs) == 0 {
		test.Fatal("authenticated plan lost candidate evidence")
	}
	for _, actual := range []time.Time{
		loaded.GeneratedAt, loaded.Candidates[0].Worktree.LastCommitAt, loaded.Candidates[0].Worktree.MetadataModifiedAt,
		loaded.Candidates[0].Evidence.Processes[0].CreatedAt, loaded.Candidates[0].Evidence.Agents[0].CreatedAt,
		loaded.Candidates[0].Evidence.Agents[0].UpdatedAt, loaded.Candidates[0].Evidence.Agents[0].ObservedAt,
		loaded.Candidates[0].Evidence.Agents[0].ProcessRefs[0].CreatedAt,
	} {
		if !actual.Equal(stamp) || actual.Location() != time.UTC {
			test.Errorf("normalized time = %v, want %v", actual, stamp.UTC())
		}
	}
	after, err := json.Marshal(value)
	if err != nil || !bytes.Equal(before, after) {
		test.Fatal("timestamp normalization mutated caller data")
	}
}

func TestPlanIntegrityRetainsPlanningOnlyEvidence(test *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	value := storedPlanFixture()
	value.Candidates[0].Evidence.Agents[0].RevalidationMode = "planning-only"
	contents, err := encodeSignedPlan(value, key)
	if err != nil {
		test.Fatal(err)
	}
	loaded, err := decodeAuthenticatedPlan(contents, key)
	if err != nil {
		test.Fatal(err)
	}
	if len(loaded.Candidates) != 1 || len(loaded.Candidates[0].Evidence.Agents) != 2 {
		test.Fatal("signed plan lost planning-only evidence")
	}
	originalFingerprint := fingerprintFor(test, loaded.Candidates[0])
	loaded.Candidates[0].Evidence.Agents[0].RawFingerprint = "changed-planning-only-record"
	if fingerprintFor(test, loaded.Candidates[0]) != originalFingerprint {
		test.Fatal("planning-only record unexpectedly changed removal fingerprint")
	}
	tampered, err := json.Marshal(loaded)
	if err != nil {
		test.Fatal(err)
	}
	if _, err := decodeAuthenticatedPlan(tampered, key); !errors.Is(err, ErrPlanIntegrity) {
		test.Fatalf("planning-only evidence is not authenticated: %v", err)
	}
}

func TestPlanIntegrityRejectsCompactUnauthenticatedArraysWithoutExpansion(test *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	objects := strings.Repeat("{},", 4095) + "{}"
	metadata, err := json.Marshal(domain.PlanIntegrity{
		Algorithm: integrityAlgorithm, KeyID: integrityKeyID(key), MAC: strings.Repeat("0", 64),
	})
	if err != nil {
		test.Fatal(err)
	}
	for name, suffix := range map[string]string{
		"missing MAC": "}",
		"invalid MAC": `,"integrity":` + string(metadata) + "}",
	} {
		test.Run(name, func(test *testing.T) {
			contents := []byte(`{"candidates":[` + objects + "]" + suffix)
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			value, err := decodeAuthenticatedPlan(contents, key)
			runtime.ReadMemStats(&after)
			if !errors.Is(err, ErrPlanIntegrity) || value.ID != "" || len(value.Candidates) != 0 {
				test.Fatalf("unauthenticated document = %q, %v", value.ID, err)
			}
			allocated := after.TotalAlloc - before.TotalAlloc
			maximum := uint64(1<<20) + 16*uint64(len(contents))
			test.Logf("%d input bytes allocated %d bytes before rejection", len(contents), allocated)
			if allocated > maximum {
				test.Fatalf("unauthenticated JSON expanded: allocated %d bytes, maximum %d", allocated, maximum)
			}
		})
	}
}

func TestPlanIntegrityRejectsAuthenticatedNoncanonicalDocuments(test *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	contents, err := encodeSignedPlan(storedPlanFixture(), key)
	if err != nil {
		test.Fatal(err)
	}
	cases := map[string][]byte{
		"duplicate field": append([]byte(`{"planId":"plan_fixture",`), contents[1:]...),
		"unknown field":   append([]byte(`{"future":true,`), contents[1:]...),
		"case alias":      bytes.Replace(contents, []byte(`"planId"`), []byte(`"PlanId"`), 1),
		"time spelling":   bytes.Replace(contents, []byte(`12:00:00Z`), []byte(`12:00:00+00:00`), 1),
		"whitespace":      append([]byte("{\n  "), contents[1:]...),
		"reordered fields": bytes.Replace(contents,
			[]byte(`{"schemaVersion":1,"planId":"plan_fixture",`),
			[]byte(`{"planId":"plan_fixture","schemaVersion":1,`), 1),
		"invalid JSON": bytes.Replace(contents, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":`), 1),
	}
	for name, changed := range cases {
		test.Run(name, func(test *testing.T) {
			marker := []byte(`"mac":"`)
			start := bytes.LastIndex(changed, marker) + len(marker)
			end := start + bytes.IndexByte(changed[start:], '"')
			if end-start != sha256.Size*2 {
				test.Fatal("fixture has no MAC field")
			}
			authenticator := hmac.New(sha256.New, key)
			_, _ = authenticator.Write(changed[:start])
			_, _ = authenticator.Write(changed[end:])
			copy(changed[start:end], hex.EncodeToString(authenticator.Sum(nil)))
			if value, err := decodeAuthenticatedPlan(changed, key); !errors.Is(err, ErrPlanIntegrity) || len(value.Candidates) != 0 {
				test.Fatalf("authenticated noncanonical document accepted: %q, %v", value.ID, err)
			}
		})
	}
}
