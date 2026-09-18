package plan

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/fssecure"
	"github.com/hellices/treeclear/internal/testutil"
)

func previewPlanFixture(test *testing.T) (domain.Plan, *testutil.Clock) {
	test.Helper()
	builder, request, worktree, clock := builderFixture(test)
	request.SelectedPaths = []string{worktree.Path}
	value, err := builder.Build(context.Background(), request)
	if err != nil {
		test.Fatal(err)
	}
	return value, clock
}

func TestPreviewStoreRoundTripAndLegacyInspection(test *testing.T) {
	preview, clock := previewPlanFixture(test)
	legacy := storedPlanFixture()
	legacy.GeneratedAt, legacy.ExpiresAt = preview.GeneratedAt, preview.ExpiresAt
	store := NewStore(filepath.Join(test.TempDir(), "state"), clock.Now, bytes.Repeat([]byte{0x42}, 32))
	for _, value := range []domain.Plan{preview, legacy} {
		path, err := store.Save(context.Background(), value)
		if err != nil {
			test.Fatalf("save schema %d: %v", value.SchemaVersion, err)
		}
		loaded, err := store.Load(context.Background(), path)
		if err != nil {
			test.Fatal(err)
		}
		loaded.Integrity = domain.PlanIntegrity{}
		before, _ := canonicalPlanJSON(value)
		after, _ := canonicalPlanJSON(loaded)
		if !bytes.Equal(before, after) {
			test.Fatalf("schema %d changed meaning on round trip", value.SchemaVersion)
		}
		if value.SchemaVersion == 1 && (!loaded.Candidates[0].Snapshot.Required || loaded.Removal != nil || loaded.Candidates[0].Selection != nil) {
			test.Fatal("legacy snapshot intent was reinterpreted")
		}
	}
}

func TestPreviewStoreRejectsInconsistentRecordsBeforeSaveAndAfterAuthentication(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*domain.Plan)
	}{
		{"missing-removal", func(value *domain.Plan) { value.Removal = nil }},
		{"missing-intent", func(value *domain.Plan) { value.Removal.Intent = "" }},
		{"unknown-intent", func(value *domain.Plan) { value.Removal.Intent = "automatic" }},
		{"intent-mismatch", func(value *domain.Plan) { value.Removal.Intent = domain.RemovalIntentInventory }},
		{"missing-disposition", func(value *domain.Plan) { value.Removal.ContentDisposition = "" }},
		{"unknown-disposition", func(value *domain.Plan) { value.Removal.ContentDisposition = "discard-tracked" }},
		{"missing-backup-mode", func(value *domain.Plan) { value.Removal.BackupMode = "" }},
		{"requested-backup", func(value *domain.Plan) { value.Removal.BackupMode = "required" }},
		{"missing-execution", func(value *domain.Plan) { value.Removal.Execution = "" }},
		{"execution-upgrade", func(value *domain.Plan) { value.Removal.Execution = "executable" }},
		{"missing-selection-paths", func(value *domain.Plan) { value.Removal.SelectedPaths = nil }},
		{"unknown-selection-path", func(value *domain.Plan) { value.Removal.SelectedPaths[0] += "-other" }},
		{"duplicate-selection-path", func(value *domain.Plan) {
			value.Removal.SelectedPaths = append(value.Removal.SelectedPaths, value.Removal.SelectedPaths[0])
		}},
		{"scheduled-selection", func(value *domain.Plan) { value.IntendedApplyMode = domain.ApplyScheduled }},
		{"missing-candidate-selection", func(value *domain.Plan) { value.Candidates[0].Selection = nil }},
		{"selection-mismatch", func(value *domain.Plan) { value.Candidates[0].Selection.Selected = false }},
		{"invalid-skip", func(value *domain.Plan) { value.Candidates[0].Selection.SkipReason = "dirty" }},
		{"unknown-skip", func(value *domain.Plan) { value.Candidates[0].Selection.SkipReason = "consent" }},
		{"action-upgrade", func(value *domain.Plan) { value.Candidates[0].Action = "remove" }},
		{"backup-upgrade", func(value *domain.Plan) { value.Candidates[0].Snapshot.Required = true }},
		{"leftover-snapshot-budget", func(value *domain.Plan) { value.Candidates[0].Snapshot.MaximumBytes = 1 }},
		{"missing-fingerprint", func(value *domain.Plan) { value.Candidates[0].Fingerprint = "" }},
		{"stale-fingerprint", func(value *domain.Plan) { value.Candidates[0].Worktree.Head += "-changed" }},
		{"missing-candidate-id", func(value *domain.Plan) { value.Candidates[0].ID = "" }},
		{"duplicate-candidate", func(value *domain.Plan) { value.Candidates = append(value.Candidates, value.Candidates[0]) }},
		{"misleading-reclaimable-bytes", func(value *domain.Plan) { value.Summary.ReclaimableBytes = 1 }},
		{"conflicting-summary", func(value *domain.Plan) { value.Summary.Safe++ }},
		{"missing-generation-time", func(value *domain.Plan) {
			value.GeneratedAt = time.Time{}
		}},
		{"legacy-with-new-policy", func(value *domain.Plan) { value.SchemaVersion = 1 }},
		{"legacy-with-selection-only", func(value *domain.Plan) {
			value.SchemaVersion = 1
			value.Removal = nil
		}},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			value, clock := previewPlanFixture(test)
			scenario.change(&value)
			if scenario.name != "missing-fingerprint" && scenario.name != "stale-fingerprint" {
				for index := range value.Candidates {
					fingerprint, err := CandidateFingerprint(value.Candidates[index])
					if err != nil {
						test.Fatal(err)
					}
					value.Candidates[index].Fingerprint = fingerprint
				}
			}
			root := filepath.Join(test.TempDir(), "state")
			key := bytes.Repeat([]byte{0x42}, 32)
			store := NewStore(root, clock.Now, key)
			if path, err := store.Save(context.Background(), value); path != "" || !errors.Is(err, ErrPlanInvalid) {
				test.Fatalf("invalid preview Save() = %q, %v", path, err)
			}
			if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
				test.Fatalf("invalid preview created state: %v", err)
			}
			contents, err := encodeSignedPlan(value, key)
			if err != nil {
				test.Fatal(err)
			}
			path := filepath.Join(root, "plans", value.ID+".json")
			if err := fssecure.WritePrivateFile(path, contents); err != nil {
				test.Fatal(err)
			}
			loaded, err := store.Load(context.Background(), path)
			if !errors.Is(err, ErrPlanInvalid) || !reflect.DeepEqual(loaded, domain.Plan{}) {
				test.Fatalf("authenticated invalid preview accepted: %v", err)
			}
		})
	}
}

func TestPreviewIntegrityBindsEveryNewField(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*domain.Plan)
	}{
		{"intent", func(value *domain.Plan) { value.Removal.Intent = domain.RemovalIntentInventory }},
		{"disposition", func(value *domain.Plan) { value.Removal.ContentDisposition = "other" }},
		{"backup", func(value *domain.Plan) { value.Removal.BackupMode = "required" }},
		{"skip-dirty", func(value *domain.Plan) { value.Removal.SkipDirty = true }},
		{"selected-paths", func(value *domain.Plan) { value.Removal.SelectedPaths[0] += "-other" }},
		{"execution", func(value *domain.Plan) { value.Removal.Execution = "executable" }},
		{"selected", func(value *domain.Plan) { value.Candidates[0].Selection.Selected = false }},
		{"skip-reason", func(value *domain.Plan) { value.Candidates[0].Selection.SkipReason = "dirty" }},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			value, _ := previewPlanFixture(test)
			key := bytes.Repeat([]byte{0x42}, 32)
			contents, err := encodeSignedPlan(value, key)
			if err != nil {
				test.Fatal(err)
			}
			if err := json.Unmarshal(contents, &value); err != nil {
				test.Fatal(err)
			}
			scenario.change(&value)
			changed, err := canonicalPlanJSON(value)
			if err != nil {
				test.Fatal(err)
			}
			if _, err := decodeAuthenticatedPlan(changed, key); !errors.Is(err, ErrPlanIntegrity) {
				test.Fatalf("unsigned change accepted: %v", err)
			}
		})
	}
}

func TestPreviewIntegrityRejectsAuthenticatedMissingDefaultFields(test *testing.T) {
	for _, missing := range []string{`"skipDirty":false,`, `"selected":true,`, `,"skipReason":""`} {
		test.Run(missing, func(test *testing.T) {
			value, _ := previewPlanFixture(test)
			key := bytes.Repeat([]byte{0x42}, 32)
			value.Integrity = domain.PlanIntegrity{Algorithm: integrityAlgorithm, KeyID: integrityKeyID(key)}
			payload, err := canonicalPlanJSON(value)
			if err != nil {
				test.Fatal(err)
			}
			changed := strings.Replace(string(payload), missing, "", 1)
			if changed == string(payload) {
				test.Fatalf("fixture lacks expected field %s", missing)
			}
			authenticator := hmac.New(sha256.New, key)
			_, _ = authenticator.Write([]byte(changed))
			contents := strings.Replace(changed, `"mac":""`, `"mac":"`+hex.EncodeToString(authenticator.Sum(nil))+`"`, 1)
			if _, err := decodeAuthenticatedPlan([]byte(contents), key); !errors.Is(err, ErrPlanIntegrity) {
				test.Fatalf("missing field defaulted to permission: %v", err)
			}
		})
	}
}

func TestPreviewCandidateFingerprintBindsSelection(test *testing.T) {
	candidate := candidateFixture()
	legacy, err := CandidateFingerprint(candidate)
	if err != nil {
		test.Fatal(err)
	}
	seen := map[string]bool{legacy: true}
	for _, selection := range []domain.CandidateSelection{{}, {Selected: true}, {Selected: true, SkipReason: "dirty"}} {
		candidate.Selection = &selection
		fingerprint, err := CandidateFingerprint(candidate)
		if err != nil || seen[fingerprint] {
			test.Fatalf("selection %#v did not change fingerprint: %v", selection, err)
		}
		seen[fingerprint] = true
	}
}
