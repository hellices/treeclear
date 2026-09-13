package plan_test

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/plan"
)

func TestPolicyDigestCanonicalJSON(test *testing.T) {
	cases := []struct {
		name          string
		settings      domain.PolicySettings
		canonicalJSON string
	}{
		{
			name:          "configured",
			settings:      policyDigestTestSettings(),
			canonicalJSON: `{"inactivityThreshold":604800000000000,"planExpiry":900000000000,"baseBranches":["develop","main","release/1"],"minimumTrustGrade":"supported-api","snapshotMaxBytes":1073741824}`,
		},
		{
			name:          "zero values",
			settings:      domain.PolicySettings{},
			canonicalJSON: `{"inactivityThreshold":0,"planExpiry":0,"baseBranches":[],"minimumTrustGrade":"","snapshotMaxBytes":0}`,
		},
		{
			name: "values outside policy business rules",
			settings: domain.PolicySettings{
				InactivityThreshold: -1,
				PlanExpiry:          -2,
				BaseBranches:        []string{"main ", "", " main"},
				MinimumTrustGrade:   " future-grade ",
				SnapshotMaxBytes:    -3,
			},
			canonicalJSON: `{"inactivityThreshold":-1,"planExpiry":-2,"baseBranches":[""," main","main "],"minimumTrustGrade":" future-grade ","snapshotMaxBytes":-3}`,
		},
		{
			name: "integer limits",
			settings: domain.PolicySettings{
				InactivityThreshold: -1 << 63,
				PlanExpiry:          1<<63 - 1,
				SnapshotMaxBytes:    1<<63 - 1,
			},
			canonicalJSON: `{"inactivityThreshold":-9223372036854775808,"planExpiry":9223372036854775807,"baseBranches":[],"minimumTrustGrade":"","snapshotMaxBytes":9223372036854775807}`,
		},
		{
			name: "valid UTF-8 and JSON escaping",
			settings: domain.PolicySettings{
				InactivityThreshold: 1,
				PlanExpiry:          2,
				BaseBranches:        []string{"é", "e\u0301", "<>&", "quote\"\\\n", "\ufffd"},
				MinimumTrustGrade:   "future\"\\\n\u2028\u2029\ufffd",
				SnapshotMaxBytes:    3,
			},
			canonicalJSON: `{"inactivityThreshold":1,"planExpiry":2,"baseBranches":["\u003c\u003e\u0026","é","quote\"\\\n","é","�"],"minimumTrustGrade":"future\"\\\n\u2028\u2029�","snapshotMaxBytes":3}`,
		},
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			expected := policyDigestTestHash(scenario.canonicalJSON)
			for attempt := range 3 {
				actual, err := plan.PolicyDigest(scenario.settings)
				if err != nil {
					test.Fatalf("PolicyDigest() attempt %d error = %v", attempt, err)
				}
				if actual != expected {
					test.Fatalf("PolicyDigest() attempt %d = %q, want %q for canonical JSON %s", attempt, actual, expected, scenario.canonicalJSON)
				}
			}
		})
	}
}

func TestPolicyDigestBranchPermutationsDoNotMutateCaller(test *testing.T) {
	permutations := [][]string{
		{"develop", "main", "release/1"},
		{"develop", "release/1", "main"},
		{"main", "develop", "release/1"},
		{"main", "release/1", "develop"},
		{"release/1", "develop", "main"},
		{"release/1", "main", "develop"},
	}
	expected := policyDigestTestHash(`{"inactivityThreshold":604800000000000,"planExpiry":900000000000,"baseBranches":["develop","main","release/1"],"minimumTrustGrade":"supported-api","snapshotMaxBytes":1073741824}`)
	for _, branches := range permutations {
		settings := policyDigestTestSettings()
		settings.BaseBranches = branches
		original := slices.Clone(branches)
		actual, err := plan.PolicyDigest(settings)
		if err != nil {
			test.Fatalf("PolicyDigest(%q) error = %v", original, err)
		}
		if actual != expected {
			test.Errorf("PolicyDigest(%q) = %q, want %q", original, actual, expected)
		}
		if !slices.Equal(branches, original) {
			test.Errorf("PolicyDigest() mutated caller branches to %q, want %q", branches, original)
		}
	}
}

func TestPolicyDigestChangesWithEverySetting(test *testing.T) {
	original, err := plan.PolicyDigest(policyDigestTestSettings())
	if err != nil {
		test.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*domain.PolicySettings)
	}{
		{"inactivity threshold", func(settings *domain.PolicySettings) { settings.InactivityThreshold++ }},
		{"plan expiry", func(settings *domain.PolicySettings) { settings.PlanExpiry++ }},
		{"base branches", func(settings *domain.PolicySettings) { settings.BaseBranches[0] = "release/2" }},
		{"minimum trust grade", func(settings *domain.PolicySettings) { settings.MinimumTrustGrade = "future-grade" }},
		{"snapshot maximum bytes", func(settings *domain.PolicySettings) { settings.SnapshotMaxBytes++ }},
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			settings := policyDigestTestSettings()
			scenario.change(&settings)
			actual, err := plan.PolicyDigest(settings)
			if err != nil {
				test.Fatalf("PolicyDigest() error = %v", err)
			}
			if actual == original {
				test.Fatalf("PolicyDigest() did not change when %s changed", scenario.name)
			}
		})
	}
}

func TestPolicyDigestNormalizesEmptyBranches(test *testing.T) {
	cases := []struct {
		name     string
		branches []string
	}{
		{"nil", nil},
		{"empty", []string{}},
		{"empty with capacity", make([]string, 0, 3)},
	}
	expected := policyDigestTestHash(`{"inactivityThreshold":604800000000000,"planExpiry":900000000000,"baseBranches":[],"minimumTrustGrade":"supported-api","snapshotMaxBytes":1073741824}`)
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			settings := policyDigestTestSettings()
			settings.BaseBranches = scenario.branches
			actual, err := plan.PolicyDigest(settings)
			if err != nil {
				test.Fatalf("PolicyDigest() error = %v", err)
			}
			if actual != expected {
				test.Fatalf("PolicyDigest() = %q, want %q", actual, expected)
			}
			if (settings.BaseBranches == nil) != (scenario.branches == nil) {
				test.Fatal("PolicyDigest() changed caller branch slice nilness")
			}
		})
	}
}

func TestPolicyDigestRetainsDuplicateBranches(test *testing.T) {
	unique, err := plan.PolicyDigest(policyDigestTestSettings())
	if err != nil {
		test.Fatal(err)
	}
	expected := policyDigestTestHash(`{"inactivityThreshold":604800000000000,"planExpiry":900000000000,"baseBranches":["develop","main","main","release/1"],"minimumTrustGrade":"supported-api","snapshotMaxBytes":1073741824}`)
	for _, branches := range [][]string{
		{"main", "release/1", "main", "develop"},
		{"main", "develop", "release/1", "main"},
	} {
		settings := policyDigestTestSettings()
		settings.BaseBranches = branches
		actual, err := plan.PolicyDigest(settings)
		if err != nil {
			test.Fatalf("PolicyDigest() error = %v", err)
		}
		if actual != expected {
			test.Errorf("PolicyDigest(%q) = %q, want %q", branches, actual, expected)
		}
		if actual == unique {
			test.Error("PolicyDigest() erased duplicate branch evidence")
		}
	}
}

func TestPolicyDigestRejectsInvalidUTF8(test *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"invalid leading byte", "\xff"},
		{"stray continuation byte", "\x80"},
		{"truncated sequence", "\xe2\x82"},
		{"overlong sequence", "\xc0\xaf"},
		{"surrogate", "\xed\xa0\x80"},
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			test.Run("base branches", func(test *testing.T) {
				for branchIndex := range policyDigestTestSettings().BaseBranches {
					settings := policyDigestTestSettings()
					settings.BaseBranches[branchIndex] += scenario.value
					original := slices.Clone(settings.BaseBranches)
					actual, err := plan.PolicyDigest(settings)
					if err == nil {
						test.Errorf("PolicyDigest() accepted invalid UTF-8 in branch %d", branchIndex)
					}
					if actual != "" {
						test.Errorf("PolicyDigest() returned digest %q for invalid UTF-8 in branch %d", actual, branchIndex)
					}
					if !slices.Equal(settings.BaseBranches, original) {
						test.Errorf("PolicyDigest() mutated caller branches on error to %q, want %q", settings.BaseBranches, original)
					}
				}
			})
			test.Run("minimum trust grade", func(test *testing.T) {
				settings := policyDigestTestSettings()
				settings.MinimumTrustGrade += domain.TrustGrade(scenario.value)
				actual, err := plan.PolicyDigest(settings)
				if err == nil {
					test.Error("PolicyDigest() accepted invalid UTF-8 in minimum trust grade")
				}
				if actual != "" {
					test.Errorf("PolicyDigest() returned digest %q for invalid UTF-8 in minimum trust grade", actual)
				}
			})
		})
	}
}

func policyDigestTestSettings() domain.PolicySettings {
	return domain.PolicySettings{
		InactivityThreshold: 7 * 24 * time.Hour,
		PlanExpiry:          15 * time.Minute,
		BaseBranches:        []string{"release/1", "main", "develop"},
		MinimumTrustGrade:   domain.TrustSupportedAPI,
		SnapshotMaxBytes:    1 << 30,
	}
}

func policyDigestTestHash(canonicalJSON string) string {
	digest := sha256.Sum256([]byte(canonicalJSON))
	return "sha256:" + hex.EncodeToString(digest[:])
}
