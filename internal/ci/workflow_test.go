package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCINativePlatformMatrix(test *testing.T) {
	workflow := readCIWorkflow(test)
	entryPattern := regexp.MustCompile(`(?m)^          - os: ([^\r\n ]+)\r?\n            goos: ([^\r\n ]+)\r?\n            goarch: ([^\r\n ]+)\r?$`)
	entries := entryPattern.FindAllStringSubmatch(workflow, -1)
	expected := map[string][2]string{
		"macos-15":       {"darwin", "arm64"},
		"macos-15-intel": {"darwin", "amd64"},
		"windows-2025":   {"windows", "amd64"},
	}
	if len(entries) != len(expected) {
		test.Fatalf("native verification matrix has %d entries, want %d", len(entries), len(expected))
	}
	for _, entry := range entries {
		platform, exists := expected[entry[1]]
		if !exists || platform != [2]string{entry[2], entry[3]} {
			test.Errorf("unexpected native matrix entry: %q", entry[1:])
		}
		delete(expected, entry[1])
	}
	if len(expected) != 0 {
		test.Errorf("native verification platforms missing: %v", expected)
	}
}

func TestCIVerifiesNativeHostAndTarget(test *testing.T) {
	workflow := readCIWorkflow(test)
	for _, required := range []string{
		"      - name: Verify native target\n",
		"$goEnvironment = go env -json GOHOSTOS GOHOSTARCH GOOS GOARCH\n          if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }",
		"$native = $goEnvironment | ConvertFrom-Json",
		"$native.GOHOSTOS -ne '${{ matrix.goos }}'",
		"$native.GOOS -ne '${{ matrix.goos }}'",
		"$native.GOHOSTARCH -ne '${{ matrix.goarch }}'",
		"$native.GOARCH -ne '${{ matrix.goarch }}'",
		"throw \"Expected native ${{ matrix.goos }}/${{ matrix.goarch }} verification\"",
	} {
		if !strings.Contains(workflow, required) {
			test.Errorf("missing native verification contract: %q", required)
		}
	}
}

func TestCIPreservesVerificationCommandsAndPinnedActions(test *testing.T) {
	workflow := readCIWorkflow(test)
	for _, required := range []string{
		"    name: verify (${{ matrix.os }})\n",
		"    runs-on: ${{ matrix.os }}\n",
		"      fail-fast: false\n",
		"  contents: read\n",
		"      GOTOOLCHAIN: local\n",
		"      GOWORK: 'off'\n",
		"      GOFLAGS: -mod=readonly\n",
		"          go-version: '1.26.5'\n",
		"gofmt -l .",
		"if ($unformatted) { throw",
		"go test -count=1 ./...",
		"go test -race -count=1 ./...",
		"go vet ./...",
		"go build ./...",
		"git status --porcelain",
		"if ($changes) { throw",
	} {
		if !strings.Contains(workflow, required) {
			test.Errorf("missing existing verification contract: %q", required)
		}
	}
	if err := workflowPinningError(workflow); err != nil {
		test.Fatal(err)
	}
}

func TestWorkflowPinningChecksNamedAndAnonymousSteps(test *testing.T) {
	pinned := "example/action@" + strings.Repeat("a", 40)
	neighbor := "      - name: Pinned neighbor\n        uses: " + pinned + "\n"
	for _, scenario := range []struct {
		name     string
		workflow string
		invalid  bool
	}{
		{name: "named pinned", workflow: neighbor},
		{name: "anonymous pinned", workflow: "      - uses: " + pinned + "\n"},
		{name: "named unpinned with pinned neighbor", workflow: neighbor + "      - name: Unpinned\n        uses: example/unpinned@main\n", invalid: true},
		{name: "anonymous unpinned with pinned neighbor", workflow: neighbor + "      - uses: example/unpinned@main\n", invalid: true},
		{name: "anonymous unpinned alone", workflow: "      - uses: example/unpinned@main\n", invalid: true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			err := workflowPinningError(scenario.workflow)
			if !scenario.invalid {
				if err != nil {
					test.Fatalf("valid pinned actions rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "not full-SHA pinned") || !strings.Contains(err.Error(), "example/unpinned@main") {
				test.Fatalf("unpinned action not detected: %v", err)
			}
		})
	}
}

func workflowPinningError(workflow string) error {
	usesPattern := regexp.MustCompile(`(?m)^[ \t]+(?:-[ \t]+)?uses:[ \t]+([^\r\n]+)\r?$`)
	pinnedAction := regexp.MustCompile(`^[^\s@]+@[0-9a-f]{40}$`)
	uses := usesPattern.FindAllStringSubmatch(workflow, -1)
	if len(uses) == 0 {
		return fmt.Errorf("no pinned workflow actions found")
	}
	for _, action := range uses {
		if !pinnedAction.MatchString(strings.TrimSpace(action[1])) {
			return fmt.Errorf("workflow action is not full-SHA pinned: %q", action[1])
		}
	}
	return nil
}

func readCIWorkflow(test *testing.T) string {
	test.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "ci.yml"))
	if err != nil {
		test.Fatal(err)
	}
	return strings.ReplaceAll(string(contents), "\r\n", "\n")
}
