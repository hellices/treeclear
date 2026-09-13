package plan

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
)

func TestEvidenceDigestCanonicalJSON(test *testing.T) {
	cases := []struct {
		name          string
		value         domain.EvidenceSet
		canonicalJSON string
	}{
		{
			name: "nil collections", value: domain.EvidenceSet{},
			canonicalJSON: `{"processes":null,"agents":null}`,
		},
		{
			name: "empty collections", value: domain.EvidenceSet{Processes: []domain.ProcessEvidence{}, Agents: []domain.AgentEvidence{}},
			canonicalJSON: `{"processes":[],"agents":[]}`,
		},
		{
			name: "zero process", value: domain.EvidenceSet{Processes: []domain.ProcessEvidence{{}}},
			canonicalJSON: `{"processes":[{"pid":0,"createdAt":"0001-01-01T00:00:00Z","executable":"","state":"","fingerprint":""}],"agents":null}`,
		},
		{
			name: "zero agent", value: domain.EvidenceSet{Agents: []domain.AgentEvidence{{}}},
			canonicalJSON: `{"processes":null,"agents":[{"adapterId":"","adapterVersion":"","bundleDigest":"","provider":"","sourceId":"","state":"","createdAt":"0001-01-01T00:00:00Z","updatedAt":"0001-01-01T00:00:00Z","observedAt":"0001-01-01T00:00:00Z","sourceKind":"","supportGrade":"","confidence":"","rawFingerprint":"","revalidationMode":""}]}`,
		},
		{
			name:          "valid UTF-8 and JSON escaping",
			value:         domain.EvidenceSet{Warnings: []string{"é", "e\u0301", "<>&", "quote\"\\\n", "\ufffd", "\u2028\u2029"}},
			canonicalJSON: `{"processes":null,"agents":null,"warnings":["é","é","\u003c\u003e\u0026","quote\"\\\n","�","\u2028\u2029"]}`,
		},
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			expected := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(scenario.canonicalJSON)))
			for attempt := range 3 {
				if actual := evidenceDigestFor(test, scenario.value); actual != expected {
					test.Fatalf("EvidenceDigest() attempt %d = %q, want %q for %s", attempt, actual, expected, scenario.canonicalJSON)
				}
			}
		})
	}
}

func TestEvidenceDigestChangesWithEveryStoredField(test *testing.T) {
	changes := []struct {
		name   string
		change func(*domain.EvidenceSet)
	}{
		{"process PID", func(value *domain.EvidenceSet) { value.Processes[0].PID++ }},
		{"process creation", func(value *domain.EvidenceSet) {
			value.Processes[0].CreatedAt = value.Processes[0].CreatedAt.Add(time.Nanosecond)
		}},
		{"process executable", func(value *domain.EvidenceSet) { value.Processes[0].Executable += "-changed" }},
		{"process cwd", func(value *domain.EvidenceSet) { value.Processes[0].CWD += "-changed" }},
		{"process state", func(value *domain.EvidenceSet) { value.Processes[0].State = domain.EvidenceActive }},
		{"process error content", func(value *domain.EvidenceSet) { value.Processes[0].Error += "-changed" }},
		{"process fingerprint", func(value *domain.EvidenceSet) { value.Processes[0].Fingerprint += "-changed" }},
		{"agent adapter ID", func(value *domain.EvidenceSet) { value.Agents[0].AdapterID += "-changed" }},
		{"agent adapter version", func(value *domain.EvidenceSet) { value.Agents[0].AdapterVersion += "-changed" }},
		{"agent bundle digest", func(value *domain.EvidenceSet) { value.Agents[0].BundleDigest += "-changed" }},
		{"agent provider", func(value *domain.EvidenceSet) { value.Agents[0].Provider += "-changed" }},
		{"agent source ID", func(value *domain.EvidenceSet) { value.Agents[0].SourceID += "-changed" }},
		{"agent session ID", func(value *domain.EvidenceSet) { value.Agents[0].SessionID += "-changed" }},
		{"agent thread ID", func(value *domain.EvidenceSet) { value.Agents[0].ThreadID += "-changed" }},
		{"agent project ID", func(value *domain.EvidenceSet) { value.Agents[0].ProjectID += "-changed" }},
		{"agent cwd", func(value *domain.EvidenceSet) { value.Agents[0].CWD += "-changed" }},
		{"agent repository root", func(value *domain.EvidenceSet) { value.Agents[0].RepositoryRoot += "-changed" }},
		{"agent worktree path", func(value *domain.EvidenceSet) { value.Agents[0].WorktreePath += "-changed" }},
		{"agent state", func(value *domain.EvidenceSet) { value.Agents[0].State = domain.EvidenceActive }},
		{"agent creation", func(value *domain.EvidenceSet) {
			value.Agents[0].CreatedAt = value.Agents[0].CreatedAt.Add(time.Nanosecond)
		}},
		{"agent update", func(value *domain.EvidenceSet) {
			value.Agents[0].UpdatedAt = value.Agents[0].UpdatedAt.Add(time.Nanosecond)
		}},
		{"agent observation", func(value *domain.EvidenceSet) {
			value.Agents[0].ObservedAt = value.Agents[0].ObservedAt.Add(time.Nanosecond)
		}},
		{"reference PID", func(value *domain.EvidenceSet) { value.Agents[0].ProcessRefs[0].PID++ }},
		{"reference creation", func(value *domain.EvidenceSet) {
			value.Agents[0].ProcessRefs[0].CreatedAt = value.Agents[0].ProcessRefs[0].CreatedAt.Add(time.Nanosecond)
		}},
		{"reference executable", func(value *domain.EvidenceSet) { value.Agents[0].ProcessRefs[0].Executable += "-changed" }},
		{"reference fingerprint", func(value *domain.EvidenceSet) { value.Agents[0].ProcessRefs[0].Fingerprint += "-changed" }},
		{"binding presence", func(value *domain.EvidenceSet) { value.Agents[0].Binding = nil }},
		{"binding kind", func(value *domain.EvidenceSet) { value.Agents[0].Binding.Kind += "-changed" }},
		{"binding identifier", func(value *domain.EvidenceSet) { value.Agents[0].Binding.Identifier += "-changed" }},
		{"binding version", func(value *domain.EvidenceSet) { value.Agents[0].Binding.Version += "-changed" }},
		{"agent source kind", func(value *domain.EvidenceSet) { value.Agents[0].SourceKind += "-changed" }},
		{"agent support grade", func(value *domain.EvidenceSet) { value.Agents[0].SupportGrade = domain.TrustProcessOnly }},
		{"agent confidence", func(value *domain.EvidenceSet) { value.Agents[0].Confidence += "-changed" }},
		{"agent schema version", func(value *domain.EvidenceSet) { value.Agents[0].SchemaVersion += "-changed" }},
		{"agent raw fingerprint", func(value *domain.EvidenceSet) { value.Agents[0].RawFingerprint += "-changed" }},
		{"agent trust record", func(value *domain.EvidenceSet) { value.Agents[0].TrustRecordDigest += "-changed" }},
		{"executable presence", func(value *domain.EvidenceSet) { value.Agents[0].Executable = nil }},
		{"executable path", func(value *domain.EvidenceSet) { value.Agents[0].Executable.Path += "-changed" }},
		{"executable version", func(value *domain.EvidenceSet) { value.Agents[0].Executable.Version += "-changed" }},
		{"executable hash", func(value *domain.EvidenceSet) { value.Agents[0].Executable.SHA256 += "-changed" }},
		{"executable arguments", func(value *domain.EvidenceSet) { value.Agents[0].Executable.Arguments[0] += "-changed" }},
		{"executable cwd", func(value *domain.EvidenceSet) { value.Agents[0].Executable.WorkingDirectory += "-changed" }},
		{"executable environment", func(value *domain.EvidenceSet) { value.Agents[0].Executable.EnvironmentDigest += "-changed" }},
		{"executable invocation", func(value *domain.EvidenceSet) { value.Agents[0].Executable.InvocationDigest += "-changed" }},
		{"agent revalidation mode", func(value *domain.EvidenceSet) { value.Agents[0].RevalidationMode += "-changed" }},
		{"agent warning content", func(value *domain.EvidenceSet) { value.Agents[0].Warnings[0] += "-changed" }},
		{"adapter ID", func(value *domain.EvidenceSet) { value.Adapters[0].AdapterID += "-changed" }},
		{"adapter applicability", func(value *domain.EvidenceSet) { value.Adapters[0].Applicable = false }},
		{"adapter health", func(value *domain.EvidenceSet) { value.Adapters[0].Healthy = false }},
		{"adapter trust", func(value *domain.EvidenceSet) { value.Adapters[0].Trusted = false }},
		{"adapter grade", func(value *domain.EvidenceSet) { value.Adapters[0].BestGrade = domain.TrustProcessOnly }},
		{"adapter offline revalidation", func(value *domain.EvidenceSet) { value.Adapters[0].OfflineRevalidatable = false }},
		{"adapter error content", func(value *domain.EvidenceSet) { value.Adapters[0].Error += "-changed" }},
		{"evidence warning content", func(value *domain.EvidenceSet) { value.Warnings[0] += "-changed" }},
	}
	for _, mode := range []string{"local-readonly", "planning-only"} {
		test.Run(mode, func(test *testing.T) {
			base := evidenceDigestFixture()
			base.Agents[0].RevalidationMode = mode
			original := evidenceDigestFor(test, base)
			for _, scenario := range changes {
				test.Run(scenario.name, func(test *testing.T) {
					value := evidenceDigestFixture()
					value.Agents[0].RevalidationMode = mode
					scenario.change(&value)
					if actual := evidenceDigestFor(test, value); actual == original {
						test.Fatalf("EvidenceDigest() did not change when %s changed", scenario.name)
					}
				})
			}
		})
	}
}

func TestEvidenceDigestIncludesFingerprintOmissions(test *testing.T) {
	base := candidateFixture()
	base.Evidence = evidenceDigestFixture()
	original := evidenceDigestFor(test, base.Evidence)
	fingerprint := fingerprintFor(test, base)
	changes := []struct {
		name   string
		change func(*domain.EvidenceSet)
	}{
		{"planning-only content", func(value *domain.EvidenceSet) { value.Agents[1].RawFingerprint += "-changed" }},
		{"planning-only creation", func(value *domain.EvidenceSet) {
			value.Agents[1].CreatedAt = value.Agents[1].CreatedAt.Add(time.Nanosecond)
		}},
		{"planning-only update", func(value *domain.EvidenceSet) {
			value.Agents[1].UpdatedAt = value.Agents[1].UpdatedAt.Add(time.Nanosecond)
		}},
		{"planning-only observation", func(value *domain.EvidenceSet) {
			value.Agents[1].ObservedAt = value.Agents[1].ObservedAt.Add(time.Nanosecond)
		}},
		{"local-readonly observation", func(value *domain.EvidenceSet) {
			value.Agents[0].ObservedAt = value.Agents[0].ObservedAt.Add(time.Nanosecond)
		}},
		{"process error content", func(value *domain.EvidenceSet) { value.Processes[0].Error += "-changed" }},
		{"adapter error content", func(value *domain.EvidenceSet) { value.Adapters[0].Error += "-changed" }},
		{"agent warning content", func(value *domain.EvidenceSet) { value.Agents[0].Warnings[0] += "-changed" }},
		{"evidence warning content", func(value *domain.EvidenceSet) { value.Warnings[0] += "-changed" }},
		{"planning-only addition", func(value *domain.EvidenceSet) { value.Agents = append(value.Agents, value.Agents[1]) }},
		{"planning-only removal", func(value *domain.EvidenceSet) { value.Agents = value.Agents[:1] }},
	}
	for _, scenario := range changes {
		test.Run(scenario.name, func(test *testing.T) {
			value := candidateFixture()
			value.Evidence = evidenceDigestFixture()
			scenario.change(&value.Evidence)
			if actual := evidenceDigestFor(test, value.Evidence); actual == original {
				test.Fatalf("EvidenceDigest() omitted %s", scenario.name)
			}
			if actual := fingerprintFor(test, value); actual != fingerprint {
				test.Fatalf("fixture %s unexpectedly changed CandidateFingerprint()", scenario.name)
			}
		})
	}
}

func TestEvidenceDigestPreservesArrayOrderAndDuplicates(test *testing.T) {
	original := evidenceDigestFor(test, evidenceDigestFixture())
	changes := []struct {
		name   string
		change func(*domain.EvidenceSet)
	}{
		{"process order", func(value *domain.EvidenceSet) { slices.Reverse(value.Processes) }},
		{"agent order", func(value *domain.EvidenceSet) { slices.Reverse(value.Agents) }},
		{"adapter order", func(value *domain.EvidenceSet) { slices.Reverse(value.Adapters) }},
		{"reference order", func(value *domain.EvidenceSet) { slices.Reverse(value.Agents[0].ProcessRefs) }},
		{"argument order", func(value *domain.EvidenceSet) { slices.Reverse(value.Agents[0].Executable.Arguments) }},
		{"agent warning order", func(value *domain.EvidenceSet) { slices.Reverse(value.Agents[0].Warnings) }},
		{"evidence warning order", func(value *domain.EvidenceSet) { slices.Reverse(value.Warnings) }},
		{"duplicate process", func(value *domain.EvidenceSet) { value.Processes = append(value.Processes, value.Processes[0]) }},
		{"duplicate agent", func(value *domain.EvidenceSet) { value.Agents = append(value.Agents, value.Agents[0]) }},
		{"duplicate adapter", func(value *domain.EvidenceSet) { value.Adapters = append(value.Adapters, value.Adapters[0]) }},
		{"duplicate reference", func(value *domain.EvidenceSet) {
			value.Agents[0].ProcessRefs = append(value.Agents[0].ProcessRefs, value.Agents[0].ProcessRefs[0])
		}},
		{"duplicate argument", func(value *domain.EvidenceSet) {
			value.Agents[0].Executable.Arguments = append(value.Agents[0].Executable.Arguments, value.Agents[0].Executable.Arguments[0])
		}},
		{"duplicate agent warning", func(value *domain.EvidenceSet) {
			value.Agents[0].Warnings = append(value.Agents[0].Warnings, value.Agents[0].Warnings[0])
		}},
		{"duplicate evidence warning", func(value *domain.EvidenceSet) { value.Warnings = append(value.Warnings, value.Warnings[0]) }},
	}
	for _, scenario := range changes {
		test.Run(scenario.name, func(test *testing.T) {
			value := evidenceDigestFixture()
			scenario.change(&value)
			if actual := evidenceDigestFor(test, value); actual == original {
				test.Fatalf("EvidenceDigest() lost %s", scenario.name)
			}
		})
	}
}

func TestEvidenceDigestPreservesJSONNilEmptySemantics(test *testing.T) {
	cases := []struct {
		name     string
		setNil   func(*domain.EvidenceSet)
		setEmpty func(*domain.EvidenceSet)
		equal    bool
	}{
		{"processes", func(value *domain.EvidenceSet) { value.Processes = nil }, func(value *domain.EvidenceSet) { value.Processes = []domain.ProcessEvidence{} }, false},
		{"agents", func(value *domain.EvidenceSet) { value.Agents = nil }, func(value *domain.EvidenceSet) { value.Agents = []domain.AgentEvidence{} }, false},
		{"adapters", func(value *domain.EvidenceSet) { value.Adapters = nil }, func(value *domain.EvidenceSet) { value.Adapters = []domain.AdapterStatus{} }, true},
		{"evidence warnings", func(value *domain.EvidenceSet) { value.Warnings = nil }, func(value *domain.EvidenceSet) { value.Warnings = []string{} }, true},
		{"references", func(value *domain.EvidenceSet) { value.Agents[0].ProcessRefs = nil }, func(value *domain.EvidenceSet) { value.Agents[0].ProcessRefs = []domain.ProcessReference{} }, true},
		{"agent warnings", func(value *domain.EvidenceSet) { value.Agents[0].Warnings = nil }, func(value *domain.EvidenceSet) { value.Agents[0].Warnings = []string{} }, true},
		{"arguments", func(value *domain.EvidenceSet) { value.Agents[0].Executable.Arguments = nil }, func(value *domain.EvidenceSet) { value.Agents[0].Executable.Arguments = []string{} }, false},
		{"binding", func(value *domain.EvidenceSet) { value.Agents[0].Binding = nil }, func(value *domain.EvidenceSet) { value.Agents[0].Binding = &domain.WorktreeBinding{} }, false},
		{"executable", func(value *domain.EvidenceSet) { value.Agents[0].Executable = nil }, func(value *domain.EvidenceSet) { value.Agents[0].Executable = &domain.ExecutableIdentity{} }, false},
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			nilValue := evidenceDigestFixture()
			emptyValue := evidenceDigestFixture()
			scenario.setNil(&nilValue)
			scenario.setEmpty(&emptyValue)
			nilDigest := evidenceDigestFor(test, nilValue)
			emptyDigest := evidenceDigestFor(test, emptyValue)
			if (nilDigest == emptyDigest) != scenario.equal {
				test.Fatalf("nil digest = %q, empty digest = %q, want equality %v", nilDigest, emptyDigest, scenario.equal)
			}
		})
	}
}

func TestEvidenceDigestNormalizesEveryTimestamp(test *testing.T) {
	base := evidenceDigestFixture()
	original := evidenceDigestFor(test, base)
	zones := []*time.Location{
		time.FixedZone("named UTC", 0),
		time.FixedZone("east", 9*60*60),
		time.FixedZone("west", -7*60*60),
		time.FixedZone("offset with seconds", 1817),
		time.FixedZone("negative offset with seconds", -1817),
		time.FixedZone("outside RFC3339 offsets", 25*60*60),
	}
	for timestampIndex := range evidenceDigestTimestamps(&base) {
		for _, zone := range zones {
			test.Run(fmt.Sprintf("timestamp-%d/%s", timestampIndex, zone), func(test *testing.T) {
				value := evidenceDigestFixture()
				timestamp := evidenceDigestTimestamps(&value)[timestampIndex]
				*timestamp = timestamp.In(zone)
				before := *timestamp
				if actual := evidenceDigestFor(test, value); actual != original {
					test.Fatalf("equal instant in %s changed digest: %q, want %q", zone, actual, original)
				}
				if *timestamp != before {
					test.Fatal("timestamp normalization mutated caller data")
				}
			})
		}
	}
}

func TestEvidenceDigestRejectsInvalidUTF8(test *testing.T) {
	invalid := string([]byte{0xff})
	changes := []struct {
		name   string
		change func(*domain.EvidenceSet)
	}{
		{"process executable", func(value *domain.EvidenceSet) { value.Processes[0].Executable = invalid }},
		{"process error", func(value *domain.EvidenceSet) { value.Processes[0].Error = invalid }},
		{"process state", func(value *domain.EvidenceSet) { value.Processes[1].State = domain.EvidenceState(invalid) }},
		{"agent identity", func(value *domain.EvidenceSet) { value.Agents[0].AdapterID = invalid }},
		{"planning-only identity", func(value *domain.EvidenceSet) { value.Agents[1].RawFingerprint = invalid }},
		{"agent warning", func(value *domain.EvidenceSet) { value.Agents[0].Warnings[1] = invalid }},
		{"planning-only warning", func(value *domain.EvidenceSet) { value.Agents[1].Warnings[1] = invalid }},
		{"reference", func(value *domain.EvidenceSet) { value.Agents[0].ProcessRefs[1].Executable = invalid }},
		{"planning-only reference", func(value *domain.EvidenceSet) { value.Agents[1].ProcessRefs[1].Fingerprint = invalid }},
		{"binding", func(value *domain.EvidenceSet) { value.Agents[0].Binding.Identifier = invalid }},
		{"executable", func(value *domain.EvidenceSet) { value.Agents[0].Executable.Path = invalid }},
		{"arguments", func(value *domain.EvidenceSet) { value.Agents[0].Executable.Arguments[1] = invalid }},
		{"environment", func(value *domain.EvidenceSet) { value.Agents[1].Executable.EnvironmentDigest = invalid }},
		{"invocation", func(value *domain.EvidenceSet) { value.Agents[1].Executable.InvocationDigest = invalid }},
		{"adapter", func(value *domain.EvidenceSet) { value.Adapters[0].AdapterID = invalid }},
		{"adapter error", func(value *domain.EvidenceSet) { value.Adapters[1].Error = invalid }},
		{"adapter grade", func(value *domain.EvidenceSet) { value.Adapters[0].BestGrade = domain.TrustGrade(invalid) }},
		{"evidence warning", func(value *domain.EvidenceSet) { value.Warnings[1] = invalid }},
	}
	for _, scenario := range changes {
		test.Run(scenario.name, func(test *testing.T) {
			value := evidenceDigestFixture()
			scenario.change(&value)
			if digest, err := EvidenceDigest(value); err == nil || digest != "" {
				test.Fatalf("invalid UTF-8 accepted: %q, %v", digest, err)
			}
		})
	}
}

func TestEvidenceDigestRejectsInvalidTimestamps(test *testing.T) {
	base := evidenceDigestFixture()
	invalidTimes := []struct {
		name  string
		value time.Time
	}{
		{"year too large", time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)},
		{"negative year", time.Date(-1, time.January, 1, 0, 0, 0, 0, time.UTC)},
		{"UTC year overflow", time.Date(9999, time.December, 31, 23, 0, 0, 0, time.FixedZone("west", -60*60))},
		{"UTC year underflow", time.Date(0, time.January, 1, 0, 0, 0, 0, time.FixedZone("east", 60*60))},
	}
	for timestampIndex := range evidenceDigestTimestamps(&base) {
		for _, scenario := range invalidTimes {
			test.Run(fmt.Sprintf("timestamp-%d/%s", timestampIndex, scenario.name), func(test *testing.T) {
				value := evidenceDigestFixture()
				*evidenceDigestTimestamps(&value)[timestampIndex] = scenario.value
				if digest, err := EvidenceDigest(value); err == nil || digest != "" {
					test.Fatalf("invalid timestamp accepted: %q, %v", digest, err)
				}
			})
		}
	}
}

func TestEvidenceDigestDoesNotMutateCaller(test *testing.T) {
	zone := time.FixedZone("offset with seconds", 1817)
	fixture := func() domain.EvidenceSet {
		value := evidenceDigestFixture()
		for _, timestamp := range evidenceDigestTimestamps(&value) {
			*timestamp = timestamp.In(zone)
		}
		value.Agents = append(value.Agents, value.Agents[0])
		return value
	}
	cases := []struct {
		name      string
		change    func(*domain.EvidenceSet)
		wantError bool
	}{
		{"success", func(value *domain.EvidenceSet) {}, false},
		{"invalid UTF-8", func(value *domain.EvidenceSet) { value.Warnings[1] = string([]byte{0xff}) }, true},
		{"invalid time", func(value *domain.EvidenceSet) {
			value.Agents[1].ObservedAt = time.Date(10000, time.January, 2, 0, 0, 0, 0, zone)
		}, true},
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			value := fixture()
			before := fixture()
			scenario.change(&value)
			scenario.change(&before)
			binding := value.Agents[0].Binding
			executable := value.Agents[0].Executable
			reference := &value.Agents[0].ProcessRefs[0]
			argument := &executable.Arguments[0]
			warning := &value.Agents[0].Warnings[0]
			for attempt := range 3 {
				digest, err := EvidenceDigest(value)
				if (err != nil) != scenario.wantError || (digest == "") != scenario.wantError {
					test.Fatalf("EvidenceDigest() attempt %d = %q, %v, want error %v", attempt, digest, err, scenario.wantError)
				}
				if !reflect.DeepEqual(value, before) {
					test.Fatal("EvidenceDigest() mutated caller-owned evidence")
				}
				if value.Agents[0].Binding != binding || value.Agents[0].Executable != executable || &value.Agents[0].ProcessRefs[0] != reference || &executable.Arguments[0] != argument || &value.Agents[0].Warnings[0] != warning {
					test.Fatal("EvidenceDigest() replaced caller-owned pointers or nested slices")
				}
			}
		})
	}
}

func TestEvidenceDigestConcurrentReadOnlyCalls(test *testing.T) {
	value := evidenceDigestFixture()
	contents, err := json.Marshal(value)
	if err != nil {
		test.Fatal(err)
	}
	expected := fmt.Sprintf("sha256:%x", sha256.Sum256(contents))
	for _, timestamp := range evidenceDigestTimestamps(&value) {
		*timestamp = timestamp.In(time.FixedZone("east", 9*60*60))
	}
	for attempt := range 8 {
		test.Run(fmt.Sprintf("reader-%d", attempt), func(test *testing.T) {
			test.Parallel()
			for range 8 {
				if actual := evidenceDigestFor(test, value); actual != expected {
					test.Fatalf("concurrent digest = %q, want %q", actual, expected)
				}
			}
		})
	}
}

func TestEvidenceDigestMatchesSignedEvidenceJSON(test *testing.T) {
	zoned := evidenceDigestFixture()
	for _, timestamp := range evidenceDigestTimestamps(&zoned) {
		*timestamp = timestamp.In(time.FixedZone("offset with seconds", -1817))
	}
	optionalEmpty := evidenceDigestFixture()
	optionalEmpty.Adapters = []domain.AdapterStatus{}
	optionalEmpty.Warnings = []string{}
	optionalEmpty.Agents[0].ProcessRefs = []domain.ProcessReference{}
	optionalEmpty.Agents[0].Warnings = []string{}
	optionalEmpty.Agents[0].Executable.Arguments = []string{}
	optionalEmpty.Agents[1].Binding = &domain.WorktreeBinding{}
	optionalEmpty.Agents[1].Executable = &domain.ExecutableIdentity{}
	duplicates := evidenceDigestFixture()
	duplicates.Processes = append(duplicates.Processes, duplicates.Processes[0])
	duplicates.Agents = append(duplicates.Agents, duplicates.Agents[1])
	duplicates.Adapters = append(duplicates.Adapters, duplicates.Adapters[0])
	duplicates.Warnings = append(duplicates.Warnings, duplicates.Warnings[0])
	duplicates.Agents[0].ProcessRefs = append(duplicates.Agents[0].ProcessRefs, duplicates.Agents[0].ProcessRefs[0])
	duplicates.Agents[0].Warnings = append(duplicates.Agents[0].Warnings, duplicates.Agents[0].Warnings[0])
	duplicates.Agents[0].Executable.Arguments = append(duplicates.Agents[0].Executable.Arguments, duplicates.Agents[0].Executable.Arguments[0])
	cases := []struct {
		name  string
		value domain.EvidenceSet
	}{
		{"complete evidence", evidenceDigestFixture()},
		{"nil collections", domain.EvidenceSet{}},
		{"empty collections", domain.EvidenceSet{Processes: []domain.ProcessEvidence{}, Agents: []domain.AgentEvidence{}}},
		{"zero agent", domain.EvidenceSet{Agents: []domain.AgentEvidence{{}}}},
		{"optional empty values", optionalEmpty},
		{"zoned timestamps", zoned},
		{"duplicates", duplicates},
	}
	key := bytes.Repeat([]byte{0x42}, 32)
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			value := domain.Plan{Candidates: []domain.Candidate{{Evidence: scenario.value}}}
			contents, err := encodeSignedPlan(value, key)
			if err != nil {
				test.Fatal(err)
			}
			var document struct {
				Candidates []struct {
					Evidence json.RawMessage `json:"evidence"`
				} `json:"candidates"`
			}
			if err := json.Unmarshal(contents, &document); err != nil {
				test.Fatal(err)
			}
			if len(document.Candidates) != 1 || len(document.Candidates[0].Evidence) == 0 {
				test.Fatal("signed plan omitted evidence")
			}
			expected := fmt.Sprintf("sha256:%x", sha256.Sum256(document.Candidates[0].Evidence))
			if actual := evidenceDigestFor(test, scenario.value); actual != expected {
				test.Fatalf("EvidenceDigest() = %q, want %q for signed evidence %s", actual, expected, document.Candidates[0].Evidence)
			}
			loaded, err := decodeAuthenticatedPlan(contents, key)
			if err != nil {
				test.Fatal(err)
			}
			if len(loaded.Candidates) != 1 || evidenceDigestFor(test, loaded.Candidates[0].Evidence) != expected {
				test.Fatal("authenticated evidence changed the digest")
			}
		})
	}
}

func TestEvidenceDigestSignedPlanCompatibility(test *testing.T) {
	value := storedPlanFixture()
	value.Candidates[0].Evidence = evidenceDigestFixture()
	zone := time.FixedZone("offset with seconds", 1817)
	for _, timestamp := range evidenceDigestTimestamps(&value.Candidates[0].Evidence) {
		*timestamp = timestamp.In(zone)
	}
	value.GeneratedAt = value.GeneratedAt.In(zone)
	value.ExpiresAt = value.ExpiresAt.In(zone)
	value.Candidates[0].Worktree.LastCommitAt = value.Candidates[0].Worktree.LastCommitAt.In(zone)
	value.Candidates[0].Worktree.MetadataModifiedAt = value.Candidates[0].Worktree.MetadataModifiedAt.In(zone)
	key := bytes.Repeat([]byte{0x42}, 32)
	keyDigest := sha256.Sum256(key)
	expected := storedPlanFixture()
	expected.Candidates[0].Evidence = evidenceDigestFixture()
	expected.Integrity = domain.PlanIntegrity{
		Algorithm: "hmac-sha256", KeyID: "sha256:" + hex.EncodeToString(keyDigest[:]),
	}
	payload, err := json.Marshal(expected)
	if err != nil {
		test.Fatal(err)
	}
	authenticator := hmac.New(sha256.New, key)
	_, _ = authenticator.Write(payload)
	expected.Integrity.MAC = hex.EncodeToString(authenticator.Sum(nil))
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		test.Fatal(err)
	}
	actual, err := encodeSignedPlan(value, key)
	if err != nil {
		test.Fatal(err)
	}
	if !bytes.Equal(actual, expectedJSON) {
		test.Fatalf("signed plan bytes changed:\nactual: %s\nexpected: %s", actual, expectedJSON)
	}
}

func evidenceDigestFor(test *testing.T, value domain.EvidenceSet) string {
	test.Helper()
	digest, err := EvidenceDigest(value)
	if err != nil {
		test.Fatal(err)
	}
	encoded, prefixed := strings.CutPrefix(digest, "sha256:")
	decoded, decodeErr := hex.DecodeString(encoded)
	if !prefixed || decodeErr != nil || len(decoded) != sha256.Size || strings.ToLower(encoded) != encoded {
		test.Fatalf("invalid SHA-256 evidence digest %q", digest)
	}
	return digest
}

func evidenceDigestFixture() domain.EvidenceSet {
	stamp := time.Date(2026, time.September, 13, 12, 34, 56, 123456789, time.UTC)
	agent := func(adapterID, mode string) domain.AgentEvidence {
		return domain.AgentEvidence{
			AdapterID: adapterID, AdapterVersion: "1.2.3", BundleDigest: "sha256:bundle", Provider: "provider",
			SourceID: "source", SessionID: "session", ThreadID: "thread", ProjectID: "project",
			CWD: "/synthetic/worktree", RepositoryRoot: "/synthetic/repo", WorktreePath: "/synthetic/worktree",
			State: domain.EvidenceInactive, CreatedAt: stamp, UpdatedAt: stamp.Add(time.Hour), ObservedAt: stamp.Add(2 * time.Hour),
			ProcessRefs: []domain.ProcessReference{
				{PID: 20, CreatedAt: stamp, Executable: "/synthetic/editor", Fingerprint: "reference-b"},
				{PID: 10, CreatedAt: stamp.Add(time.Minute), Executable: "/synthetic/helper", Fingerprint: "reference-a"},
			},
			Binding:    &domain.WorktreeBinding{Kind: "worktree", Identifier: "binding", Version: "1"},
			SourceKind: "file", SupportGrade: domain.TrustVersionedPrivate, Confidence: "high",
			SchemaVersion: "1", RawFingerprint: "raw", TrustRecordDigest: "sha256:trust",
			Executable: &domain.ExecutableIdentity{
				Path: "/synthetic/provider", Version: "1.2.3", SHA256: "sha256:binary", Arguments: []string{"--read-only", "--json"},
				WorkingDirectory: "/synthetic/worktree", EnvironmentDigest: "sha256:environment", InvocationDigest: "sha256:invocation",
			},
			RevalidationMode: mode, Warnings: []string{"second agent warning", "first agent warning"},
		}
	}
	return domain.EvidenceSet{
		Processes: []domain.ProcessEvidence{
			{PID: 20, CreatedAt: stamp, Executable: "/synthetic/editor", CWD: "/synthetic/worktree", State: domain.EvidenceUnknown, Error: "process access denied", Fingerprint: "process-b"},
			{PID: 10, CreatedAt: stamp.Add(time.Minute), Executable: "/synthetic/helper", CWD: "/synthetic/worktree", State: domain.EvidenceInactive, Fingerprint: "process-a"},
		},
		Agents: []domain.AgentEvidence{agent("adapter-b", "local-readonly"), agent("adapter-a", "planning-only")},
		Adapters: []domain.AdapterStatus{
			{AdapterID: "adapter-b", Applicable: true, Healthy: true, Trusted: true, BestGrade: domain.TrustVersionedPrivate, OfflineRevalidatable: true, Error: "adapter access denied"},
			{AdapterID: "adapter-a", Applicable: true, Healthy: false, Trusted: false, BestGrade: domain.TrustProcessOnly, OfflineRevalidatable: false, Error: "adapter unavailable"},
		},
		Warnings: []string{"second evidence warning", "first evidence warning"},
	}
}

func evidenceDigestTimestamps(value *domain.EvidenceSet) []*time.Time {
	var timestamps []*time.Time
	for processIndex := range value.Processes {
		timestamps = append(timestamps, &value.Processes[processIndex].CreatedAt)
	}
	for agentIndex := range value.Agents {
		agent := &value.Agents[agentIndex]
		timestamps = append(timestamps, &agent.CreatedAt, &agent.UpdatedAt, &agent.ObservedAt)
		for referenceIndex := range agent.ProcessRefs {
			timestamps = append(timestamps, &agent.ProcessRefs[referenceIndex].CreatedAt)
		}
	}
	return timestamps
}
