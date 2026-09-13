package cli

import (
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
			var candidate domain.Candidate
			if err := json.Unmarshal(output, &candidate); err != nil {
				test.Fatal(err)
			}
			if candidate.Fingerprint != requested.Candidates[0].Fingerprint {
				test.Fatalf("root-relative file selected another authenticated plan: other=%t", candidate.Fingerprint == other.Candidates[0].Fingerprint)
			}
		})
	}
}

func TestPlanWindowsAnchorsConfiguredPaths(test *testing.T) {
	for _, kind := range []string{"root_relative", "drive_relative"} {
		for _, setting := range []string{"root", "state"} {
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
				if setting == "root" {
					overrides.Roots = []string{reference}
				} else {
					dependencies.DataDirectory = reference
				}
				configuration, runtime, err := scanConfiguration(dependencies, overrides)
				if err != nil {
					test.Fatal(err)
				}
				actual := runtime.DataDirectory
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
