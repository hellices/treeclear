package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/pathutil"
	"github.com/hellices/treeclear/internal/process"
	"github.com/hellices/treeclear/internal/testutil"
)

type inventoryStub struct {
	worktrees []domain.Worktree
	errors    []error
	roots     []string
	calls     int
}

func (inventory *inventoryStub) Load(ctx context.Context, roots []string) ([]domain.Worktree, []error) {
	inventory.calls++
	inventory.roots = append([]string(nil), roots...)
	return inventory.worktrees, inventory.errors
}

type processStub struct {
	collection process.Collection
	errors     []error
}

func (collector processStub) Collect(context.Context, []domain.Worktree) (process.Collection, []error) {
	return collector.collection, collector.errors
}

func scanFixture(test *testing.T) (Dependencies, *inventoryStub) {
	test.Helper()
	root := test.TempDir()
	if err := os.WriteFile(filepath.Join(root, "user.toml"), []byte("roots = [\".\"]\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	now := time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)
	inventory := &inventoryStub{worktrees: []domain.Worktree{{
		Path: filepath.Join(root, "feature"), RepositoryRoot: root, Branch: "topic", PathSafe: true,
		GitStateKnown: true, Recoverable: true, LastCommitAt: now.Add(-14 * 24 * time.Hour), MetadataModifiedAt: now.Add(-14 * 24 * time.Hour),
	}}}
	return Dependencies{
		Inventory: inventory, Processes: processStub{collection: process.Collection{Complete: true}},
		Now: func() time.Time { return now }, WorkingDirectory: root, BuildVersion: "test",
		UserConfigPath: filepath.Join(root, "user.toml"), RepositoryConfigPath: filepath.Join(root, "treeclear.toml"), DataDirectory: filepath.Join(root, "state"),
	}, inventory
}

func runScan(test *testing.T, dependencies Dependencies, arguments ...string) (ScanResult, string, error) {
	test.Helper()
	var stdout, stderr bytes.Buffer
	dependencies.Stdout, dependencies.Stderr = &stdout, &stderr
	command := NewRootCommand(dependencies)
	command.SetArgs(append([]string{"scan", "--format", "json"}, arguments...))
	err := command.ExecuteContext(context.Background())
	var result ScanResult
	if stdout.Len() != 0 {
		if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
			test.Fatalf("invalid scan JSON: %v; %q", decodeErr, stdout.String())
		}
	}
	return result, stderr.String(), err
}

func TestScanJSONAndRepeatedRoots(test *testing.T) {
	dependencies, inventory := scanFixture(test)
	first := filepath.Join(dependencies.WorkingDirectory, "one,with,commas")
	second := filepath.Join(dependencies.WorkingDirectory, "two")
	result, _, err := runScan(test, dependencies, "--root", first, "--root", second)
	if err != nil || !result.Complete || result.SchemaVersion != 1 || len(result.Worktrees) != 1 {
		test.Fatalf("scan = %#v, error = %v", result, err)
	}
	if !reflect.DeepEqual(inventory.roots, []string{first, second}) || result.Worktrees[0].Decision.Classification != domain.Safe {
		test.Fatalf("roots = %q, scan = %#v", inventory.roots, result)
	}
}

func TestScanUsesDurationOverride(test *testing.T) {
	dependencies, inventory := scanFixture(test)
	result, _, err := runScan(test, dependencies, "--inactivity-threshold", "30d")
	if err != nil || len(result.Worktrees) != 1 || result.Worktrees[0].Decision.Reasons[0].Code != "recent" {
		test.Fatalf("scan = %#v, error = %v", result, err)
	}
	workingDirectory, err := pathutil.Canonical(dependencies.WorkingDirectory)
	if err != nil {
		test.Fatal(err)
	}
	if len(inventory.roots) != 1 || filepath.Clean(inventory.roots[0]) != workingDirectory {
		test.Fatalf("configured roots = %q", inventory.roots)
	}
}

func TestScanDefaultsToContainingRepository(test *testing.T) {
	dependencies, inventory := scanFixture(test)
	if err := os.Remove(dependencies.UserConfigPath); err != nil {
		test.Fatal(err)
	}
	repository := testutil.NewRepository(test)
	dependencies.WorkingDirectory = filepath.Join(repository.Root, "subdirectory")
	if err := os.Mkdir(dependencies.WorkingDirectory, 0o700); err != nil {
		test.Fatal(err)
	}
	if _, _, err := runScan(test, dependencies); err != nil {
		test.Fatal(err)
	}
	if !reflect.DeepEqual(inventory.roots, []string{repository.Root}) {
		test.Fatalf("default roots = %q, want containing repository %q", inventory.roots, repository.Root)
	}
}

func TestScanOutsideRepositoryDoesNotStartCollection(test *testing.T) {
	dependencies, inventory := scanFixture(test)
	if err := os.Remove(dependencies.UserConfigPath); err != nil {
		test.Fatal(err)
	}
	result, _, err := runScan(test, dependencies)
	if err == nil || !strings.Contains(err.Error(), "--root") || inventory.calls != 0 || len(result.Worktrees) != 0 {
		test.Fatalf("unconfigured scan = %#v, error = %v, inventory calls = %d", result, err, inventory.calls)
	}
}

func TestScanRejectsFlagsBeforeCollection(test *testing.T) {
	for _, arguments := range [][]string{
		{"--format", "yaml"}, {"--root", ""}, {"--inactivity-threshold", "0"},
		{"--inactivity-threshold", "9223372036854775807d"}, {"--force"}, {"unexpected"},
	} {
		dependencies, inventory := scanFixture(test)
		_, _, err := runScan(test, dependencies, arguments...)
		if err == nil || inventory.calls != 0 {
			test.Fatalf("arguments %q: error = %v, collection calls = %d", arguments, err, inventory.calls)
		}
	}
}

func TestScanIncompleteEvidenceIsProtected(test *testing.T) {
	for _, failInventory := range []bool{false, true} {
		dependencies, inventory := scanFixture(test)
		if failInventory {
			inventory.errors = []error{errors.New("unreadable root")}
		} else {
			dependencies.Processes = processStub{errors: []error{errors.New("process enumeration denied")}}
		}
		result, _, err := runScan(test, dependencies)
		if err == nil || result.Complete || len(result.Worktrees) != 1 || result.Worktrees[0].Decision.Classification != domain.Protected {
			test.Fatalf("incomplete scan = %#v, error = %v", result, err)
		}
	}
}

func TestScanCollectionErrorsDoNotInventProcessEvidence(test *testing.T) {
	for _, source := range []string{"inventory", "process inspection"} {
		test.Run(source, func(test *testing.T) {
			dependencies, inventory := scanFixture(test)
			first := inventory.worktrees[0].Path
			other := inventory.worktrees[0]
			other.Path = filepath.Join(dependencies.WorkingDirectory, "other")
			inventory.worktrees = append(inventory.worktrees, other)
			warning := source + " denied"
			collector := processStub{collection: process.Collection{Complete: true}}
			var localEvidence []domain.ProcessEvidence
			if source == "inventory" {
				inventory.errors = []error{errors.New(warning)}
			} else {
				localEvidence = []domain.ProcessEvidence{{PID: 42, State: domain.EvidenceUnknown, Error: warning}}
				collector.collection.ByWorktree = map[string][]domain.ProcessEvidence{first: localEvidence}
				collector.errors = []error{errors.New(warning)}
			}
			dependencies.Processes = collector
			result, _, err := runScan(test, dependencies)
			if err == nil || result.Complete || len(result.Worktrees) != 2 {
				test.Fatalf("incomplete scan = %#v, error = %v", result, err)
			}
			for _, item := range result.Worktrees {
				wantProcesses := []domain.ProcessEvidence{}
				if item.Worktree.Path == first {
					wantProcesses = append(wantProcesses, localEvidence...)
				}
				if !reflect.DeepEqual(item.Evidence.Processes, wantProcesses) {
					test.Errorf("collection error changed process evidence: got %#v, want %#v", item.Evidence.Processes, wantProcesses)
				}
				if item.Decision.Classification != domain.Protected || !reflect.DeepEqual(item.Evidence.Warnings, []string{warning}) {
					test.Errorf("collection failure was not retained as protective warning: %#v", item)
				}
			}
		})
	}
}

func TestScanHumanEscapesControlCharacters(test *testing.T) {
	dependencies, inventory := scanFixture(test)
	inventory.worktrees[0].Path += "\nname\x1b[31m"
	var stdout bytes.Buffer
	dependencies.Stdout, dependencies.Stderr = &stdout, &bytes.Buffer{}
	command := NewRootCommand(dependencies)
	command.SetArgs([]string{"scan"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		test.Fatal(err)
	}
	if strings.Contains(stdout.String(), "\x1b") || !strings.Contains(stdout.String(), `\nname`) || !strings.Contains(stdout.String(), "CLASSIFICATION") {
		test.Fatalf("unsafe human output: %q", stdout.String())
	}
}

func TestDefaultHelpDoesNotCollect(test *testing.T) {
	dependencies, inventory := scanFixture(test)
	dependencies.Stdout, dependencies.Stderr = &bytes.Buffer{}, &bytes.Buffer{}
	command := NewRootCommand(dependencies)
	command.SetArgs(nil)
	if err := command.ExecuteContext(context.Background()); err != nil || inventory.calls != 0 {
		test.Fatalf("default help error = %v, collection calls = %d", err, inventory.calls)
	}
}

func TestCanceledScanDoesNotStartCollection(test *testing.T) {
	dependencies, inventory := scanFixture(test)
	dependencies.Stdout, dependencies.Stderr = &bytes.Buffer{}, &bytes.Buffer{}
	command := NewRootCommand(dependencies)
	command.SetArgs([]string{"scan"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := command.ExecuteContext(ctx); !errors.Is(err, context.Canceled) || inventory.calls != 0 {
		test.Fatalf("canceled scan error = %v, collection calls = %d", err, inventory.calls)
	}
}
