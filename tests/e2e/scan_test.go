package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/cli"
	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/process"
	"github.com/hellices/treeclear/internal/testutil"
)

type syntheticProcesses struct {
	infos []process.Info
	err   error
}

func (source syntheticProcesses) List(context.Context) ([]process.Info, error) {
	return source.infos, source.err
}

func TestScanUsesRealGitWithoutMutation(test *testing.T) {
	for _, scenario := range []string{"inactive", "active", "unknown"} {
		test.Run(scenario, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			linked := repository.AddWorktree(test, "feature with spaces", "topic")
			child := filepath.Join(linked, "child")
			if err := os.Mkdir(child, 0o700); err != nil {
				test.Fatal(err)
			}
			registrations := repository.Git(test, "worktree", "list", "--porcelain", "-z")
			branches := repository.Git(test, "for-each-ref", "--format=%(refname):%(objectname)", "refs/heads/")
			admin := repository.Git(test, "-C", linked, "rev-parse", "--absolute-git-dir")
			indexPath := filepath.Join(admin, "index")
			indexBefore, err := os.ReadFile(indexPath)
			if err != nil {
				test.Fatal(err)
			}
			now := time.Now().UTC().Add(30 * 24 * time.Hour)
			source := syntheticProcesses{}
			want := domain.Safe
			if scenario == "active" {
				executable, err := os.Executable()
				if err != nil {
					test.Fatal(err)
				}
				source.infos = []process.Info{{PID: 42, CreatedAt: now.Add(-time.Hour), Executable: executable, CWD: child, CommandLine: []string{executable}, Inspectable: true, Owner: "fixture", OwnerRelation: process.OwnerSame}}
				want = domain.Protected
			} else if scenario == "unknown" {
				source.err = errors.New("synthetic process enumeration failure")
				want = domain.Protected
			}
			settings := test.TempDir()
			stateDirectory := filepath.Join(settings, "state")
			var stdout, stderr bytes.Buffer
			command := cli.NewRootCommand(cli.Dependencies{
				Stdout: &stdout, Stderr: &stderr, BuildVersion: "e2e", WorkingDirectory: repository.Root,
				UserConfigPath: filepath.Join(settings, "user.toml"), RepositoryConfigPath: filepath.Join(settings, "repository.toml"), DataDirectory: stateDirectory,
				Processes: process.Collector{Source: source}, Now: func() time.Time { return now },
			})
			command.SetArgs([]string{"scan", "--root", repository.Root, "--format", "json"})
			err = command.ExecuteContext(context.Background())
			if (err != nil) != (scenario == "unknown") {
				test.Fatalf("scan error = %v, stdout = %s, stderr = %s", err, stdout.String(), stderr.String())
			}
			var result cli.ScanResult
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				test.Fatal(err)
			}
			if len(result.Worktrees) != 2 || result.Complete != (scenario != "unknown") {
				test.Fatalf("scan = %#v", result)
			}
			found := false
			for _, item := range result.Worktrees {
				if item.Worktree.Primary && item.Decision.Classification != domain.Protected {
					test.Fatal("primary worktree was not protected")
				}
				if item.Worktree.Path == linked {
					found = true
					if item.Decision.Classification != want || item.Worktree.EstimatedBytes <= 0 {
						test.Fatalf("linked worktree = %#v", item)
					}
				}
			}
			if !found {
				test.Fatalf("missing linked worktree %q", linked)
			}
			indexAfter, err := os.ReadFile(indexPath)
			if err != nil || !bytes.Equal(indexBefore, indexAfter) {
				test.Fatalf("scan changed the index: %v", err)
			}
			if repository.Git(test, "worktree", "list", "--porcelain", "-z") != registrations || repository.Git(test, "for-each-ref", "--format=%(refname):%(objectname)", "refs/heads/") != branches {
				test.Fatal("scan changed worktree registrations or branches")
			}
			if _, err := os.Stat(stateDirectory); !errors.Is(err, os.ErrNotExist) {
				test.Fatalf("scan created state files: %v", err)
			}
		})
	}
}

func TestBinaryHelpAndVersionDoNotNeedGit(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	binary := filepath.Join(test.TempDir(), "treeclear")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(ctx, "go", "build", "-ldflags=-X github.com/hellices/treeclear/internal/version.Value=e2e", "-o", binary, "../../cmd/treeclear")
	if output, err := build.CombinedOutput(); err != nil {
		test.Fatalf("build CLI: %v\n%s", err, output)
	}
	home := test.TempDir()
	environment := execx.SanitizedEnvironment(os.Environ(), map[string]string{
		"HOME": home, "USERPROFILE": home, "APPDATA": home, "LOCALAPPDATA": home, "XDG_CONFIG_HOME": home, "PATH": filepath.Join(home, "no-git"),
	})
	for _, arguments := range [][]string{{"version"}, {"--help"}, {"apply"}} {
		command := exec.CommandContext(ctx, binary, arguments...)
		command.Dir, command.Env = home, environment
		output, err := command.CombinedOutput()
		switch arguments[0] {
		case "version":
			if err != nil || string(output) != "e2e\n" {
				test.Fatalf("version: %v, %q", err, output)
			}
		case "--help":
			if err != nil || !strings.Contains(string(output), "scan") {
				test.Fatalf("help: %v, %q", err, output)
			}
		case "apply":
			if err == nil || !strings.Contains(string(output), "unknown command") {
				test.Fatalf("mutation command must not exist: %v, %q", err, output)
			}
		}
	}
}
