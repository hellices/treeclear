package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/testutil"
	"golang.org/x/sys/unix"
)

type installedProbeRequest struct {
	Version         int       `json:"version"`
	Challenge       string    `json:"challenge"`
	CallerUID       uint32    `json:"caller_uid"`
	Roots           []string  `json:"roots"`
	ActiveRoot      string    `json:"active_root"`
	ActivePID       int32     `json:"active_pid"`
	ActiveCreatedAt time.Time `json:"active_created_at"`
}

type installedProbeReport struct {
	Version              int            `json:"version"`
	Challenge            string         `json:"challenge"`
	Platform             string         `json:"platform"`
	Architecture         string         `json:"architecture"`
	EffectiveUID         int            `json:"effective_uid"`
	RootCount            int            `json:"root_count"`
	RootsMatched         bool           `json:"roots_matched"`
	EnumerationComplete  bool           `json:"enumeration_complete"`
	ErrorCount           int            `json:"error_count"`
	RetainedErrorCount   int            `json:"retained_error_count"`
	UninspectableCount   int            `json:"uninspectable_count"`
	GlobalUnknownCount   int            `json:"global_unknown_count"`
	ScopedUnknownCount   int            `json:"scoped_unknown_count"`
	DeniedErrorCount     int            `json:"denied_error_count"`
	MissingPathCount     int            `json:"missing_path_count"`
	OtherErrorCount      int            `json:"other_error_count"`
	InvalidArgumentCount int            `json:"invalid_argument_count"`
	NativeReadErrorCount int            `json:"native_read_error_count"`
	ActiveMatched        bool           `json:"active_matched"`
	Complete             bool           `json:"complete"`
	FirstFailureStages   map[string]int `json:"first_failure_stages"`
	NativePathErrnos     map[string]int `json:"native_path_errnos"`
}

func TestInstalledProbeRejectsInvalidReports(test *testing.T) {
	challenge := strings.Repeat("12", 32)
	valid := installedProbeReport{
		Version: 1, Challenge: challenge, Platform: "darwin", Architecture: runtime.GOARCH,
		RootCount: 2, RootsMatched: true, EnumerationComplete: true, ActiveMatched: true, Complete: true,
	}
	encode := func(report installedProbeReport) []byte {
		encoded, err := json.Marshal(report)
		if err != nil {
			test.Fatal(err)
		}
		return encoded
	}
	encoded := encode(valid)
	if _, err := decodeInstalledProbeReport(encoded, challenge, 2); err != nil {
		test.Fatalf("valid report rejected: %v", err)
	}
	cases := map[string][]byte{
		"empty": nil, "truncated": encoded[:len(encoded)-1],
		"oversized": bytes.Repeat([]byte(" "), 16385),
		"duplicate": append([]byte(`{"version":1,`), encoded[1:]...),
		"unknown":   append([]byte(`{"secret":"never display this",`), encoded[1:]...),
		"trailing":  append(bytes.Clone(encoded), []byte("{}")...),
	}
	for name, mutate := range map[string]func(*installedProbeReport){
		"version":             func(report *installedProbeReport) { report.Version = 2 },
		"challenge":           func(report *installedProbeReport) { report.Challenge = strings.Repeat("ab", 32) },
		"non-root":            func(report *installedProbeReport) { report.EffectiveUID = 501 },
		"platform":            func(report *installedProbeReport) { report.Platform = "windows" },
		"architecture":        func(report *installedProbeReport) { report.Architecture = "invalid" },
		"roots":               func(report *installedProbeReport) { report.RootCount = 0 },
		"partial-enumeration": func(report *installedProbeReport) { report.EnumerationComplete = false; report.Complete = false },
		"missing-active":      func(report *installedProbeReport) { report.ActiveMatched = false; report.Complete = false },
		"hidden-errors":       func(report *installedProbeReport) { report.ErrorCount = 1; report.OtherErrorCount = 1 },
		"unknowns":            func(report *installedProbeReport) { report.GlobalUnknownCount = 1; report.Complete = false },
		"negative-count":      func(report *installedProbeReport) { report.ErrorCount = -1 },
		"invalid-counts":      func(report *installedProbeReport) { report.OtherErrorCount = 1 },
		"false-complete":      func(report *installedProbeReport) { report.Complete = false },
	} {
		changed := valid
		mutate(&changed)
		cases[name] = encode(changed)
	}
	for name, contents := range cases {
		test.Run(name, func(test *testing.T) {
			if _, err := decodeInstalledProbeReport(contents, challenge, 2); err == nil {
				test.Fatal("invalid or incomplete probe report passed")
			}
		})
	}
}

func TestInstalledProbeValidatesDiagnosticCounts(test *testing.T) {
	challenge := strings.Repeat("12", 32)
	partial := installedProbeReport{
		Version: 1, Challenge: challenge, Platform: "darwin", Architecture: runtime.GOARCH,
		RootCount: 2, RootsMatched: true, EnumerationComplete: true, ActiveMatched: true,
		ErrorCount: 1, OtherErrorCount: 1, FirstFailureStages: map[string]int{"cwd": 1},
	}
	decode := func(report installedProbeReport) (installedProbeReport, error) {
		contents, err := json.Marshal(report)
		if err != nil {
			test.Fatal(err)
		}
		return decodeInstalledProbeReport(contents, challenge, 2)
	}
	for _, kind := range []string{"other", "invalid-argument", "native-read"} {
		report := partial
		if kind == "invalid-argument" {
			report.OtherErrorCount, report.InvalidArgumentCount = 0, 1
		}
		if kind == "native-read" {
			report.OtherErrorCount, report.NativeReadErrorCount = 0, 1
		}
		actual, err := decode(report)
		if err == nil || actual.Version != 1 || actual.Complete || actual.FirstFailureStages["cwd"] != 1 {
			test.Errorf("valid %s diagnostics were lost or made incomplete evidence pass", kind)
		}
	}
	native := partial
	native.FirstFailureStages, native.NativePathErrnos = map[string]int{"executable": 1}, map[string]int{"ESRCH": 1}
	if actual, err := decode(native); err == nil || actual.Version != 1 || actual.NativePathErrnos["ESRCH"] != 1 {
		test.Fatal("valid native errno diagnostics were lost or passed incomplete collection")
	}
	for name, mutate := range map[string]func(*installedProbeReport){
		"unknown-native-code":    func(report *installedProbeReport) { report.NativePathErrnos = map[string]int{"private-host-data": 1} },
		"negative-native-count":  func(report *installedProbeReport) { report.NativePathErrnos = map[string]int{"ESRCH": -1} },
		"oversized-native-count": func(report *installedProbeReport) { report.NativePathErrnos = map[string]int{"ESRCH": 1048577} },
		"zero-native-count":      func(report *installedProbeReport) { report.NativePathErrnos = map[string]int{"ESRCH": 0} },
		"wrong-native-total":     func(report *installedProbeReport) { report.NativePathErrnos = map[string]int{"ESRCH": 2} },
		"wrong-native-scope":     func(report *installedProbeReport) { report.NativePathErrnos = map[string]int{"ESRCH": 1} },
		"unknown-stage":          func(report *installedProbeReport) { report.FirstFailureStages = map[string]int{"private-host-data": 1} },
		"negative-stage":         func(report *installedProbeReport) { report.FirstFailureStages = map[string]int{"cwd": -1} },
		"oversized-stage":        func(report *installedProbeReport) { report.FirstFailureStages = map[string]int{"cwd": 1048577} },
		"zero-stage":             func(report *installedProbeReport) { report.FirstFailureStages = map[string]int{"cwd": 0, "other": 1} },
		"wrong-total":            func(report *installedProbeReport) { report.FirstFailureStages = map[string]int{"cwd": 1, "other": 1} },
		"missing-stage":          func(report *installedProbeReport) { report.FirstFailureStages = nil },
		"negative-kind":          func(report *installedProbeReport) { report.InvalidArgumentCount = -1 },
		"oversized-kind":         func(report *installedProbeReport) { report.NativeReadErrorCount = 1048577 },
		"hidden-kind":            func(report *installedProbeReport) { report.NativeReadErrorCount = 1 },
	} {
		test.Run(name, func(test *testing.T) {
			report := partial
			mutate(&report)
			actual, err := decode(report)
			if err == nil || actual.Version != 0 || strings.Contains(err.Error(), "private-host-data") {
				test.Fatal("invalid diagnostics were retained or exposed")
			}
		})
	}
}

func decodeInstalledProbeReport(contents []byte, challenge string, roots int) (installedProbeReport, error) {
	var report installedProbeReport
	invalid := errors.New("invalid process probe report; raw output withheld")
	if len(contents) > 16384 {
		return report, invalid
	}
	contents = bytes.TrimSuffix(contents, []byte("\n"))
	if err := json.Unmarshal(contents, &report); err != nil {
		return installedProbeReport{}, invalid
	}
	canonical, err := json.Marshal(report)
	if err != nil || !bytes.Equal(contents, canonical) || report.Version != 1 || report.Challenge != challenge || report.EffectiveUID != 0 || report.Platform != "darwin" || report.Architecture != runtime.GOARCH {
		return installedProbeReport{}, invalid
	}
	for _, count := range []int{report.RootCount, report.ErrorCount, report.RetainedErrorCount, report.UninspectableCount, report.GlobalUnknownCount, report.ScopedUnknownCount, report.DeniedErrorCount, report.MissingPathCount, report.OtherErrorCount, report.InvalidArgumentCount, report.NativeReadErrorCount} {
		if count < 0 || count > 1048576 {
			return installedProbeReport{}, invalid
		}
	}
	if report.ErrorCount != report.DeniedErrorCount+report.MissingPathCount+report.OtherErrorCount+report.InvalidArgumentCount+report.NativeReadErrorCount {
		return installedProbeReport{}, invalid
	}
	stageTotal := 0
	for stage, count := range report.FirstFailureStages {
		switch stage {
		case "enumeration", "worktree-path", "creation-time", "executable", "cwd", "command-line", "owner", "name", "inspection", "containment", "other":
		default:
			return installedProbeReport{}, invalid
		}
		if count <= 0 || count > 1048576 {
			return installedProbeReport{}, invalid
		}
		stageTotal += count
	}
	if stageTotal != report.ErrorCount {
		return installedProbeReport{}, invalid
	}
	nativeTotal := 0
	for code, count := range report.NativePathErrnos {
		switch code {
		case "unavailable", "EPERM", "ENOENT", "ESRCH", "EIO", "EBADF", "ENOMEM", "EACCES", "EBUSY", "ENOTDIR", "EINVAL", "EAGAIN", "ENOTSUP", "ENAMETOOLONG", "EOVERFLOW", "other":
		default:
			return installedProbeReport{}, invalid
		}
		if count <= 0 || count > 1048576 {
			return installedProbeReport{}, invalid
		}
		nativeTotal += count
	}
	if nativeTotal > report.FirstFailureStages["executable"] {
		return installedProbeReport{}, invalid
	}
	complete := report.RootsMatched && report.RootCount == roots && report.EnumerationComplete && report.ErrorCount == 0 && report.RetainedErrorCount == 0 && report.UninspectableCount == 0 && report.GlobalUnknownCount == 0 && report.ScopedUnknownCount == 0 && report.ActiveMatched
	if complete != report.Complete {
		return installedProbeReport{}, invalid
	}
	if !complete {
		return report, errors.New("native process/path collection is incomplete")
	}
	return report, nil
}

func TestInstalledProcessProbeRefusesUnprivilegedExecution(test *testing.T) {
	if os.Getuid() == 0 || os.Geteuid() == 0 {
		test.Fatal("the installed probe test driver must remain unprivileged")
	}
	binary, checksum := installProcessProbe(test)
	home := test.TempDir()
	for _, arguments := range [][]string{nil, {"--unexpected"}} {
		result, err := runProcessProbe(test.Context(), binary, arguments, strings.NewReader("{}"), home)
		if err == nil || result.ExitCode != 2 || len(result.Stdout) != 0 || string(result.Stderr) != "process probe refused or failed protocol validation\n" {
			test.Fatal("installed probe did not refuse unprivileged/unsupported execution")
		}
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		test.Fatal("installed probe wrote user state")
	}
	if readProbeChecksum(test, binary) != checksum {
		test.Fatal("installed probe bytes changed")
	}
}

func TestAuthorizedProcessProbe(test *testing.T) {
	if os.Getenv("TREECLEAR_TEST_PROCESS_PROBE") == "" {
		test.Skip("requires explicitly opted-in manual hosted-macOS CI")
	}
	if !hostedProcessProbeAllowed(os.Getenv) || os.Getuid() == 0 || os.Geteuid() != os.Getuid() {
		test.Fatal("probe requires explicit manual hosted-macOS CI and an ordinary-user test driver")
	}
	binary, checksum := installProcessProbe(test)
	home := test.TempDir()
	test.Logf("installed test-only probe platform=darwin architecture=%s sha256=%x", runtime.GOARCH, checksum)
	test.Run("root-side-expiry", func(test *testing.T) {
		input, heldOpen, err := os.Pipe()
		if err != nil {
			test.Fatal("create private probe input pipe")
		}
		defer input.Close()
		defer heldOpen.Close()
		result, err := runProcessProbe(test.Context(), "/usr/bin/sudo", []string{"-n", "--", binary}, input, home)
		if err == nil || result.ExitCode != 124 || len(result.Stdout) != 0 || len(result.Stderr) != 0 {
			test.Fatalf("root-side expiry not established: exit=%d stdout_bytes=%d stderr_bytes=%d; raw output withheld", result.ExitCode, len(result.Stdout), len(result.Stderr))
		}
	})
	if test.Failed() {
		return
	}
	repository := testutil.NewRepository(test)
	active := repository.AddWorktree(test, "active with spaces 한글", "probe-active")
	sleeper := exec.CommandContext(test.Context(), "/bin/sleep", "180")
	sleeper.Dir, sleeper.Env, sleeper.WaitDelay = active, probeEnvironment(home), time.Second
	if err := sleeper.Start(); err != nil {
		test.Fatal("start owned probe fixture process")
	}
	test.Cleanup(func() {
		_ = sleeper.Process.Kill()
		_ = sleeper.Wait()
	})
	created := ownedProbeProcessCreation(test, sleeper.Process.Pid)
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		test.Fatal("generate private probe challenge")
	}
	request := installedProbeRequest{
		Version: 1, Challenge: hex.EncodeToString(challenge), CallerUID: uint32(os.Getuid()),
		Roots: []string{repository.Root, active}, ActiveRoot: active,
		ActivePID: int32(sleeper.Process.Pid), ActiveCreatedAt: created,
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		test.Fatal("encode private probe request")
	}
	before := processProbeFixtureDigest(test, filepath.Dir(repository.Root))
	result, runErr := runProcessProbe(test.Context(), "/usr/bin/sudo", []string{"-n", "--", binary}, bytes.NewReader(encoded), home)
	if !reflect.DeepEqual(before, processProbeFixtureDigest(test, filepath.Dir(repository.Root))) {
		test.Fatal("probe changed fixture contents, modes, indexes, registrations, branches or ownership")
	}
	if !ownedProbeProcessCreation(test, sleeper.Process.Pid).Equal(created) {
		test.Fatal("owned fixture process identity changed")
	}
	if readProbeChecksum(test, binary) != checksum {
		test.Fatal("installed probe bytes changed")
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		test.Fatal("probe wrote ordinary-user state")
	}
	report, reportErr := decodeInstalledProbeReport(result.Stdout, request.Challenge, len(request.Roots))
	if report.Version == 1 {
		test.Logf("native process/path complete=%v enumeration=%v active_identity=%v errors=%d retained_errors=%d uninspectable=%d global_unknown=%d scoped_unknown=%d denied_error_strings=%d missing_path_error_strings=%d other_error_strings=%d invalid_argument_error_strings=%d native_read_error_strings=%d first_failure_stages=%v native_path_errnos=%v", report.Complete, report.EnumerationComplete, report.ActiveMatched, report.ErrorCount, report.RetainedErrorCount, report.UninspectableCount, report.GlobalUnknownCount, report.ScopedUnknownCount, report.DeniedErrorCount, report.MissingPathCount, report.OtherErrorCount, report.InvalidArgumentCount, report.NativeReadErrorCount, report.FirstFailureStages, report.NativePathErrnos)
	}
	if runErr != nil || result.ExitCode != 0 || len(result.Stderr) != 0 || reportErr != nil || !report.Complete {
		test.Fatalf("native process/path feasibility not established: exit=%d stdout_bytes=%d stderr_bytes=%d report_error=%v; raw output withheld", result.ExitCode, len(result.Stdout), len(result.Stderr), reportErr)
	}
	test.Log("process/path feasibility only; this is not installed treeclear scan/plan or desktop permission qualification")
}

func hostedProcessProbeAllowed(getenv func(string) string) bool {
	return getenv("TREECLEAR_TEST_PROCESS_PROBE") == "1" &&
		getenv("GITHUB_ACTIONS") == "true" &&
		getenv("GITHUB_EVENT_NAME") == "workflow_dispatch" &&
		getenv("RUNNER_ENVIRONMENT") == "github-hosted" &&
		getenv("RUNNER_OS") == "macOS" &&
		getenv("GITHUB_JOB") == "process-visibility-probe"
}

func TestAuthorizedProbeRequiresManualHostedCI(test *testing.T) {
	valid := map[string]string{
		"TREECLEAR_TEST_PROCESS_PROBE": "1", "GITHUB_ACTIONS": "true",
		"GITHUB_EVENT_NAME": "workflow_dispatch", "RUNNER_ENVIRONMENT": "github-hosted",
		"RUNNER_OS": "macOS", "GITHUB_JOB": "process-visibility-probe",
	}
	if !hostedProcessProbeAllowed(func(name string) string { return valid[name] }) {
		test.Fatal("explicit manual hosted CI was rejected")
	}
	cases := map[string]map[string]string{"local": {"TREECLEAR_TEST_PROCESS_PROBE": "1"}, "absent": {}}
	for name := range valid {
		missing := maps.Clone(valid)
		delete(missing, name)
		cases["missing-"+name] = missing
	}
	for name, field := range map[string][2]string{
		"wrong-opt-in": {"TREECLEAR_TEST_PROCESS_PROBE", "true"},
		"push":         {"GITHUB_EVENT_NAME", "push"},
		"pull-request": {"GITHUB_EVENT_NAME", "pull_request"},
		"scheduled":    {"GITHUB_EVENT_NAME", "schedule"},
		"self-hosted":  {"RUNNER_ENVIRONMENT", "self-hosted"},
		"windows":      {"RUNNER_OS", "Windows"},
		"linux":        {"RUNNER_OS", "Linux"},
		"ordinary-job": {"GITHUB_JOB", "verify"},
		"not-actions":  {"GITHUB_ACTIONS", "false"},
	} {
		changed := maps.Clone(valid)
		changed[field[0]] = field[1]
		cases[name] = changed
	}
	for name, environment := range cases {
		test.Run(name, func(test *testing.T) {
			if hostedProcessProbeAllowed(func(key string) string { return environment[key] }) {
				test.Fatal("unintended execution context can reach the privileged probe")
			}
		})
	}
}

func installProcessProbe(test *testing.T) (string, [32]byte) {
	test.Helper()
	installation := newInstallFixture(test)
	installation.environment["GOBIN"] = filepath.Join(test.TempDir(), "probe install 한글", "bin")
	if err := os.MkdirAll(installation.environment["GOBIN"], 0o700); err != nil {
		test.Fatal("create private probe installation directory")
	}
	ctx, cancel := context.WithTimeout(test.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "install", "-trimpath", "./tests/processprobe")
	command.Dir = filepath.Join("..", "..")
	command.Env = execx.SanitizedEnvironment(os.Environ(), installation.environment)
	command.WaitDelay = time.Second
	if output, err := command.CombinedOutput(); err != nil {
		test.Fatalf("install test-only process probe: %v\n%s", err, output)
	}
	binary, err := filepath.EvalSymlinks(filepath.Join(installation.environment["GOBIN"], "processprobe"))
	if err != nil {
		test.Fatal("resolve installed probe")
	}
	return binary, readProbeChecksum(test, binary)
}

func readProbeChecksum(test *testing.T, binary string) [32]byte {
	test.Helper()
	metadata, err := os.Lstat(binary)
	if err != nil || !metadata.Mode().IsRegular() || metadata.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		test.Fatal("probe must be an ordinary regular executable, never setuid/setgid")
	}
	owner, valid := metadata.Sys().(*syscall.Stat_t)
	if !valid || owner.Uid != uint32(os.Getuid()) {
		test.Fatal("probe installation must remain ordinary-user-owned")
	}
	contents, err := os.ReadFile(binary)
	if err != nil {
		test.Fatal("read exact installed probe bytes")
	}
	return sha256.Sum256(contents)
}

func ownedProbeProcessCreation(test *testing.T, pid int) time.Time {
	test.Helper()
	record, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || record == nil || record.Proc.P_pid != int32(pid) || record.Eproc.Ucred.Uid != uint32(os.Getuid()) || record.Proc.P_starttime.Sec <= 0 || record.Proc.P_starttime.Usec < 0 || record.Proc.P_starttime.Usec >= 1000000 {
		test.Fatal("cannot establish owned fixture process creation identity")
	}
	return time.Unix(record.Proc.P_starttime.Sec, int64(record.Proc.P_starttime.Usec)*int64(time.Microsecond)).UTC()
}

func processProbeFixtureDigest(test *testing.T, root string) map[string]string {
	test.Helper()
	digests := installedFixtureDigest(test, root)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		metadata, err := entry.Info()
		if err != nil {
			return err
		}
		owner, valid := metadata.Sys().(*syscall.Stat_t)
		if !valid || owner.Uid != uint32(os.Getuid()) {
			return errors.New("fixture ownership does not match the ordinary test user")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		digests[relative] += fmt.Sprintf(" uid=%d gid=%d", owner.Uid, owner.Gid)
		return nil
	})
	if err != nil {
		test.Fatal("cannot validate temporary fixture ownership")
	}
	return digests
}

func probeEnvironment(home string) []string {
	return []string{"HOME=" + home, "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C", "LC_ALL=C", "GOTRACEBACK=none"}
}

func runProcessProbe(parent context.Context, binary string, arguments []string, input io.Reader, home string) (execx.Result, error) {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Dir, command.Env, command.Stdin, command.WaitDelay = "/", probeEnvironment(home), input, time.Second
	stdout, stderr := probeLimitedOutput{cancel: cancel}, probeLimitedOutput{cancel: cancel}
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	result := execx.Result{Stdout: stdout.buffer.Bytes(), Stderr: stderr.buffer.Bytes(), ExitCode: -1}
	if command.ProcessState != nil {
		result.ExitCode = command.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return result, err
}

type probeLimitedOutput struct {
	buffer bytes.Buffer
	cancel context.CancelFunc
}

func (output *probeLimitedOutput) Write(contents []byte) (int, error) {
	remaining := 16384 - output.buffer.Len()
	if len(contents) > remaining {
		output.cancel()
		written, _ := output.buffer.Write(contents[:remaining])
		return written, execx.ErrOutputLimit
	}
	return output.buffer.Write(contents)
}

func TestProcessProbeOutputLimit(test *testing.T) {
	ctx, cancel := context.WithCancel(test.Context())
	defer cancel()
	output := probeLimitedOutput{cancel: cancel}
	if written, err := output.Write(bytes.Repeat([]byte("x"), 16384)); written != 16384 || err != nil || ctx.Err() != nil {
		test.Fatal("bounded probe output was not accepted")
	}
	if written, err := output.Write([]byte("x")); written != 0 || !errors.Is(err, execx.ErrOutputLimit) || ctx.Err() == nil || output.buffer.Len() != 16384 {
		test.Fatal("excess probe output was not capped and canceled")
	}
}
