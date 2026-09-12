package policy

import (
	"time"

	"github.com/hellices/treeclear/internal/domain"
)

func Evaluate(worktree domain.Worktree, evidence domain.EvidenceSet, policy domain.Policy) domain.Decision {
	inactiveSince := worktree.LastCommitAt
	if worktree.MetadataModifiedAt.After(inactiveSince) {
		inactiveSince = worktree.MetadataModifiedAt
	}
	inactiveFor := policy.Now.Sub(inactiveSince)
	if inactiveFor < 0 {
		inactiveFor = 0
	}
	decide := func(classification domain.Classification, code, message string) domain.Decision {
		return domain.Decision{Classification: classification, InactiveFor: inactiveFor, Reasons: []domain.Reason{{Code: code, Message: message}}}
	}
	switch {
	case worktree.Primary:
		return decide(domain.Protected, "primary_worktree", "primary worktrees are never removable")
	case worktree.Current:
		return decide(domain.Protected, "current_worktree", "Treeclear is running inside this worktree")
	case !worktree.PathSafe:
		return decide(domain.Protected, "unsafe_path", "canonical path identity is not safe")
	case worktree.Locked:
		return decide(domain.Protected, "locked", "Git reports the worktree as locked")
	case worktree.Prunable:
		return decide(domain.Protected, "prunable_registration", "missing worktree registration requires explicit metadata cleanup")
	case !worktree.GitStateKnown || worktree.LastCommitAt.IsZero() || worktree.MetadataModifiedAt.IsZero():
		return decide(domain.Protected, "unknown_git_state", "required Git state could not be collected")
	case !worktree.Status.Clean():
		return decide(domain.Protected, "dirty", "worktree has staged, unstaged, unmerged, or untracked changes")
	case policy.Now.IsZero() || policy.Settings.InactivityThreshold <= 0 || trustRank(policy.Settings.MinimumTrustGrade) == trustRank(""):
		return decide(domain.Protected, "invalid_policy", "clock, inactivity threshold, or minimum trust grade is invalid")
	case hasActiveProcess(evidence):
		return decide(domain.Protected, "active_process", "a process cwd is inside the worktree")
	case hasActiveAgent(evidence, policy.Now, policy.Settings.InactivityThreshold):
		return decide(domain.Protected, "active_agent", "an agent session is active or recent")
	case hasUnhealthyAdapter(evidence):
		return decide(domain.Protected, "adapter_unhealthy", "a relevant adapter is unhealthy")
	case hasUnknownEvidence(evidence):
		return decide(domain.Protected, "unknown_evidence", "relevant evidence is unknown or conflicting")
	case inactiveFor < policy.Settings.InactivityThreshold:
		return decide(domain.Protected, "recent", "worktree has not exceeded the inactivity threshold")
	case worktree.Detached:
		return decide(domain.Review, "detached", "detached HEAD requires review")
	case !worktree.Recoverable:
		return decide(domain.Review, "unrecoverable_commits", "branch state has no proven recovery path")
	case hasNonOfflineAdapter(evidence):
		return decide(domain.Review, "offline_revalidation", "relevant adapter evidence cannot be fully revalidated offline")
	case hasInsufficientTrust(evidence, policy.Settings.MinimumTrustGrade):
		return decide(domain.Review, "adapter_trust", "relevant adapter evidence does not meet the trust policy")
	default:
		return decide(domain.Safe, "safe", "clean, inactive, and recoverable under collected evidence")
	}
}

func hasActiveProcess(evidence domain.EvidenceSet) bool {
	for _, process := range evidence.Processes {
		if process.State == domain.EvidenceActive {
			return true
		}
	}
	return false
}

func hasActiveAgent(evidence domain.EvidenceSet, now time.Time, threshold time.Duration) bool {
	for _, agent := range evidence.Agents {
		switch agent.State {
		case domain.EvidenceActive:
			return true
		case domain.EvidenceIdle, domain.EvidenceCompleted, domain.EvidenceArchived, domain.EvidenceInactive:
			latest := agent.UpdatedAt
			if agent.CreatedAt.After(latest) {
				latest = agent.CreatedAt
			}
			if !latest.IsZero() && now.Sub(latest) < threshold {
				return true
			}
		}
	}
	return false
}

func hasUnhealthyAdapter(evidence domain.EvidenceSet) bool {
	for _, adapter := range evidence.Adapters {
		if adapter.Applicable && !adapter.Healthy {
			return true
		}
	}
	return false
}

func hasUnknownEvidence(evidence domain.EvidenceSet) bool {
	if len(evidence.Warnings) != 0 {
		return true
	}
	for _, process := range evidence.Processes {
		if process.Error != "" || (process.State != domain.EvidenceActive && process.State != domain.EvidenceInactive && process.State != domain.EvidenceNotApplicable) {
			return true
		}
	}
	type identity struct{ adapter, source, session, thread string }
	seen := make(map[identity]domain.AgentEvidence)
	for _, agent := range evidence.Agents {
		if agent.State == domain.EvidenceNotApplicable {
			continue
		}
		switch agent.State {
		case domain.EvidenceActive, domain.EvidenceIdle, domain.EvidenceCompleted, domain.EvidenceArchived, domain.EvidenceInactive:
		default:
			return true
		}
		if len(agent.Warnings) != 0 {
			return true
		}
		if agent.SessionID != "" || agent.ThreadID != "" {
			key := identity{agent.AdapterID, agent.SourceID, agent.SessionID, agent.ThreadID}
			if prior, found := seen[key]; found && (prior.State != agent.State || prior.CWD != agent.CWD || prior.WorktreePath != agent.WorktreePath || prior.RepositoryRoot != agent.RepositoryRoot) {
				return true
			}
			seen[key] = agent
		}
	}
	return false
}

func hasNonOfflineAdapter(evidence domain.EvidenceSet) bool {
	for _, adapter := range evidence.Adapters {
		if adapter.Applicable && !adapter.OfflineRevalidatable {
			return true
		}
	}
	return false
}

func hasInsufficientTrust(evidence domain.EvidenceSet, minimum domain.TrustGrade) bool {
	for _, adapter := range evidence.Adapters {
		if adapter.Applicable && (!adapter.Trusted || trustRank(adapter.BestGrade) > trustRank(minimum)) {
			return true
		}
	}
	for _, agent := range evidence.Agents {
		if agent.State != domain.EvidenceNotApplicable && trustRank(agent.SupportGrade) > trustRank(minimum) {
			return true
		}
	}
	return false
}

func trustRank(grade domain.TrustGrade) int {
	grades := [...]domain.TrustGrade{domain.TrustSupportedAPI, domain.TrustSupportedAppServer, domain.TrustSupportedCLI, domain.TrustExperimentalAPI, domain.TrustVersionedPrivate, domain.TrustUnversionedPrivate, domain.TrustProcessOnly}
	for rank, known := range grades {
		if grade == known {
			return rank
		}
	}
	return len(grades)
}
