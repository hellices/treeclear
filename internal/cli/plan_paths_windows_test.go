package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/config"
	"github.com/hellices/treeclear/internal/pathutil"
)

func TestPlanWindowsAnchorsSelectedPaths(test *testing.T) {
	for _, kind := range []string{"root_relative", "drive_relative"} {
		test.Run(kind, func(test *testing.T) {
			dependencies, inventory := selectionFixture(test, "selected, with spaces")
			target := inventory.worktrees[0].Path
			volume := filepath.VolumeName(target)
			reference := strings.TrimPrefix(target, volume)
			if kind == "drive_relative" {
				relative, err := filepath.Rel(dependencies.WorkingDirectory, target)
				if err != nil {
					test.Fatal(err)
				}
				reference = volume + relative
			}
			value, _, _, err := runPlan(test, dependencies, "--worktree", reference)
			if err != nil {
				test.Fatal(err)
			}
			assertSelectionPreview(test, value, []string{target}, false)
		})
	}
}

func TestPlanAndExplainWindowsRootRelativeFiles(test *testing.T) {
	for _, operation := range []string{"explain", "export"} {
		test.Run(operation, func(test *testing.T) {
			dependencies, inventory := planFixture(test)
			destination := filepath.Join(test.TempDir(), "report.json")
			if !strings.EqualFold(filepath.VolumeName(destination), filepath.VolumeName(dependencies.WorkingDirectory)) {
				test.Skip("fixture directories must share a drive")
			}
			reference := strings.TrimPrefix(destination, filepath.VolumeName(destination))
			if filepath.IsAbs(reference) || !strings.HasPrefix(reference, `\`) {
				test.Fatalf("fixture is not a rooted-relative Windows path: %q", reference)
			}
			if operation == "export" {
				if _, _, _, err := runPlan(test, dependencies, "--output", reference); err != nil {
					test.Fatal(err)
				}
				if _, err := os.Stat(destination); err != nil {
					test.Fatalf("root-relative export missed requested path: %v", err)
				}
				return
			}
			requested, _, _, err := runPlan(test, dependencies, "--output", destination)
			if err != nil {
				test.Fatal(err)
			}
			inventory.worktrees[0].Head = strings.Repeat("b", 40)
			incorrect := dependencies.WorkingDirectory + string(filepath.Separator) + reference
			other, _, _, err := runPlan(test, dependencies, "--output", incorrect)
			if err != nil {
				test.Fatal(err)
			}
			output, _, err := runExplain(dependencies, requested.Candidates[0].ID, "--plan", reference, "--format", "json")
			if err != nil {
				test.Fatal(err)
			}
			explanation := decodePreviewExplanation(test, output)
			if explanation.PlanID != requested.ID || explanation.Candidate.Fingerprint != requested.Candidates[0].Fingerprint {
				test.Fatalf("root-relative file selected another authenticated plan: other=%t", explanation.Candidate.Fingerprint == other.Candidates[0].Fingerprint)
			}
		})
	}
}

func TestPlanWindowsAnchorsConfiguredPaths(test *testing.T) {
	for _, kind := range []string{"root_relative", "drive_relative"} {
		for _, setting := range []string{"root", "state", "user_config", "repository_config"} {
			test.Run(kind+"_"+setting, func(test *testing.T) {
				dependencies, _ := planFixture(test)
				destination := test.TempDir()
				volume := filepath.VolumeName(destination)
				if !strings.EqualFold(volume, filepath.VolumeName(dependencies.WorkingDirectory)) {
					test.Skip("fixture directories must share a drive")
				}
				reference := strings.TrimPrefix(destination, volume)
				if kind == "drive_relative" {
					relative, err := filepath.Rel(dependencies.WorkingDirectory, destination)
					if err != nil {
						test.Fatal(err)
					}
					reference = volume + relative
				}
				overrides := config.Overrides{Roots: []string{dependencies.WorkingDirectory}}
				switch setting {
				case "root":
					overrides.Roots = []string{reference}
				case "state":
					dependencies.DataDirectory = reference
				case "user_config":
					dependencies.UserConfigPath = reference + `\user.toml`
				case "repository_config":
					dependencies.RepositoryConfigPath = reference + `\repository.toml`
				}
				configuration, runtime, err := scanConfiguration(dependencies, overrides)
				if err != nil {
					test.Fatal(err)
				}
				actual := runtime.DataDirectory
				if setting == "user_config" {
					actual = strings.TrimSuffix(runtime.UserConfigPath, `\user.toml`)
				} else if setting == "repository_config" {
					actual = strings.TrimSuffix(runtime.RepositoryConfigPath, `\repository.toml`)
				}
				if setting == "root" {
					_, request, err := configuredPlanBuilder(context.Background(), runtime, configuration)
					if err != nil || len(request.Roots) != 1 {
						test.Fatalf("root resolution failed: %q, %v", request.Roots, err)
					}
					actual = request.Roots[0]
				}
				actual, err = pathutil.Canonical(actual)
				want, wantErr := pathutil.Canonical(destination)
				if err != nil || wantErr != nil || actual != want {
					test.Fatalf("configured %s = %q, want %q: %v, %v", setting, actual, want, err, wantErr)
				}
			})
		}
	}
}

func TestResolveInputPathWindowsVolumes(test *testing.T) {
	for _, scenario := range []struct {
		name, cwd, input, want string
	}{
		{"rooted", `C:\work`, `\reports\plan.json`, `C:\reports\plan.json`},
		{"rooted_slash", `C:\work`, `/reports/plan.json`, `C:/reports/plan.json`},
		{"same_drive", `C:\work`, `c:alias\..\plan.json`, `C:\work\alias\..\plan.json`},
		{"drive_only", `C:\work`, `C:`, `C:\work\`},
		{"absolute_drive", `C:\work`, `D:\alias\..\plan.json`, `D:\alias\..\plan.json`},
		{"absolute_unc", `C:\work`, `\\server\share\plan.json`, `\\server\share\plan.json`},
		{"unc_rooted", `\\server\share\work`, `\reports\plan.json`, `\\server\share\reports\plan.json`},
		{"other_drive_relative", `C:\work`, `D:plan.json`, ""},
		{"unc_drive_relative", `\\server\share\work`, `C:plan.json`, ""},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			resolved, err := resolveInputPath(scenario.cwd, scenario.input)
			if scenario.want == "" {
				if err == nil || !strings.Contains(err.Error(), "absolute path") {
					test.Fatalf("ambiguous drive input = %q, %v", resolved, err)
				}
			} else if err != nil || resolved != scenario.want {
				test.Fatalf("resolved = %q, want %q: %v", resolved, scenario.want, err)
			}
		})
	}
}
