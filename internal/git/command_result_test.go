package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestClientRawReadsDistinguishQuietConfigExit(test *testing.T) {
	repository := testutil.NewRepository(test)
	writeRawFixtureFile(test, filepath.Join(repository.Root, "untracked.txt"), []byte("synthetic guard fixture\n"))
	for _, operation := range rawReadOperations()[1:] {
		for _, scenario := range quietExitScenarios() {
			test.Run(operation.name+"/"+scenario.name, func(test *testing.T) {
				client, captured := captureRawFixtureReads(test, repository.Root)
				runner := client.Runner
				client.Runner = runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
					result, err := runner.Run(ctx, request)
					if request.Args[0] == "config" {
						assertNativeQuietExitOne(test, result, err)
						return scenario.change(result, err)
					}
					return result, err
				})
				raw, err := operation.read(test, client, test.Context(), repository.Root)
				if scenario.blocked {
					if err == nil || raw != nil || len(captured["ls-files"]) != 0 || len(captured[operation.command]) != 0 {
						test.Fatalf("ambiguous config exit exposed raw=%d, error=%v, commands=%v", len(raw), err, captured)
					}
					if scenario.cause != nil && !errors.Is(err, scenario.cause) {
						test.Fatalf("guard lost failure cause %v: %v", scenario.cause, err)
					}
				} else if err != nil || len(captured["ls-files"]) != 1 || len(captured[operation.command]) != 1 {
					test.Fatalf("confirmed quiet exit blocked collection: %v, commands=%v", err, captured)
				}
				if len(captured["config"]) != 1 {
					test.Fatalf("configuration calls=%d, want one", len(captured["config"]))
				}
			})
		}
	}
}

func TestClientInspectionDistinguishesQuietDetachedExit(test *testing.T) {
	repository := testutil.NewRepository(test)
	linked := repository.AddWorktree(test, "detached", "topic")
	repository.Git(test, "-C", linked, "switch", "--detach", "HEAD")
	worktrees, err := NewClient(nil).ListWorktrees(test.Context(), repository.Root)
	if err != nil {
		test.Fatal(err)
	}
	var candidate domain.Worktree
	for _, worktree := range worktrees {
		if worktree.Path == linked {
			candidate = worktree
		}
	}
	if !candidate.Detached || candidate.Path == "" {
		test.Fatalf("missing detached fixture: %#v", worktrees)
	}
	for _, scenario := range quietExitScenarios() {
		test.Run(scenario.name, func(test *testing.T) {
			symbolicCalls := 0
			client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
				result, err := (execx.OSRunner{}).Run(ctx, request)
				if request.Args[0] == "symbolic-ref" {
					symbolicCalls++
					assertNativeQuietExitOne(test, result, err)
					return scenario.change(result, err)
				}
				return result, err
			}))
			actual, err := client.InspectWorktree(test.Context(), repository.Root, candidate)
			if scenario.blocked {
				if err == nil || actual.GitStateKnown || len(actual.CollectionErrors) == 0 {
					test.Fatalf("ambiguous detached exit remained known: known=%t, error=%v", actual.GitStateKnown, err)
				}
				if scenario.cause != nil && !errors.Is(err, scenario.cause) {
					test.Fatalf("inspection lost failure cause %v: %v", scenario.cause, err)
				}
			} else if err != nil || !actual.GitStateKnown {
				test.Fatalf("confirmed detached exit blocked inspection: known=%t, error=%v", actual.GitStateKnown, err)
			}
			if symbolicCalls != 1 {
				test.Fatalf("symbolic-ref calls=%d, want one", symbolicCalls)
			}
		})
	}
}

type quietExitScenario struct {
	name    string
	blocked bool
	cause   error
	change  func(execx.Result, error) (execx.Result, error)
}

func quietExitScenarios() []quietExitScenario {
	transportFailure := errors.New("synthetic transport failure")
	return []quietExitScenario{
		{"native exit", false, nil, func(result execx.Result, err error) (execx.Result, error) {
			return result, err
		}},
		{"wrapped native exit", false, nil, func(result execx.Result, err error) (execx.Result, error) {
			return result, fmt.Errorf("process context: %w", err)
		}},
		{"reported exit without runner error", false, nil, func(result execx.Result, err error) (execx.Result, error) {
			return result, nil
		}},
		{"transport failure", true, transportFailure, func(result execx.Result, err error) (execx.Result, error) {
			return result, transportFailure
		}},
		{"joined process and transport errors", true, transportFailure, func(result execx.Result, err error) (execx.Result, error) {
			return result, errors.Join(err, transportFailure)
		}},
		{"joined process and wait delay", true, exec.ErrWaitDelay, func(result execx.Result, err error) (execx.Result, error) {
			return result, errors.Join(err, exec.ErrWaitDelay)
		}},
		{"conflicting stdout", true, nil, func(result execx.Result, err error) (execx.Result, error) {
			result.Stdout = []byte("unexpected content\n")
			return result, err
		}},
		{"unexpected stderr", true, nil, func(result execx.Result, err error) (execx.Result, error) {
			result.Stderr = []byte("unexpected diagnostic\n")
			return result, err
		}},
		{"conflicting process status", true, nil, func(result execx.Result, err error) (execx.Result, error) {
			result.ExitCode = 2
			return result, err
		}},
		{"untyped status text", true, nil, func(result execx.Result, err error) (execx.Result, error) {
			return result, errors.New("exit status 1")
		}},
	}
}

func assertNativeQuietExitOne(test *testing.T, result execx.Result, err error) {
	test.Helper()
	var processExit *exec.ExitError
	if result.ExitCode != 1 || len(result.Stdout) != 0 || len(result.Stderr) != 0 || !errors.As(err, &processExit) || processExit.ExitCode() != 1 {
		test.Fatalf("fixture did not produce a quiet native exit one: result=%#v, error=%v", result, err)
	}
	if strings.Contains(err.Error(), "synthetic") {
		test.Fatalf("expected actual Git exit error, got %v", err)
	}
}
