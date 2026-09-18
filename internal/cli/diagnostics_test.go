package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/process"
)

func TestPlanWarningPreviewIsBounded(test *testing.T) {
	for _, scenario := range []string{"many", "long"} {
		test.Run(scenario, func(test *testing.T) {
			warnings := []string{strings.Repeat("한글\u202e\x1b\xff", 1000)}
			if scenario == "many" {
				warnings = nil
				for index := range 200 {
					warnings = append(warnings, fmt.Sprintf("warning-%03d: %s", index, strings.Repeat("detail ", 200)))
				}
			}
			original := append([]string(nil), warnings...)
			var output bytes.Buffer
			if err := writePlanWarnings(&output, warnings); err != nil {
				test.Fatal(err)
			}
			if output.Len() > 4096 {
				test.Fatalf("warning preview is unbounded: %d bytes", output.Len())
			}
			if !reflect.DeepEqual(original, warnings) || !strings.Contains(output.String(), "saved plan JSON") {
				test.Fatal("preview changed warnings or omitted the full-evidence location")
			}
			if scenario == "many" && !strings.Contains(output.String(), "195 additional warnings") {
				test.Fatal("preview omitted the exact remaining-warning count")
			}
			if scenario == "long" && !strings.Contains(output.String(), "truncated") {
				test.Fatal("preview did not disclose text truncation")
			}
			for _, line := range strings.Split(output.String(), "\n") {
				quoted, found := strings.CutPrefix(line, "warning: ")
				if !found {
					continue
				}
				if len(quoted) > 512 || !utf8.ValidString(quoted) || strings.ContainsAny(quoted, "\x1b\u202e") {
					test.Fatal("preview split or failed to escape terminal text")
				}
				if _, err := strconv.Unquote(quoted); err != nil {
					test.Fatalf("preview split a quoted escape: %v", err)
				}
			}
		})
	}
}

func TestPlanWarningPreviewPreservesShortText(test *testing.T) {
	warnings := []string{"", "short warning", "한글 café 😀\n\u0085\u202e\xff", strings.Repeat("a", 510)}
	var output, expected bytes.Buffer
	for _, warning := range warnings {
		fmt.Fprintf(&expected, "warning: %s\n", strconv.Quote(warning))
	}
	if err := writePlanWarnings(&output, warnings); err != nil || !bytes.Equal(output.Bytes(), expected.Bytes()) {
		test.Fatalf("short warning representation changed: %v", err)
	}
}

func TestPlanWarningPreviewPropagatesWriterFailures(test *testing.T) {
	for _, scenario := range []struct {
		name     string
		warnings []string
		failAt   int
	}{
		{"first warning", []string{"first"}, 1},
		{"omitted warning notice", []string{"1", "2", "3", "4", "5", "6"}, 6},
		{"truncated text notice", []string{strings.Repeat("long", 2000)}, 2},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			writer := &warningFailureWriter{failAt: scenario.failAt}
			if err := writePlanWarnings(writer, scenario.warnings); !errors.Is(err, io.ErrClosedPipe) {
				test.Fatalf("writer failure was lost: %v", err)
			}
		})
	}
}

func TestScanWarningPreviewPreservesFullJSON(test *testing.T) {
	value := ScanResult{SchemaVersion: 1, Warnings: []string{strings.Repeat("\u202e\x1b native failure ", 2000)}}
	var human, machine bytes.Buffer
	if err := renderScan(&human, "human", value); err != nil {
		test.Fatal(err)
	}
	if human.Len() > 4096 || !strings.Contains(human.String(), "--format json") {
		test.Fatalf("scan warning preview lacks a bounded JSON-directed summary: %d bytes", human.Len())
	}
	if err := renderScan(&machine, "json", value); err != nil {
		test.Fatal(err)
	}
	var decoded ScanResult
	if err := json.Unmarshal(machine.Bytes(), &decoded); err != nil || !reflect.DeepEqual(value.Warnings, decoded.Warnings) {
		test.Fatalf("scan JSON lost full warnings: %v", err)
	}
}

func TestPartialPlanDiagnosticsStayCompactAndLossless(test *testing.T) {
	dependencies, _ := planFixture(test)
	var failures []error
	for index := range 20 {
		failures = append(failures, fmt.Errorf("native failure %02d: %s", index, strings.Repeat("detail ", 200)))
	}
	dependencies.Processes = processStub{collection: process.Collection{Complete: false}, errors: failures}
	export := filepath.Join(test.TempDir(), "report.json")
	value, output, diagnostics, err := runPlan(test, dependencies, "--output", export)
	if err == nil || !errors.Is(err, failures[0]) || !errors.Is(err, failures[len(failures)-1]) {
		test.Fatal("partial collection lost its failure or underlying error identities")
	}
	if len(diagnostics)+len(strconv.Quote(err.Error())) > 4096 {
		test.Fatalf("partial plan repeats unbounded diagnostics: warnings=%d bytes error=%d bytes", len(diagnostics), len(err.Error()))
	}
	if !strings.Contains(err.Error(), value.ID) || !strings.Contains(err.Error(), "saved") || !strings.Contains(err.Error(), "incomplete") {
		test.Fatal("partial-plan summary omits its saved identity or incomplete status")
	}
	if len(value.Candidates) != 1 || value.Candidates[0].Action != "none" || value.Candidates[0].Decision.Classification != domain.Protected {
		test.Fatal("diagnostic formatting changed protective decisions")
	}
	for _, failure := range failures {
		if !bytes.Contains(output, []byte(failure.Error())) {
			test.Fatal("canonical JSON dropped a complete collection diagnostic")
		}
	}
	for _, path := range []string{export, filepath.Join(dependencies.DataDirectory, "plans", value.ID+".json")} {
		contents, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(contents, bytes.TrimSuffix(output, []byte("\n"))) {
			test.Fatalf("diagnostic formatting changed canonical storage/export: %v", readErr)
		}
	}
	explained, explainDiagnostics, explainErr := runExplain(dependencies, value.Candidates[0].ID, "--plan", value.ID, "--format", "json")
	if explainErr != nil {
		test.Fatal(explainErr)
	}
	explanation := decodePreviewExplanation(test, explained)
	if explanation.PlanID != value.ID || !reflect.DeepEqual(explanation.Removal, value.Removal) || !reflect.DeepEqual(explanation.Candidate, value.Candidates[0]) {
		test.Fatal("explanation no longer preserves complete authenticated candidate evidence")
	}
	if len(explainDiagnostics) > 4096 || !strings.Contains(explainDiagnostics, "saved plan JSON") {
		test.Fatalf("explanation repeats unbounded warnings: %d bytes", len(explainDiagnostics))
	}
}

type warningFailureWriter struct {
	writes int
	failAt int
}

func (writer *warningFailureWriter) Write(contents []byte) (int, error) {
	writer.writes++
	if writer.writes == writer.failAt {
		return 0, io.ErrClosedPipe
	}
	return len(contents), nil
}
