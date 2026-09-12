package policy

import (
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
)

func testPolicy() domain.Policy {
	return domain.Policy{
		Now:      time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC),
		Settings: domain.PolicySettings{InactivityThreshold: 7 * 24 * time.Hour, MinimumTrustGrade: domain.TrustVersionedPrivate},
	}
}

func eligibleWorktree() domain.Worktree {
	stale := testPolicy().Now.Add(-30 * 24 * time.Hour)
	return domain.Worktree{Path: "/repo/linked", PathSafe: true, GitStateKnown: true, Recoverable: true, LastCommitAt: stale, MetadataModifiedAt: stale}
}

func TestEvaluateDecisionTable(test *testing.T) {
	cases := []struct {
		name   string
		change func(*domain.Worktree, *domain.EvidenceSet)
		class  domain.Classification
		reason string
	}{
		{"primary", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) { worktree.Primary = true }, domain.Protected, "primary_worktree"},
		{"current", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) { worktree.Current = true }, domain.Protected, "current_worktree"},
		{"unsafe", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) { worktree.PathSafe = false }, domain.Protected, "unsafe_path"},
		{"locked", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) { worktree.Locked = true }, domain.Protected, "locked"},
		{"prunable", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) { worktree.Prunable = true }, domain.Protected, "prunable_registration"},
		{"unknown git", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) { worktree.GitStateKnown = false }, domain.Protected, "unknown_git_state"},
		{"dirty", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) { worktree.Status.Untracked = 1 }, domain.Protected, "dirty"},
		{"active process", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) {
			evidence.Processes = []domain.ProcessEvidence{{State: domain.EvidenceActive}}
		}, domain.Protected, "active_process"},
		{"active agent", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) {
			evidence.Agents = []domain.AgentEvidence{{State: domain.EvidenceActive}}
		}, domain.Protected, "active_agent"},
		{"unknown process", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) {
			evidence.Processes = []domain.ProcessEvidence{{State: domain.EvidenceUnknown}}
		}, domain.Protected, "unknown_evidence"},
		{"unknown agent", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) {
			evidence.Agents = []domain.AgentEvidence{{State: domain.EvidenceUnknown}}
		}, domain.Protected, "unknown_evidence"},
		{"unhealthy adapter", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) {
			evidence.Adapters = []domain.AdapterStatus{{Applicable: true}}
		}, domain.Protected, "adapter_unhealthy"},
		{"adapter with error", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) {
			evidence.Adapters = []domain.AdapterStatus{{Applicable: true, Healthy: true, Trusted: true, OfflineRevalidatable: true, BestGrade: domain.TrustSupportedAPI, Error: "collection failed"}}
		}, domain.Protected, "adapter_unhealthy"},
		{"non-applicable adapter error", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) {
			evidence.Adapters = []domain.AdapterStatus{{Error: "unrelated provider is unavailable"}}
		}, domain.Safe, "safe"},
		{"recent", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) {
			worktree.MetadataModifiedAt = testPolicy().Now
		}, domain.Protected, "recent"},
		{"detached", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) { worktree.Detached = true }, domain.Review, "detached"},
		{"unrecoverable", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) { worktree.Recoverable = false }, domain.Review, "unrecoverable_commits"},
		{"offline", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) {
			evidence.Adapters = []domain.AdapterStatus{{Applicable: true, Healthy: true, Trusted: true, BestGrade: domain.TrustSupportedAPI}}
		}, domain.Review, "offline_revalidation"},
		{"low trust", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) {
			evidence.Adapters = []domain.AdapterStatus{{Applicable: true, Healthy: true, Trusted: true, OfflineRevalidatable: true, BestGrade: domain.TrustProcessOnly}}
		}, domain.Review, "adapter_trust"},
		{"safe", func(worktree *domain.Worktree, evidence *domain.EvidenceSet) {}, domain.Safe, "safe"},
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			worktree := eligibleWorktree()
			evidence := domain.EvidenceSet{}
			scenario.change(&worktree, &evidence)
			actual := Evaluate(worktree, evidence, testPolicy())
			if actual.Classification != scenario.class || len(actual.Reasons) != 1 || actual.Reasons[0].Code != scenario.reason {
				test.Fatalf("decision = %#v", actual)
			}
		})
	}
}

func TestEvaluatePrecedenceAndInactivity(test *testing.T) {
	worktree := eligibleWorktree()
	worktree.Primary, worktree.Locked = true, true
	worktree.Status.Staged = 1
	actual := Evaluate(worktree, domain.EvidenceSet{}, testPolicy())
	if actual.Reasons[0].Code != "primary_worktree" {
		test.Fatalf("precedence = %#v", actual)
	}
	worktree = eligibleWorktree()
	worktree.MetadataModifiedAt = testPolicy().Now.Add(-time.Hour)
	actual = Evaluate(worktree, domain.EvidenceSet{}, testPolicy())
	if actual.Reasons[0].Code != "recent" || actual.InactiveFor != time.Hour {
		test.Fatalf("inactivity = %#v", actual)
	}
}

func TestEvaluateRejectsMissingTimeAndUnknownStates(test *testing.T) {
	worktree := eligibleWorktree()
	worktree.LastCommitAt = time.Time{}
	if actual := Evaluate(worktree, domain.EvidenceSet{}, testPolicy()); actual.Classification != domain.Protected {
		test.Fatalf("missing timestamp = %#v", actual)
	}
	worktree = eligibleWorktree()
	for _, state := range []domain.EvidenceState{"", "future-state"} {
		actual := Evaluate(worktree, domain.EvidenceSet{Agents: []domain.AgentEvidence{{State: state}}}, testPolicy())
		if actual.Classification != domain.Protected || actual.Reasons[0].Code != "unknown_evidence" {
			test.Fatalf("unknown state = %#v", actual)
		}
	}
}

func TestEvaluateRecentCompletedAgentIsProtected(test *testing.T) {
	evidence := domain.EvidenceSet{Agents: []domain.AgentEvidence{{State: domain.EvidenceCompleted, UpdatedAt: testPolicy().Now.Add(-time.Hour)}}}
	actual := Evaluate(eligibleWorktree(), evidence, testPolicy())
	if actual.Classification != domain.Protected || actual.Reasons[0].Code != "active_agent" {
		test.Fatalf("recent agent = %#v", actual)
	}
}

func TestEvaluateTrustAndApplicability(test *testing.T) {
	for _, grade := range []domain.TrustGrade{domain.TrustProcessOnly, "future-grade", ""} {
		evidence := domain.EvidenceSet{Adapters: []domain.AdapterStatus{{Applicable: true, Healthy: true, Trusted: true, OfflineRevalidatable: true, BestGrade: grade}}}
		if actual := Evaluate(eligibleWorktree(), evidence, testPolicy()); actual.Reasons[0].Code != "adapter_trust" {
			test.Fatalf("weak trust = %#v", actual)
		}
	}
	evidence := domain.EvidenceSet{Adapters: []domain.AdapterStatus{{Applicable: false}}}
	if actual := Evaluate(eligibleWorktree(), evidence, testPolicy()); actual.Classification != domain.Safe {
		test.Fatalf("irrelevant adapter = %#v", actual)
	}
}
