package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
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

func TestDarwinSourceKeepsLiveUnlinkedExecutableUnknown(test *testing.T) {
	if os.Getuid() == 0 || os.Geteuid() != os.Getuid() {
		test.Fatal("owned executable fixture requires an unprivileged test driver")
	}
	root, err := filepath.EvalSymlinks(test.TempDir())
	if err != nil {
		test.Fatal("canonicalize temporary process fixture")
	}
	contents, err := os.ReadFile("/bin/sleep")
	if err != nil {
		test.Fatal("read system executable for an owned temporary copy")
	}
	executable := filepath.Join(root, "owned-sleep")
	if err := os.WriteFile(executable, contents, 0o700); err != nil {
		test.Fatal("create owned executable fixture")
	}
	child := exec.CommandContext(test.Context(), executable, "180")
	child.Dir, child.WaitDelay = root, time.Second
	child.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C", "HOME=" + root, "TMPDIR=" + root}
	if err := child.Start(); err != nil {
		test.Fatal("start owned fixture process")
	}
	test.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	before, err := unix.SysctlKinfoProc("kern.proc.pid", child.Process.Pid)
	if err != nil || before.Proc.P_pid != int32(child.Process.Pid) {
		test.Fatal("read owned process identity")
	}
	record := darwinProcess{
		pid: before.Proc.P_pid, parentPID: before.Eproc.Ppid, uid: before.Eproc.Ucred.Uid,
		created: time.Unix(before.Proc.P_starttime.Sec, int64(before.Proc.P_starttime.Usec)*int64(time.Microsecond)).UTC(),
		name:    unix.ByteSliceToString(before.Proc.P_comm[:]), state: before.Proc.P_stat, flags: before.Proc.P_flag,
	}
	reader := darwinReader{Process: &gprocess.Process{Pid: record.pid}, record: record}
	if path, err := reader.ExeWithContext(test.Context()); err != nil || path != executable {
		test.Fatal("owned executable was unavailable before unlink")
	}
	if err := os.Remove(executable); err != nil {
		test.Fatal("unlink only the owned temporary executable")
	}
	if path, err := reader.ExeWithContext(test.Context()); path != "" || !errors.Is(err, unix.ENOENT) {
		test.Fatal("live unlinked executable did not retain native ENOENT")
	}
	source := DarwinSource{snapshot: func(context.Context) ([]darwinProcess, error) {
		return []darwinProcess{darwinKernel(), record}, nil
	}}
	collection, failures := (Collector{Source: source}).Collect(test.Context(), []domain.Worktree{{Path: root}})
	if len(failures) == 0 || collection.Uninspectable[record.pid].State != domain.EvidenceUnknown || len(collection.ByWorktree[root]) != 1 || collection.ByWorktree[root][0].State != domain.EvidenceUnknown {
		test.Fatal("live process with an unavailable executable was dropped or became usable evidence")
	}
	after, err := unix.SysctlKinfoProc("kern.proc.pid", child.Process.Pid)
	if err != nil || after.Proc.P_pid != before.Proc.P_pid || after.Proc.P_starttime != before.Proc.P_starttime || after.Proc.P_stat == darwinZombie {
		test.Fatal("owned fixture process was no longer alive with the same creation identity")
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
