//go:build darwin || linux

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/config"
	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/pathutil"
)

func planParentAlias(test *testing.T, root string) (string, string) {
	test.Helper()
	outer := filepath.Join(root, "outer")
	target := filepath.Join(outer, "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		test.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "alias")); err != nil {
		test.Fatal(err)
	}
	return outer, "alias/../report.json"
}

func TestExplainResolvesExplicitFileBeforeParentTraversal(test *testing.T) {
	dependencies, inventory := planFixture(test)
	outer, reference := planParentAlias(test, dependencies.WorkingDirectory)
	first, _, _, err := runPlan(test, dependencies, "--output", "report.json")
	if err != nil {
		test.Fatal(err)
	}
	inventory.worktrees[0].Head = strings.Repeat("b", 40)
	second, _, _, err := runPlan(test, dependencies, "--output", filepath.Join(outer, "report.json"))
	if err != nil {
		test.Fatal(err)
	}
	if first.Candidates[0].ID != second.Candidates[0].ID || first.Candidates[0].Fingerprint == second.Candidates[0].Fingerprint {
		test.Fatal("fixture does not distinguish two plans for the same candidate")
	}
	for _, path := range []string{reference, dependencies.WorkingDirectory + "/" + reference} {
		output, _, err := runExplain(dependencies, second.Candidates[0].ID, "--plan", path, "--format", "json")
		if err != nil {
			test.Fatal(err)
		}
		var candidate domain.Candidate
		if err := json.Unmarshal(output, &candidate); err != nil {
			test.Fatal(err)
		}
		if candidate.Fingerprint != second.Candidates[0].Fingerprint {
			test.Fatalf("explicit file %q selected another authenticated plan", path)
		}
	}
}

func TestPlanExportResolvesParentBeforeTraversal(test *testing.T) {
	dependencies, _ := planFixture(test)
	outer, destination := planParentAlias(test, dependencies.WorkingDirectory)
	_, output, _, err := runPlan(test, dependencies, "--output", destination)
	if err != nil {
		test.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(outer, "report.json"))
	if err != nil || !bytes.Equal(contents, bytes.TrimSuffix(output, []byte("\n"))) {
		test.Fatalf("export missed the filesystem parent: %q, %v", contents, err)
	}
	if _, err := os.Lstat(filepath.Join(dependencies.WorkingDirectory, "report.json")); !os.IsNotExist(err) {
		test.Fatalf("export touched wrong parent: %v", err)
	}
}

func TestPlanResolvesRelativeRootSymlinkBeforeParentTraversal(test *testing.T) {
	dependencies, _ := planFixture(test)
	outer := filepath.Join(dependencies.WorkingDirectory, "outer")
	target := filepath.Join(outer, "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		test.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dependencies.WorkingDirectory, "alias")); err != nil {
		test.Fatal(err)
	}
	configuration, runtime, err := scanConfiguration(dependencies, config.Overrides{Roots: []string{"alias/.."}})
	if err != nil {
		test.Fatal(err)
	}
	_, request, err := configuredPlanBuilder(context.Background(), runtime, configuration)
	want, canonicalErr := pathutil.Canonical(outer)
	if err != nil || canonicalErr != nil || len(request.Roots) != 1 || request.Roots[0] != want {
		test.Fatalf("plan roots = %q, want %q, errors = %v, %v", request.Roots, want, err, canonicalErr)
	}
}
