package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestDefaultSafetyValues(test *testing.T) {
	expected := Config{
		InactivityThreshold: 7 * 24 * time.Hour,
		PlanExpiry:          15 * time.Minute,
		BaseBranches:        []string{"main", "master"},
		MinimumTrustGrade:   "versioned-private",
		SnapshotMaxBytes:    64 << 20,
	}
	if actual := Default(); !reflect.DeepEqual(actual, expected) {
		test.Fatalf("Default() = %#v, want %#v", actual, expected)
	}
}

func TestDefaultDoesNotShareBaseBranches(test *testing.T) {
	first := Default()
	second := Default()
	first.BaseBranches[0] = "changed"
	if second.BaseBranches[0] != "main" || Default().BaseBranches[0] != "main" {
		test.Fatal("Default() shares mutable base branches")
	}
}

func TestLoadConfigPrecedence(test *testing.T) {
	userPath := writeConfig(test, `
roots = ["user-root"]
inactivity_threshold = "30d"
plan_expiry = "1h"
base_branches = ["user-base"]
minimum_trust_grade = "process-only"
snapshot_max_bytes = 8192
`)
	repositoryPath := writeConfig(test, `
roots = ["repository-root"]
inactivity_threshold = "14d"
plan_expiry = "30m"
base_branches = ["repository-base"]
minimum_trust_grade = "supported-cli"
snapshot_max_bytes = 4096
`)
	userConfig := Config{
		Roots:               []string{"user-root"},
		InactivityThreshold: 30 * 24 * time.Hour,
		PlanExpiry:          time.Hour,
		BaseBranches:        []string{"user-base"},
		MinimumTrustGrade:   "process-only",
		SnapshotMaxBytes:    8192,
	}
	repositoryConfig := Config{
		Roots:               []string{"repository-root"},
		InactivityThreshold: 14 * 24 * time.Hour,
		PlanExpiry:          30 * time.Minute,
		BaseBranches:        []string{"repository-base"},
		MinimumTrustGrade:   "supported-cli",
		SnapshotMaxBytes:    4096,
	}
	overrideConfig := Config{
		Roots:               []string{"override-root"},
		InactivityThreshold: 7 * 24 * time.Hour,
		PlanExpiry:          10 * time.Minute,
		BaseBranches:        []string{"override-base"},
		MinimumTrustGrade:   "supported-api",
		SnapshotMaxBytes:    2048,
	}
	testCases := []struct {
		name           string
		userPath       string
		repositoryPath string
		overrides      Overrides
		expected       Config
	}{
		{name: "defaults", expected: Default()},
		{name: "user", userPath: userPath, expected: userConfig},
		{name: "repository", userPath: userPath, repositoryPath: repositoryPath, expected: repositoryConfig},
		{
			name:           "overrides",
			userPath:       userPath,
			repositoryPath: repositoryPath,
			overrides: Overrides{
				Roots:               overrideConfig.Roots,
				InactivityThreshold: &overrideConfig.InactivityThreshold,
				PlanExpiry:          &overrideConfig.PlanExpiry,
				BaseBranches:        overrideConfig.BaseBranches,
				MinimumTrustGrade:   &overrideConfig.MinimumTrustGrade,
				SnapshotMaxBytes:    &overrideConfig.SnapshotMaxBytes,
			},
			expected: overrideConfig,
		},
		{
			name:           "partial layers preserve other settings",
			userPath:       userPath,
			repositoryPath: writeConfig(test, `plan_expiry = "5m"`),
			overrides:      Overrides{InactivityThreshold: durationPtr(10 * 24 * time.Hour)},
			expected: Config{
				Roots:               userConfig.Roots,
				InactivityThreshold: 10 * 24 * time.Hour,
				PlanExpiry:          5 * time.Minute,
				BaseBranches:        userConfig.BaseBranches,
				MinimumTrustGrade:   userConfig.MinimumTrustGrade,
				SnapshotMaxBytes:    userConfig.SnapshotMaxBytes,
			},
		},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			actual, err := Load(testCase.userPath, testCase.repositoryPath, testCase.overrides)
			if err != nil {
				test.Fatal(err)
			}
			if !reflect.DeepEqual(actual, testCase.expected) {
				test.Fatalf("Load() = %#v, want %#v", actual, testCase.expected)
			}
		})
	}
}

func TestLoadSkipsMissingOptionalFiles(test *testing.T) {
	missingPath := filepath.Join(test.TempDir(), "missing.toml")
	configuredPath := writeConfig(test, `inactivity_threshold = "14d"`)
	testCases := []struct {
		name           string
		userPath       string
		repositoryPath string
		threshold      time.Duration
	}{
		{"both missing", missingPath, missingPath, 7 * 24 * time.Hour},
		{"user missing", missingPath, configuredPath, 14 * 24 * time.Hour},
		{"repository missing", configuredPath, missingPath, 14 * 24 * time.Hour},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			actual, err := Load(testCase.userPath, testCase.repositoryPath, Overrides{})
			if err != nil {
				test.Fatal(err)
			}
			expected := Default()
			expected.InactivityThreshold = testCase.threshold
			if !reflect.DeepEqual(actual, expected) {
				test.Fatalf("Load() = %#v, want %#v", actual, expected)
			}
		})
	}
}

func TestLoadEmptyListsAndOverrides(test *testing.T) {
	userPath := writeConfig(test, "roots = [\"user-root\"]\nbase_branches = [\"user-base\"]")
	testCases := []struct {
		name      string
		content   string
		overrides Overrides
		roots     []string
		branches  []string
	}{
		{name: "omitted lists preserve values", roots: []string{"user-root"}, branches: []string{"user-base"}},
		{name: "file can clear lists", content: "roots = []\nbase_branches = []"},
		{
			name:      "empty root override is absent but base branches can clear",
			overrides: Overrides{Roots: []string{}, BaseBranches: []string{}},
			roots:     []string{"user-root"},
		},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			actual, err := Load(userPath, writeConfig(test, testCase.content), testCase.overrides)
			if err != nil {
				test.Fatal(err)
			}
			if !slices.Equal(actual.Roots, testCase.roots) || !slices.Equal(actual.BaseBranches, testCase.branches) {
				test.Fatalf("Load() lists = %v, %v; want %v, %v", actual.Roots, actual.BaseBranches, testCase.roots, testCase.branches)
			}
		})
	}
}

func TestLoadCopiesOverrides(test *testing.T) {
	overrides := Overrides{Roots: []string{"root"}, BaseBranches: []string{"base"}}
	actual, err := Load("", "", overrides)
	if err != nil {
		test.Fatal(err)
	}
	overrides.Roots[0] = "changed root"
	overrides.BaseBranches[0] = "changed base"
	if actual.Roots[0] != "root" || actual.BaseBranches[0] != "base" {
		test.Fatal("Load() shares mutable override slices")
	}
}

func TestLoadRejectsInvalidFiles(test *testing.T) {
	testCases := []struct {
		name    string
		content string
	}{
		{"malformed TOML", "roots = ["},
		{"wrong duration type", "inactivity_threshold = 7"},
		{"invalid duration", `plan_expiry = "later"`},
		{"zero inactivity", `inactivity_threshold = "0s"`},
		{"negative inactivity", `inactivity_threshold = "-1h"`},
		{"zero expiry", `plan_expiry = "0s"`},
		{"negative expiry", `plan_expiry = "-1h"`},
		{"zero snapshot limit", "snapshot_max_bytes = 0"},
		{"negative snapshot limit", "snapshot_max_bytes = -1"},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			path := writeConfig(test, testCase.content)
			for _, paths := range [][2]string{{path, ""}, {"", path}} {
				actual, err := Load(paths[0], paths[1], Overrides{})
				if err == nil || !reflect.DeepEqual(actual, Config{}) {
					test.Fatalf("Load(%q, %q) = %#v, %v; want zero config and error", paths[0], paths[1], actual, err)
				}
			}
		})
	}
}

func TestLoadRejectsInvalidOverrides(test *testing.T) {
	zeroBytes := int64(0)
	negativeBytes := int64(-1)
	testCases := []struct {
		name      string
		overrides Overrides
	}{
		{"zero inactivity", Overrides{InactivityThreshold: durationPtr(0)}},
		{"negative inactivity", Overrides{InactivityThreshold: durationPtr(-time.Hour)}},
		{"zero expiry", Overrides{PlanExpiry: durationPtr(0)}},
		{"negative expiry", Overrides{PlanExpiry: durationPtr(-time.Minute)}},
		{"zero snapshot limit", Overrides{SnapshotMaxBytes: &zeroBytes}},
		{"negative snapshot limit", Overrides{SnapshotMaxBytes: &negativeBytes}},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			actual, err := Load("", "", testCase.overrides)
			if err == nil || !reflect.DeepEqual(actual, Config{}) {
				test.Fatalf("Load() = %#v, %v; want zero config and error", actual, err)
			}
		})
	}
}

func TestLoadDoesNotIgnoreReadErrors(test *testing.T) {
	directory := test.TempDir()
	for _, paths := range [][2]string{{directory, ""}, {"", directory}} {
		actual, err := Load(paths[0], paths[1], Overrides{})
		if err == nil || !reflect.DeepEqual(actual, Config{}) {
			test.Fatalf("Load(%q, %q) = %#v, %v; want zero config and read error", paths[0], paths[1], actual, err)
		}
	}
}

func TestLoadRejectsWholeDayOverflow(test *testing.T) {
	for _, field := range []string{"inactivity_threshold", "plan_expiry"} {
		test.Run(field, func(test *testing.T) {
			path := writeConfig(test, field+` = "213504d"`)
			for _, paths := range [][2]string{{path, ""}, {"", path}} {
				actual, err := Load(paths[0], paths[1], Overrides{})
				if err == nil || !reflect.DeepEqual(actual, Config{}) {
					test.Fatalf("Load(%q, %q) = %#v, %v; want zero config and overflow error", paths[0], paths[1], actual, err)
				}
			}
		})
	}
}

func TestLoadRejectsUnknownSafetySettings(test *testing.T) {
	for _, contents := range []string{`inactivty_threshold = "30d"`, `minimum_trust_grade = "unrecognized"`} {
		if _, err := Load(writeConfig(test, contents), "", Overrides{}); err == nil {
			test.Fatalf("accepted unknown safety setting: %s", contents)
		}
	}
}

func durationPtr(value time.Duration) *time.Duration {
	return &value
}

func writeConfig(test *testing.T, content string) string {
	test.Helper()
	path := filepath.Join(test.TempDir(), "treeclear.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		test.Fatal(err)
	}
	return path
}
