package process

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/pathutil"
	gprocess "github.com/shirou/gopsutil/v4/process"
)

func TestGopsutilSourceCurrentProcess(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pid := int32(os.Getpid())
	native := &gprocess.Process{Pid: pid}
	source := GopsutilSource{
		processes: func(context.Context) ([]processHandle, error) {
			return []processHandle{{pid: pid, reader: native}}, nil
		},
		pids: func(context.Context) ([]int32, error) { return []int32{pid}, nil },
	}
	actual, err := source.List(ctx)
	if err != nil || len(actual) != 1 {
		test.Fatalf("List(current process) = %#v, %v", actual, err)
	}
	info := actual[0]
	if !info.Inspectable || info.Error != "" || info.PID != pid || info.OwnerRelation != OwnerSame {
		test.Fatalf("current process inspection = %#v", info)
	}
	if info.CreatedAt.IsZero() || info.CreatedAt.After(time.Now()) || len(info.CommandLine) == 0 {
		test.Fatalf("missing native process identity = %#v", info)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		test.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		test.Fatal(err)
	}
	for expected, observed := range map[string]string{workingDirectory: info.CWD, executable: info.Executable} {
		want, err := pathutil.Canonical(expected)
		if err != nil {
			test.Fatal(err)
		}
		got, err := pathutil.Canonical(observed)
		if err != nil || got != want {
			test.Errorf("native path = %q, %v; want %q", got, err, want)
		}
	}
}
