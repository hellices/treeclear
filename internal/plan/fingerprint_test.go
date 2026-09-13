package plan

import (
	"encoding/hex"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
)

func candidateFixture() domain.Candidate {
	stamp := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	agent := domain.AgentEvidence{
		AdapterID: "adapter-b", AdapterVersion: "1", BundleDigest: "bundle", Provider: "provider",
		SourceID: "source", SessionID: "session", ThreadID: "thread", ProjectID: "project",
		CWD: "/repo/feature", RepositoryRoot: "/repo/main", WorktreePath: "/repo/feature",
		State: domain.EvidenceInactive, CreatedAt: stamp, UpdatedAt: stamp, ObservedAt: stamp.Add(time.Hour),
		ProcessRefs: []domain.ProcessReference{
			{PID: 20, CreatedAt: stamp, Executable: "/bin/editor", Fingerprint: "ref-b"},
			{PID: 10, CreatedAt: stamp, Executable: "/bin/editor", Fingerprint: "ref-a"},
		},
		Binding:    &domain.WorktreeBinding{Kind: "worktree", Identifier: "binding", Version: "1"},
		SourceKind: "file", SupportGrade: domain.TrustVersionedPrivate, Confidence: "high",
		SchemaVersion: "1", RawFingerprint: "raw", TrustRecordDigest: "trust",
		Executable: &domain.ExecutableIdentity{
			Path: "/bin/provider", Version: "1", SHA256: "binary", Arguments: []string{"--first", "--second"},
			WorkingDirectory: "/repo/feature", EnvironmentDigest: "env", InvocationDigest: "invocation",
		},
		RevalidationMode: "local-readonly",
	}
	other := agent
	other.AdapterID = "adapter-a"
	return domain.Candidate{
		ID: "candidate", Fingerprint: "previous", Action: "remove",
		Worktree: domain.Worktree{
			Path: "/repo/feature", RepositoryRoot: "/repo/main", CommonGitDir: "/repo/main/.git",
			AdminDir: "/repo/main/.git/worktrees/feature", Head: "head", Branch: "feature", Upstream: "origin/main",
			PathSafe: true, GitStateKnown: true, Recoverable: true, LastCommitAt: stamp, MetadataModifiedAt: stamp,
			EstimatedBytes: 100, IndexHash: "index", AdminHash: "admin",
		},
		Evidence: domain.EvidenceSet{
			Processes: []domain.ProcessEvidence{
				{PID: 20, CreatedAt: stamp, Executable: "/bin/editor", CWD: "/repo/feature", State: domain.EvidenceInactive, Fingerprint: "proc-b"},
				{PID: 10, CreatedAt: stamp, Executable: "/bin/editor", CWD: "/repo/feature", State: domain.EvidenceInactive, Fingerprint: "proc-a"},
			},
			Agents: []domain.AgentEvidence{agent, other},
			Adapters: []domain.AdapterStatus{
				{AdapterID: "adapter-b", Applicable: true, Healthy: true, Trusted: true, BestGrade: domain.TrustVersionedPrivate, OfflineRevalidatable: true},
				{AdapterID: "adapter-a", Applicable: true, Healthy: true, Trusted: true, BestGrade: domain.TrustVersionedPrivate, OfflineRevalidatable: true},
			},
		},
		Decision: domain.Decision{Classification: domain.Safe, Reasons: []domain.Reason{{Code: "safe", Message: "Safe"}, {Code: "offline", Message: "Offline"}}, InactiveFor: 8 * 24 * time.Hour},
		Snapshot: domain.SnapshotPlan{Required: true, MaximumBytes: 1024, UntrackedFiles: 1, UntrackedBytes: 20},
	}
}

func cloneCandidate(test *testing.T, value domain.Candidate) domain.Candidate {
	test.Helper()
	contents, err := json.Marshal(value)
	if err != nil {
		test.Fatal(err)
	}
	var clone domain.Candidate
	if err := json.Unmarshal(contents, &clone); err != nil {
		test.Fatal(err)
	}
	return clone
}

func fingerprintFor(test *testing.T, value domain.Candidate) string {
	test.Helper()
	fingerprint, err := CandidateFingerprint(value)
	if err != nil {
		test.Fatal(err)
	}
	encoded, prefixed := strings.CutPrefix(fingerprint, "sha256:")
	decoded, decodeErr := hex.DecodeString(encoded)
	if !prefixed || decodeErr != nil || len(decoded) != 32 || strings.ToLower(encoded) != encoded {
		test.Fatalf("invalid SHA-256 fingerprint %q", fingerprint)
	}
	return fingerprint
}

func TestCandidateFingerprintChangesWithPreconditions(test *testing.T) {
	base := candidateFixture()
	original := fingerprintFor(test, base)
	changes := []struct {
		name   string
		change func(*domain.Candidate)
	}{
		{"path", func(value *domain.Candidate) { value.Worktree.Path += "-changed" }},
		{"repository", func(value *domain.Candidate) { value.Worktree.RepositoryRoot += "-changed" }},
		{"common dir", func(value *domain.Candidate) { value.Worktree.CommonGitDir += "-changed" }},
		{"admin dir", func(value *domain.Candidate) { value.Worktree.AdminDir += "-changed" }},
		{"HEAD", func(value *domain.Candidate) { value.Worktree.Head += "-changed" }},
		{"branch", func(value *domain.Candidate) { value.Worktree.Branch += "-changed" }},
		{"upstream", func(value *domain.Candidate) { value.Worktree.Upstream += "-changed" }},
		{"index hash", func(value *domain.Candidate) { value.Worktree.IndexHash += "-changed" }},
		{"admin hash", func(value *domain.Candidate) { value.Worktree.AdminHash += "-changed" }},
		{"primary", func(value *domain.Candidate) { value.Worktree.Primary = true }},
		{"current", func(value *domain.Candidate) { value.Worktree.Current = true }},
		{"path safety", func(value *domain.Candidate) { value.Worktree.PathSafe = false }},
		{"detached", func(value *domain.Candidate) { value.Worktree.Detached = true }},
		{"locked", func(value *domain.Candidate) { value.Worktree.Locked = true }},
		{"prunable", func(value *domain.Candidate) { value.Worktree.Prunable = true }},
		{"Git known", func(value *domain.Candidate) { value.Worktree.GitStateKnown = false }},
		{"collection errors", func(value *domain.Candidate) { value.Worktree.CollectionErrors = []string{"unknown"} }},
		{"recoverable", func(value *domain.Candidate) { value.Worktree.Recoverable = false }},
		{"staged", func(value *domain.Candidate) { value.Worktree.Status.Staged++ }},
		{"unstaged", func(value *domain.Candidate) { value.Worktree.Status.Unstaged++ }},
		{"unmerged", func(value *domain.Candidate) { value.Worktree.Status.Unmerged++ }},
		{"untracked", func(value *domain.Candidate) { value.Worktree.Status.Untracked++ }},
		{"commit time", func(value *domain.Candidate) {
			value.Worktree.LastCommitAt = value.Worktree.LastCommitAt.Add(time.Nanosecond)
		}},
		{"metadata time", func(value *domain.Candidate) {
			value.Worktree.MetadataModifiedAt = value.Worktree.MetadataModifiedAt.Add(time.Nanosecond)
		}},
		{"snapshot required", func(value *domain.Candidate) { value.Snapshot.Required = false }},
		{"snapshot maximum", func(value *domain.Candidate) { value.Snapshot.MaximumBytes++ }},
		{"snapshot files", func(value *domain.Candidate) { value.Snapshot.UntrackedFiles++ }},
		{"snapshot bytes", func(value *domain.Candidate) { value.Snapshot.UntrackedBytes++ }},
		{"snapshot sensitive", func(value *domain.Candidate) { value.Snapshot.SensitiveBlocked = true }},
		{"action", func(value *domain.Candidate) { value.Action = "none" }},
		{"classification", func(value *domain.Candidate) { value.Decision.Classification = domain.Protected }},
		{"reason code", func(value *domain.Candidate) { value.Decision.Reasons[0].Code = "unknown" }},
		{"process PID", func(value *domain.Candidate) { value.Evidence.Processes[0].PID++ }},
		{"process creation", func(value *domain.Candidate) {
			value.Evidence.Processes[0].CreatedAt = value.Evidence.Processes[0].CreatedAt.Add(time.Nanosecond)
		}},
		{"process executable", func(value *domain.Candidate) { value.Evidence.Processes[0].Executable += "-changed" }},
		{"process cwd", func(value *domain.Candidate) { value.Evidence.Processes[0].CWD += "-changed" }},
		{"process state", func(value *domain.Candidate) { value.Evidence.Processes[0].State = domain.EvidenceActive }},
		{"process fingerprint", func(value *domain.Candidate) { value.Evidence.Processes[0].Fingerprint += "-changed" }},
		{"process error presence", func(value *domain.Candidate) { value.Evidence.Processes[0].Error = "denied" }},
		{"evidence warnings presence", func(value *domain.Candidate) { value.Evidence.Warnings = []string{""} }},
		{"agent adapter", func(value *domain.Candidate) { value.Evidence.Agents[0].AdapterID += "-changed" }},
		{"agent adapter version", func(value *domain.Candidate) { value.Evidence.Agents[0].AdapterVersion += "-changed" }},
		{"agent bundle", func(value *domain.Candidate) { value.Evidence.Agents[0].BundleDigest += "-changed" }},
		{"agent provider", func(value *domain.Candidate) { value.Evidence.Agents[0].Provider += "-changed" }},
		{"agent source", func(value *domain.Candidate) { value.Evidence.Agents[0].SourceID += "-changed" }},
		{"agent session", func(value *domain.Candidate) { value.Evidence.Agents[0].SessionID += "-changed" }},
		{"agent thread", func(value *domain.Candidate) { value.Evidence.Agents[0].ThreadID += "-changed" }},
		{"agent project", func(value *domain.Candidate) { value.Evidence.Agents[0].ProjectID += "-changed" }},
		{"agent cwd", func(value *domain.Candidate) { value.Evidence.Agents[0].CWD += "-changed" }},
		{"agent repository", func(value *domain.Candidate) { value.Evidence.Agents[0].RepositoryRoot += "-changed" }},
		{"agent worktree", func(value *domain.Candidate) { value.Evidence.Agents[0].WorktreePath += "-changed" }},
		{"agent state", func(value *domain.Candidate) { value.Evidence.Agents[0].State = domain.EvidenceActive }},
		{"agent creation", func(value *domain.Candidate) {
			value.Evidence.Agents[0].CreatedAt = value.Evidence.Agents[0].CreatedAt.Add(time.Nanosecond)
		}},
		{"agent update", func(value *domain.Candidate) {
			value.Evidence.Agents[0].UpdatedAt = value.Evidence.Agents[0].UpdatedAt.Add(time.Nanosecond)
		}},
		{"agent ref PID", func(value *domain.Candidate) { value.Evidence.Agents[0].ProcessRefs[0].PID++ }},
		{"agent ref creation", func(value *domain.Candidate) {
			value.Evidence.Agents[0].ProcessRefs[0].CreatedAt = value.Evidence.Agents[0].ProcessRefs[0].CreatedAt.Add(time.Nanosecond)
		}},
		{"agent ref executable", func(value *domain.Candidate) { value.Evidence.Agents[0].ProcessRefs[0].Executable += "-changed" }},
		{"agent ref fingerprint", func(value *domain.Candidate) { value.Evidence.Agents[0].ProcessRefs[0].Fingerprint += "-changed" }},
		{"agent binding presence", func(value *domain.Candidate) { value.Evidence.Agents[0].Binding = nil }},
		{"agent binding kind", func(value *domain.Candidate) { value.Evidence.Agents[0].Binding.Kind += "-changed" }},
		{"agent binding identity", func(value *domain.Candidate) { value.Evidence.Agents[0].Binding.Identifier += "-changed" }},
		{"agent binding version", func(value *domain.Candidate) { value.Evidence.Agents[0].Binding.Version += "-changed" }},
		{"agent source kind", func(value *domain.Candidate) { value.Evidence.Agents[0].SourceKind += "-changed" }},
		{"agent grade", func(value *domain.Candidate) { value.Evidence.Agents[0].SupportGrade = domain.TrustProcessOnly }},
		{"agent confidence", func(value *domain.Candidate) { value.Evidence.Agents[0].Confidence = "low" }},
		{"agent schema", func(value *domain.Candidate) { value.Evidence.Agents[0].SchemaVersion += "-changed" }},
		{"agent raw fingerprint", func(value *domain.Candidate) { value.Evidence.Agents[0].RawFingerprint += "-changed" }},
		{"agent trust", func(value *domain.Candidate) { value.Evidence.Agents[0].TrustRecordDigest += "-changed" }},
		{"agent warnings presence", func(value *domain.Candidate) { value.Evidence.Agents[0].Warnings = []string{""} }},
		{"agent executable presence", func(value *domain.Candidate) { value.Evidence.Agents[0].Executable = nil }},
		{"agent executable path", func(value *domain.Candidate) { value.Evidence.Agents[0].Executable.Path += "-changed" }},
		{"agent executable version", func(value *domain.Candidate) { value.Evidence.Agents[0].Executable.Version += "-changed" }},
		{"agent executable digest", func(value *domain.Candidate) { value.Evidence.Agents[0].Executable.SHA256 += "-changed" }},
		{"agent executable argv", func(value *domain.Candidate) { slices.Reverse(value.Evidence.Agents[0].Executable.Arguments) }},
		{"agent executable cwd", func(value *domain.Candidate) { value.Evidence.Agents[0].Executable.WorkingDirectory += "-changed" }},
		{"agent executable environment", func(value *domain.Candidate) { value.Evidence.Agents[0].Executable.EnvironmentDigest += "-changed" }},
		{"agent executable invocation", func(value *domain.Candidate) { value.Evidence.Agents[0].Executable.InvocationDigest += "-changed" }},
		{"agent revalidation mode", func(value *domain.Candidate) { value.Evidence.Agents[0].RevalidationMode = "planning-only" }},
		{"adapter identity", func(value *domain.Candidate) { value.Evidence.Adapters[0].AdapterID += "-changed" }},
		{"adapter applicability", func(value *domain.Candidate) { value.Evidence.Adapters[0].Applicable = false }},
		{"adapter health", func(value *domain.Candidate) { value.Evidence.Adapters[0].Healthy = false }},
		{"adapter trust", func(value *domain.Candidate) { value.Evidence.Adapters[0].Trusted = false }},
		{"adapter grade", func(value *domain.Candidate) { value.Evidence.Adapters[0].BestGrade = domain.TrustProcessOnly }},
		{"adapter offline", func(value *domain.Candidate) { value.Evidence.Adapters[0].OfflineRevalidatable = false }},
		{"adapter error presence", func(value *domain.Candidate) { value.Evidence.Adapters[0].Error = "denied" }},
	}
	for _, change := range changes {
		test.Run(change.name, func(test *testing.T) {
			changed := cloneCandidate(test, base)
			change.change(&changed)
			if fingerprintFor(test, changed) == original {
				test.Fatal("changed precondition did not change fingerprint")
			}
		})
	}
}

func TestCandidateFingerprintIgnoresPresentation(test *testing.T) {
	base := candidateFixture()
	base.Evidence.Warnings = []string{"first warning"}
	base.Evidence.Agents[0].Warnings = []string{"first warning"}
	base.Evidence.Processes[0].Error = "first diagnostic"
	base.Evidence.Adapters[0].Error = "first diagnostic"
	changed := cloneCandidate(test, base)
	changed.ID = "new display id"
	changed.Fingerprint = "new cached fingerprint"
	changed.Worktree.LockReason = "human lock reason"
	changed.Worktree.EstimatedBytes++
	changed.Decision.InactiveFor += time.Hour
	changed.Decision.Reasons[0].Message = "translated message"
	changed.Evidence.Warnings = []string{"translated warning", "more context"}
	changed.Evidence.Processes[0].Error = "translated diagnostic"
	changed.Evidence.Adapters[0].Error = "translated diagnostic"
	changed.Evidence.Agents[0].Warnings = []string{"translated warning"}
	changed.Evidence.Agents[0].ObservedAt = changed.Evidence.Agents[0].ObservedAt.Add(time.Hour)
	if fingerprintFor(test, base) != fingerprintFor(test, changed) {
		test.Fatal("presentation or observation data changed removal fingerprint")
	}
}

func TestCandidateFingerprintCanonicalOrderAndNoMutation(test *testing.T) {
	base := candidateFixture()
	base.Worktree.CollectionErrors = []string{"second", "first"}
	processDuplicate := base.Evidence.Processes[0]
	processDuplicate.Fingerprint = "conflicting-content"
	base.Evidence.Processes = append(base.Evidence.Processes, processDuplicate)
	agentDuplicate := cloneCandidate(test, base).Evidence.Agents[0]
	agentDuplicate.RawFingerprint = "conflicting-content"
	base.Evidence.Agents = append(base.Evidence.Agents, agentDuplicate)
	adapterDuplicate := base.Evidence.Adapters[0]
	adapterDuplicate.Healthy = false
	base.Evidence.Adapters = append(base.Evidence.Adapters, adapterDuplicate)
	unchanged := cloneCandidate(test, base)
	original := fingerprintFor(test, base)
	if !reflect.DeepEqual(base, unchanged) {
		test.Fatal("fingerprinting mutated the input")
	}
	changed := cloneCandidate(test, base)
	slices.Reverse(changed.Worktree.CollectionErrors)
	slices.Reverse(changed.Decision.Reasons)
	slices.Reverse(changed.Evidence.Processes)
	slices.Reverse(changed.Evidence.Agents)
	slices.Reverse(changed.Evidence.Adapters)
	for index := range changed.Evidence.Agents {
		slices.Reverse(changed.Evidence.Agents[index].ProcessRefs)
	}
	if fingerprintFor(test, changed) != original {
		test.Fatal("enumeration order changed fingerprint, including duplicate identities")
	}
	changed.Evidence.Processes = append(changed.Evidence.Processes, changed.Evidence.Processes[0])
	if fingerprintFor(test, changed) == original {
		test.Fatal("fingerprint silently discarded duplicate evidence")
	}
}

func TestCandidateFingerprintCanonicalTimes(test *testing.T) {
	base := candidateFixture()
	changed := cloneCandidate(test, base)
	zone := time.FixedZone("equivalent", 9*60*60)
	changed.Worktree.LastCommitAt = changed.Worktree.LastCommitAt.In(zone)
	changed.Worktree.MetadataModifiedAt = changed.Worktree.MetadataModifiedAt.In(zone)
	for index := range changed.Evidence.Processes {
		changed.Evidence.Processes[index].CreatedAt = changed.Evidence.Processes[index].CreatedAt.In(zone)
	}
	for index := range changed.Evidence.Agents {
		agent := &changed.Evidence.Agents[index]
		agent.CreatedAt = agent.CreatedAt.In(zone)
		agent.UpdatedAt = agent.UpdatedAt.In(zone)
		for reference := range agent.ProcessRefs {
			agent.ProcessRefs[reference].CreatedAt = agent.ProcessRefs[reference].CreatedAt.In(zone)
		}
	}
	if fingerprintFor(test, base) != fingerprintFor(test, changed) {
		test.Fatal("equivalent timestamp offsets changed fingerprint")
	}
}

func TestCandidateFingerprintCanonicalEmptyLists(test *testing.T) {
	base := domain.Candidate{}
	changed := domain.Candidate{
		Worktree: domain.Worktree{CollectionErrors: []string{}},
		Evidence: domain.EvidenceSet{Processes: []domain.ProcessEvidence{}, Agents: []domain.AgentEvidence{}, Adapters: []domain.AdapterStatus{}, Warnings: []string{}},
		Decision: domain.Decision{Reasons: []domain.Reason{}},
	}
	if fingerprintFor(test, base) != fingerprintFor(test, changed) {
		test.Fatal("nil and empty lists produced different fingerprints")
	}
	base = candidateFixture()
	base.Evidence.Agents[0].ProcessRefs = nil
	base.Evidence.Agents[0].Executable.Arguments = nil
	changed = cloneCandidate(test, base)
	changed.Evidence.Agents[0].ProcessRefs = []domain.ProcessReference{}
	changed.Evidence.Agents[0].Executable.Arguments = []string{}
	if fingerprintFor(test, base) != fingerprintFor(test, changed) {
		test.Fatal("nested nil and empty lists produced different fingerprints")
	}
}

func TestCandidateFingerprintExcludesPlanningOnlyEvidence(test *testing.T) {
	base := candidateFixture()
	changed := cloneCandidate(test, base)
	planning := cloneCandidate(test, base).Evidence.Agents[0]
	planning.RevalidationMode = "planning-only"
	planning.RawFingerprint = "changing foreground command output"
	planning.Warnings = []string{"planning warning"}
	planning.UpdatedAt = planning.UpdatedAt.Add(time.Hour)
	changed.Evidence.Agents = append(changed.Evidence.Agents, planning)
	if fingerprintFor(test, base) != fingerprintFor(test, changed) {
		test.Fatal("planning-only evidence entered the removal fingerprint")
	}
	changed.Evidence.Adapters[0].OfflineRevalidatable = false
	if fingerprintFor(test, base) == fingerprintFor(test, changed) {
		test.Fatal("adapter offline safety was excluded with planning-only evidence")
	}
}

func TestCandidateFingerprintRejectsMalformedInputs(test *testing.T) {
	invalidText := string([]byte{0xff})
	invalidTime := time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, testCase := range []struct {
		name   string
		change func(*domain.Candidate)
	}{
		{"path UTF-8", func(value *domain.Candidate) { value.Worktree.Path = invalidText }},
		{"process UTF-8", func(value *domain.Candidate) { value.Evidence.Processes[0].Executable = invalidText }},
		{"agent UTF-8", func(value *domain.Candidate) { value.Evidence.Agents[0].RawFingerprint = invalidText }},
		{"binding UTF-8", func(value *domain.Candidate) { value.Evidence.Agents[0].Binding.Identifier = invalidText }},
		{"argv UTF-8", func(value *domain.Candidate) { value.Evidence.Agents[0].Executable.Arguments[0] = invalidText }},
		{"reason UTF-8", func(value *domain.Candidate) { value.Decision.Reasons[0].Code = invalidText }},
		{"adapter UTF-8", func(value *domain.Candidate) { value.Evidence.Adapters[0].AdapterID = invalidText }},
		{"worktree time", func(value *domain.Candidate) { value.Worktree.LastCommitAt = invalidTime }},
		{"process time", func(value *domain.Candidate) { value.Evidence.Processes[0].CreatedAt = invalidTime }},
		{"agent time", func(value *domain.Candidate) { value.Evidence.Agents[0].UpdatedAt = invalidTime }},
		{"reference time", func(value *domain.Candidate) { value.Evidence.Agents[0].ProcessRefs[0].CreatedAt = invalidTime }},
		{"missing revalidation mode", func(value *domain.Candidate) { value.Evidence.Agents[0].RevalidationMode = "" }},
		{"unknown revalidation mode", func(value *domain.Candidate) { value.Evidence.Agents[0].RevalidationMode = "unknown" }},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			value := cloneCandidate(test, candidateFixture())
			testCase.change(&value)
			if fingerprint, err := CandidateFingerprint(value); err == nil || fingerprint != "" {
				test.Fatalf("malformed precondition accepted: %q, %v", fingerprint, err)
			}
		})
	}
}
