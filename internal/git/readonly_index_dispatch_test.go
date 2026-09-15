package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hellices/treeclear/internal/execx"
)

func TestClientRepeatsSplitIndexPreflightBetweenCommands(test *testing.T) {
	for _, operation := range rawReadOperations()[1:] {
		test.Run(operation.name, func(test *testing.T) {
			directory := test.TempDir()
			administrative := test.TempDir()
			backing := filepath.Join(administrative, "sharedindex.late-orphan")
			var created readonlyIndexFileEvidence
			var commands []string
			client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
				assertRawReadRequest(test, request, directory)
				commands = append(commands, request.Args[0])
				switch request.Args[0] {
				case "config":
					return execx.Result{ExitCode: 1}, nil
				case "rev-parse":
					if !reflect.DeepEqual(request.Args, []string{"rev-parse", "--absolute-git-dir"}) {
						test.Fatalf("unexpected preflight: %q", request.Args)
					}
					return execx.Result{Stdout: []byte(administrative + "\n")}, nil
				case "ls-files":
					contents := []byte("owned transition creates an opaque backing artifact")
					if err := os.WriteFile(backing, contents, 0o600); err != nil {
						test.Fatal(err)
					}
					created = readonlyIndexFileObservation(test, backing)
					return execx.Result{}, nil
				default:
					test.Errorf("unexpected index command after backing entry appeared: %q", request.Args)
					return execx.Result{}, nil
				}
			}))
			raw, err := operation.read(test, client, test.Context(), directory)
			if raw != nil || !errors.Is(err, errors.ErrUnsupported) || !reflect.DeepEqual(commands, []string{"config", "rev-parse", "ls-files", "rev-parse"}) {
				test.Fatalf("between-command guard returned %q, commands %q, error %v", raw, commands, err)
			}
			assertReadonlyIndexEvidence(test, map[string]readonlyIndexFileEvidence{"backing": created}, map[string]readonlyIndexFileEvidence{"backing": readonlyIndexFileObservation(test, backing)})
		})
	}
}
