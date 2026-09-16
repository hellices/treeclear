package process

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gprocess "github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/unix"
)

func TestDarwinReaderPreservesExecutableErrno(test *testing.T) {
	const missingPID = int32(1<<31 - 1)
	reader := darwinReader{
		Process: &gprocess.Process{Pid: missingPID},
		record:  darwinProcess{pid: missingPID},
	}
	path, err := reader.ExeWithContext(test.Context())
	if path != "" || !errors.Is(err, unix.ESRCH) {
		test.Fatalf("executable lookup did not preserve native ESRCH (returned_path=%t): %v", path != "", err)
	}
}

func TestDarwinReaderReadsOwnedExecutable(test *testing.T) {
	expected, err := os.Executable()
	if err != nil {
		test.Fatal("locate owned test executable")
	}
	expected, err = filepath.EvalSymlinks(expected)
	if err != nil {
		test.Fatal("canonicalize owned test executable")
	}
	reader := darwinReader{
		Process: &gprocess.Process{Pid: int32(os.Getpid())},
		record:  darwinProcess{pid: int32(os.Getpid())},
	}
	actual, err := reader.ExeWithContext(test.Context())
	if err != nil {
		test.Fatal("read owned executable through native binding")
	}
	actual, err = filepath.EvalSymlinks(actual)
	if err != nil || actual != expected {
		test.Fatal("native binding returned a different executable")
	}
}

func TestDarwinExecutableReadValidatesNativeResults(test *testing.T) {
	maximumPath := "/" + strings.Repeat("a", darwinProcessPathLimit-2)
	for _, fixture := range []struct {
		name     string
		contents string
		count    int32
		errno    unix.Errno
		wantPath string
		wantErr  error
	}{
		{"valid", "/fixture/app", 12, 0, "/fixture/app", nil},
		{"stale-errno-on-success", "/fixture/app", 12, unix.EPERM, "/fixture/app", nil},
		{"maximum", maximumPath, int32(len(maximumPath)), 0, maximumPath, nil},
		{"permission", "", 0, unix.EPERM, "", unix.EPERM},
		{"missing-path", "", 0, unix.ENOENT, "", unix.ENOENT},
		{"missing-process", "", 0, unix.ESRCH, "", unix.ESRCH},
		{"negative-result", "", -1, unix.EINVAL, "", unix.EINVAL},
		{"missing-errno", "", 0, 0, "", unix.EIO},
		{"failed-buffer", "/fixture/app", 0, unix.EACCES, "", unix.EACCES},
		{"oversized", strings.Repeat("a", darwinProcessPathLimit), darwinProcessPathLimit, 0, "", nil},
		{"unterminated", "/tmpx", 4, 0, "", nil},
		{"early-terminator", "/t\x00p", 4, 0, "", nil},
		{"relative", "relative", 8, 0, "", nil},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			calls := 0
			read := func(pid int32, buffer []byte) (int32, unix.Errno) {
				calls++
				if pid != 123 || len(buffer) != darwinProcessPathLimit {
					test.Fatal("native call changed PID or allocation bound")
				}
				copy(buffer, fixture.contents)
				return fixture.count, fixture.errno
			}
			actual, err := readDarwinExecutable(test.Context(), 123, read)
			if calls != 1 || actual != fixture.wantPath || (err == nil) != (fixture.wantPath != "") || fixture.wantErr != nil && !errors.Is(err, fixture.wantErr) {
				test.Fatal("native result was accepted incorrectly or lost its errno")
			}
			if fixture.count <= 0 && fixture.errno == 0 && !strings.HasPrefix(err.Error(), "proc_pidpath errno 0:") {
				test.Fatal("fallback error was misreported as an OS-supplied errno")
			}
		})
	}
}

func TestDarwinExecutableReadRejectsInvalidCallsAndCancellation(test *testing.T) {
	for _, pid := range []int32{0, -1} {
		called := false
		_, err := readDarwinExecutable(test.Context(), pid, func(int32, []byte) (int32, unix.Errno) {
			called = true
			return 0, 0
		})
		if err == nil || called {
			test.Fatal("invalid PID reached native inspection")
		}
	}
	if _, err := readDarwinExecutable(test.Context(), 123, nil); err == nil {
		test.Fatal("missing native reader was accepted")
	}
	for _, during := range []bool{false, true} {
		ctx, cancel := context.WithCancel(test.Context())
		if !during {
			cancel()
		}
		calls := 0
		actual, err := readDarwinExecutable(ctx, 123, func(_ int32, buffer []byte) (int32, unix.Errno) {
			calls++
			copy(buffer, "/fixture/app")
			cancel()
			return 12, 0
		})
		cancel()
		if actual != "" || !errors.Is(err, context.Canceled) || (calls == 1) != during {
			test.Fatal("cancellation reached a late native call or returned usable evidence")
		}
	}
}
