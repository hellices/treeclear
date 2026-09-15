package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
)

func TestClientStatusSnapshotUsesOneGuardedObservation(test *testing.T) {
	directory := test.TempDir()
	contents, wantStatus, wantPaths := statusPathsMixedFixture()
	var commands []string
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		commands = append(commands, request.Args[0])
		return statusSnapshotFixtureResult(test, request, directory, contents), nil
	}))
	actual, err := client.StatusSnapshot(test.Context(), directory)
	if err != nil || actual.Status != wantStatus || !bytes.Equal(actual.Raw, contents) || !reflect.DeepEqual(actual.UntrackedPaths, wantPaths) {
		test.Fatalf("status snapshot = %#v, error = %v", actual, err)
	}
	if !reflect.DeepEqual(commands, []string{"rev-parse", "config", "rev-parse", "ls-files", "rev-parse", "status"}) {
		test.Fatalf("status command sequence = %q", commands)
	}
}

func TestClientStatusSnapshotOwnsResults(test *testing.T) {
	directory := test.TempDir()
	contents := []byte("? first\x00? second\x00")
	original := bytes.Clone(contents)
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		return statusSnapshotFixtureResult(test, request, directory, contents), nil
	}))
	first, err := client.StatusSnapshot(test.Context(), directory)
	if err != nil || len(first.Raw) == 0 || len(first.UntrackedPaths) != 2 {
		test.Fatalf("first observation = %#v, error = %v", first, err)
	}
	first.Raw[2] = 'X'
	if first.UntrackedPaths[0] != "first" || !bytes.Equal(contents, original) {
		test.Fatal("returned raw bytes alias path text or retained runner output")
	}
	first.UntrackedPaths[0] = "changed"
	second, err := client.StatusSnapshot(test.Context(), directory)
	if err != nil || !bytes.Equal(second.Raw, original) || !reflect.DeepEqual(second.UntrackedPaths, []string{"first", "second"}) {
		test.Fatalf("caller mutation changed a later observation: %#v, %v", second, err)
	}
	second.UntrackedPaths[1] = "also changed"
	if first.UntrackedPaths[1] != "second" || !bytes.Equal(second.Raw, original) {
		test.Fatal("path slices alias another result or raw bytes")
	}
}

func TestClientStatusSnapshotEmptyObservation(test *testing.T) {
	directory := test.TempDir()
	var commands []string
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		commands = append(commands, request.Args[0])
		return statusSnapshotFixtureResult(test, request, directory, nil), nil
	}))
	actual, err := client.StatusSnapshot(test.Context(), directory)
	if err != nil || actual.Status != (domain.GitStatus{}) || actual.Raw != nil || actual.UntrackedPaths != nil || !reflect.DeepEqual(commands, []string{"rev-parse", "config", "rev-parse", "ls-files", "rev-parse", "status"}) {
		test.Fatalf("empty status observation = %#v, commands %q, error %v", actual, commands, err)
	}
}

func TestClientStatusSnapshotRootIOFailure(test *testing.T) {
	directory := test.TempDir()
	missing := filepath.Join(directory, "missing")
	var commands []string
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		commands = append(commands, request.Args[0])
		assertRawReadRequest(test, request, directory)
		if request.Args[0] == "rev-parse" {
			return execx.Result{Stdout: []byte(filepath.ToSlash(missing) + "\n")}, nil
		}
		return statusSnapshotFixtureResult(test, request, directory, nil), nil
	}))
	actual, err := client.StatusSnapshot(test.Context(), directory)
	assertStatusSnapshotError(test, actual, err)
	if !errors.Is(err, os.ErrNotExist) || !reflect.DeepEqual(commands, []string{"rev-parse"}) {
		test.Fatalf("lost root I/O failure or continued collection: commands %q, error %v", commands, err)
	}
}

func TestClientStatusSnapshotCanceledAfterRoot(test *testing.T) {
	directory := test.TempDir()
	ctx, cancel := context.WithCancel(test.Context())
	defer cancel()
	var commands []string
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		commands = append(commands, request.Args[0])
		if request.Args[0] == "rev-parse" {
			cancel()
		}
		return statusSnapshotFixtureResult(test, request, directory, []byte("? selected\x00")), nil
	}))
	actual, err := client.StatusSnapshot(ctx, directory)
	assertStatusSnapshotError(test, actual, err)
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(commands, []string{"rev-parse"}) {
		test.Fatalf("continued collection after root cancellation: commands %q, error %v", commands, err)
	}
}

func TestClientStatusSnapshotDiscardsCommandFailures(test *testing.T) {
	directory := test.TempDir()
	sequence := []string{"rev-parse", "config", "rev-parse", "ls-files", "rev-parse", "status"}
	for position, command := range sequence {
		for _, cause := range []error{errors.New("synthetic transport failure"), context.Canceled, context.DeadlineExceeded, execx.ErrOutputLimit} {
			test.Run(fmt.Sprintf("%d-%s/%s", position, command, cause), func(test *testing.T) {
				var commands []string
				client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
					commands = append(commands, request.Args[0])
					if len(commands) == position+1 {
						assertRawReadRequest(test, request, directory)
						return execx.Result{Stdout: []byte("? partial\x00"), ExitCode: 1}, cause
					}
					return statusSnapshotFixtureResult(test, request, directory, nil), nil
				}))
				actual, err := client.StatusSnapshot(test.Context(), directory)
				assertStatusSnapshotError(test, actual, err)
				if !errors.Is(err, cause) || !reflect.DeepEqual(commands, sequence[:position+1]) {
					test.Fatalf("lost failure or executed later commands: commands %q, error %v; want %v", commands, err, cause)
				}
			})
		}
	}
}

func TestClientStatusSnapshotPreservesGuards(test *testing.T) {
	directory := test.TempDir()
	for _, testCase := range []struct {
		name    string
		command string
		result  execx.Result
		want    []string
	}{
		{"filter", "config", execx.Result{Stdout: []byte("filter.tripwire.clean\nnot-executed\x00")}, []string{"rev-parse", "config"}},
		{"ambiguous filter exit", "config", execx.Result{ExitCode: 1, Stderr: []byte("configuration warning")}, []string{"rev-parse", "config"}},
		{"unsafe index", "ls-files", execx.Result{Stdout: []byte("S 100644 " + strings.Repeat("a", 40) + " 0\tfile\x00")}, []string{"rev-parse", "config", "rev-parse", "ls-files"}},
		{"nonzero status", "status", execx.Result{ExitCode: 1, Stdout: []byte("? partial\x00")}, []string{"rev-parse", "config", "rev-parse", "ls-files", "rev-parse", "status"}},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			var commands []string
			client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
				commands = append(commands, request.Args[0])
				if request.Args[0] == testCase.command {
					assertRawReadRequest(test, request, directory)
					return testCase.result, nil
				}
				return statusSnapshotFixtureResult(test, request, directory, nil), nil
			}))
			actual, err := client.StatusSnapshot(test.Context(), directory)
			assertStatusSnapshotError(test, actual, err)
			if !reflect.DeepEqual(commands, testCase.want) {
				test.Fatalf("guard command sequence = %q, want %q", commands, testCase.want)
			}
		})
	}
}

func TestClientStatusSnapshotRejectsDifferentRoot(test *testing.T) {
	directory := test.TempDir()
	other := test.TempDir()
	var commands []string
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		commands = append(commands, request.Args[0])
		assertRawReadRequest(test, request, directory)
		if request.Args[0] == "rev-parse" {
			return execx.Result{Stdout: []byte(filepath.ToSlash(other) + "\n")}, nil
		}
		return statusSnapshotFixtureResult(test, request, directory, []byte("? unsafe\x00")), nil
	}))
	actual, err := client.StatusSnapshot(test.Context(), directory)
	assertStatusSnapshotError(test, actual, err)
	if !reflect.DeepEqual(commands, []string{"rev-parse"}) {
		test.Fatalf("root mismatch reached status collection: %q", commands)
	}
}

func TestClientStatusSnapshotCancellation(test *testing.T) {
	for _, before := range []bool{true, false} {
		name := "after payload"
		if before {
			name = "before commands"
		}
		test.Run(name, func(test *testing.T) {
			directory := test.TempDir()
			ctx, cancel := context.WithCancel(test.Context())
			defer cancel()
			if before {
				cancel()
			}
			var commands []string
			client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
				commands = append(commands, request.Args[0])
				if request.Args[0] == "status" {
					cancel()
				}
				return statusSnapshotFixtureResult(test, request, directory, []byte("? selected\x00")), nil
			}))
			actual, err := client.StatusSnapshot(ctx, directory)
			assertStatusSnapshotError(test, actual, err)
			if !errors.Is(err, context.Canceled) || before && len(commands) != 0 || !before && len(commands) != 6 {
				test.Fatalf("cancellation returned error %v after commands %q", err, commands)
			}
		})
	}
}

func TestClientStatusSnapshotDiscardsMalformedOrOverLimitPayload(test *testing.T) {
	for _, contents := range [][]byte{[]byte("? earlier\x00broken\x00"), statusPathsCountFixture(4097)} {
		directory := test.TempDir()
		client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
			return statusSnapshotFixtureResult(test, request, directory, contents), nil
		}))
		actual, err := client.StatusSnapshot(test.Context(), directory)
		assertStatusSnapshotError(test, actual, err)
	}
}

func TestClientStatusLegacyKeepsLargeSummaries(test *testing.T) {
	directory := test.TempDir()
	contents := statusPathsCountFixture(4097)
	var commands []string
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		commands = append(commands, request.Args[0])
		return statusSnapshotFixtureResult(test, request, directory, contents), nil
	}))
	status, err := client.Status(test.Context(), directory)
	if err != nil || status != (domain.GitStatus{Untracked: 4097}) || !reflect.DeepEqual(commands, []string{"config", "rev-parse", "ls-files", "rev-parse", "status"}) {
		test.Fatalf("legacy Status = %#v, commands %q, error %v", status, commands, err)
	}
	commands = nil
	status, raw, err := client.StatusRaw(test.Context(), directory)
	if err != nil || status != (domain.GitStatus{Untracked: 4097}) || !bytes.Equal(raw, contents) || !reflect.DeepEqual(commands, []string{"config", "rev-parse", "ls-files", "rev-parse", "status"}) {
		test.Fatalf("legacy StatusRaw = %#v, raw length %d, commands %q, error %v", status, len(raw), commands, err)
	}
}

func statusSnapshotFixtureResult(test *testing.T, request execx.Request, directory string, contents []byte) execx.Result {
	test.Helper()
	assertRawReadRequest(test, request, directory)
	var want []string
	var result execx.Result
	switch request.Args[0] {
	case "rev-parse":
		if reflect.DeepEqual(request.Args, []string{"rev-parse", "--absolute-git-dir"}) {
			return readonlyIndexPreflightResult(test, request, directory)
		}
		want = []string{"rev-parse", "--path-format=absolute", "--show-toplevel"}
		result.Stdout = []byte(filepath.ToSlash(directory) + "\n")
	case "config":
		want = []string{"config", "--null", "--get-regexp", `^filter\..*\.(clean|smudge|process)$`}
		result.ExitCode = 1
	case "ls-files":
		want = []string{"ls-files", "--cached", "--stage", "-v", "-z", "--no-recurse-submodules"}
	case "status":
		want = []string{"status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=none"}
		result.Stdout = contents
	default:
		test.Fatalf("unexpected command %q", request.Args)
	}
	if !reflect.DeepEqual(request.Args, want) {
		test.Fatalf("command arguments = %q, want %q", request.Args, want)
	}
	return result
}

func assertStatusSnapshotError(test *testing.T, actual StatusSnapshot, err error) {
	test.Helper()
	if err == nil || actual.Status != (domain.GitStatus{}) || actual.Raw != nil || actual.UntrackedPaths != nil {
		test.Fatalf("failure returned status %#v, raw length %d, %d paths, error %v", actual.Status, len(actual.Raw), len(actual.UntrackedPaths), err)
	}
}
