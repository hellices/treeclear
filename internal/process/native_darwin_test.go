package process

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
)

func TestNativeSourceUsesDarwin(test *testing.T) {
	if _, native := NativeSource().(DarwinSource); !native {
		test.Fatalf("native source = %T, want DarwinSource", NativeSource())
	}
}

func TestDarwinSourceBracketsInspectionWithFreshIdentitySnapshots(test *testing.T) {
	source, record, fixture := darwinFixture(test)
	var calls []string
	source.snapshot = func(context.Context) ([]darwinProcess, error) {
		calls = append(calls, "snapshot")
		return []darwinProcess{darwinKernel(), record}, nil
	}
	reader := source.reader
	source.reader = func(record darwinProcess) processReader {
		calls = append(calls, "inspect")
		return reader(record)
	}
	actual, err := source.List(context.Background())
	if err != nil || len(actual) != 1 || !actual[0].Inspectable || actual[0].PID != fixture.info.PID || !actual[0].CreatedAt.Equal(record.created) {
		test.Fatalf("List() = %#v, %v", actual, err)
	}
	if !reflect.DeepEqual(calls, []string{"snapshot", "inspect", "snapshot"}) {
		test.Fatalf("inspection order = %v", calls)
	}
}

func TestDarwinSourceRequiresPositiveKernelProof(test *testing.T) {
	for _, field := range []string{"parent", "uid", "flags", "state", "name", "creation", "duplicate", "missing"} {
		test.Run(field, func(test *testing.T) {
			source, record, _ := darwinFixture(test)
			kernel := darwinKernel()
			switch field {
			case "parent":
				kernel.parentPID = 1
			case "uid":
				kernel.uid = 501
			case "flags":
				kernel.flags = 0
			case "state":
				kernel.state = 5
			case "name":
				kernel.name = "ordinary"
			case "creation":
				kernel.created = time.Time{}
			}
			records := []darwinProcess{kernel, record}
			if field == "duplicate" {
				records = append(records, kernel)
			}
			if field == "missing" {
				records = records[1:]
			}
			source.snapshot = func(context.Context) ([]darwinProcess, error) { return records, nil }
			if _, err := source.List(context.Background()); err == nil {
				test.Fatal("ambiguous kernel evidence became complete enumeration")
			}
		})
	}
}

func TestDarwinSourceOnlyExcludesConfirmedZombies(test *testing.T) {
	for _, state := range []int8{1, 2, 3, 4, 5} {
		test.Run(strconv.Itoa(int(state)), func(test *testing.T) {
			source, record, _ := darwinFixture(test)
			record.state, record.flags = state, 0x200|0x2000
			source.snapshot = func(context.Context) ([]darwinProcess, error) {
				return []darwinProcess{darwinKernel(), record}, nil
			}
			actual, err := source.List(context.Background())
			want := 1
			if state == 5 {
				want = 0
			}
			if err != nil || len(actual) != want {
				test.Fatalf("state %d: records=%d, error=%v", state, len(actual), err)
			}
		})
	}
}

func TestDarwinSourceDiscardsWholeChangedAttempts(test *testing.T) {
	for _, change := range []string{"birth", "exit", "reuse", "owner", "zombie", "name", "kernel"} {
		test.Run(change, func(test *testing.T) {
			source, initial, _ := darwinFixture(test)
			changed := initial
			changedRecords := []darwinProcess{darwinKernel(), changed}
			switch change {
			case "birth":
				changed.pid++
				changedRecords = append(changedRecords, changed)
			case "exit":
				changed.pid++
				changedRecords[1] = changed
			case "reuse":
				changed.created = changed.created.Add(time.Microsecond)
				changedRecords[1] = changed
			case "owner":
				changed.uid++
				changedRecords[1] = changed
			case "zombie":
				changed.state = 5
				changedRecords[1] = changed
			case "name":
				changed.name = "new-image"
				changedRecords[1] = changed
			case "kernel":
				changedRecords[0].created = changedRecords[0].created.Add(time.Microsecond)
			}
			calls := 0
			source.snapshot = func(context.Context) ([]darwinProcess, error) {
				calls++
				if calls == 1 {
					return []darwinProcess{darwinKernel(), initial}, nil
				}
				return changedRecords, nil
			}
			reader := source.reader
			source.reader = func(record darwinProcess) processReader {
				value := reader(record).(*fakeProcess)
				value.info.CommandLine = []string{fmt.Sprintf("attempt-%d", calls)}
				return value
			}
			actual, err := source.List(context.Background())
			if err != nil || calls != 4 {
				test.Fatalf("changed snapshot: calls=%d, error=%v", calls, err)
			}
			want := len(changedRecords) - 1
			if change == "zombie" {
				want = 0
			}
			if len(actual) != want {
				test.Fatalf("records=%d, want %d", len(actual), want)
			}
			for _, info := range actual {
				if !reflect.DeepEqual(info.CommandLine, []string{"attempt-3"}) {
					test.Fatal("inspection from a discarded attempt was retained")
				}
			}
		})
	}
}

func TestDarwinSourceDoesNotRetryAwayInspectionDenial(test *testing.T) {
	for _, owner := range []uint32{0, 501, 502} {
		test.Run(strconv.FormatUint(uint64(owner), 10), func(test *testing.T) {
			source, record, fixture := darwinFixture(test)
			record.uid = owner
			source.snapshot = func(context.Context) ([]darwinProcess, error) { return []darwinProcess{darwinKernel(), record}, nil }
			reader := source.reader
			source.reader = func(record darwinProcess) processReader {
				value := reader(record).(*fakeProcess)
				value.info.CWD = ""
				value.failures = map[string]error{"cwd": errors.New("access denied")}
				return value
			}
			collection, errs := (Collector{Source: source}).Collect(context.Background(), fixture.worktrees())
			if len(errs) == 0 || len(collection.GlobalUnknown) != 1 || collection.GlobalUnknown[0].State != domain.EvidenceUnknown {
				test.Fatalf("owner %d lost global uncertainty", owner)
			}
		})
	}
}

func TestDarwinSourceBoundsChurnAndPreservesLastEvidence(test *testing.T) {
	source, record, _ := darwinFixture(test)
	calls := 0
	source.snapshot = func(context.Context) ([]darwinProcess, error) {
		calls++
		record.created = record.created.Add(time.Microsecond)
		return []darwinProcess{darwinKernel(), record}, nil
	}
	actual, err := source.List(context.Background())
	if err == nil || calls != 6 || len(actual) != 1 || !actual[0].CreatedAt.Equal(record.created.Add(-time.Microsecond)) {
		test.Fatalf("churn: snapshots=%d records=%d error=%v", calls, len(actual), err)
	}
}

func TestDarwinSourceRejectsInvalidSnapshots(test *testing.T) {
	for _, invalid := range []string{"empty", "no users", "duplicate", "negative pid", "missing time", "bad name", "bad state", "too many", "before error", "after error"} {
		test.Run(invalid, func(test *testing.T) {
			source, record, _ := darwinFixture(test)
			records := []darwinProcess{darwinKernel(), record}
			switch invalid {
			case "empty":
				records = nil
			case "no users":
				records = records[:1]
			case "duplicate":
				records = append(records, record)
			case "negative pid":
				records[1].pid = -1
			case "missing time":
				records[1].created = time.Time{}
			case "bad name":
				records[1].name = "bad\x00name"
			case "bad state":
				records[1].state = 0
			case "too many":
				records = make([]darwinProcess, 65537)
			}
			calls := 0
			source.snapshot = func(context.Context) ([]darwinProcess, error) {
				calls++
				if invalid == "before error" || invalid == "after error" && calls == 2 {
					return records, errors.New("native enumeration unavailable")
				}
				return records, nil
			}
			if _, err := source.List(context.Background()); err == nil {
				test.Fatal("invalid native snapshot became complete")
			}
		})
	}
}

func TestDarwinSourceCancellationAndDeadline(test *testing.T) {
	for _, stage := range []string{"before", "inspection", "after"} {
		test.Run(stage, func(test *testing.T) {
			source, record, _ := darwinFixture(test)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			source.snapshot = func(ctx context.Context) ([]darwinProcess, error) {
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 20*time.Second {
					test.Fatal("native collection lacks a bounded total deadline")
				}
				calls++
				if stage == "after" && calls == 2 {
					cancel()
				}
				return []darwinProcess{darwinKernel(), record}, nil
			}
			if stage == "before" {
				cancel()
			}
			reader := source.reader
			source.reader = func(record darwinProcess) processReader {
				if stage == "inspection" {
					cancel()
				}
				return reader(record)
			}
			if _, err := source.List(ctx); !errors.Is(err, context.Canceled) {
				test.Fatalf("cancellation became success: %v", err)
			}
		})
	}
}

func darwinKernel() darwinProcess {
	return darwinProcess{pid: 0, uid: 0, flags: 0x200, state: 2, name: "kernel_task", created: time.Unix(10, 0).UTC()}
}

func darwinFixture(test *testing.T) (DarwinSource, darwinProcess, processFixture) {
	test.Helper()
	fixture := newProcessFixture(test)
	record := darwinProcess{pid: fixture.info.PID, parentPID: 1, uid: 501, name: "fixture", state: 2, created: fixture.info.CreatedAt.Add(123 * time.Microsecond)}
	source := DarwinSource{
		snapshot:   func(context.Context) ([]darwinProcess, error) { return []darwinProcess{darwinKernel(), record}, nil },
		currentUID: func() int { return 501 },
		reader: func(record darwinProcess) processReader {
			info := fixture.info
			info.PID, info.CreatedAt, info.Owner = record.pid, record.created, strconv.FormatUint(uint64(record.uid), 10)
			return &fakeProcess{info: info, name: record.name}
		},
	}
	return source, record, fixture
}

func TestDarwinSourceOwnerFailureIsGlobal(test *testing.T) {
	source, _, _ := darwinFixture(test)
	source.currentUID = func() int { return -1 }
	if _, err := source.List(context.Background()); err == nil || !strings.Contains(err.Error(), "owner") {
		test.Fatalf("invalid caller owner: %v", err)
	}
}
