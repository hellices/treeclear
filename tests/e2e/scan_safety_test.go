package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/cli"
	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/git"
	"github.com/hellices/treeclear/internal/process"
	"github.com/hellices/treeclear/internal/testutil"
)

func executeFixtureScan(test *testing.T, directory string, arguments ...string) (cli.ScanResult, error) {
	test.Helper()
	settings := test.TempDir()
	var stdout, stderr bytes.Buffer
	command := cli.NewRootCommand(cli.Dependencies{
		Stdout: &stdout, Stderr: &stderr, BuildVersion: "e2e", WorkingDirectory: directory,
		UserConfigPath: filepath.Join(settings, "user.toml"), RepositoryConfigPath: filepath.Join(settings, "repository.toml"),
		DataDirectory: filepath.Join(settings, "state"), Processes: process.Collector{Source: syntheticProcesses{}},
		Now: func() time.Time { return time.Now().UTC().Add(30 * 24 * time.Hour) },
	})
	command.SetArgs(append([]string{"scan", "--format", "json"}, arguments...))
	scanError := command.ExecuteContext(context.Background())
	var result cli.ScanResult
	if stdout.Len() != 0 {
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			test.Fatalf("invalid scan output: %v; %s", err, stdout.String())
		}
	}
	if _, err := os.Stat(filepath.Join(settings, "state")); !errors.Is(err, os.ErrNotExist) {
		test.Fatalf("scan created state files: %v", err)
	}
	return result, scanError
}

func TestScanProtectsTrackedEditsHiddenByGitSettings(test *testing.T) {
	for _, setting := range []string{"ordinary", "assume-unchanged", "skip-worktree", "redirected-worktree"} {
		test.Run(setting, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			linked := repository.AddWorktree(test, "linked", "topic")
			admin := repository.Git(test, "-C", linked, "rev-parse", "--absolute-git-dir")
			switch setting {
			case "assume-unchanged", "skip-worktree":
				repository.Git(test, "-C", linked, "update-index", "--"+setting, "--", "seed.txt")
			case "redirected-worktree":
				repository.Git(test, "config", "extensions.worktreeConfig", "true")
				repository.Git(test, "-C", linked, "config", "--worktree", "core.worktree", repository.Root)
			}
			if err := os.WriteFile(filepath.Join(linked, "seed.txt"), []byte("uncommitted user changes\n"), 0o600); err != nil {
				test.Fatal(err)
			}
			indexPath := filepath.Join(admin, "index")
			indexBefore, err := os.ReadFile(indexPath)
			if err != nil {
				test.Fatal(err)
			}
			result, scanError := executeFixtureScan(test, repository.Root, "--root", repository.Root)
			hidden := setting != "ordinary"
			if (scanError != nil) != hidden || result.Complete == hidden {
				test.Errorf("complete = %t, error = %v; hidden = %t", result.Complete, scanError, hidden)
			}
			found := false
			for _, item := range result.Worktrees {
				if item.Worktree.Path != linked {
					continue
				}
				found = true
				if item.Decision.Classification != domain.Protected || (hidden && item.Worktree.GitStateKnown) {
					test.Errorf("hidden tracked edits were trusted: %#v", item)
				}
				if setting == "redirected-worktree" && item.Worktree.PathSafe {
					test.Error("redirected Git worktree identity was marked path-safe")
				}
			}
			if !found {
				test.Fatalf("missing linked worktree %q", linked)
			}
			indexAfter, err := os.ReadFile(indexPath)
			if err != nil || !bytes.Equal(indexBefore, indexAfter) {
				test.Fatalf("scan modified the index or its flags: %v", err)
			}
		})
	}
}

func TestScanDoesNotExecuteSubmoduleFilters(test *testing.T) {
	for _, operation := range []string{"status", "scan"} {
		test.Run(operation, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			module := testutil.NewRepository(test)
			head := module.Git(test, "rev-parse", "HEAD")
			marker := filepath.Join(test.TempDir(), "executed")
			program := filepath.Join(test.TempDir(), "filter-program")
			quote := func(path string) string { return "'" + strings.ReplaceAll(filepath.ToSlash(path), "'", "'\\''") + "'" }
			if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf invoked > "+quote(marker)+"\ncat\n"), 0o700); err != nil {
				test.Fatal(err)
			}
			module.Git(test, "config", "filter.tripwire.clean", quote(program))
			if err := os.WriteFile(filepath.Join(module.Root, ".git", "info", "attributes"), []byte("seed.txt filter=tripwire\n"), 0o600); err != nil {
				test.Fatal(err)
			}
			if err := os.Chtimes(filepath.Join(module.Root, "seed.txt"), time.Unix(100, 0), time.Unix(100, 0)); err != nil {
				test.Fatal(err)
			}
			if err := os.Rename(module.Root, filepath.Join(repository.Root, "module")); err != nil {
				test.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repository.Root, ".gitmodules"), []byte("[submodule \"module\"]\n\tpath = module\n\turl = ./module\n"), 0o600); err != nil {
				test.Fatal(err)
			}
			repository.Git(test, "update-index", "--add", "--cacheinfo", "160000,"+head+",module")
			repository.Git(test, "add", "--", ".gitmodules")
			repository.Git(test, "commit", "-m", "Register local fixture submodule")
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				test.Fatalf("fixture setup executed filter: %v", err)
			}
			if operation == "status" {
				if _, err := git.NewClient(nil).Status(context.Background(), repository.Root); err == nil {
					test.Error("submodule collection was not rejected")
				}
			} else {
				result, err := executeFixtureScan(test, repository.Root, "--root", repository.Root)
				if err == nil || result.Complete || len(result.Worktrees) == 0 {
					test.Errorf("submodule scan = %#v, error = %v", result, err)
				}
				for _, item := range result.Worktrees {
					if item.Decision.Classification != domain.Protected {
						test.Errorf("incomplete submodule scan was not protected: %#v", item)
					}
				}
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				test.Fatalf("read-only collection executed a submodule filter: %v", err)
			}
		})
	}
}

func TestScanRequiresIntentionalScopeOutsideRepository(test *testing.T) {
	repository := testutil.NewRepository(test)
	repository.AddWorktree(test, "linked", "topic")
	outside := filepath.Dir(repository.Root)
	result, err := executeFixtureScan(test, outside)
	if err == nil || !strings.Contains(err.Error(), "--root") || len(result.Worktrees) != 0 {
		test.Errorf("implicit outside-repository scan = %#v, error = %v", result, err)
	}
	result, err = executeFixtureScan(test, outside, "--root", ".")
	if err != nil || !result.Complete || len(result.Worktrees) != 2 {
		test.Fatalf("explicit recursive scan = %#v, error = %v", result, err)
	}
}
