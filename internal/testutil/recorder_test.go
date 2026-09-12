package testutil

import (
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"sync"
	"testing"
)

func TestRecorderFailuresAndOrder(test *testing.T) {
	injected := fmt.Errorf("locked fixture: %w", fs.ErrPermission)
	failures := map[string]error{"remove": injected}
	recorder := NewRecorder(failures)
	failures["remove"] = errors.New("replacement")
	failures["inspect"] = fs.ErrNotExist
	if err := recorder.Record("inspect"); err != nil {
		test.Fatalf("Record(inspect) = %v, want nil", err)
	}
	if err := recorder.Record("remove"); err != injected || !errors.Is(err, fs.ErrPermission) {
		test.Fatalf("Record(remove) = %v, want original permission error", err)
	}
	if err := recorder.Record("inspect"); err != nil {
		test.Fatalf("second Record(inspect) = %v, want nil", err)
	}
	expected := []string{"inspect", "remove", "inspect"}
	operations := recorder.Operations()
	if !reflect.DeepEqual(operations, expected) {
		test.Fatalf("Operations() = %v, want %v", operations, expected)
	}
	operations[0] = "changed"
	operations = append(operations, "appended")
	if actual := recorder.Operations(); !reflect.DeepEqual(actual, expected) {
		test.Fatalf("Operations() exposed its backing slice: %v", actual)
	}
	if err := NewRecorder(nil).Record("no failure"); err != nil {
		test.Errorf("nil failure map: %v", err)
	}
	if actual := NewRecorder(nil).Operations(); len(actual) != 0 {
		test.Errorf("new recorder has operations: %v", actual)
	}

	test.Run("concurrent access", func(test *testing.T) {
		const workers = 12
		const records = 80
		recorder := NewRecorder(map[string]error{"denied": fs.ErrPermission})
		var group sync.WaitGroup
		for worker := range workers {
			group.Go(func() {
				for sequence := range records {
					operation := fmt.Sprintf("worker-%d:%d", worker, sequence)
					if err := recorder.Record(operation); err != nil {
						test.Errorf("Record(%q): %v", operation, err)
					}
					if err := recorder.Record("denied"); !errors.Is(err, fs.ErrPermission) {
						test.Errorf("Record(denied) = %v", err)
					}
					snapshot := recorder.Operations()
					if len(snapshot) > 0 {
						snapshot[0] = "private snapshot"
					}
				}
			})
		}
		group.Wait()
		operations := recorder.Operations()
		if len(operations) != workers*records*2 {
			test.Fatalf("recorded %d operations, want %d", len(operations), workers*records*2)
		}
		seen := make(map[string]bool)
		lastSequence := make(map[int]int)
		denied := 0
		for _, operation := range operations {
			if operation == "denied" {
				denied++
				continue
			}
			var worker, sequence int
			if _, err := fmt.Sscanf(operation, "worker-%d:%d", &worker, &sequence); err != nil {
				test.Fatalf("unexpected operation %q: %v", operation, err)
			}
			if seen[operation] || sequence != lastSequence[worker] {
				test.Errorf("duplicate or out-of-order operation %q", operation)
			}
			seen[operation] = true
			lastSequence[worker]++
		}
		if denied != workers*records || len(seen) != workers*records {
			test.Errorf("denied = %d, unique successful records = %d", denied, len(seen))
		}
	})
}
