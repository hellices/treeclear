package git

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestClientIndexReadsRejectNativeSplitIndex(test *testing.T) {
	for _, linked := range []bool{false, true} {
		layout := "primary"
		if linked {
			layout = "linked"
		}
		for _, arguments := range readonlyIndexCommands() {
			test.Run(layout+"/"+arguments[0], func(test *testing.T) {
				repository := testutil.NewRepository(test)
				directory := repository.Root
				if linked {
					directory = repository.AddWorktree(test, "split linked", "topic/split")
				}
				repository.Git(test, "-C", directory, "update-index", "--split-index")
				administrative := repository.Git(test, "-C", directory, "rev-parse", "--absolute-git-dir")
				before := readonlyIndexEvidence(test, administrative)
				if len(before) < 2 {
					test.Fatalf("split-index fixture has no shared backing entries: %v", before)
				}
				client, commands := readonlyIndexNativeClient(test, directory, arguments)
				result, err := client.run(test.Context(), directory, arguments...)
				if !errors.Is(err, errors.ErrUnsupported) || !reflect.DeepEqual(result, execx.Result{}) {
					test.Errorf("split-index read = %#v, error %v; want zero result and ErrUnsupported", result, err)
				}
				if !reflect.DeepEqual(*commands, []string{"rev-parse"}) {
					test.Errorf("split-index dispatch = %q; want only index-free rev-parse", *commands)
				}
				assertReadonlyIndexEvidence(test, before, readonlyIndexEvidence(test, administrative))
			})
		}
	}
}

func TestClientIndexReadsPreserveNativeOrdinaryIndex(test *testing.T) {
	for _, linked := range []bool{false, true} {
		layout := "primary"
		if linked {
			layout = "linked"
		}
		for _, arguments := range readonlyIndexCommands() {
			test.Run(layout+"/"+arguments[0], func(test *testing.T) {
				repository := testutil.NewRepository(test)
				directory := repository.Root
				if linked {
					directory = repository.AddWorktree(test, "ordinary linked", "topic/ordinary")
				}
				administrative := repository.Git(test, "-C", directory, "rev-parse", "--absolute-git-dir")
				before := readonlyIndexEvidence(test, administrative)
				if len(before) != 1 {
					test.Fatalf("ordinary-index fixture has unexpected backing entries: %v", before)
				}
				client, commands := readonlyIndexNativeClient(test, directory, arguments)
				if _, err := client.run(test.Context(), directory, arguments...); err != nil {
					test.Fatalf("ordinary-index read: %v", err)
				}
				if !reflect.DeepEqual(*commands, []string{"rev-parse", arguments[0]}) {
					test.Errorf("ordinary-index dispatch = %q; want preflight then %s", *commands, arguments[0])
				}
				assertReadonlyIndexEvidence(test, before, readonlyIndexEvidence(test, administrative))
			})
		}
	}
}

func TestReadonlyIndexEvidenceRetainsNativeIdentity(test *testing.T) {
	directory := test.TempDir()
	path := filepath.Join(directory, "index")
	if err := os.WriteFile(path, []byte("owned fixture index bytes"), 0o600); err != nil {
		test.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		test.Fatal(err)
	}
	pinned, err := file.Stat()
	if err := errors.Join(err, file.Close()); err != nil {
		test.Fatal(err)
	}
	observed := readonlyIndexEvidence(test, directory)["index"]
	if err := os.Rename(path, filepath.Join(directory, "moved-index")); err != nil {
		test.Fatal(err)
	}
	if !os.SameFile(pinned, observed.information) {
		test.Fatal("index evidence reloaded its identity from a moved path")
	}
}

func TestClientNativeSplitIndexTransitionBetweenCommands(test *testing.T) {
	for _, linked := range []bool{false, true} {
		layout := "primary"
		if linked {
			layout = "linked"
		}
		for _, operation := range rawReadOperations()[1:] {
			test.Run(layout+"/"+operation.name, func(test *testing.T) {
				repository := testutil.NewRepository(test)
				directory := repository.Root
				if linked {
					directory = repository.AddWorktree(test, "transition linked", "topic/transition")
				}
				administrative := repository.Git(test, "-C", directory, "rev-parse", "--absolute-git-dir")
				var commands []string
				var afterMutation map[string]readonlyIndexFileEvidence
				client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
					assertRawReadRequest(test, request, directory)
					commands = append(commands, request.Args[0])
					switch request.Args[0] {
					case "rev-parse":
						if !reflect.DeepEqual(request.Args, []string{"rev-parse", "--absolute-git-dir"}) {
							test.Fatalf("unexpected native preflight: %q", request.Args)
						}
					case "config", "ls-files":
					default:
						if request.Args[0] != operation.command {
							test.Fatalf("unexpected native command: %q", request.Args)
						}
					}
					result, err := (execx.OSRunner{}).Run(ctx, request)
					if request.Args[0] == "ls-files" && err == nil {
						if afterMutation != nil {
							test.Fatal("native index guard unexpectedly repeated")
						}
						repository.Git(test, "-C", directory, "update-index", "--split-index")
						afterMutation = readonlyIndexEvidence(test, administrative)
						if len(afterMutation) < 2 {
							test.Fatal("owned transition did not create a shared backing index")
						}
					}
					return result, err
				}))
				raw, err := operation.read(test, client, test.Context(), directory)
				if raw != nil || !errors.Is(err, errors.ErrUnsupported) || !reflect.DeepEqual(commands, []string{"config", "rev-parse", "ls-files", "rev-parse"}) {
					test.Errorf("native transition returned %q, commands %q, error %v", raw, commands, err)
				}
				if afterMutation == nil {
					test.Fatal("native transition never reached the owned mutation")
				}
				assertReadonlyIndexEvidence(test, afterMutation, readonlyIndexEvidence(test, administrative))
			})
		}
	}
}

func readonlyIndexCommands() [][]string {
	return [][]string{
		{"ls-files", "--cached", "--stage", "-v", "-z", "--no-recurse-submodules"},
		{"status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=none"},
		{"diff", "--binary", "--full-index", "--no-color", "--no-ext-diff", "--no-textconv", "--"},
	}
}

func readonlyIndexNativeClient(test *testing.T, directory string, arguments []string) (*Client, *[]string) {
	test.Helper()
	var commands []string
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		assertRawReadRequest(test, request, directory)
		if !reflect.DeepEqual(request.Args, []string{"rev-parse", "--absolute-git-dir"}) && !reflect.DeepEqual(request.Args, arguments) {
			test.Fatalf("unexpected Git command: %q", request.Args)
		}
		commands = append(commands, request.Args[0])
		return (execx.OSRunner{}).Run(ctx, request)
	}))
	return client, &commands
}

type readonlyIndexFileEvidence struct {
	information os.FileInfo
	contents    []byte
}

func readonlyIndexEvidence(test *testing.T, directory string) map[string]readonlyIndexFileEvidence {
	test.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		test.Fatal(err)
	}
	evidence := make(map[string]readonlyIndexFileEvidence)
	for _, entry := range entries {
		if entry.Name() != "index" && !strings.HasPrefix(strings.ToLower(entry.Name()), "sharedindex.") {
			continue
		}
		evidence[entry.Name()] = readonlyIndexFileObservation(test, filepath.Join(directory, entry.Name()))
	}
	if _, found := evidence["index"]; !found {
		test.Fatal("fixture index is missing")
	}
	return evidence
}

func readonlyIndexFileObservation(test *testing.T, path string) readonlyIndexFileEvidence {
	test.Helper()
	information, err := os.Lstat(path)
	if err != nil || !information.Mode().IsRegular() {
		test.Fatalf("fixture index metadata %q is not regular: %v", path, err)
	}
	file, err := os.Open(path)
	if err != nil {
		test.Fatal(err)
	}
	information, err = file.Stat()
	if err != nil || !information.Mode().IsRegular() {
		test.Fatalf("opened fixture index metadata %q is not regular: %v", path, errors.Join(err, file.Close()))
	}
	contents, readErr := io.ReadAll(file)
	if err := errors.Join(readErr, file.Close()); err != nil {
		test.Fatal(err)
	}
	return readonlyIndexFileEvidence{information: information, contents: contents}
}

func assertReadonlyIndexEvidence(test *testing.T, before, after map[string]readonlyIndexFileEvidence) {
	test.Helper()
	if len(before) != len(after) {
		test.Errorf("index entry count changed: before %d, after %d", len(before), len(after))
	}
	for name, original := range before {
		current, found := after[name]
		if !found {
			test.Errorf("index entry disappeared: %q", name)
			continue
		}
		if !os.SameFile(original.information, current.information) || original.information.Mode() != current.information.Mode() || original.information.Size() != current.information.Size() || !original.information.ModTime().Equal(current.information.ModTime()) || !bytes.Equal(original.contents, current.contents) {
			test.Errorf("index evidence changed for %q: mtime before %s, after %s; bytes equal %t", name, original.information.ModTime(), current.information.ModTime(), bytes.Equal(original.contents, current.contents))
		}
	}
}
