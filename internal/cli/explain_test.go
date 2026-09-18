package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/plan"
	"github.com/hellices/treeclear/internal/process"
)

func TestExplainLegacyV1RemainsCandidateOnly(test *testing.T) {
	dependencies, value := legacyExplainFixture(test)
	output, _, err := runExplain(dependencies, value.Candidates[0].ID, "--plan", value.ID, "--format", "json")
	if err != nil {
		test.Fatal(err)
	}
	expected, err := json.Marshal(value.Candidates[0])
	if err != nil || !bytes.Equal(bytes.TrimSuffix(output, []byte("\n")), expected) {
		test.Fatalf("legacy candidate-only JSON changed: %q, %v", output, err)
	}
	output, _, err = runExplain(dependencies, value.Candidates[0].ID, "--plan", value.ID)
	if err != nil {
		test.Fatal(err)
	}
	snapshot, err := json.MarshalIndent(value.Candidates[0].Snapshot, "", "  ")
	if err != nil || !bytes.Contains(output, snapshot) || !bytes.Contains(output, []byte(`Action: "remove"`)) || bytes.Contains(output, []byte("discard-all")) {
		test.Fatalf("legacy snapshot intent was reinterpreted: %q, %v", output, err)
	}
	if !bytes.Contains(output, []byte("inspection only")) || !bytes.Contains(output, []byte("apply is unavailable")) {
		test.Fatalf("legacy recorded actions appear executable: %q", output)
	}
}

func legacyExplainFixture(test *testing.T) (Dependencies, domain.Plan) {
	test.Helper()
	dependencies, _ := planFixture(test)
	value, _, _, err := runPlan(test, dependencies)
	if err != nil {
		test.Fatal(err)
	}
	value.ID = "plan_legacy"
	value.SchemaVersion = 1
	value.Removal = nil
	value.Summary.ReclaimableBytes = value.Candidates[0].Worktree.EstimatedBytes
	for index := range value.Candidates {
		candidate := &value.Candidates[index]
		candidate.Selection = nil
		candidate.Action = "remove"
		candidate.Snapshot = domain.SnapshotPlan{Required: true, MaximumBytes: 1024}
		candidate.Fingerprint, err = plan.CandidateFingerprint(*candidate)
		if err != nil {
			test.Fatal(err)
		}
	}
	store := plan.NewStore(dependencies.DataDirectory, dependencies.Now, nil)
	if _, err := store.Save(context.Background(), value); err != nil {
		test.Fatal(err)
	}
	value, err = store.Load(context.Background(), value.ID)
	if err != nil {
		test.Fatal(err)
	}
	return dependencies, value
}

func TestExplainReportsRecordedPlanWarnings(test *testing.T) {
	dependencies, _ := planFixture(test)
	value, _, _, err := runPlan(test, dependencies)
	if err != nil {
		test.Fatal(err)
	}
	_, diagnostics, err := runExplain(dependencies, value.Candidates[0].ID, "--format", "json")
	if err != nil || !strings.Contains(diagnostics, "Core-only") {
		test.Fatalf("explain lost plan warning: %q, %v", diagnostics, err)
	}
}

func TestExplainHumanIncludesCompleteEvidenceAndInactivity(test *testing.T) {
	dependencies, inventory := planFixture(test)
	dependencies.Processes = processStub{collection: process.Collection{
		Complete: true,
		ByWorktree: map[string][]domain.ProcessEvidence{inventory.worktrees[0].Path: {{
			PID: 42, CreatedAt: dependencies.Now().Add(-30 * 24 * time.Hour), Executable: filepath.Join(dependencies.WorkingDirectory, "synthetic-tool"),
			CWD: inventory.worktrees[0].Path, State: domain.EvidenceInactive, Fingerprint: "synthetic-process-fingerprint",
		}}},
	}}
	value, _, _, err := runPlan(test, dependencies)
	if err != nil {
		test.Fatal(err)
	}
	candidate := value.Candidates[0]
	output, _, err := runExplain(dependencies, candidate.ID)
	if err != nil {
		test.Fatal(err)
	}
	for _, record := range []any{candidate.Worktree, candidate.Evidence, candidate.Snapshot} {
		encoded, err := json.MarshalIndent(record, "", "  ")
		if err != nil || !bytes.Contains(output, encoded) {
			test.Fatalf("human explain lost a complete evidence record: %v", err)
		}
	}
	for _, reason := range candidate.Decision.Reasons {
		if !bytes.Contains(output, []byte(strconv.Quote(reason.Code))) || !bytes.Contains(output, []byte(strconv.Quote(reason.Message))) {
			test.Fatalf("human explain lost reason %#v", reason)
		}
	}
	if !bytes.Contains(output, []byte("Inactive for: "+candidate.Decision.InactiveFor.String())) {
		test.Fatalf("human explain lost inactivity: %s", output)
	}
}

func TestExplainHumanEscapesUnicodeControlsWithoutChangingJSON(test *testing.T) {
	text := "readable 한글 café 😀\u0085\u009b\u00ad\u202e\u2066\u2069\U000e0001"
	candidate := domain.Candidate{
		ID: "candidate_test", Worktree: domain.Worktree{Path: text, Branch: text},
		Evidence: domain.EvidenceSet{
			Processes: []domain.ProcessEvidence{{PID: 42, Executable: text, Error: text}},
			Agents:    []domain.AgentEvidence{{AdapterID: "synthetic", Warnings: []string{text}}},
			Warnings:  []string{text},
		},
		Decision: domain.Decision{Reasons: []domain.Reason{{Code: "synthetic", Message: text}}},
		Snapshot: domain.SnapshotPlan{Required: true},
	}
	var machine bytes.Buffer
	if err := renderExplanation(&machine, "json", "plan_test", candidate); err != nil {
		test.Fatal(err)
	}
	var decoded domain.Candidate
	if err := json.Unmarshal(machine.Bytes(), &decoded); err != nil || !reflect.DeepEqual(decoded, candidate) {
		test.Fatalf("machine output changed recorded data: %#v, %v", decoded, err)
	}
	var human bytes.Buffer
	if err := renderExplanation(&human, "human", "plan_test", candidate); err != nil {
		test.Fatal(err)
	}
	for _, character := range human.String() {
		if character != '\n' && !unicode.IsPrint(character) {
			test.Fatalf("human output contains terminal control %U: %q", character, human.String())
		}
	}
	for _, escaped := range []string{`\u0085`, `\u009b`, `\u00ad`, `\u202e`, `\u2066`, `\u2069`, `\udb40\udc01`, "readable 한글 café 😀"} {
		if !strings.Contains(human.String(), escaped) {
			test.Errorf("human output lacks %q", escaped)
		}
	}
	titles := []string{"Git", "Evidence", "Snapshot"}
	for index, record := range []any{candidate.Worktree, candidate.Evidence, candidate.Snapshot} {
		_, section, found := strings.Cut(human.String(), titles[index]+":\n")
		if !found {
			test.Fatalf("missing section %q", titles[index])
		}
		if index+1 < len(titles) {
			section, _, _ = strings.Cut(section, "\n"+titles[index+1]+":\n")
		}
		var actual, expected any
		if err := json.Unmarshal([]byte(section), &actual); err != nil {
			test.Fatalf("human section is not valid JSON: %v", err)
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			test.Fatal(err)
		}
		if err := json.Unmarshal(encoded, &expected); err != nil || !reflect.DeepEqual(actual, expected) {
			test.Fatalf("human section changed recorded data: %#v, %v", actual, err)
		}
	}
}

func TestExplainRejectsTamperedAndExpiredPlans(test *testing.T) {
	for _, scenario := range []string{"tampered", "expired"} {
		test.Run(scenario, func(test *testing.T) {
			dependencies, _ := planFixture(test)
			value, _, _, err := runPlan(test, dependencies)
			if err != nil {
				test.Fatal(err)
			}
			want := plan.ErrPlanExpired
			if scenario == "tampered" {
				path := filepath.Join(dependencies.DataDirectory, "plans", value.ID+".json")
				contents, err := os.ReadFile(path)
				if err != nil {
					test.Fatal(err)
				}
				changed := bytes.Replace(contents, []byte(`"action":"none"`), []byte(`"action":"remove"`), 1)
				if bytes.Equal(contents, changed) {
					test.Fatal("fixture did not contain the action being tampered")
				}
				if err := os.WriteFile(path, changed, 0o600); err != nil {
					test.Fatal(err)
				}
				want = plan.ErrPlanIntegrity
			} else {
				dependencies.Now = func() time.Time { return value.ExpiresAt }
			}
			output, _, err := runExplain(dependencies, value.Candidates[0].ID, "--plan", value.ID, "--format", "json")
			if !errors.Is(err, want) || len(output) != 0 {
				test.Fatalf("unsafe explain = %q, %v, want %v", output, err, want)
			}
		})
	}
}

func TestExplainNewestDoesNotFallBackForMissingCandidate(test *testing.T) {
	dependencies, inventory := planFixture(test)
	first, _, _, err := runPlan(test, dependencies)
	if err != nil {
		test.Fatal(err)
	}
	dependencies.Now = func() time.Time { return first.GeneratedAt.Add(time.Minute) }
	inventory.worktrees[0].Path += "-new-identity"
	latest, _, _, err := runPlan(test, dependencies)
	if err != nil || latest.Candidates[0].ID == first.Candidates[0].ID {
		test.Fatalf("second plan fixture: %v", err)
	}
	output, _, err := runExplain(dependencies, first.Candidates[0].ID, "--format", "json")
	if err == nil || len(output) != 0 || !strings.Contains(err.Error(), latest.ID) {
		test.Fatalf("explain fell back to older candidate: %q, %v", output, err)
	}
}

func TestExplainExportFilenameMatchingIDNeedsDisambiguation(test *testing.T) {
	dependencies, _ := planFixture(test)
	value, _, _, err := runPlan(test, dependencies, "--output", "plan_export")
	if err != nil {
		test.Fatal(err)
	}
	output, _, err := runExplain(dependencies, value.Candidates[0].ID, "--plan", "plan_export", "--format", "json")
	if err == nil || len(output) != 0 {
		test.Fatalf("plan ID unexpectedly resolved as a filename: %q, %v", output, err)
	}
	output, _, err = runExplain(dependencies, value.Candidates[0].ID, "--plan", "./plan_export", "--format", "json")
	if err != nil || len(output) == 0 {
		test.Fatalf("explicit relative path did not resolve: %q, %v", output, err)
	}
}

func TestExplainRejectsAmbiguousAuthenticatedCandidateIDs(test *testing.T) {
	dependencies, value := legacyExplainFixture(test)
	value.ID = "plan_ambiguous"
	value.Candidates = append(value.Candidates, value.Candidates[0])
	store := plan.NewStore(dependencies.DataDirectory, dependencies.Now, nil)
	if _, err := store.Save(context.Background(), value); err != nil {
		test.Fatal(err)
	}
	output, _, err := runExplain(dependencies, value.Candidates[0].ID, "--plan", value.ID, "--format", "json")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") || len(output) != 0 {
		test.Fatalf("ambiguous explain = %q, %v", output, err)
	}
}
