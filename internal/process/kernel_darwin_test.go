package process

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinKernelMetadataContract(test *testing.T) {
	records, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		test.Fatalf("native process enumeration failed: %v", err)
	}
	found := 0
	for _, record := range records {
		if record.Proc.P_pid != 0 {
			continue
		}
		found++
		if record.Eproc.Ppid != 0 || record.Eproc.Ucred.Uid != 0 || record.Proc.P_flag&0x200 == 0 || record.Proc.P_stat != 2 || unix.ByteSliceToString(record.Proc.P_comm[:]) != "kernel_task" {
			test.Fatal("PID 0 lacks the native kernel-only identity contract")
		}
		if record.Proc.P_starttime.Sec <= 0 || record.Proc.P_starttime.Usec < 0 || record.Proc.P_starttime.Usec >= 1000000 {
			test.Fatal("native kernel creation identity is invalid")
		}
	}
	if found != 1 {
		test.Fatalf("native kernel record count = %d, want 1", found)
	}
}
