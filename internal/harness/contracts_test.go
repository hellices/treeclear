package harness

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCIWorkflowSafetyContract(test *testing.T) {
	root := sourceRoot(test)
	workflow := readContract(test, root, ".github/workflows/ci.yml")
	for _, required := range []string{"pull_request:", "push:", "branches: [main]", "contents: read", "concurrency:", "cancel-in-progress: true", "timeout-minutes:", "fail-fast: false", "macos-15", "windows-2025", "go-version: '1.26.5'", "GOTOOLCHAIN: local", "go run ./tools/harness verify"} {
		if !strings.Contains(workflow, required) {
			test.Errorf("CI missing contract %q", required)
		}
	}
	for _, forbidden := range []string{"pull_request_target", "write-all", "contents: write", "secrets.", "continue-on-error: true", "paths-ignore:"} {
		if strings.Contains(workflow, forbidden) {
			test.Errorf("unsafe CI configuration %q", forbidden)
		}
	}
	uses := regexp.MustCompile(`(?m)^\s*-?\s*uses:\s*(\S+)`).FindAllStringSubmatch(workflow, -1)
	if len(uses) < 2 {
		test.Fatal("checkout and Go setup actions are missing")
	}
	pinned := regexp.MustCompile(`^[A-Za-z0-9_./-]+@[a-f0-9]{40}$`)
	for _, action := range uses {
		if !pinned.MatchString(action[1]) {
			test.Errorf("action is not full-SHA pinned: %s", action[1])
		}
	}
}

func TestRepositoryGuidanceContract(test *testing.T) {
	root := sourceRoot(test)
	agents := readContract(test, root, "AGENTS.md")
	if len(strings.Split(agents, "\n")) >= 200 {
		test.Fatal("AGENTS.md must stay below 200 lines")
	}
	for _, required := range []string{"go run ./tools/harness verify", "Unknown", "offline", "branches", "temporary", "independent review", "human approval", "docs/plans/"} {
		if !strings.Contains(agents, required) {
			test.Errorf("contributor guidance missing %q", required)
		}
	}
	shim := readContract(test, root, "CLAUDE.md")
	if shim != "@AGENTS.md\n\n## Claude Code\n\nUse the same repository rules as `AGENTS.md`.\n" {
		test.Fatal("CLAUDE.md must remain a thin AGENTS.md import")
	}
	template := readContract(test, root, ".github/pull_request_template.md")
	for _, required := range []string{"Stage", "Validation", "Independent review", "Human approval", "Unverified", "not merged automatically"} {
		if !strings.Contains(template, required) {
			test.Errorf("PR template missing %q", required)
		}
	}
	if err := CheckDocs(root); err != nil {
		test.Fatal(err)
	}
}

func sourceRoot(test *testing.T) string {
	test.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		test.Fatal(err)
	}
	root, err := findRoot(cwd)
	if err != nil {
		test.Fatal(err)
	}
	return root
}

func readContract(test *testing.T, root, relative string) string {
	test.Helper()
	contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		test.Fatal(err)
	}
	return string(contents)
}
