package correlate

import (
	"context"
	"path/filepath"
	"reflect"
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

func TestGroupPreservesCollectorEnumerationFailureOnce(test *testing.T) {
	worktrees := []domain.Worktree{{Path: test.TempDir()}, {Path: test.TempDir()}}
	collection, failures := (process.Collector{}).Collect(context.Background(), worktrees)
	if collection.Complete || len(failures) == 0 || len(collection.GlobalUnknown) != 1 {
		test.Fatalf("expected a collector enumeration failure: %#v, %v", collection, failures)
	}
	actual := Group(worktrees, collection, nil)
	for _, worktree := range worktrees {
		if !reflect.DeepEqual(actual[worktree.Path].Processes, collection.GlobalUnknown) {
			test.Errorf("enumeration failure changed during correlation: got %#v, want %#v", actual[worktree.Path].Processes, collection.GlobalUnknown)
		}
	}
}

func TestGroupIncompleteEnumerationRetainsFallbackForProcessRecords(test *testing.T) {
	path := test.TempDir()
	for _, record := range []domain.ProcessEvidence{
		{PID: 42, State: domain.EvidenceUnknown, Error: "process inspection denied"},
		{State: domain.EvidenceInactive},
		{State: domain.EvidenceUnknown},
	} {
		collection := process.Collection{GlobalUnknown: []domain.ProcessEvidence{record}}
		actual := Group([]domain.Worktree{{Path: path}}, collection, nil)[path].Processes
		if len(actual) != 2 || !reflect.DeepEqual(actual[0], record) || actual[1].PID != 0 || actual[1].State != domain.EvidenceUnknown || actual[1].Error != "process enumeration is incomplete" {
			test.Fatalf("incomplete enumeration lost its fallback: %#v", actual)
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
