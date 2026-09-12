package correlate

import (
	"path/filepath"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/process"
)

func TestGroupPreservesGlobalUnknownAndLocalProcesses(test *testing.T) {
	root := test.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	worktrees := []domain.Worktree{{Path: first}, {Path: second}}
	collection := process.Collection{
		Complete:      true,
		ByWorktree:    map[string][]domain.ProcessEvidence{first: {{PID: 10, State: domain.EvidenceActive}}},
		GlobalUnknown: []domain.ProcessEvidence{{PID: 20, State: domain.EvidenceUnknown}},
	}
	actual := Group(worktrees, collection, nil)
	if len(actual[first].Processes) != 2 || len(actual[second].Processes) != 1 || actual[second].Processes[0].PID != 20 {
		test.Fatalf("grouped = %#v", actual)
	}
	actual[first].Processes[0].PID = 99
	if collection.ByWorktree[first][0].PID != 10 {
		test.Fatal("grouping aliased source evidence")
	}
}

func TestGroupIncompleteAndUnboundProcessesCannotDisappear(test *testing.T) {
	path := test.TempDir()
	for _, collection := range []process.Collection{
		{},
		{Complete: true, Uninspectable: map[int32]domain.ProcessEvidence{50: {PID: 50, State: domain.EvidenceUnknown}}},
	} {
		actual := Group([]domain.Worktree{{Path: path}}, collection, nil)
		if len(actual[path].Processes) == 0 || actual[path].Processes[0].State != domain.EvidenceUnknown {
			test.Fatalf("dropped unknown process evidence: %#v", actual)
		}
	}
}

func TestGroupBindsAgentPathsAndUnknownPID(test *testing.T) {
	root := test.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "first-other")
	worktrees := []domain.Worktree{{Path: first, RepositoryRoot: root}, {Path: second, RepositoryRoot: root}}
	agent := domain.AgentEvidence{WorktreePath: first, CWD: filepath.Join(first, "child"), State: domain.EvidenceInactive, ProcessRefs: []domain.ProcessReference{{PID: 50}}}
	collection := process.Collection{Complete: true, Uninspectable: map[int32]domain.ProcessEvidence{50: {PID: 50, State: domain.EvidenceUnknown}}}
	actual := Group(worktrees, collection, []domain.AgentEvidence{agent})
	if len(actual[first].Agents) != 1 || len(actual[second].Agents) != 0 || len(actual[first].Processes) == 0 {
		test.Fatalf("agent correlation = %#v", actual)
	}
}

func TestGroupConflictingAndMissingBindingsStayUnknown(test *testing.T) {
	root := test.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	worktrees := []domain.Worktree{{Path: first, RepositoryRoot: root}, {Path: second, RepositoryRoot: root}}
	for _, agent := range []domain.AgentEvidence{
		{WorktreePath: first, CWD: second, State: domain.EvidenceInactive},
		{RepositoryRoot: root, State: domain.EvidenceInactive},
		{State: domain.EvidenceInactive},
	} {
		actual := Group(worktrees, process.Collection{Complete: true}, []domain.AgentEvidence{agent})
		for _, path := range []string{first, second} {
			if len(actual[path].Agents) != 1 || actual[path].Agents[0].State != domain.EvidenceUnknown {
				test.Fatalf("binding became known for %q: %#v", path, actual[path])
			}
		}
	}
}
