package correlate

import (
	"path/filepath"
	"slices"
	"sort"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/process"
)

func Group(worktrees []domain.Worktree, processes process.Collection, agents []domain.AgentEvidence) map[string]domain.EvidenceSet {
	global := append([]domain.ProcessEvidence(nil), processes.GlobalUnknown...)
	hasEnumerationFailure := slices.ContainsFunc(global, func(evidence domain.ProcessEvidence) bool {
		return evidence.PID == 0 && evidence.State == domain.EvidenceUnknown && evidence.Error != ""
	})
	if !processes.Complete && !hasEnumerationFailure {
		global = append(global, domain.ProcessEvidence{State: domain.EvidenceUnknown, Error: "process enumeration is incomplete"})
	}
	represented := make(map[int32]bool)
	for _, evidence := range global {
		represented[evidence.PID] = true
	}
	for _, records := range processes.ByWorktree {
		for _, evidence := range records {
			represented[evidence.PID] = true
		}
	}
	pids := make([]int, 0, len(processes.Uninspectable))
	for pid := range processes.Uninspectable {
		pids = append(pids, int(pid))
	}
	sort.Ints(pids)
	for _, pid := range pids {
		if !represented[int32(pid)] {
			evidence := processes.Uninspectable[int32(pid)]
			evidence.State = domain.EvidenceUnknown
			global = append(global, evidence)
		}
	}
	grouped := make(map[string]domain.EvidenceSet, len(worktrees))
	for _, worktree := range worktrees {
		evidence := domain.EvidenceSet{Processes: []domain.ProcessEvidence{}, Agents: []domain.AgentEvidence{}}
		evidence.Processes = append(evidence.Processes, processes.ByWorktree[worktree.Path]...)
		evidence.Processes = append(evidence.Processes, global...)
		for _, agent := range agents {
			matches, uncertain := binding(worktree, agent)
			if !matches {
				continue
			}
			if uncertain && agent.State != domain.EvidenceNotApplicable {
				agent.State = domain.EvidenceUnknown
				agent.Warnings = append(append([]string(nil), agent.Warnings...), "agent worktree binding is missing or conflicting")
			}
			evidence.Agents = append(evidence.Agents, agent)
			for _, reference := range agent.ProcessRefs {
				if record, found := processes.Uninspectable[reference.PID]; found {
					record.State = domain.EvidenceUnknown
					evidence.Processes = append(evidence.Processes, record)
				}
			}
		}
		grouped[worktree.Path] = evidence
	}
	return grouped
}

func binding(worktree domain.Worktree, agent domain.AgentEvidence) (bool, bool) {
	pathMatch := samePath(worktree.Path, agent.WorktreePath)
	cwdMatch := contains(worktree.Path, agent.CWD)
	if pathMatch || cwdMatch {
		conflict := (agent.WorktreePath != "" && !pathMatch) || (agent.CWD != "" && !cwdMatch) || (agent.RepositoryRoot != "" && !samePath(worktree.RepositoryRoot, agent.RepositoryRoot))
		return true, conflict
	}
	if agent.WorktreePath != "" || agent.CWD != "" {
		if (agent.WorktreePath != "" && !filepath.IsAbs(agent.WorktreePath)) || (agent.CWD != "" && !filepath.IsAbs(agent.CWD)) {
			return true, true
		}
		return false, false
	}
	if agent.RepositoryRoot != "" {
		return samePath(worktree.RepositoryRoot, agent.RepositoryRoot), true
	}
	return agent.State != domain.EvidenceNotApplicable, true
}

func samePath(left, right string) bool {
	return contains(left, right) && contains(right, left)
}

func contains(parent, child string) bool {
	if !filepath.IsAbs(parent) || !filepath.IsAbs(child) {
		return false
	}
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	return err == nil && filepath.IsLocal(relative)
}
