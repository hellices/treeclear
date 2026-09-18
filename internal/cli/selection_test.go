package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/pathutil"
	"github.com/hellices/treeclear/internal/plan"
	"github.com/hellices/treeclear/internal/process"
)

func TestPlanBackupRequestFailsBeforeCollection(test *testing.T) {
	dependencies, inventory := planFixture(test)
	collector := &selectionProcessCounter{}
	dependencies.Processes = collector
	_, output, _, err := runPlan(test, dependencies, "--backup")
	if err == nil || !strings.Contains(err.Error(), "backup is not implemented") || len(output) != 0 || inventory.calls != 0 || collector.calls != 0 {
		test.Fatalf("backup request was not refused explicitly: error %v, output %q, inventory calls %d, process calls %d", err, output, inventory.calls, collector.calls)
	}
	assertNoSelectionState(test, dependencies)
}

func TestPlanBackupRequestPrecedesConfigurationAndTargetResolution(test *testing.T) {
	for _, arguments := range [][]string{
		{"--backup"}, {"--backup=true"}, {"--backup", "--worktree", "", "--skip-dirty"},
	} {
		test.Run(strings.Join(arguments, "_"), func(test *testing.T) {
			dependencies, inventory := planFixture(test)
			if err := os.WriteFile(dependencies.UserConfigPath, []byte("not valid toml = ["), 0o600); err != nil {
				test.Fatal(err)
			}
			dependencies.WorkingDirectory = filepath.Join(dependencies.WorkingDirectory, "missing")
			_, output, diagnostics, err := runPlan(test, dependencies, arguments...)
			if err == nil || !strings.Contains(err.Error(), "backup is not implemented") || len(output) != 0 || diagnostics != "" || inventory.calls != 0 {
				test.Fatalf("backup reached configuration or collection: error %v, output %q, diagnostics %q, calls %d", err, output, diagnostics, inventory.calls)
			}
			assertNoSelectionState(test, dependencies)
		})
	}
}

func TestPlanUsesLiteralRepeatedRelativeSelection(test *testing.T) {
	dependencies, inventory := selectionFixture(test, "zeta, with spaces", "alpha worktree", "unselected")
	value, _, _, err := runPlan(test, dependencies, "--worktree", "./zeta, with spaces", "--worktree", inventory.worktrees[1].Path)
	if err != nil {
		test.Fatal(err)
	}
	assertSelectionPreview(test, value, []string{inventory.worktrees[1].Path, inventory.worktrees[0].Path}, false)
	if value.Summary.Safe != 3 || len(value.Candidates) != 3 {
		test.Fatalf("ordinary inventory classification changed: %#v", value)
	}
	for _, candidate := range value.Candidates {
		want := domain.CandidateSelection{Selected: candidate.Worktree.Path != inventory.worktrees[2].Path}
		if !reflect.DeepEqual(candidate.Selection, &want) {
			test.Fatalf("selection for %q = %#v, want %#v", candidate.Worktree.Path, candidate.Selection, want)
		}
	}
}

func TestPlanInventoryDefaultsAndExplicitFalseOptions(test *testing.T) {
	for _, arguments := range [][]string{nil, {"--skip-dirty=false", "--backup=false"}} {
		test.Run(strings.Join(arguments, "_"), func(test *testing.T) {
			dependencies, _ := selectionFixture(test, "feature")
			value, _, _, err := runPlan(test, dependencies, arguments...)
			if err != nil {
				test.Fatal(err)
			}
			assertSelectionPreview(test, value, []string{}, false)
			if !reflect.DeepEqual(value.Candidates[0].Selection, &domain.CandidateSelection{}) {
				test.Fatalf("inventory implicitly selected a target: %#v", value.Candidates[0])
			}
		})
	}
}

func TestPlanSkipDirtyRequiresTargetsBeforeCollection(test *testing.T) {
	for _, argument := range []string{"--skip-dirty", "--skip-dirty=true"} {
		test.Run(argument, func(test *testing.T) {
			dependencies, inventory := planFixture(test)
			collector := &selectionProcessCounter{}
			dependencies.Processes = collector
			_, output, _, err := runPlan(test, dependencies, argument)
			if err == nil || !strings.Contains(err.Error(), "--skip-dirty requires --worktree") || len(output) != 0 || inventory.calls != 0 || collector.calls != 0 {
				test.Fatalf("skip-dirty without targets: error %v, output %q, inventory calls %d, process calls %d", err, output, inventory.calls, collector.calls)
			}
			assertNoSelectionState(test, dependencies)
		})
	}
}

func TestPlanSelectedDirtyChoicesRemainConservativePreviews(test *testing.T) {
	for _, status := range []struct {
		name    string
		value   domain.GitStatus
		unknown bool
	}{
		{"staged", domain.GitStatus{Staged: 1}, false},
		{"unstaged", domain.GitStatus{Unstaged: 1}, false},
		{"unmerged", domain.GitStatus{Unmerged: 1}, false},
		{"untracked", domain.GitStatus{Untracked: 1}, false},
		{"ignored-only", domain.GitStatus{}, false},
		{"unknown", domain.GitStatus{Unstaged: 1}, true},
	} {
		for _, option := range []struct {
			name      string
			arguments []string
			skipDirty bool
		}{
			{"default", nil, false},
			{"explicit-false", []string{"--skip-dirty=false", "--backup=false"}, false},
			{"skip", []string{"--skip-dirty"}, true},
		} {
			test.Run(status.name+"/"+option.name, func(test *testing.T) {
				dependencies, inventory := selectionFixture(test, "selected", "unselected")
				inventory.worktrees[0].Status = status.value
				inventory.worktrees[0].GitStateKnown = !status.unknown
				if status.name == "ignored-only" {
					if err := os.WriteFile(filepath.Join(inventory.worktrees[0].Path, ".env"), []byte("synthetic ignored content"), 0o600); err != nil {
						test.Fatal(err)
					}
				}
				arguments := append([]string{"--worktree", "selected"}, option.arguments...)
				value, _, _, err := runPlan(test, dependencies, arguments...)
				if status.unknown {
					var incomplete *incompletePlanError
					if !errors.As(err, &incomplete) || incomplete.planID != value.ID {
						test.Fatalf("unknown evidence did not produce an incomplete preview: %v", err)
					}
				} else if err != nil {
					test.Fatal(err)
				}
				assertSelectionPreview(test, value, []string{inventory.worktrees[0].Path}, option.skipDirty)
				if len(value.Candidates) != len(inventory.worktrees) {
					test.Fatalf("preview omitted selected or unselected candidates: %#v", value.Candidates)
				}
				for _, candidate := range value.Candidates {
					want := domain.CandidateSelection{Selected: candidate.Worktree.Path == inventory.worktrees[0].Path}
					if want.Selected && option.skipDirty && !status.unknown && !status.value.Clean() {
						want.SkipReason = "dirty"
					}
					if !reflect.DeepEqual(candidate.Selection, &want) {
						test.Fatalf("selection = %#v, want %#v", candidate.Selection, want)
					}
					classification := domain.Safe
					if want.Selected && (!status.value.Clean() || status.unknown) {
						classification = domain.Protected
					}
					if candidate.Decision.Classification != classification {
						test.Fatalf("selection changed ordinary classification: %#v", candidate.Decision)
					}
				}
				if status.name == "ignored-only" {
					contents, err := os.ReadFile(filepath.Join(inventory.worktrees[0].Path, ".env"))
					if err != nil || string(contents) != "synthetic ignored content" {
						test.Fatalf("preview changed ignored sentinel: %q, %v", contents, err)
					}
				}
			})
		}
	}
}

func TestPlanRejectsInvalidSelectionPathsBeforeCollection(test *testing.T) {
	for _, target := range []string{"", "bad\x00path", "bad-\xff", "missing", "missing-\n\r\t\x1b\u202e", "feat*", "feature,unselected"} {
		test.Run(strconv.Quote(target), func(test *testing.T) {
			dependencies, inventory := selectionFixture(test, "feature", "unselected")
			collector := &selectionProcessCounter{}
			dependencies.Processes = collector
			_, output, _, err := runPlan(test, dependencies, "--worktree", "feature", "--worktree", target, "--output", "report.json")
			if err == nil || !strings.Contains(err.Error(), strconv.Quote(target)) || len(output) != 0 || inventory.calls != 0 || collector.calls != 0 {
				test.Fatalf("invalid selection: error %v, output %q, inventory calls %d, process calls %d", err, output, inventory.calls, collector.calls)
			}
			assertPrintableSelectionText(test, err.Error())
			assertNoSelectionState(test, dependencies)
			if _, err := os.Stat(filepath.Join(dependencies.WorkingDirectory, "report.json")); !errors.Is(err, fs.ErrNotExist) {
				test.Fatalf("invalid selection created an export: %v", err)
			}
		})
	}
}

func TestPlanRejectsDuplicatePrimaryAndUnmatchedSelection(test *testing.T) {
	for _, scenario := range []struct {
		name    string
		targets []string
	}{
		{"duplicate", []string{"selected", "selected"}},
		{"duplicate-alias", []string{"selected", "./selected/."}},
		{"primary", []string{"."}},
		{"unmatched", []string{"outside-inventory"}},
		{"descendant", []string{"selected/child"}},
		{"overlap", []string{"selected", "selected/child"}},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			dependencies, inventory := selectionFixture(test, "selected", "unselected")
			for _, path := range []string{"outside-inventory", "selected/child"} {
				if err := os.MkdirAll(filepath.Join(dependencies.WorkingDirectory, path), 0o700); err != nil {
					test.Fatal(err)
				}
			}
			if scenario.name == "primary" {
				inventory.worktrees[0].Path = dependencies.WorkingDirectory
				inventory.worktrees[0].Primary = true
			}
			if scenario.name == "overlap" {
				inventory.worktrees[1].Path = filepath.Join(inventory.worktrees[0].Path, "child")
			}
			var arguments []string
			for _, target := range scenario.targets {
				arguments = append(arguments, "--worktree", target)
			}
			_, output, _, err := runPlan(test, dependencies, arguments...)
			if !errors.Is(err, plan.ErrPlanInvalid) || len(output) != 0 {
				test.Fatalf("non-exact selection accepted: output %q, error %v", output, err)
			}
			assertNoSelectionState(test, dependencies)
		})
	}
}

func TestPlanAndExplainHumanSelectionPreview(test *testing.T) {
	dependencies, inventory := selectionFixture(test, "selected", "skipped", "unselected")
	inventory.worktrees[1].Status.Untracked = 1
	var stdout, stderr bytes.Buffer
	dependencies.Stdout, dependencies.Stderr = &stdout, &stderr
	command := NewRootCommand(dependencies)
	command.SetArgs([]string{"plan", "--worktree", "selected", "--worktree", "skipped", "--skip-dirty"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		test.Fatal(err)
	}
	assertSelectionPreviewNotice(test, stdout.String())
	store := plan.NewStore(dependencies.DataDirectory, dependencies.Now, nil)
	value, err := store.Latest(context.Background())
	if err != nil {
		test.Fatal(err)
	}
	states := map[string]string{
		inventory.worktrees[0].Path: "selected",
		inventory.worktrees[1].Path: "skipped (dirty)",
		inventory.worktrees[2].Path: "unselected",
	}
	for _, candidate := range value.Candidates {
		state := states[candidate.Worktree.Path]
		found := false
		for _, line := range strings.Split(stdout.String(), "\n") {
			if strings.Contains(line, strconv.Quote(candidate.Worktree.Path)) {
				found = strings.Contains(line, strconv.Quote(state)) && strings.Contains(line, strconv.Quote("none"))
			}
		}
		if !found {
			test.Fatalf("plan did not distinguish %q for %q: %s", state, candidate.Worktree.Path, stdout.String())
		}
		output, _, err := runExplain(dependencies, candidate.ID, "--plan", value.ID)
		if err != nil {
			test.Fatal(err)
		}
		assertSelectionPreviewNotice(test, string(output))
		if !bytes.Contains(output, []byte("Selection: "+strconv.Quote(state))) {
			test.Fatalf("explanation lost selection %q: %s", state, output)
		}
	}
	if inventory.calls != 1 {
		test.Fatalf("explain recollected selection: calls %d", inventory.calls)
	}
}

func TestPlanHumanQuotesEveryRecordedField(test *testing.T) {
	text := "synthetic\t\r\n\x1b\u202e\u0085"
	value := domain.Plan{
		SchemaVersion: 2, ID: text,
		Removal: &domain.RemovalPlan{Intent: "inventory-preview", ContentDisposition: "discard-all", BackupMode: "none", Execution: "preview-only", SelectedPaths: []string{}},
		Candidates: []domain.Candidate{{
			ID: text, Worktree: domain.Worktree{Path: text, Branch: text}, Action: text,
			Decision:  domain.Decision{Classification: domain.Classification(text), Reasons: []domain.Reason{{Code: text}}},
			Selection: &domain.CandidateSelection{Selected: true, SkipReason: text},
		}},
	}
	var output bytes.Buffer
	if err := renderPlan(&output, "human", text, value, nil); err != nil {
		test.Fatal(err)
	}
	assertPrintableSelectionText(test, strings.ReplaceAll(output.String(), "\n", ""))
	for _, escaped := range []string{`\t`, `\r`, `\n`, `\x1b`, `\u202e`, `\u0085`} {
		if !strings.Contains(output.String(), escaped) {
			test.Errorf("human output lost escaped %q: %q", escaped, output.String())
		}
	}
}

func TestExplainPreviewJSONIncludesDisposalAndSelection(test *testing.T) {
	for _, arguments := range [][]string{nil, {"--worktree", "selected, with spaces", "--skip-dirty=false", "--backup=false"}} {
		test.Run(strings.Join(arguments, "_"), func(test *testing.T) {
			dependencies, _ := selectionFixture(test, "selected, with spaces")
			value, _, _, err := runPlan(test, dependencies, arguments...)
			if err != nil {
				test.Fatal(err)
			}
			output, _, err := runExplain(dependencies, value.Candidates[0].ID, "--plan", value.ID, "--format", "json")
			if err != nil {
				test.Fatal(err)
			}
			explanation := decodePreviewExplanation(test, output)
			if explanation.PlanID != value.ID || !reflect.DeepEqual(explanation.Removal, value.Removal) || !reflect.DeepEqual(explanation.Candidate, value.Candidates[0]) {
				test.Fatalf("explanation changed authenticated preview: %#v", explanation)
			}
		})
	}
}

func TestExplicitPreviewDoesNotExposeMutationOrConfirmation(test *testing.T) {
	for _, arguments := range [][]string{
		{"plan", "--force"}, {"plan", "--yes"}, {"plan", "--apply"},
		{"apply"}, {"apply", "--yes"},
	} {
		test.Run(strings.Join(arguments, "_"), func(test *testing.T) {
			dependencies, inventory := planFixture(test)
			collector := &selectionProcessCounter{}
			dependencies.Processes = collector
			var stdout, stderr bytes.Buffer
			dependencies.Stdout, dependencies.Stderr = &stdout, &stderr
			command := NewRootCommand(dependencies)
			command.SetArgs(arguments)
			if err := command.ExecuteContext(context.Background()); err == nil || stdout.Len() != 0 || inventory.calls != 0 || collector.calls != 0 {
				test.Fatalf("unsupported request was not refused without side effects: error %v, output %q, inventory calls %d, process calls %d", err, stdout.String(), inventory.calls, collector.calls)
			}
			assertNoSelectionState(test, dependencies)
		})
	}
}

func selectionFixture(test *testing.T, names ...string) (Dependencies, *inventoryStub) {
	test.Helper()
	dependencies, inventory := planFixture(test)
	prototype := inventory.worktrees[0]
	inventory.worktrees = nil
	for index, name := range names {
		worktree := prototype
		path := filepath.Join(dependencies.WorkingDirectory, name)
		if err := os.MkdirAll(path, 0o700); err != nil {
			test.Fatal(err)
		}
		canonical, err := pathutil.Canonical(path)
		if err != nil {
			test.Fatal(err)
		}
		worktree.Path = canonical
		worktree.Branch = fmt.Sprintf("topic-%d", index)
		worktree.AdminDir = filepath.Join(worktree.CommonGitDir, "worktrees", fmt.Sprintf("fixture-%d", index))
		inventory.worktrees = append(inventory.worktrees, worktree)
	}
	return dependencies, inventory
}

func assertSelectionPreview(test *testing.T, value domain.Plan, selectedPaths []string, skipDirty bool) {
	test.Helper()
	intent := "inventory-preview"
	if len(selectedPaths) != 0 {
		intent = "explicit-worktree-removal"
	}
	want := domain.RemovalPlan{
		Intent: intent, ContentDisposition: "discard-all", BackupMode: "none", SkipDirty: skipDirty,
		SelectedPaths: selectedPaths, Execution: "preview-only",
	}
	if value.SchemaVersion != 2 || value.Summary.ReclaimableBytes != 0 || !reflect.DeepEqual(value.Removal, &want) {
		test.Fatalf("preview contract = %#v, removal = %#v, want %#v", value, value.Removal, want)
	}
	for _, candidate := range value.Candidates {
		if candidate.Action != "none" || candidate.Snapshot != (domain.SnapshotPlan{}) || candidate.Selection == nil {
			test.Fatalf("preview authorizes mutation or loses selection: %#v", candidate)
		}
	}
}

func assertNoSelectionState(test *testing.T, dependencies Dependencies) {
	test.Helper()
	if _, err := os.Stat(dependencies.DataDirectory); !errors.Is(err, fs.ErrNotExist) {
		test.Fatalf("refused selection created state: %v", err)
	}
}

func assertSelectionPreviewNotice(test *testing.T, output string) {
	test.Helper()
	for _, want := range []string{
		"preview-only", "permanently non-executable", "ordinary conservative classification", "not explicit-removal eligibility",
		"discard-all", "including ignored files", "no backup", "local branches are preserved", "apply is unavailable",
	} {
		if !strings.Contains(strings.ToLower(output), want) {
			test.Errorf("preview notice lacks %q: %s", want, output)
		}
	}
}

func assertPrintableSelectionText(test *testing.T, text string) {
	test.Helper()
	if !utf8.ValidString(text) {
		test.Fatalf("selection text contains invalid UTF-8: %q", text)
	}
	for _, character := range text {
		if !unicode.IsPrint(character) {
			test.Fatalf("selection text contains unescaped control %U: %q", character, text)
		}
	}
}

type previewExplanation struct {
	SchemaVersion int                 `json:"schemaVersion"`
	PlanID        string              `json:"planId"`
	Removal       *domain.RemovalPlan `json:"removal"`
	Candidate     domain.Candidate    `json:"candidate"`
}

func decodePreviewExplanation(test *testing.T, output []byte) previewExplanation {
	test.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(output, &fields); err != nil || len(fields) != 4 {
		test.Fatalf("invalid preview explanation envelope: %q, %v", output, err)
	}
	for _, field := range []string{"schemaVersion", "planId", "removal", "candidate"} {
		if _, present := fields[field]; !present {
			test.Fatalf("preview explanation lacks %q: %s", field, output)
		}
	}
	var explanation previewExplanation
	if err := json.Unmarshal(output, &explanation); err != nil || explanation.SchemaVersion != 2 || explanation.PlanID == "" || explanation.Removal == nil || explanation.Candidate.ID == "" {
		test.Fatalf("invalid preview explanation: %#v, %v", explanation, err)
	}
	return explanation
}

type selectionProcessCounter struct {
	calls int
}

func (collector *selectionProcessCounter) Collect(context.Context, []domain.Worktree) (process.Collection, []error) {
	collector.calls++
	return process.Collection{Complete: true}, nil
}
