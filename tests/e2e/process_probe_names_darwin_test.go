package e2e

import (
	"bytes"
	"encoding/json"
	"maps"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestInstalledProbeNamesRequireExplicitConsent(test *testing.T) {
	challenge := strings.Repeat("12", 32)
	partial := installedProbeReport{
		Version: 1, Challenge: challenge, Platform: "darwin", Architecture: runtime.GOARCH,
		RootCount: 2, RootsMatched: true, EnumerationComplete: true, ActiveMatched: true,
		UninspectableCount: 1, UninspectableRoles: map[string]int{"ci-worker": 1},
		UninspectableParentRoles: map[string]int{"ci-listener": 1},
	}
	contents, err := json.Marshal(partial)
	if err != nil {
		test.Fatal(err)
	}
	contents = append(contents[:len(contents)-1], []byte(`,"uninspectable_names":[{"process":"Runner.Worker","parent":"Runner.Listener"}]}`)...)
	if actual, err := decodeInstalledProbeReport(contents, challenge, 2, false); err == nil || !reflect.DeepEqual(actual, installedProbeReport{}) || strings.Contains(err.Error(), "Runner.") {
		test.Fatal("unapproved names were retained or disclosed")
	}
	actual, err := decodeInstalledProbeReport(contents, challenge, 2, true)
	if err == nil || actual.Version != 1 || actual.Complete {
		test.Fatal("approved names were lost or made incomplete evidence qualify")
	}
	retained, err := json.Marshal(actual)
	if err != nil || !bytes.Equal(retained, contents) {
		test.Fatal("approved report did not retain its bounded name pairs")
	}
}

func TestInstalledProbeValidatesNameDiagnostics(test *testing.T) {
	challenge := strings.Repeat("12", 32)
	partial := installedProbeReport{
		Version: 1, Challenge: challenge, Platform: "darwin", Architecture: runtime.GOARCH,
		RootCount: 2, RootsMatched: true, EnumerationComplete: true, ActiveMatched: true,
		UninspectableCount: 1, UninspectableRoles: map[string]int{"other": 1},
		UninspectableParentRoles: map[string]int{"other": 1},
	}
	decode := func(report installedProbeReport, names string) (installedProbeReport, error) {
		contents, err := json.Marshal(report)
		if err != nil {
			test.Fatal(err)
		}
		if names != "" {
			contents = append(contents[:len(contents)-1], []byte(`,"uninspectable_names":`+names+`}`)...)
		}
		return decodeInstalledProbeReport(contents, challenge, 2, true)
	}
	for _, names := range []string{"", `[{"process":"Tool_1.2-test"}]`, `[{"process":"abcdefghijklmnop","parent":"abcdefghijklmnop"}]`} {
		actual, err := decode(partial, names)
		if err == nil || actual.Version != 1 || actual.Complete {
			test.Fatal("valid optional names were lost or qualified incomplete collection")
		}
	}
	for name, names := range map[string]string{
		"null": "null", "empty": "[]", "object": `{}`, "scalar": "true",
		"empty-target":   `[{"process":""}]`,
		"missing-target": `[{"parent":"parent"}]`,
		"long-target":    `[{"process":"abcdefghijklmnopq"}]`,
		"long-parent":    `[{"process":"target","parent":"abcdefghijklmnopq"}]`,
		"target-path":    `[{"process":"/private/target"}]`,
		"parent-path":    `[{"process":"target","parent":"/private/parent"}]`,
		"space":          `[{"process":"private data"}]`,
		"newline":        `[{"process":"private\ndata"}]`,
		"escape":         `[{"process":"private\u001bdata"}]`,
		"nul":            `[{"process":"private\u0000data"}]`,
		"unicode":        `[{"process":"이름"}]`,
		"empty-parent":   `[{"process":"target","parent":""}]`,
		"null-parent":    `[{"process":"target","parent":null}]`,
		"extra-field":    `[{"process":"target","pid":123}]`,
		"duplicate":      `[{"process":"target","process":"private-data"}]`,
		"too-many-pairs": `[{"process":"target"},{"process":"second"}]`,
	} {
		test.Run(name, func(test *testing.T) {
			actual, err := decode(partial, names)
			if err == nil || !reflect.DeepEqual(actual, installedProbeReport{}) || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "target") {
				test.Fatal("malformed names were retained or echoed by the decoder")
			}
		})
	}
	for name, mutate := range map[string]func(*installedProbeReport){
		"unavailable-target": func(report *installedProbeReport) {
			report.UninspectableRoles, report.UninspectableParentRoles = map[string]int{"unavailable": 1}, map[string]int{"unavailable": 1}
		},
		"changed-target": func(report *installedProbeReport) {
			report.UninspectableRoles, report.UninspectableParentRoles = map[string]int{"identity-changed": 1}, map[string]int{"identity-changed": 1}
		},
		"unsampled-target": func(report *installedProbeReport) {
			report.UninspectableRoles, report.UninspectableParentRoles = map[string]int{"not-sampled": 1}, map[string]int{"not-sampled": 1}
		},
		"unavailable-parent": func(report *installedProbeReport) {
			report.UninspectableParentRoles = map[string]int{"unavailable": 1}
		},
		"complete": func(report *installedProbeReport) {
			report.UninspectableCount, report.Complete = 0, true
			report.UninspectableRoles, report.UninspectableParentRoles = nil, nil
		},
	} {
		test.Run(name, func(test *testing.T) {
			report := partial
			mutate(&report)
			actual, err := decode(report, `[{"process":"target","parent":"parent"}]`)
			if err == nil || !reflect.DeepEqual(actual, installedProbeReport{}) {
				test.Fatal("name diagnostics exceeded their attributable identity coverage")
			}
		})
	}
	missingParent := partial
	missingParent.UninspectableParentRoles = map[string]int{"unavailable": 1}
	if actual, err := decode(missingParent, `[{"process":"target"}]`); err == nil || actual.Version != 1 || actual.Complete {
		test.Fatal("verified target name required an unavailable parent name")
	}
	bounded := partial
	bounded.UninspectableCount = 17
	bounded.UninspectableRoles, bounded.UninspectableParentRoles = map[string]int{"other": 16, "not-sampled": 1}, map[string]int{"other": 16, "not-sampled": 1}
	for _, count := range []int{16, 17} {
		names := "[" + strings.TrimSuffix(strings.Repeat(`{"process":"target","parent":"parent"},`, count), ",") + "]"
		actual, err := decode(bounded, names)
		if err == nil || count == 16 && actual.Version != 1 || count == 17 && actual.Version != 0 {
			test.Fatal("decoder did not enforce the shared name sampling cap")
		}
	}
	parents := partial
	parents.UninspectableCount = 2
	parents.UninspectableRoles, parents.UninspectableParentRoles = map[string]int{"other": 2}, map[string]int{"other": 1, "unavailable": 1}
	if actual, err := decode(parents, `[{"process":"first","parent":"parent"},{"process":"second"}]`); err == nil || actual.Version != 1 {
		test.Fatal("valid partial parent-name coverage was rejected")
	}
	if actual, err := decode(parents, `[{"process":"first","parent":"parent"},{"process":"second","parent":"parent"}]`); err == nil || actual.Version != 0 {
		test.Fatal("name pairs exceeded the attributable parent count")
	}
}

func TestAuthorizedProbeNamesRequireSeparateManualHostedOptIn(test *testing.T) {
	valid := map[string]string{
		"TREECLEAR_TEST_PROCESS_PROBE": "1", "TREECLEAR_TEST_PROCESS_PROBE_NAMES": "1",
		"GITHUB_ACTIONS": "true", "GITHUB_EVENT_NAME": "workflow_dispatch",
		"RUNNER_ENVIRONMENT": "github-hosted", "RUNNER_OS": "macOS", "GITHUB_JOB": "process-visibility-probe",
	}
	if !hostedProcessProbeNamesAllowed(func(name string) string { return valid[name] }) {
		test.Fatal("explicit name disclosure in a manual hosted probe was rejected")
	}
	cases := map[string]map[string]string{
		"absent": {}, "local": {"TREECLEAR_TEST_PROCESS_PROBE": "1", "TREECLEAR_TEST_PROCESS_PROBE_NAMES": "1"},
	}
	for field := range valid {
		missing := maps.Clone(valid)
		delete(missing, field)
		cases["missing-"+field] = missing
	}
	for name, change := range map[string][2]string{
		"invalid-name-flag": {"TREECLEAR_TEST_PROCESS_PROBE_NAMES", "true"},
		"zero-name-flag":    {"TREECLEAR_TEST_PROCESS_PROBE_NAMES", "0"},
		"invalid-probe":     {"TREECLEAR_TEST_PROCESS_PROBE", "true"},
		"push":              {"GITHUB_EVENT_NAME", "push"},
		"pull-request":      {"GITHUB_EVENT_NAME", "pull_request"},
		"scheduled":         {"GITHUB_EVENT_NAME", "schedule"},
		"self-hosted":       {"RUNNER_ENVIRONMENT", "self-hosted"},
		"windows":           {"RUNNER_OS", "Windows"},
		"wrong-job":         {"GITHUB_JOB", "verify"},
		"not-actions":       {"GITHUB_ACTIONS", "false"},
	} {
		changed := maps.Clone(valid)
		changed[change[0]] = change[1]
		cases[name] = changed
	}
	for name, environment := range cases {
		test.Run(name, func(test *testing.T) {
			if hostedProcessProbeNamesAllowed(func(field string) string { return environment[field] }) {
				test.Fatal("unapproved execution context enabled name disclosure")
			}
		})
	}
}
