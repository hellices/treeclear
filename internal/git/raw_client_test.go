package git

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
)

func TestClientListWorktreesRawPreservesBytesAndLegacyValues(test *testing.T) {
	common := test.TempDir()
	primary := filepath.Join(common, "primary with spaces")
	linked := filepath.Join(common, "linked\nname\t ")
	head := strings.Repeat("a", 40)
	contents := []byte("worktree " + filepath.ToSlash(primary) + "\x00HEAD " + head + "\x00branch refs/heads/main\x00\x00" +
		"worktree " + filepath.ToSlash(linked) + "\x00HEAD " + head + "\x00branch refs/heads/@\x00locked agent\nactive \x00\x00")
	var commands []string
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		commands = append(commands, request.Args[0])
		assertRawReadRequest(test, request, primary)
		switch request.Args[0] {
		case "worktree":
			if !reflect.DeepEqual(request.Args, []string{"worktree", "list", "--porcelain", "-z"}) {
				test.Fatalf("list arguments = %q", request.Args)
			}
			return execx.Result{Stdout: bytes.Clone(contents)}, nil
		case "rev-parse":
			if !reflect.DeepEqual(request.Args, []string{"rev-parse", "--path-format=absolute", "--git-common-dir"}) {
				test.Fatalf("metadata arguments = %q", request.Args)
			}
			return execx.Result{Stdout: []byte(common + "\n")}, nil
		default:
			test.Fatalf("unexpected command %q", request.Args)
			return execx.Result{}, nil
		}
	}))
	worktrees, raw, err := client.ListWorktreesRaw(context.Background(), primary)
	if err != nil || !bytes.Equal(raw, contents) || len(worktrees) != 2 {
		test.Fatalf("raw inventory = %#v, bytes %q, error %v", worktrees, raw, err)
	}
	canonicalCommon, err := filepath.EvalSymlinks(common)
	if err != nil {
		test.Fatal(err)
	}
	want := []domain.Worktree{
		{Path: primary, RepositoryRoot: primary, CommonGitDir: canonicalCommon, Head: head, Branch: "main", Primary: true},
		{Path: linked, RepositoryRoot: primary, CommonGitDir: canonicalCommon, Head: head, Branch: "@", Locked: true, LockReason: "agent\nactive "},
	}
	if !reflect.DeepEqual(worktrees, want) || !reflect.DeepEqual(commands, []string{"worktree", "rev-parse"}) {
		test.Fatalf("inventory = %#v, commands %q", worktrees, commands)
	}
	commands = nil
	legacy, err := client.ListWorktrees(context.Background(), primary)
	if err != nil || !reflect.DeepEqual(legacy, want) || !reflect.DeepEqual(commands, []string{"worktree", "rev-parse"}) {
		test.Fatalf("legacy inventory = %#v, commands %q, error %v", legacy, commands, err)
	}
}

func TestClientStatusRawPreservesBytesAndLegacyValues(test *testing.T) {
	directory := test.TempDir()
	head := strings.Repeat("a", 40)
	index := strings.Repeat("b", 40)
	other := strings.Repeat("c", 40)
	contents := []byte("1 M. N... 100644 100644 100644 " + head + " " + index + " staged file\x00" +
		"1 .M N... 100644 100644 100644 " + head + " " + head + " changed\nfile\x00" +
		"2 R. N... 100644 100644 100644 " + head + " " + head + " R100 new\xff\tname \x00old\nname \x00" +
		"u UU N... 100644 100644 100644 100644 " + head + " " + index + " " + other + " conflict\x00" +
		"? untracked\n\t file \x00")
	indexContents := []byte("H 100644 " + index + " 0\tstaged file\x00" +
		"H 100644 " + head + " 0\tchanged\nfile\x00" +
		"H 100644 " + head + " 0\tnew\xff\tname \x00" +
		"M 100644 " + head + " 1\tconflict\x00" +
		"M 100644 " + index + " 2\tconflict\x00" +
		"M 100644 " + other + " 3\tconflict\x00")
	var commands []string
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		commands = append(commands, request.Args[0])
		assertRawReadRequest(test, request, directory)
		switch request.Args[0] {
		case "config":
			return execx.Result{ExitCode: 1}, nil
		case "rev-parse":
			return readonlyIndexPreflightResult(test, request, directory), nil
		case "ls-files":
			if !reflect.DeepEqual(request.Args, []string{"ls-files", "--cached", "--stage", "-v", "-z", "--no-recurse-submodules"}) {
				test.Fatalf("index guard arguments = %q", request.Args)
			}
			return execx.Result{Stdout: bytes.Clone(indexContents)}, nil
		case "status":
			if !reflect.DeepEqual(request.Args, []string{"status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=none"}) {
				test.Fatalf("status arguments = %q", request.Args)
			}
			return execx.Result{Stdout: bytes.Clone(contents)}, nil
		default:
			test.Fatalf("unexpected command %q", request.Args)
			return execx.Result{}, nil
		}
	}))
	status, raw, err := client.StatusRaw(context.Background(), directory)
	want := domain.GitStatus{Staged: 2, Unstaged: 1, Unmerged: 1, Untracked: 1}
	if err != nil || status != want || !bytes.Equal(raw, contents) || !reflect.DeepEqual(commands, []string{"config", "rev-parse", "ls-files", "rev-parse", "status"}) {
		test.Fatalf("raw status = %#v, bytes %q, commands %q, error %v", status, raw, commands, err)
	}
	commands = nil
	legacy, err := client.Status(context.Background(), directory)
	if err != nil || legacy != want || !reflect.DeepEqual(commands, []string{"config", "rev-parse", "ls-files", "rev-parse", "status"}) {
		test.Fatalf("legacy status = %#v, commands %q, error %v", legacy, commands, err)
	}
}

func TestClientRawReadsDiscardFailedCommandOutput(test *testing.T) {
	directory := test.TempDir()
	transportFailure := errors.New("synthetic read failure")
	for _, operation := range rawReadOperations() {
		for _, failure := range []struct {
			name     string
			err      error
			exitCode int
		}{
			{"nonzero exit", nil, 128},
			{"transport", transportFailure, 0},
			{"canceled", context.Canceled, -1},
			{"deadline", context.DeadlineExceeded, -1},
			{"output limit", execx.ErrOutputLimit, -1},
		} {
			test.Run(operation.name+"/"+failure.name, func(test *testing.T) {
				contents := []byte("partial\x00\xff output \n")
				if failure.name == "nonzero exit" || failure.name == "transport" {
					switch operation.command {
					case "worktree":
						contents = []byte("worktree " + filepath.ToSlash(directory) + "\x00HEAD " + strings.Repeat("a", 40) + "\x00branch refs/heads/main\x00\x00")
					case "status":
						contents = []byte("? untracked file\x00")
					case "diff":
						contents = []byte("diff --git a/file b/file\n--- a/file\n+++ b/file\n@@ -1 +1 @@\n-old\n+new\n")
					}
				}
				var commands []string
				client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
					commands = append(commands, request.Args[0])
					assertRawReadRequest(test, request, directory)
					if request.Args[0] == operation.command {
						return execx.Result{Stdout: contents, ExitCode: failure.exitCode}, failure.err
					}
					if request.Args[0] == "rev-parse" {
						return readonlyIndexPreflightResult(test, request, directory), nil
					}
					if request.Args[0] == "config" {
						return execx.Result{ExitCode: 1}, nil
					}
					if request.Args[0] == "ls-files" {
						return execx.Result{}, nil
					}
					test.Fatalf("unexpected command after failure: %q", request.Args)
					return execx.Result{}, nil
				}))
				raw, err := operation.read(test, client, context.Background(), directory)
				if err == nil || raw != nil || failure.err != nil && !errors.Is(err, failure.err) {
					test.Fatalf("failed collection returned %q, error %v; want %v", raw, err, failure.err)
				}
				want := []string{operation.command}
				if operation.command != "worktree" {
					want = []string{"config", "rev-parse", "ls-files", "rev-parse", operation.command}
				}
				if !reflect.DeepEqual(commands, want) {
					test.Fatalf("command order = %q, want %q", commands, want)
				}
			})
		}
	}
}

func TestClientRawReadsPreserveFilterAndIndexGuards(test *testing.T) {
	directory := test.TempDir()
	head := strings.Repeat("a", 40)
	for _, operation := range rawReadOperations()[1:] {
		for _, guard := range []struct {
			name    string
			command string
			result  execx.Result
			err     error
		}{
			{"executable filter", "config", execx.Result{Stdout: []byte("filter.tripwire.clean\nnever-run-this\x00")}, nil},
			{"filter canceled exit one", "config", execx.Result{ExitCode: 1}, context.Canceled},
			{"filter deadline exit one", "config", execx.Result{ExitCode: 1}, context.DeadlineExceeded},
			{"filter output limit exit one", "config", execx.Result{ExitCode: 1}, execx.ErrOutputLimit},
			{"filter transport exit one", "config", execx.Result{ExitCode: 1}, errors.New("synthetic transport failure")},
			{"filter wait delay exit one", "config", execx.Result{ExitCode: 1}, exec.ErrWaitDelay},
			{"filter conflicting stdout", "config", execx.Result{ExitCode: 1, Stdout: []byte("filter.tripwire.clean\nnever-run-this\x00")}, nil},
			{"filter diagnostic exit one", "config", execx.Result{ExitCode: 1, Stderr: []byte("synthetic configuration warning")}, nil},
			{"skip worktree", "ls-files", execx.Result{Stdout: []byte("S 100644 " + head + " 0\tfile\x00")}, nil},
			{"assume unchanged", "ls-files", execx.Result{Stdout: []byte("h 100644 " + head + " 0\tfile\x00")}, nil},
			{"submodule", "ls-files", execx.Result{Stdout: []byte("H 160000 " + head + " 0\tmodule\x00")}, nil},
			{"malformed index", "ls-files", execx.Result{Stdout: []byte("H 100644 " + head + " 0\tfile")}, nil},
			{"index read failure", "ls-files", execx.Result{Stdout: []byte("partial index")}, context.Canceled},
		} {
			test.Run(operation.name+"/"+guard.name, func(test *testing.T) {
				var commands []string
				client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
					commands = append(commands, request.Args[0])
					if request.Args[0] == guard.command {
						return guard.result, guard.err
					}
					if request.Args[0] == "rev-parse" {
						return readonlyIndexPreflightResult(test, request, directory), nil
					}
					if request.Args[0] == "config" {
						return execx.Result{ExitCode: 1}, nil
					}
					test.Fatalf("guard allowed later command %q", request.Args)
					return execx.Result{}, nil
				}))
				raw, err := operation.read(test, client, context.Background(), directory)
				if raw != nil || err == nil || guard.err != nil && !errors.Is(err, guard.err) {
					test.Fatalf("guard returned %q, error %v; want %v", raw, err, guard.err)
				}
				want := []string{"config"}
				if guard.command == "ls-files" {
					want = append(want, "rev-parse", "ls-files")
				}
				if !reflect.DeepEqual(commands, want) {
					test.Fatalf("guard command order = %q, want %q", commands, want)
				}
			})
		}
	}
}

func TestClientRawReadsDiscardMalformedPorcelain(test *testing.T) {
	directory := test.TempDir()
	for _, operation := range rawReadOperations()[:2] {
		test.Run(operation.name, func(test *testing.T) {
			contents := []byte("? valid untracked\x00malformed\x00")
			if operation.command == "worktree" {
				contents = []byte("worktree " + filepath.ToSlash(directory) + "\x00HEAD " + strings.Repeat("a", 40) + "\x00branch refs/heads/main\x00\x00malformed\x00\x00")
			}
			client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
				if request.Args[0] == operation.command {
					return execx.Result{Stdout: contents}, nil
				}
				if request.Args[0] == "rev-parse" {
					return readonlyIndexPreflightResult(test, request, directory), nil
				}
				if request.Args[0] == "config" {
					return execx.Result{ExitCode: 1}, nil
				}
				if request.Args[0] == "ls-files" {
					return execx.Result{}, nil
				}
				test.Fatalf("malformed output allowed later command %q", request.Args)
				return execx.Result{}, nil
			}))
			raw, err := operation.read(test, client, context.Background(), directory)
			if raw != nil || err == nil {
				test.Fatalf("malformed output returned %q, error %v", raw, err)
			}
		})
	}
}

func TestClientListWorktreesRawDiscardsUnsupportedBranchNames(test *testing.T) {
	directory := test.TempDir()
	head := strings.Repeat("a", 40)
	for _, entry := range []struct{ name, branch string }{
		{"consecutive dots", "foo..bar"},
		{"leading dash", "-name"},
		{"space", "has space"},
		{"newline", "line\nname"},
		{"tab", "branch\tname"},
		{"hidden component", "feature/.hidden"},
		{"lock component", "feature.lock/child"},
		{"trailing dot", "topic."},
		{"checkout expression", "@{-1}"},
		{"reserved HEAD", "HEAD"},
		{"invalid UTF-8", "topic\xff"},
		{"overlong", strings.Repeat("a", 1025)},
	} {
		test.Run(entry.name, func(test *testing.T) {
			contents := []byte("worktree " + filepath.ToSlash(filepath.Join(directory, "primary")) + "\x00HEAD " + head + "\x00branch refs/heads/main\x00\x00" +
				"worktree " + filepath.ToSlash(filepath.Join(directory, "linked")) + "\x00HEAD " + head + "\x00branch refs/heads/" + entry.branch + "\x00\x00")
			var commands []string
			client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
				commands = append(commands, request.Args[0])
				switch request.Args[0] {
				case "worktree":
					return execx.Result{Stdout: bytes.Clone(contents)}, nil
				case "rev-parse":
					return execx.Result{Stdout: []byte(directory + "\n")}, nil
				default:
					test.Fatalf("unexpected command: %q", request.Args)
					return execx.Result{}, nil
				}
			}))
			parsed, raw, err := client.ListWorktreesRaw(context.Background(), directory)
			if err == nil || parsed != nil || raw != nil || !reflect.DeepEqual(commands, []string{"worktree"}) {
				test.Fatalf("unsupported branch %q: parsed=%d, raw=%d, error=%v, commands=%q", entry.branch, len(parsed), len(raw), err, commands)
			}
		})
	}
}

func TestClientListWorktreesRawDiscardsMetadataFailures(test *testing.T) {
	directory := test.TempDir()
	head := strings.Repeat("a", 40)
	failure := errors.New("metadata unavailable")
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		if request.Args[0] == "worktree" {
			return execx.Result{Stdout: []byte("worktree " + filepath.ToSlash(directory) + "\x00HEAD " + head + "\x00branch refs/heads/main\x00\x00")}, nil
		}
		return execx.Result{Stdout: []byte("partial metadata")}, failure
	}))
	worktrees, raw, err := client.ListWorktreesRaw(context.Background(), directory)
	if worktrees != nil || raw != nil || !errors.Is(err, failure) {
		test.Fatalf("metadata failure returned %#v, %q, %v", worktrees, raw, err)
	}
}

func TestClientListWorktreesRawRejectsBareAnchor(test *testing.T) {
	calls := 0
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		calls++
		return execx.Result{Stdout: []byte("worktree /fixture/bare\x00bare\x00\x00")}, nil
	}))
	worktrees, raw, err := client.ListWorktreesRaw(context.Background(), test.TempDir())
	if worktrees != nil || raw != nil || err == nil || calls != 1 {
		test.Fatalf("bare inventory returned %#v, %q, %v, calls %d", worktrees, raw, err, calls)
	}
}

func TestClientListWorktreesRawDiscardsInvalidCommonDirectory(test *testing.T) {
	directory := test.TempDir()
	for _, location := range []string{"relative-path", filepath.Join(directory, "missing")} {
		client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
			if request.Args[0] == "worktree" {
				return execx.Result{Stdout: []byte("worktree " + filepath.ToSlash(directory) + "\x00HEAD " + strings.Repeat("a", 40) + "\x00branch refs/heads/main\x00\x00")}, nil
			}
			return execx.Result{Stdout: []byte(location + "\n")}, nil
		}))
		parsed, raw, err := client.ListWorktreesRaw(context.Background(), directory)
		if parsed != nil || raw != nil || err == nil {
			test.Fatalf("invalid common directory %q returned %#v, %q, %v", location, parsed, raw, err)
		}
	}
}

func TestClientRawReadsForwardCanceledContext(test *testing.T) {
	for _, operation := range rawReadOperations() {
		test.Run(operation.name, func(test *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			calls := 0
			client := NewClient(runnerFunc(func(received context.Context, request execx.Request) (execx.Result, error) {
				calls++
				if received != ctx || !errors.Is(received.Err(), context.Canceled) {
					test.Fatal("raw read replaced the canceled caller context")
				}
				return execx.Result{Stdout: []byte("partial"), ExitCode: -1}, received.Err()
			}))
			raw, err := operation.read(test, client, ctx, test.TempDir())
			if raw != nil || !errors.Is(err, context.Canceled) || calls != 1 {
				test.Fatalf("canceled read returned %q, %v, calls %d", raw, err, calls)
			}
		})
	}
}

func TestClientRawReadsRejectMissingDirectory(test *testing.T) {
	for _, operation := range rawReadOperations() {
		test.Run(operation.name, func(test *testing.T) {
			calls := 0
			client := NewClient(runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
				calls++
				return execx.Result{}, nil
			}))
			raw, err := operation.read(test, client, context.Background(), "")
			if raw != nil || err == nil || calls != 0 {
				test.Fatalf("empty directory returned %q, %v, calls %d", raw, err, calls)
			}
		})
	}
}

func TestClientDiffPreservesSuccessfulBytesAndSafeArguments(test *testing.T) {
	directory := test.TempDir()
	contents := []byte(" leading\r\n\x00\xffpatch\ntrailing \n")
	for _, staged := range []bool{false, true} {
		client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
			assertRawReadRequest(test, request, directory)
			if request.Args[0] == "rev-parse" {
				return readonlyIndexPreflightResult(test, request, directory), nil
			}
			if request.Args[0] == "config" {
				return execx.Result{ExitCode: 1}, nil
			}
			if request.Args[0] == "ls-files" {
				return execx.Result{}, nil
			}
			want := []string{"diff", "--binary", "--full-index", "--no-color", "--no-ext-diff", "--no-textconv"}
			if staged {
				want = append(want, "--cached")
			}
			want = append(want, "--")
			if !reflect.DeepEqual(request.Args, want) {
				test.Fatalf("diff arguments = %q, want %q", request.Args, want)
			}
			return execx.Result{Stdout: bytes.Clone(contents)}, nil
		}))
		actual, err := client.Diff(context.Background(), directory, staged)
		if err != nil || !bytes.Equal(actual, contents) {
			test.Fatalf("staged %t: diff = %q, error %v", staged, actual, err)
		}
	}
}

func assertRawReadRequest(test *testing.T, request execx.Request, directory string) {
	test.Helper()
	if request.Name != "git" || request.Directory != directory || request.Timeout != 30*time.Second || request.MaxBytes != 16<<20 {
		test.Fatalf("unsafe raw read request: %#v", request)
	}
	environment := "\n" + strings.Join(request.Env, "\n") + "\n"
	for _, required := range []string{"GIT_OPTIONAL_LOCKS=0", "GIT_NO_LAZY_FETCH=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_KEY_1=protocol.allow", "GIT_CONFIG_VALUE_1=never", "LC_ALL=C"} {
		if !strings.Contains(environment, "\n"+required+"\n") {
			test.Fatalf("raw read omitted environment guard %q", required)
		}
	}
}

type rawReadOperation struct {
	name    string
	command string
	read    func(*testing.T, *Client, context.Context, string) ([]byte, error)
}

func rawReadOperations() []rawReadOperation {
	return []rawReadOperation{
		{"inventory", "worktree", func(test *testing.T, client *Client, ctx context.Context, directory string) ([]byte, error) {
			parsed, raw, err := client.ListWorktreesRaw(ctx, directory)
			if err != nil && parsed != nil {
				test.Fatalf("failed inventory returned partial records: %#v", parsed)
			}
			return raw, err
		}},
		{"status", "status", func(test *testing.T, client *Client, ctx context.Context, directory string) ([]byte, error) {
			parsed, raw, err := client.StatusRaw(ctx, directory)
			if err != nil && parsed != (domain.GitStatus{}) {
				test.Fatalf("failed status returned partial counts: %#v", parsed)
			}
			return raw, err
		}},
		{"unstaged diff", "diff", func(test *testing.T, client *Client, ctx context.Context, directory string) ([]byte, error) {
			return client.Diff(ctx, directory, false)
		}},
		{"staged diff", "diff", func(test *testing.T, client *Client, ctx context.Context, directory string) ([]byte, error) {
			return client.Diff(ctx, directory, true)
		}},
	}
}
