//go:build darwin || linux

package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/execx"
)

func TestPlanAndExplainUsePhysicalWorkingDirectory(test *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		test.Fatal(err)
	}
	for _, mode := range []string{"alias", "alias_parent"} {
		for _, operation := range []string{"explain", "export"} {
			test.Run(mode+"_"+operation, func(test *testing.T) {
				root, err := filepath.EvalSymlinks(test.TempDir())
				if err != nil {
					test.Fatal(err)
				}
				outer := filepath.Join(root, "outer")
				target := filepath.Join(outer, "target")
				home := filepath.Join(root, "home")
				for _, directory := range []string{target, home} {
					if err := os.MkdirAll(directory, 0o700); err != nil {
						test.Fatal(err)
					}
				}
				alias := filepath.Join(root, "alias")
				if err := os.Symlink(target, alias); err != nil {
					test.Fatal(err)
				}
				logical, physical := alias, target
				if mode == "alias_parent" {
					logical, physical = alias+"/..", outer
				}
				command := exec.CommandContext(test.Context(), executable, "-test.run=^TestCLIPhysicalWorkingDirectoryHelper$")
				command.Dir = physical
				command.Env = execx.SanitizedEnvironment(os.Environ(), map[string]string{
					"HOME": home, "USERPROFILE": home, "APPDATA": home, "LOCALAPPDATA": home, "XDG_CONFIG_HOME": home,
					"PWD": logical, "TREECLEAR_TEST_CLI_ROOT": root,
					"TREECLEAR_TEST_CLI_PHYSICAL": physical, "TREECLEAR_TEST_CLI_OPERATION": operation,
				})
				if output, err := command.CombinedOutput(); err != nil {
					test.Fatalf("isolated CLI PWD probe: %v\n%s", err, output)
				}
			})
		}
	}
}

func TestCLIPhysicalWorkingDirectoryHelper(test *testing.T) {
	operation := os.Getenv("TREECLEAR_TEST_CLI_OPERATION")
	if operation == "" {
		return
	}
	root, physical := os.Getenv("TREECLEAR_TEST_CLI_ROOT"), os.Getenv("TREECLEAR_TEST_CLI_PHYSICAL")
	workingDirectory, err := os.Getwd()
	if err != nil || workingDirectory != os.Getenv("PWD") {
		test.Fatalf("PWD fixture not accepted by os.Getwd: %q, %v", workingDirectory, err)
	}
	dependencies, inventory := planFixture(test)
	if operation == "export" {
		scope := dependencies.WorkingDirectory
		dependencies.WorkingDirectory = ""
		_, output, _, err := runPlan(test, dependencies, "--root", scope, "--output", "new-export.json")
		if err != nil {
			test.Fatal(err)
		}
		contents, err := os.ReadFile(filepath.Join(physical, "new-export.json"))
		if err != nil || !bytes.Equal(contents, bytes.TrimSuffix(output, []byte("\n"))) {
			test.Fatalf("export missed physical cwd: %v", err)
		}
		return
	}
	first, _, _, err := runPlan(test, dependencies, "--output", filepath.Join(root, "report.json"))
	if err != nil {
		test.Fatal(err)
	}
	inventory.worktrees[0].Head = strings.Repeat("b", 40)
	second, secondBytes, _, err := runPlan(test, dependencies, "--output", filepath.Join(physical, "report.json"))
	if err != nil {
		test.Fatal(err)
	}
	ordinary, err := os.ReadFile("report.json")
	if err != nil || !bytes.Equal(ordinary, bytes.TrimSuffix(secondBytes, []byte("\n"))) {
		test.Fatalf("OS relative read did not select second plan: %v", err)
	}
	dependencies.WorkingDirectory = ""
	output, _, err := runExplain(dependencies, second.Candidates[0].ID, "--plan", "report.json", "--format", "json")
	if err != nil {
		test.Fatal(err)
	}
	explanation := decodePreviewExplanation(test, output)
	if explanation.PlanID != second.ID || explanation.Candidate.Fingerprint != second.Candidates[0].Fingerprint {
		test.Fatalf("relative file with PWD=%q selected another authenticated plan: first=%t", workingDirectory, explanation.Candidate.Fingerprint == first.Candidates[0].Fingerprint)
	}
}
