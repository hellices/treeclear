package process

import (
	"context"
	"errors"
	"os/user"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestGopsutilSourceReadsEveryFieldAndRetainsPartialIdentity(test *testing.T) {
	for _, failingField := range []string{"", "creation time", "executable", "cwd", "command line", "owner", "name"} {
		test.Run(failingField, func(test *testing.T) {
			fixture := newProcessFixture(test)
			fixture.info.CommandLine = []string{fixture.info.Executable, fixture.child}
			reader := &fakeProcess{info: fixture.info, name: "fixture", failures: map[string]error{}}
			if failingField != "" {
				reader.failures[failingField] = errors.New("access denied")
			}
			source := syntheticGopsutilSource(reader)
			actual, err := source.List(context.Background())
			if err != nil || len(actual) != 1 {
				test.Fatalf("List() = %#v, %v; inspection errors must retain complete enumeration", actual, err)
			}
			info := actual[0]
			if info.PID != reader.info.PID || !info.CreatedAt.Equal(reader.info.CreatedAt) || info.Executable != reader.info.Executable || info.CWD != reader.info.CWD || info.Owner != reader.info.Owner || !reflect.DeepEqual(info.CommandLine, reader.info.CommandLine) {
				test.Fatalf("available identity was lost: %#v", info)
			}
			if info.Inspectable != (failingField == "") {
				test.Fatalf("Inspectable = %v for failed field %q", info.Inspectable, failingField)
			}
			if failingField != "" && (!strings.Contains(info.Error, failingField) || !strings.Contains(info.Error, "access denied")) {
				test.Fatalf("inspection error was lost: %#v", info)
			}
			if failingField == "owner" && info.OwnerRelation != OwnerUnknown {
				test.Fatalf("failed lookup became a proven owner relation: %#v", info)
			}
			wantCalls := []string{"creation time", "executable", "cwd", "command line", "owner", "name"}
			if !reflect.DeepEqual(reader.calls, wantCalls) {
				test.Fatalf("property reads = %v, want %v", reader.calls, wantCalls)
			}
		})
	}
}

func TestGopsutilSourceOwnerLookupIsTriState(test *testing.T) {
	tests := []struct {
		name         string
		owner        string
		currentOwner string
		currentError error
		want         OwnerRelation
	}{
		{"same", "fixture-user", "fixture-user", nil, OwnerSame},
		{"other", "other-user", "fixture-user", nil, OwnerOther},
		{"unknown process owner", "", "fixture-user", nil, OwnerUnknown},
		{"unknown current owner", "fixture-user", "", nil, OwnerUnknown},
		{"failed current lookup", "other-user", "fixture-user", errors.New("lookup unavailable"), OwnerUnknown},
	}
	if runtime.GOOS == "windows" {
		tests = append(tests, struct {
			name         string
			owner        string
			currentOwner string
			currentError error
			want         OwnerRelation
		}{"Windows username case", `DOMAIN\Fixture`, `domain\fixture`, nil, OwnerSame})
	}
	for _, entry := range tests {
		test.Run(entry.name, func(test *testing.T) {
			fixture := newProcessFixture(test)
			reader := &fakeProcess{info: fixture.info, name: "fixture"}
			reader.info.Owner = entry.owner
			source := syntheticGopsutilSource(reader)
			source.currentUser = func() (*user.User, error) {
				return &user.User{Username: entry.currentOwner}, entry.currentError
			}
			actual, err := source.List(context.Background())
			if err != nil || len(actual) != 1 || actual[0].OwnerRelation != entry.want {
				test.Fatalf("List() = %#v, %v; want relation %q", actual, err, entry.want)
			}
			if entry.want == OwnerUnknown && (actual[0].Error == "" || actual[0].Inspectable) {
				test.Fatalf("unknown ownership was returned as successful inspection: %#v", actual[0])
			}
		})
	}
}

func TestGopsutilSourceRejectsEmptySuccessfulIdentityReads(test *testing.T) {
	for _, field := range []string{"creation time", "executable", "cwd", "name"} {
		test.Run(field, func(test *testing.T) {
			fixture := newProcessFixture(test)
			reader := &fakeProcess{info: fixture.info, name: "fixture"}
			switch field {
			case "creation time":
				reader.info.CreatedAt = time.Time{}
			case "executable":
				reader.info.Executable = ""
			case "cwd":
				reader.info.CWD = ""
			case "name":
				reader.name = ""
			}
			actual, err := syntheticGopsutilSource(reader).List(context.Background())
			if err != nil || len(actual) != 1 || actual[0].Inspectable || !strings.Contains(actual[0].Error, field) {
				test.Fatalf("empty %s became a successful process: %#v, %v", field, actual, err)
			}
		})
	}
}

func TestGopsutilSourceRejectsMalformedReads(test *testing.T) {
	for _, field := range []string{"relative executable", "nul executable", "relative cwd", "nul cwd", "command line", "owner", "name"} {
		test.Run(field, func(test *testing.T) {
			fixture := newProcessFixture(test)
			reader := &fakeProcess{info: fixture.info, name: "fixture"}
			switch field {
			case "relative executable":
				reader.info.Executable = "program"
			case "nul executable":
				reader.info.Executable += "\x00"
			case "relative cwd":
				reader.info.CWD = "."
			case "nul cwd":
				reader.info.CWD += "\x00"
			case "command line":
				reader.info.CommandLine = []string{"program", "bad\x00argument"}
			case "owner":
				reader.info.Owner += "\x00"
			case "name":
				reader.name += "\x00"
			}
			actual, err := syntheticGopsutilSource(reader).List(context.Background())
			if err != nil || len(actual) != 1 || actual[0].Inspectable || actual[0].Error == "" {
				test.Fatalf("malformed %s became inspectable: %#v, %v", field, actual, err)
			}
			if field == "owner" && actual[0].OwnerRelation != OwnerUnknown {
				test.Fatalf("malformed owner became a proven relation: %#v", actual[0])
			}
		})
	}
}

func TestGopsutilSourceRetainsPIDsWhenCanceledAfterEnumeration(test *testing.T) {
	fixture := newProcessFixture(test)
	reader := &fakeProcess{info: fixture.info, name: "fixture"}
	source := syntheticGopsutilSource(reader)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source.processes = func(context.Context) ([]processHandle, error) {
		cancel()
		return []processHandle{{pid: reader.info.PID, reader: reader}}, nil
	}
	actual, err := source.List(ctx)
	if !errors.Is(err, context.Canceled) || len(actual) != 1 || actual[0].PID != reader.info.PID || actual[0].Inspectable || actual[0].Error == "" {
		test.Fatalf("canceled enumeration lost its PID or uncertainty: %#v, %v", actual, err)
	}
	if len(reader.calls) != 0 {
		test.Fatalf("continued inspection after cancellation: %v", reader.calls)
	}
}

func TestGopsutilSourceDetectsMissingPIDs(test *testing.T) {
	fixture := newProcessFixture(test)
	reader := &fakeProcess{info: fixture.info, name: "fixture"}
	source := syntheticGopsutilSource(reader)
	source.pids = func(context.Context) ([]int32, error) {
		return []int32{reader.info.PID, 99}, nil
	}
	actual, err := source.List(context.Background())
	if err == nil || len(actual) != 2 {
		test.Fatalf("missing PID was silently dropped: %#v, %v", actual, err)
	}
	missing := actual[1]
	if missing.PID != 99 || missing.Inspectable || missing.OwnerRelation != OwnerUnknown || missing.Error == "" {
		test.Fatalf("missing PID identity = %#v", missing)
	}
	collection, errs := (Collector{Source: fakeSource{processes: actual, err: err}}).Collect(context.Background(), fixture.worktrees())
	assertIncomplete(test, collection, errs)
	if collection.Uninspectable[99].State != "unknown" {
		test.Fatalf("missing PID was not retained for future bindings: %#v", collection.Uninspectable)
	}
}

func TestGopsutilSourcePreservesEnumerationFailures(test *testing.T) {
	for _, failed := range []string{"processes", "pids", "empty", "nil reader", "nil current user"} {
		test.Run(failed, func(test *testing.T) {
			fixture := newProcessFixture(test)
			reader := &fakeProcess{info: fixture.info, name: "fixture"}
			source := syntheticGopsutilSource(reader)
			failure := errors.New("enumeration unavailable")
			switch failed {
			case "processes":
				source.processes = func(context.Context) ([]processHandle, error) {
					return []processHandle{{pid: reader.info.PID, reader: reader}}, failure
				}
			case "pids":
				source.pids = func(context.Context) ([]int32, error) { return nil, failure }
			case "empty":
				source.processes = func(context.Context) ([]processHandle, error) { return nil, nil }
				source.pids = func(context.Context) ([]int32, error) { return nil, nil }
			case "nil reader":
				source.processes = func(context.Context) ([]processHandle, error) {
					return []processHandle{{pid: reader.info.PID}}, nil
				}
			case "nil current user":
				source.currentUser = func() (*user.User, error) { return nil, nil }
			}
			actual, err := source.List(context.Background())
			if failed == "nil current user" {
				if err != nil || len(actual) != 1 || actual[0].OwnerRelation != OwnerUnknown || actual[0].Inspectable {
					test.Fatalf("nil current user became other-user or inspectable: %#v, %v", actual, err)
				}
				return
			}
			if err == nil {
				test.Fatalf("incomplete enumeration returned success: %#v", actual)
			}
			if (failed == "processes" || failed == "pids") && (!errors.Is(err, failure) || len(actual) != 1) {
				test.Fatalf("partial result or original failure was lost: %#v, %v", actual, err)
			}
		})
	}
}

func TestGopsutilSourceHonorsCanceledContext(test *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := GopsutilSource{
		processes: func(context.Context) ([]processHandle, error) {
			test.Fatal("canceled source must not enumerate")
			return nil, nil
		},
	}
	if _, err := source.List(ctx); !errors.Is(err, context.Canceled) {
		test.Fatalf("List(canceled) error = %v", err)
	}
}

type fakeProcess struct {
	info     Info
	name     string
	failures map[string]error
	calls    []string
}

func (reader *fakeProcess) CreateTimeWithContext(context.Context) (int64, error) {
	reader.calls = append(reader.calls, "creation time")
	if reader.info.CreatedAt.IsZero() {
		return 0, reader.failures["creation time"]
	}
	return reader.info.CreatedAt.UnixMilli(), reader.failures["creation time"]
}

func (reader *fakeProcess) ExeWithContext(context.Context) (string, error) {
	reader.calls = append(reader.calls, "executable")
	return reader.info.Executable, reader.failures["executable"]
}

func (reader *fakeProcess) CwdWithContext(context.Context) (string, error) {
	reader.calls = append(reader.calls, "cwd")
	return reader.info.CWD, reader.failures["cwd"]
}

func (reader *fakeProcess) CmdlineSliceWithContext(context.Context) ([]string, error) {
	reader.calls = append(reader.calls, "command line")
	return reader.info.CommandLine, reader.failures["command line"]
}

func (reader *fakeProcess) UsernameWithContext(context.Context) (string, error) {
	reader.calls = append(reader.calls, "owner")
	return reader.info.Owner, reader.failures["owner"]
}

func (reader *fakeProcess) NameWithContext(context.Context) (string, error) {
	reader.calls = append(reader.calls, "name")
	return reader.name, reader.failures["name"]
}

func syntheticGopsutilSource(reader *fakeProcess) GopsutilSource {
	return GopsutilSource{
		processes: func(context.Context) ([]processHandle, error) {
			return []processHandle{{pid: reader.info.PID, reader: reader}}, nil
		},
		pids: func(context.Context) ([]int32, error) {
			return []int32{reader.info.PID}, nil
		},
		currentUser: func() (*user.User, error) {
			return &user.User{Username: "fixture-user"}, nil
		},
	}
}
