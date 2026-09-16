package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/execx"
)

func TestRejectSplitIndexBackingNames(test *testing.T) {
	for _, name := range []string{
		"sharedindex." + strings.Repeat("a", 40),
		"sharedindex." + strings.Repeat("b", 64),
		"sharedindex.orphan-remnant",
		"sharedindex.invalid.lock",
		"SHAREDINDEX." + strings.Repeat("c", 40),
		"SharedIndex.mixed-case",
	} {
		for _, kind := range []string{"file", "directory"} {
			test.Run(name+"/"+kind, func(test *testing.T) {
				directory := test.TempDir()
				administrative := test.TempDir()
				path := filepath.Join(administrative, name)
				if kind == "directory" {
					if err := os.Mkdir(path, 0o700); err != nil {
						test.Fatal(err)
					}
				} else if err := os.WriteFile(path, []byte("not a Git index; name alone blocks reads"), 0o600); err != nil {
					test.Fatal(err)
				}
				client, commands := readonlyIndexDirectoryClient(test, directory, administrative)
				if err := client.rejectSplitIndex(test.Context(), directory); !errors.Is(err, errors.ErrUnsupported) {
					test.Errorf("backing entry %q: error %v; want ErrUnsupported", name, err)
				}
				if !reflect.DeepEqual(*commands, []string{"rev-parse"}) {
					test.Errorf("preflight commands = %q; want only actual-directory rev-parse", *commands)
				}
			})
		}
	}
}

func TestRejectSplitIndexAllowsOrdinaryDirectoryNames(test *testing.T) {
	directory := test.TempDir()
	administrative := test.TempDir()
	for _, name := range []string{"index", "sharedindex", "sharedindex_lock", "sharedindex-backup", "notsharedindex.any", ".sharedindex.any"} {
		if err := os.WriteFile(filepath.Join(administrative, name), []byte("opaque ordinary metadata"), 0o600); err != nil {
			test.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(administrative, "objects", "sharedindex.nested"), 0o700); err != nil {
		test.Fatal(err)
	}
	client, commands := readonlyIndexDirectoryClient(test, directory, administrative)
	if err := client.rejectSplitIndex(test.Context(), directory); err != nil {
		test.Fatalf("ordinary metadata: %v", err)
	}
	if !reflect.DeepEqual(*commands, []string{"rev-parse"}) {
		test.Errorf("preflight commands = %q; want actual-directory rev-parse", *commands)
	}
}

func TestRejectSplitIndexDirectoryCapacity(test *testing.T) {
	directory := test.TempDir()
	administrative := test.TempDir()
	for position := range maxAdminEntries {
		if err := os.WriteFile(filepath.Join(administrative, fmt.Sprintf("entry-%04d", position)), nil, 0o600); err != nil {
			test.Fatal(err)
		}
	}
	client, commands := readonlyIndexDirectoryClient(test, directory, administrative)
	if err := client.rejectSplitIndex(test.Context(), directory); err != nil {
		test.Fatalf("exact directory capacity rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(administrative, "one-too-many"), nil, 0o600); err != nil {
		test.Fatal(err)
	}
	if err := client.rejectSplitIndex(test.Context(), directory); !errors.Is(err, ErrIndexPreflightLimit) || errors.Is(err, errors.ErrUnsupported) {
		test.Fatalf("over-capacity directory error = %v; want distinct capacity refusal", err)
	}
	if !reflect.DeepEqual(*commands, []string{"rev-parse", "rev-parse"}) {
		test.Errorf("capacity commands = %q; want two index-free resolutions", *commands)
	}
}

func TestReadonlyIndexDirectoryPathCapacity(test *testing.T) {
	prefix := readonlyIndexCanonicalTemporaryDirectory(test) + string(filepath.Separator)
	for _, size := range []int{32 << 10, 32<<10 + 1} {
		test.Run(fmt.Sprintf("%d-bytes", size), func(test *testing.T) {
			directory := prefix + strings.Repeat("d", size-len(prefix))
			err := validateReadonlyIndexDirectoryPath(directory)
			if size == 32<<10 {
				if err != nil {
					test.Fatalf("exact path-byte capacity rejected: %v", err)
				}
			} else if !errors.Is(err, ErrIndexPreflightLimit) {
				test.Fatalf("path-byte overflow error = %v; want ErrIndexPreflightLimit", err)
			}
		})
	}
	for _, directory := range []string{"", "relative", prefix + "nul\x00path"} {
		if err := validateReadonlyIndexDirectoryPath(directory); !errors.Is(err, fs.ErrInvalid) || errors.Is(err, ErrIndexPreflightLimit) {
			test.Errorf("malformed path %q error = %v; want fs.ErrInvalid, not a capacity category", directory, err)
		}
	}
}

func TestRejectSplitIndexPreservesResolutionErrors(test *testing.T) {
	directory := test.TempDir()
	regular := filepath.Join(directory, "ordinary-file")
	if err := os.WriteFile(regular, nil, 0o600); err != nil {
		test.Fatal(err)
	}
	transport := errors.New("sharedindex. and changed state are only diagnostic text")
	for _, scenario := range []struct {
		name   string
		output string
		cause  error
		want   error
	}{
		{name: "transport", output: directory, cause: transport, want: transport},
		{name: "relative", output: "relative-admin"},
		{name: "empty"},
		{name: "nul", output: directory + "\x00"},
		{name: "missing", output: filepath.Join(directory, "missing"), want: fs.ErrNotExist},
		{name: "regular root", output: regular},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			calls := 0
			client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
				calls++
				assertRawReadRequest(test, request, directory)
				if !reflect.DeepEqual(request.Args, []string{"rev-parse", "--absolute-git-dir"}) {
					test.Fatalf("unexpected command: %q", request.Args)
				}
				return execx.Result{Stdout: []byte(scenario.output + "\n")}, scenario.cause
			}))
			err := client.rejectSplitIndex(test.Context(), directory)
			if err == nil || errors.Is(err, errors.ErrUnsupported) || scenario.want != nil && !errors.Is(err, scenario.want) || calls != 1 {
				test.Fatalf("resolution calls %d, error %v; want preserved %v, not inferred unsupported", calls, err, scenario.want)
			}
		})
	}
}

func TestRejectSplitIndexCancellation(test *testing.T) {
	for _, before := range []bool{true, false} {
		test.Run(fmt.Sprintf("before-resolution-%t", before), func(test *testing.T) {
			directory := test.TempDir()
			ctx, cancel := context.WithCancel(test.Context())
			defer cancel()
			if before {
				cancel()
			}
			calls := 0
			client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
				calls++
				if !reflect.DeepEqual(request.Args, []string{"rev-parse", "--absolute-git-dir"}) {
					test.Fatalf("unexpected command: %q", request.Args)
				}
				cancel()
				return execx.Result{Stdout: []byte(directory + "\n")}, nil
			}))
			err := client.rejectSplitIndex(ctx, directory)
			wantCalls := 1
			if before {
				wantCalls = 0
			}
			if !errors.Is(err, context.Canceled) || calls != wantCalls {
				test.Fatalf("canceled preflight calls %d, error %v; want %d calls and context.Canceled", calls, err, wantCalls)
			}
		})
	}
}

func TestReadonlyIndexDirectoryPathObservationRetainsNativeIdentity(test *testing.T) {
	directory := readonlyIndexCanonicalTemporaryDirectory(test)
	file, err := os.Open(directory)
	if err != nil {
		test.Fatal(err)
	}
	pinned, err := file.Stat()
	if err := errors.Join(err, file.Close()); err != nil {
		test.Fatal(err)
	}
	observed, err := defaultReadonlyIndexOperations().lstat(directory)
	if err != nil {
		test.Fatal(err)
	}
	if err := os.Rename(directory, directory+"-moved"); err != nil {
		test.Fatal(err)
	}
	if !os.SameFile(pinned, observed) {
		test.Fatal("root observation reloaded its identity from a moved path")
	}
}

func readonlyIndexDirectoryClient(test *testing.T, directory, administrative string) (*Client, *[]string) {
	test.Helper()
	var commands []string
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		assertRawReadRequest(test, request, directory)
		if !reflect.DeepEqual(request.Args, []string{"rev-parse", "--absolute-git-dir"}) {
			test.Fatalf("unexpected command during directory preflight: %q", request.Args)
		}
		commands = append(commands, request.Args[0])
		return execx.Result{Stdout: []byte(administrative + "\n")}, nil
	}))
	return client, &commands
}

func readonlyIndexPreflightResult(test *testing.T, request execx.Request, directory string) execx.Result {
	test.Helper()
	assertRawReadRequest(test, request, directory)
	if !reflect.DeepEqual(request.Args, []string{"rev-parse", "--absolute-git-dir"}) {
		test.Fatalf("unexpected index preflight: %q", request.Args)
	}
	return execx.Result{Stdout: []byte(directory + "\n")}
}
