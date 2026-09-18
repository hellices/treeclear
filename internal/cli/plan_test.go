package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/pathutil"
	"github.com/hellices/treeclear/internal/process"
)

func planFixture(test *testing.T) (Dependencies, *inventoryStub) {
	test.Helper()
	dependencies, inventory := scanFixture(test)
	root, err := pathutil.Canonical(dependencies.WorkingDirectory)
	if err != nil {
		test.Fatal(err)
	}
	dependencies.WorkingDirectory = root
	worktree := &inventory.worktrees[0]
	worktree.Path = filepath.Join(root, "feature")
	worktree.RepositoryRoot = root
	worktree.CommonGitDir = filepath.Join(root, ".git")
	worktree.AdminDir = filepath.Join(worktree.CommonGitDir, "worktrees", "feature")
	worktree.Head = strings.Repeat("a", 40)
	worktree.IndexHash = "sha256:" + strings.Repeat("b", 64)
	worktree.AdminHash = "sha256:" + strings.Repeat("c", 64)
	worktree.EstimatedBytes = 128
	return dependencies, inventory
}

func runPlan(test *testing.T, dependencies Dependencies, arguments ...string) (domain.Plan, []byte, string, error) {
	test.Helper()
	var stdout, stderr bytes.Buffer
	dependencies.Stdout, dependencies.Stderr = &stdout, &stderr
	command := NewRootCommand(dependencies)
	command.SetArgs(append([]string{"plan", "--format", "json"}, arguments...))
	err := command.ExecuteContext(context.Background())
	var result domain.Plan
	if stdout.Len() != 0 {
		if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
			test.Fatalf("invalid plan JSON: %v; %q", decodeErr, stdout.String())
		}
	}
	return result, bytes.Clone(stdout.Bytes()), stderr.String(), err
}

func runExplain(dependencies Dependencies, arguments ...string) ([]byte, string, error) {
	var stdout, stderr bytes.Buffer
	dependencies.Stdout, dependencies.Stderr = &stdout, &stderr
	command := NewRootCommand(dependencies)
	command.SetArgs(append([]string{"explain"}, arguments...))
	err := command.ExecuteContext(context.Background())
	return bytes.Clone(stdout.Bytes()), stderr.String(), err
}

func TestPlanSavesAndExportsCanonicalJSON(test *testing.T) {
	dependencies, _ := planFixture(test)
	value, output, diagnostics, err := runPlan(test, dependencies, "--output", "report")
	if err != nil {
		test.Fatalf("plan error = %v, diagnostics = %q", err, diagnostics)
	}
	if value.SchemaVersion != 2 || value.IntendedApplyMode != domain.ApplyInteractive || value.Integrity.MAC == "" || value.GeneratedAt.IsZero() {
		test.Fatalf("plan metadata = %#v", value)
	}
	if value.ExpiresAt.Sub(value.GeneratedAt) != 15*time.Minute || value.Summary.Safe != 1 {
		test.Fatalf("plan defaults = %#v", value)
	}
	assertSelectionPreview(test, value, []string{}, false)
	savedPath := filepath.Join(dependencies.DataDirectory, "plans", value.ID+".json")
	saved, err := os.ReadFile(savedPath)
	if err != nil || !bytes.Equal(output, append(bytes.Clone(saved), '\n')) {
		test.Fatalf("JSON output differs from canonical storage: %v", err)
	}
	exported, err := os.ReadFile(filepath.Join(dependencies.WorkingDirectory, "report"))
	if err != nil || !bytes.Equal(exported, saved) {
		test.Fatalf("export differs from canonical storage: %v", err)
	}
	if !strings.Contains(diagnostics, "Core-only") {
		test.Fatalf("missing core-only diagnostic: %q", diagnostics)
	}
	output, _, err = runExplain(dependencies, value.Candidates[0].ID, "--plan", "report", "--format", "json")
	if err != nil {
		test.Fatalf("explain bare export: %v", err)
	}
	explanation := decodePreviewExplanation(test, output)
	if explanation.PlanID != value.ID || !reflect.DeepEqual(explanation.Removal, value.Removal) || !reflect.DeepEqual(explanation.Candidate, value.Candidates[0]) {
		test.Fatalf("explain preview mismatch: %#v", explanation)
	}
}

func TestPlanUsesRepeatedRootsAndDurationOverride(test *testing.T) {
	dependencies, inventory := planFixture(test)
	var roots []string
	for _, name := range []string{"one,with,commas", "two"} {
		root := filepath.Join(dependencies.WorkingDirectory, name)
		if err := os.Mkdir(root, 0o700); err != nil {
			test.Fatal(err)
		}
		canonical, err := pathutil.Canonical(root)
		if err != nil {
			test.Fatal(err)
		}
		roots = append(roots, canonical)
	}
	value, _, _, err := runPlan(test, dependencies, "--root", roots[0], "--root", roots[1], "--inactivity-threshold", "30d")
	if err != nil || !reflect.DeepEqual(inventory.roots, roots) || value.Summary.Protected != 1 || value.Candidates[0].Action != "none" {
		test.Fatalf("plan roots = %q, plan = %#v, error = %v", inventory.roots, value, err)
	}
}

func TestPlanRejectsInvalidFlagsBeforeCollection(test *testing.T) {
	for _, arguments := range [][]string{
		{"--format", "yaml"}, {"--root", ""}, {"--output", ""},
		{"--inactivity-threshold", "0"}, {"--inactivity-threshold", "9223372036854775807d"},
		{"--force"}, {"unexpected"},
	} {
		test.Run(strings.Join(arguments, "_"), func(test *testing.T) {
			dependencies, inventory := planFixture(test)
			_, output, _, err := runPlan(test, dependencies, arguments...)
			if err == nil || len(output) != 0 || inventory.calls != 0 {
				test.Fatalf("invalid plan: error %v, output %q, calls %d", err, output, inventory.calls)
			}
			if _, err := os.Stat(dependencies.DataDirectory); !errors.Is(err, fs.ErrNotExist) {
				test.Fatalf("invalid flags created state: %v", err)
			}
		})
	}
}

func TestPlanPersistsBlockedIncompleteCollection(test *testing.T) {
	dependencies, _ := planFixture(test)
	dependencies.Processes = processStub{collection: process.Collection{Complete: false}}
	value, output, diagnostics, err := runPlan(test, dependencies)
	if err == nil || len(output) == 0 || len(value.Candidates) != 1 || value.Candidates[0].Action != "none" || value.Summary.Protected != 1 {
		test.Fatalf("incomplete plan = %#v, error = %v", value, err)
	}
	if len(value.Warnings) < 2 || !strings.Contains(diagnostics, "incomplete") {
		test.Fatalf("incomplete diagnostics = %q, warnings = %q", diagnostics, value.Warnings)
	}
	if _, err := os.Stat(filepath.Join(dependencies.DataDirectory, "plans", value.ID+".json")); err != nil {
		test.Fatalf("blocked plan not saved: %v", err)
	}
}

func TestPlanExportFailurePreservesSavedPlan(test *testing.T) {
	dependencies, _ := planFixture(test)
	destination := filepath.Join(dependencies.WorkingDirectory, "report")
	if err := os.WriteFile(destination, []byte("existing output"), 0o600); err != nil {
		test.Fatal(err)
	}
	_, _, _, err := runPlan(test, dependencies, "--output", destination)
	if err == nil || !strings.Contains(err.Error(), "saved") {
		test.Fatalf("export error = %v", err)
	}
	contents, readErr := os.ReadFile(destination)
	if readErr != nil || string(contents) != "existing output" {
		test.Fatalf("existing export changed: %q, %v", contents, readErr)
	}
	entries, readErr := os.ReadDir(filepath.Join(dependencies.DataDirectory, "plans"))
	if readErr != nil || len(entries) != 1 {
		test.Fatalf("saved plan lost after export failure: %v, %v", entries, readErr)
	}
}

func TestPlanCanceledBeforeCollectionDoesNotCreateState(test *testing.T) {
	dependencies, inventory := planFixture(test)
	var output bytes.Buffer
	dependencies.Stdout, dependencies.Stderr = &output, &output
	command := NewRootCommand(dependencies)
	command.SetArgs([]string{"plan"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := command.ExecuteContext(ctx); !errors.Is(err, context.Canceled) || inventory.calls != 0 || output.Len() != 0 {
		test.Fatalf("canceled plan = %v, calls = %d, output = %q", err, inventory.calls, output.String())
	}
	if _, err := os.Stat(dependencies.DataDirectory); !errors.Is(err, fs.ErrNotExist) {
		test.Fatalf("canceled plan created state: %v", err)
	}
}

func TestExplainUsesLatestWithoutRecollectionOrCurrentConfig(test *testing.T) {
	dependencies, inventory := planFixture(test)
	first, _, _, err := runPlan(test, dependencies)
	if err != nil {
		test.Fatal(err)
	}
	dependencies.Now = func() time.Time { return first.GeneratedAt.Add(time.Minute) }
	inventory.worktrees[0].Head = "new-head"
	latest, _, _, err := runPlan(test, dependencies)
	if err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(dependencies.RepositoryConfigPath, []byte("not valid toml = ["), 0o600); err != nil {
		test.Fatal(err)
	}
	calls := inventory.calls
	output, _, err := runExplain(dependencies, latest.Candidates[0].ID, "--format", "json")
	if err != nil || inventory.calls != calls {
		test.Fatalf("explain recollected or read current config: %v, calls = %d", err, inventory.calls)
	}
	explanation := decodePreviewExplanation(test, output)
	if explanation.PlanID != latest.ID || explanation.Candidate.Worktree.Head != "new-head" {
		test.Fatalf("explain did not use latest plan: %q", output)
	}
	output, _, err = runExplain(dependencies, first.Candidates[0].ID, "--plan", first.ID, "--format", "json")
	if err != nil {
		test.Fatal(err)
	}
	explanation = decodePreviewExplanation(test, output)
	if explanation.PlanID != first.ID || !reflect.DeepEqual(explanation.Candidate, first.Candidates[0]) {
		test.Fatalf("explicit plan ignored: %q", output)
	}
}

func TestExplainFailureDoesNotCreateState(test *testing.T) {
	for _, arguments := range [][]string{
		{"missing", "--format", "json"}, {"missing", "--plan", "plan_absent"},
		{"missing", "--plan", ""}, {"missing", "--format", "yaml"}, {}, {"one", "two"},
	} {
		dependencies, inventory := planFixture(test)
		output, _, err := runExplain(dependencies, arguments...)
		if err == nil || len(output) != 0 || inventory.calls != 0 {
			test.Fatalf("invalid explain = %q, %v, calls = %d", output, err, inventory.calls)
		}
		if _, err := os.Stat(dependencies.DataDirectory); !errors.Is(err, fs.ErrNotExist) {
			test.Fatalf("explain created state: %v", err)
		}
	}
}

func TestPlanHumanOutputEscapesPathsAndBranchNames(test *testing.T) {
	dependencies, inventory := planFixture(test)
	inventory.worktrees[0].Branch = "topic\x1b[31m"
	var stdout, stderr bytes.Buffer
	dependencies.Stdout, dependencies.Stderr = &stdout, &stderr
	command := NewRootCommand(dependencies)
	command.SetArgs([]string{"plan"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		test.Fatal(err)
	}
	for _, want := range []string{"CANDIDATE", "CLASSIFICATION", "Safe: 1", "No worktrees were removed.", `topic\x1b[31m`} {
		if !strings.Contains(stdout.String(), want) {
			test.Errorf("human plan lacks %q: %s", want, stdout.String())
		}
	}
	if strings.ContainsRune(stdout.String(), '\x1b') || !strings.Contains(stderr.String(), "Core-only") {
		test.Fatalf("unsafe human output or missing diagnostics: %q, %q", stdout.String(), stderr.String())
	}
}

func TestPlanOutputFailuresPreserveCanonicalStorage(test *testing.T) {
	for _, failing := range []string{"stdout", "stderr"} {
		test.Run(failing, func(test *testing.T) {
			dependencies, _ := planFixture(test)
			var output bytes.Buffer
			dependencies.Stdout, dependencies.Stderr = &output, &output
			if failing == "stdout" {
				dependencies.Stdout = failingWriter{}
			} else {
				dependencies.Stderr = failingWriter{}
			}
			command := NewRootCommand(dependencies)
			command.SetArgs([]string{"plan", "--format", "json"})
			if err := command.ExecuteContext(context.Background()); !errors.Is(err, io.ErrClosedPipe) || !strings.Contains(err.Error(), "saved") {
				test.Fatalf("failed output error = %v", err)
			}
			entries, err := os.ReadDir(filepath.Join(dependencies.DataDirectory, "plans"))
			if err != nil || len(entries) != 1 {
				test.Fatalf("output failure lost saved plan: %v, %v", entries, err)
			}
		})
	}
}
