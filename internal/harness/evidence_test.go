package harness

import (
	"strings"
	"testing"
)

func passingEvidence() string {
	return `{"Action":"start","Package":"github.com/hellices/treeclear/internal/harness"}
{"Action":"run","Package":"github.com/hellices/treeclear/internal/harness","Test":"TestEvidence"}
{"Action":"pass","Package":"github.com/hellices/treeclear/internal/harness","Test":"TestEvidence"}
{"Action":"pass","Package":"github.com/hellices/treeclear/internal/harness"}`
}

func evidenceRequirement() []Requirement {
	return []Requirement{{ID: "H001", Description: "Evidence", Package: "./internal/harness", Test: "TestEvidence"}}
}

func TestEvidenceRequiresExecutedPassingTests(test *testing.T) {
	if err := CheckEvidence(strings.NewReader(passingEvidence()), evidenceRequirement()); err != nil {
		test.Fatal(err)
	}
	if err := CheckEvidence(strings.NewReader(passingEvidence()), nil); err == nil {
		test.Fatal("empty requirement selection accepted")
	}
	cases := map[string]string{
		"empty":                   "",
		"malformed":               "not-json",
		"missing test":            `{"Action":"pass","Package":"github.com/hellices/treeclear/internal/harness"}`,
		"skipped":                 strings.Replace(passingEvidence(), `"pass"`, `"skip"`, 1),
		"failed":                  strings.Replace(passingEvidence(), `"pass"`, `"fail"`, 1),
		"wrong package":           strings.ReplaceAll(passingEvidence(), `/internal/harness`, `/internal/other`),
		"wrong test":              strings.ReplaceAll(passingEvidence(), `TestEvidence`, `TestEvidenceOther`),
		"no run event":            strings.Replace(passingEvidence(), `"run"`, `"output"`, 1),
		"package fails":           passingEvidence() + "\n" + `{"Action":"fail","Package":"github.com/hellices/treeclear/internal/harness"}`,
		"package never completes": strings.TrimSuffix(passingEvidence(), `{"Action":"pass","Package":"github.com/hellices/treeclear/internal/harness"}`),
		"skipped subtest":         strings.Replace(passingEvidence(), `{"Action":"pass","Package":"github.com/hellices/treeclear/internal/harness","Test":"TestEvidence"}`, `{"Action":"skip","Package":"github.com/hellices/treeclear/internal/harness","Test":"TestEvidence/native"}`+"\n"+`{"Action":"pass","Package":"github.com/hellices/treeclear/internal/harness","Test":"TestEvidence"}`, 1),
	}
	for name, input := range cases {
		test.Run(name, func(test *testing.T) {
			if err := CheckEvidence(strings.NewReader(input), evidenceRequirement()); err == nil {
				test.Fatal("unverified evidence accepted")
			}
		})
	}
}

func TestEvidenceDoesNotConfuseLogOutputWithEvents(test *testing.T) {
	input := strings.Replace(passingEvidence(), `{"Action":"run","Package":"github.com/hellices/treeclear/internal/harness","Test":"TestEvidence"}`, `{"Action":"output","Package":"github.com/hellices/treeclear/internal/harness","Output":"--- PASS: TestEvidence"}`, 1)
	if err := CheckEvidence(strings.NewReader(input), evidenceRequirement()); err == nil {
		test.Fatal("a passing-looking output line substituted for test execution")
	}
}
