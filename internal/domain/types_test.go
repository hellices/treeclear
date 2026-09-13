package domain

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestPlanJSONUsesStableSchemaNames(test *testing.T) {
	data, err := json.Marshal(Plan{SchemaVersion: 1, ID: "plan_test", Candidates: []Candidate{{ID: "candidate_test"}}})
	if err != nil {
		test.Fatal(err)
	}
	for _, expected := range []string{`"schemaVersion":1`, `"planId":"plan_test"`, `"candidateId":"candidate_test"`} {
		if !strings.Contains(string(data), expected) {
			test.Fatalf("plan JSON %s does not contain %s", data, expected)
		}
	}
}

func TestDomainJSONFieldNames(test *testing.T) {
	testCases := []struct {
		name   string
		value  any
		fields string
	}{
		{"git status", GitStatus{}, "staged unstaged unmerged untracked"},
		{
			name: "worktree",
			value: Worktree{
				Branch: "topic", Upstream: "origin/topic", LockReason: "locked", CollectionErrors: []string{"unknown"},
			},
			fields: "path repositoryRoot commonGitDir adminDir head branch upstream primary current pathSafe detached locked lockReason prunable gitStateKnown collectionErrors status recoverable lastCommitAt metadataModifiedAt estimatedBytes indexHash adminHash",
		},
		{
			name:   "process evidence",
			value:  ProcessEvidence{CWD: "worktree", Error: "unknown"},
			fields: "pid createdAt executable cwd state error fingerprint",
		},
		{"process reference", ProcessReference{}, "pid createdAt executable fingerprint"},
		{"executable identity", ExecutableIdentity{}, "path version sha256 arguments workingDirectory environmentDigest invocationDigest"},
		{"worktree binding", WorktreeBinding{Version: "1"}, "kind identifier version"},
		{
			name: "agent evidence",
			value: AgentEvidence{
				SessionID: "session", ThreadID: "thread", ProjectID: "project", CWD: "worktree",
				RepositoryRoot: "repository", WorktreePath: "worktree", ProcessRefs: []ProcessReference{{}},
				Binding: &WorktreeBinding{}, SchemaVersion: "1", TrustRecordDigest: "trust",
				Executable: &ExecutableIdentity{}, Warnings: []string{"unknown"},
			},
			fields: "adapterId adapterVersion bundleDigest provider sourceId sessionId threadId projectId cwd repositoryRoot worktreePath state createdAt updatedAt observedAt processRefs binding sourceKind supportGrade confidence schemaVersion rawFingerprint trustRecordDigest executable revalidationMode warnings",
		},
		{
			name:   "evidence set",
			value:  EvidenceSet{Adapters: []AdapterStatus{{}}, Warnings: []string{"unknown"}},
			fields: "processes agents adapters warnings",
		},
		{
			name:   "adapter status",
			value:  AdapterStatus{BestGrade: TrustVersionedPrivate, Error: "unknown"},
			fields: "adapterId applicable healthy trusted bestGrade offlineRevalidatable error",
		},
		{"reason", Reason{}, "code message"},
		{"decision", Decision{}, "classification reasons inactiveFor"},
		{"policy", Policy{}, "now settings"},
		{"policy settings", PolicySettings{}, "inactivityThreshold planExpiry baseBranches minimumTrustGrade snapshotMaxBytes"},
		{
			name:   "plan",
			value:  Plan{ExecutableIdentities: []ExecutableIdentity{{}}, Warnings: []string{"unknown"}},
			fields: "schemaVersion planId generatedAt expiresAt toolVersion intendedApplyMode policyDigest adapterLockDigest executableIdentities scope candidates summary warnings integrity",
		},
		{"plan integrity", PlanIntegrity{}, "algorithm keyId mac"},
		{"plan scope", PlanScope{}, "roots"},
		{"candidate", Candidate{}, "candidateId worktree evidence decision action fingerprint snapshot"},
		{"snapshot plan", SnapshotPlan{}, "required maximumBytes untrackedFiles untrackedBytes sensitiveBlocked"},
		{"plan summary", PlanSummary{}, "safe review protected reclaimableBytes"},
		{"apply journal", ApplyJournal{}, "schemaVersion planId startedAt updatedAt state entries"},
		{"journal entry", JournalEntry{SnapshotID: "snapshot", Error: "unknown"}, "candidateId state snapshotId error"},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			data, err := json.Marshal(testCase.value)
			if err != nil {
				test.Fatal(err)
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(data, &object); err != nil {
				test.Fatal(err)
			}
			actual := make([]string, 0, len(object))
			for field := range object {
				actual = append(actual, field)
			}
			expected := strings.Fields(testCase.fields)
			slices.Sort(actual)
			slices.Sort(expected)
			if !slices.Equal(actual, expected) {
				test.Fatalf("JSON fields = %v, want %v", actual, expected)
			}
		})
	}
}

func TestDomainJSONOmitsAbsentOptionalFields(test *testing.T) {
	testCases := []struct {
		name   string
		value  any
		absent string
	}{
		{"worktree", Worktree{}, "branch upstream lockReason collectionErrors"},
		{"process evidence", ProcessEvidence{}, "cwd error"},
		{"worktree binding", WorktreeBinding{}, "version"},
		{"agent evidence", AgentEvidence{}, "sessionId threadId projectId cwd repositoryRoot worktreePath processRefs binding schemaVersion trustRecordDigest executable warnings"},
		{"evidence set", EvidenceSet{}, "adapters warnings"},
		{"adapter status", AdapterStatus{}, "bestGrade error"},
		{"plan", Plan{}, "executableIdentities warnings"},
		{"journal entry", JournalEntry{}, "snapshotId error"},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			data, err := json.Marshal(testCase.value)
			if err != nil {
				test.Fatal(err)
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(data, &object); err != nil {
				test.Fatal(err)
			}
			for _, field := range strings.Fields(testCase.absent) {
				if _, exists := object[field]; exists {
					test.Errorf("JSON %s contains absent optional field %q", data, field)
				}
			}
		})
	}
}

func TestDomainEnumValues(test *testing.T) {
	testCases := []struct {
		name     string
		actual   string
		expected string
	}{
		{"EvidenceActive", string(EvidenceActive), "active"},
		{"EvidenceIdle", string(EvidenceIdle), "idle"},
		{"EvidenceCompleted", string(EvidenceCompleted), "completed"},
		{"EvidenceArchived", string(EvidenceArchived), "archived"},
		{"EvidenceInactive", string(EvidenceInactive), "inactive"},
		{"EvidenceUnknown", string(EvidenceUnknown), "unknown"},
		{"EvidenceNotApplicable", string(EvidenceNotApplicable), "not-applicable"},
		{"TrustSupportedAPI", string(TrustSupportedAPI), "supported-api"},
		{"TrustSupportedAppServer", string(TrustSupportedAppServer), "supported-app-server"},
		{"TrustSupportedCLI", string(TrustSupportedCLI), "supported-cli"},
		{"TrustExperimentalAPI", string(TrustExperimentalAPI), "experimental-api"},
		{"TrustVersionedPrivate", string(TrustVersionedPrivate), "versioned-private"},
		{"TrustUnversionedPrivate", string(TrustUnversionedPrivate), "unversioned-private"},
		{"TrustProcessOnly", string(TrustProcessOnly), "process-only"},
		{"Protected", string(Protected), "protected"},
		{"Review", string(Review), "review"},
		{"Safe", string(Safe), "safe"},
		{"ApplyInteractive", string(ApplyInteractive), "interactive"},
		{"ApplyScheduled", string(ApplyScheduled), "scheduled"},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			if testCase.actual != testCase.expected {
				test.Fatalf("%s = %q, want %q", testCase.name, testCase.actual, testCase.expected)
			}
		})
	}
}

func TestGitStatusClean(test *testing.T) {
	testCases := []struct {
		name     string
		status   GitStatus
		expected bool
	}{
		{"empty", GitStatus{}, true},
		{"staged", GitStatus{Staged: 1}, false},
		{"unstaged", GitStatus{Unstaged: 1}, false},
		{"unmerged", GitStatus{Unmerged: 1}, false},
		{"untracked", GitStatus{Untracked: 1}, false},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			if actual := testCase.status.Clean(); actual != testCase.expected {
				test.Fatalf("%#v.Clean() = %t, want %t", testCase.status, actual, testCase.expected)
			}
		})
	}
}

func TestGitStatusCleanRejectsInvalidOrOverflowingCounts(test *testing.T) {
	maximumInt := int(^uint(0) >> 1)
	minimumInt := -maximumInt - 1
	testCases := []struct {
		name   string
		status GitStatus
	}{
		{"negative staged", GitStatus{Staged: -1}},
		{"negative unstaged", GitStatus{Unstaged: -1}},
		{"negative unmerged", GitStatus{Unmerged: -1}},
		{"negative untracked", GitStatus{Untracked: -1}},
		{"staged cancels untracked", GitStatus{Staged: -1, Untracked: 1}},
		{"unstaged cancels staged", GitStatus{Staged: 1, Unstaged: -1}},
		{"unmerged cancels staged", GitStatus{Staged: 1, Unmerged: -1}},
		{"untracked cancels staged", GitStatus{Staged: 1, Untracked: -1}},
		{"positive counts overflow", GitStatus{Staged: maximumInt, Unstaged: maximumInt, Unmerged: 2}},
		{"negative counts overflow", GitStatus{Staged: minimumInt, Unstaged: minimumInt}},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			if testCase.status.Clean() {
				test.Fatalf("%#v.Clean() = true, want false", testCase.status)
			}
		})
	}
}
