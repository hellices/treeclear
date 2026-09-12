package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type testEvent struct {
	Action  string
	Package string
	Test    string
	Output  string
}

type testState struct {
	ran     bool
	passed  bool
	blocked bool
}

func CheckEvidence(reader io.Reader, requirements []Requirement) error {
	if len(requirements) == 0 {
		return errors.New("no acceptance requirements selected")
	}
	states := make([]testState, len(requirements))
	packages := make(map[string]testState)
	decoder := json.NewDecoder(reader)
	for {
		var event testEvent
		if err := decoder.Decode(&event); err == io.EOF {
			break
		} else if err != nil {
			return fmt.Errorf("invalid go test evidence: %w", err)
		}
		if event.Package == "" || event.Action == "" {
			return errors.New("go test evidence is missing package or action")
		}
		if event.Action == "fail" {
			return fmt.Errorf("failed test evidence: %s %s", event.Package, event.Test)
		}
		if event.Test == "" {
			state := packages[event.Package]
			switch event.Action {
			case "start":
				state.ran = true
			case "pass":
				state.passed = true
			case "skip":
				state.blocked = true
			}
			packages[event.Package] = state
		}
		for index, requirement := range requirements {
			if event.Package != requirement.importPath() {
				continue
			}
			if event.Action == "skip" && (event.Test == requirement.Test || strings.HasPrefix(event.Test, requirement.Test+"/")) {
				states[index].blocked = true
			}
			if event.Test == requirement.Test {
				switch event.Action {
				case "run":
					states[index].ran = true
				case "pass":
					states[index].passed = true
				}
			}
		}
	}
	var missing []error
	for index, requirement := range requirements {
		state := states[index]
		packageState := packages[requirement.importPath()]
		if !state.ran || !state.passed || state.blocked || !packageState.ran || !packageState.passed || packageState.blocked {
			missing = append(missing, fmt.Errorf("unverified %s: %s %s (must run and pass without skipped subtests)", requirement.ID, requirement.Package, requirement.Test))
		}
	}
	return errors.Join(missing...)
}
