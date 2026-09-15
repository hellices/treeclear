package process

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinTableBoundsNativeGrowthRetries(test *testing.T) {
	calls := 0
	_, err := readDarwinTable(context.Background(), func(buffer []unix.KinfoProc, size *uintptr) error {
		calls++
		if len(buffer) == 0 {
			*size = 2 * unix.SizeofKinfoProc
			return nil
		}
		return unix.ENOMEM
	})
	if !errors.Is(err, unix.ENOMEM) || calls != 6 {
		test.Fatalf("native growth: calls=%d error=%v", calls, err)
	}
}

func TestDarwinTableChecksCancellationBetweenNativeReads(test *testing.T) {
	for _, stage := range []string{"before", "sizing", "reading"} {
		test.Run(stage, func(test *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if stage == "before" {
				cancel()
			}
			calls := 0
			_, err := readDarwinTable(ctx, func(buffer []unix.KinfoProc, size *uintptr) error {
				calls++
				*size = 2 * unix.SizeofKinfoProc
				if stage == "sizing" || stage == "reading" && len(buffer) != 0 {
					cancel()
				}
				return nil
			})
			wanted := map[string]int{"before": 0, "sizing": 1, "reading": 2}[stage]
			if !errors.Is(err, context.Canceled) || calls != wanted {
				test.Fatalf("cancellation: calls=%d want=%d error=%v", calls, wanted, err)
			}
		})
	}
}

func TestDarwinTableRejectsSizesBeforeAllocation(test *testing.T) {
	for _, size := range []uintptr{0, 1, 2*unix.SizeofKinfoProc + 1, 65537 * unix.SizeofKinfoProc, ^uintptr(0)} {
		calls := 0
		_, err := readDarwinTable(context.Background(), func(buffer []unix.KinfoProc, actual *uintptr) error {
			calls++
			if len(buffer) != 0 {
				test.Fatal("invalid native size was allocated before validation")
			}
			*actual = size
			return nil
		})
		if err == nil || calls != 1 {
			test.Fatalf("invalid size %d: calls=%d error=%v", size, calls, err)
		}
	}
}

func TestDarwinTableRejectsInvalidReadResults(test *testing.T) {
	for _, size := range []uintptr{0, 1, 3 * unix.SizeofKinfoProc} {
		_, err := readDarwinTable(context.Background(), func(buffer []unix.KinfoProc, actual *uintptr) error {
			*actual = 2 * unix.SizeofKinfoProc
			if len(buffer) != 0 {
				*actual = size
			}
			return nil
		})
		if err == nil {
			test.Fatalf("native read with invalid size %d became complete", size)
		}
	}
}

func TestDarwinTableRetainsNativeErrors(test *testing.T) {
	for _, stage := range []int{1, 2} {
		calls := 0
		_, err := readDarwinTable(context.Background(), func(buffer []unix.KinfoProc, size *uintptr) error {
			calls++
			*size = 2 * unix.SizeofKinfoProc
			if calls == stage {
				return unix.EPERM
			}
			return nil
		})
		if !errors.Is(err, unix.EPERM) || calls != stage {
			test.Fatalf("native denial: calls=%d error=%v", calls, err)
		}
	}
}

func TestDarwinTableAcceptsWholeRecordShrink(test *testing.T) {
	records, err := readDarwinTable(context.Background(), func(buffer []unix.KinfoProc, size *uintptr) error {
		*size = 3 * unix.SizeofKinfoProc
		if len(buffer) != 0 {
			buffer[0].Proc.P_pid = 123
			*size = 2 * unix.SizeofKinfoProc
		}
		return nil
	})
	if err != nil || len(records) != 2 || records[0].Proc.P_pid != 123 {
		test.Fatalf("native read returned %d records, error=%v", len(records), err)
	}
}
