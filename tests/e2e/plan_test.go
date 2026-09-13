package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/cli"
	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/plan"
	"github.com/hellices/treeclear/internal/process"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestPlanAndExplainUseRealGitWithoutMutation(test *testing.T) {
	for _, scenario := range []string{"inactive", "active", "unknown"} {
		test.Run(scenario, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			linked := repository.AddWorktree(test, "feature with spaces", "topic")
			registrations := repository.Git(test, "worktree", "list", "--porcelain", "-z")
			branches := repository.Git(test, "for-each-ref", "--format=%(refname):%(objectname)", "refs/heads/")
			admin := repository.Git(test, "-C", linked, "rev-parse", "--absolute-git-dir")
			indexPath := filepath.Join(admin, "index")
			indexBefore, err := os.ReadFile(indexPath)
			if err != nil {
				test.Fatal(err)
			}
			clock := testutil.NewClock(time.Now().UTC().Add(30 * 24 * time.Hour))
			source := syntheticProcesses{}
			want := domain.Safe
			if scenario == "active" {
				executable, err := os.Executable()
				if err != nil {
					test.Fatal(err)
				}
				source.infos = []process.Info{{PID: 42, CreatedAt: clock.Now().Add(-time.Hour), Executable: executable, CWD: linked, CommandLine: []string{executable}, Inspectable: true, Owner: "fixture", OwnerRelation: process.OwnerSame}}
				want = domain.Protected
			} else if scenario == "unknown" {
				source.err = errors.New("synthetic enumeration failure")
				want = domain.Protected
			}
			settings := test.TempDir()
			exportPath := filepath.Join(settings, "report")
			var stdout, stderr bytes.Buffer
			dependencies := cli.Dependencies{
				Stdout: &stdout, Stderr: &stderr, BuildVersion: "e2e", WorkingDirectory: repository.Root,
				UserConfigPath: filepath.Join(settings, "user.toml"), RepositoryConfigPath: filepath.Join(settings, "repository.toml"), DataDirectory: filepath.Join(settings, "state"),
				Processes: process.Collector{Source: source}, Now: clock.Now,
			}
			command := cli.NewRootCommand(dependencies)
			command.SetArgs([]string{"plan", "--root", repository.Root, "--format", "json", "--output", exportPath})
			err = command.ExecuteContext(context.Background())
			if (err != nil) != (scenario == "unknown") {
				test.Fatalf("plan error = %v, stdout = %s, stderr = %s", err, stdout.String(), stderr.String())
			}
			var value domain.Plan
			if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
				test.Fatal(err)
			}
			if len(value.Candidates) != 2 || value.ExpiresAt.Sub(value.GeneratedAt) != 15*time.Minute {
				test.Fatalf("plan = %#v", value)
			}
			var selected domain.Candidate
			for _, candidate := range value.Candidates {
				if candidate.Worktree.Primary && (candidate.Decision.Classification != domain.Protected || candidate.Action != "none") {
					test.Fatal("primary worktree is actionable")
				}
				if candidate.Worktree.Path == linked {
					selected = candidate
				}
				fingerprint, err := plan.CandidateFingerprint(candidate)
				if err != nil || candidate.Fingerprint != fingerprint {
					test.Fatalf("candidate fingerprint = %q, want %q, error = %v", candidate.Fingerprint, fingerprint, err)
				}
			}
			if selected.ID == "" || selected.Decision.Classification != want || (selected.Action == "remove") != (want == domain.Safe) || selected.Snapshot.Required != (want == domain.Safe) {
				test.Fatalf("linked candidate = %#v", selected)
			}
			exported, err := os.ReadFile(exportPath)
			if err != nil || !bytes.Equal(exported, bytes.TrimSuffix(stdout.Bytes(), []byte("\n"))) {
				test.Fatalf("export changed canonical bytes: %v", err)
			}
			stdout.Reset()
			stderr.Reset()
			command = cli.NewRootCommand(dependencies)
			command.SetArgs([]string{"explain", selected.ID, "--plan", exportPath, "--format", "json"})
			if err := command.ExecuteContext(context.Background()); err != nil {
				test.Fatalf("explain = %v", err)
			}
			var explained domain.Candidate
			if err := json.Unmarshal(stdout.Bytes(), &explained); err != nil || explained.Fingerprint != selected.Fingerprint {
				test.Fatalf("explain lost candidate evidence: %v", err)
			}
			clock.Advance(15 * time.Minute)
			stdout.Reset()
			command = cli.NewRootCommand(dependencies)
			command.SetArgs([]string{"explain", selected.ID, "--plan", value.ID, "--format", "json"})
			if err := command.ExecuteContext(context.Background()); !errors.Is(err, plan.ErrPlanExpired) || stdout.Len() != 0 {
				test.Fatalf("expired explain = %v, output = %q", err, stdout.String())
			}
			indexAfter, err := os.ReadFile(indexPath)
			if err != nil || !bytes.Equal(indexBefore, indexAfter) {
				test.Fatalf("commands changed index: %v", err)
			}
			if actual := repository.Git(test, "worktree", "list", "--porcelain", "-z"); actual != registrations {
				test.Fatal("commands changed worktree registrations")
			}
			if actual := repository.Git(test, "for-each-ref", "--format=%(refname):%(objectname)", "refs/heads/"); actual != branches {
				test.Fatal("commands changed local branches")
			}
		})
	}
}
