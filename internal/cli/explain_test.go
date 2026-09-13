package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/plan"
	"github.com/hellices/treeclear/internal/process"
)

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
				changed := bytes.Replace(contents, []byte(`"action":"remove"`), []byte(`"action":"none"`), 1)
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
	dependencies, _ := planFixture(test)
	value, _, _, err := runPlan(test, dependencies)
	if err != nil {
		test.Fatal(err)
	}
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
