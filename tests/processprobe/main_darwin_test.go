package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/process"
)

func validProbeRequest() probeRequest {
	return probeRequest{
		Version: 1, Challenge: strings.Repeat("a1", 32), CallerUID: 501, DriverPID: 456,
		Roots:      []string{"/fixture/primary", "/fixture/active"},
		ActiveRoot: "/fixture/active", ActivePID: 123,
		ActiveCreatedAt: time.Unix(1700000000, 123456000).UTC(),
	}
}

func encodeProbeRequest(test *testing.T, request probeRequest) []byte {
	test.Helper()
	contents, err := json.Marshal(request)
	if err != nil {
		test.Fatal(err)
	}
	return contents
}

func completeProbeCollection(request probeRequest) process.Collection {
	return process.Collection{
		Complete: true,
		ByWorktree: map[string][]domain.ProcessEvidence{
			request.Roots[0]: nil,
			request.ActiveRoot: {{
				PID: request.ActivePID, CreatedAt: request.ActiveCreatedAt,
				State: domain.EvidenceActive, CWD: request.ActiveRoot,
			}},
		},
	}
}

func TestProbeRejectsUnprivilegedAndInvalidCaller(test *testing.T) {
	for _, identity := range []struct {
		name        string
		effectiveID int
		originalID  string
	}{
		{"ordinary", 501, "501"}, {"missing", 0, ""},
		{"root-caller", 0, "0"}, {"malformed", 0, "not-a-uid"},
		{"noncanonical", 0, "0501"}, {"overflow", 0, "4294967296"},
		{"different-caller", 0, "502"},
	} {
		test.Run(identity.name, func(test *testing.T) {
			var output bytes.Buffer
			called := false
			collect := func(context.Context, []domain.Worktree) (process.Collection, []error) {
				called = true
				return process.Collection{}, nil
			}
			status := runProbe(test.Context(), bytes.NewReader(encodeProbeRequest(test, validProbeRequest())), &output, identity.effectiveID, identity.originalID, collect)
			if status == 0 || called || output.Len() != 0 {
				test.Fatal("invalid privilege or caller reached collection or emitted a report")
			}
		})
	}
}

func TestProbeRejectsMalformedRequests(test *testing.T) {
	valid := encodeProbeRequest(test, validProbeRequest())
	cases := map[string][]byte{
		"empty": nil, "null": []byte("null"), "array": []byte("[]"),
		"truncated": valid[:len(valid)-1], "trailing": append(bytes.Clone(valid), []byte("{}")...),
		"duplicate":  append([]byte(`{"version":1,`), valid[1:]...),
		"unknown":    append([]byte(`{"unexpected":true,`), valid[1:]...),
		"whitespace": append([]byte(" "), valid...),
		"oversized":  bytes.Repeat([]byte(" "), 16385),
	}
	mutations := map[string]func(*probeRequest){
		"version":                func(request *probeRequest) { request.Version = 2 },
		"challenge":              func(request *probeRequest) { request.Challenge = "bad" },
		"uppercase-challenge":    func(request *probeRequest) { request.Challenge = strings.Repeat("A1", 32) },
		"root-caller":            func(request *probeRequest) { request.CallerUID = 0 },
		"no-roots":               func(request *probeRequest) { request.Roots = nil },
		"excess-roots":           func(request *probeRequest) { request.Roots = make([]string, 9) },
		"duplicate-roots":        func(request *probeRequest) { request.Roots[0] = request.Roots[1] },
		"relative-root":          func(request *probeRequest) { request.Roots[0] = "relative" },
		"unclean-root":           func(request *probeRequest) { request.Roots[0] = "/fixture/../elsewhere" },
		"nul-root":               func(request *probeRequest) { request.Roots[0] = "/fixture\x00" },
		"long-root":              func(request *probeRequest) { request.Roots[0] = "/" + strings.Repeat("a", 4096) },
		"unbound-active-root":    func(request *probeRequest) { request.ActiveRoot = "/other" },
		"invalid-pid":            func(request *probeRequest) { request.ActivePID = 0 },
		"invalid-driver-pid":     func(request *probeRequest) { request.DriverPID = 0 },
		"missing-created":        func(request *probeRequest) { request.ActiveCreatedAt = time.Time{} },
		"submicrosecond-created": func(request *probeRequest) { request.ActiveCreatedAt = request.ActiveCreatedAt.Add(time.Nanosecond) },
	}
	for name, mutate := range mutations {
		request := validProbeRequest()
		mutate(&request)
		cases[name] = encodeProbeRequest(test, request)
	}
	for name, input := range cases {
		test.Run(name, func(test *testing.T) {
			var output bytes.Buffer
			called := false
			collect := func(context.Context, []domain.Worktree) (process.Collection, []error) {
				called = true
				return process.Collection{}, nil
			}
			if runProbe(test.Context(), bytes.NewReader(input), &output, 0, "501", collect) == 0 || called || output.Len() != 0 {
				test.Fatal("invalid request was accepted or reached collection")
			}
		})
	}
}

func TestProbeReportsExactActiveIdentityAndScope(test *testing.T) {
	request := validProbeRequest()
	var output bytes.Buffer
	collect := func(ctx context.Context, worktrees []domain.Worktree) (process.Collection, []error) {
		if _, bounded := ctx.Deadline(); !bounded {
			test.Error("collection has no deadline")
		}
		if len(worktrees) != len(request.Roots) {
			test.Fatal("request roots were not preserved")
		}
		for index, root := range request.Roots {
			if worktrees[index].Path != root {
				test.Fatal("request roots were changed")
			}
		}
		return completeProbeCollection(request), nil
	}
	if status := runProbe(test.Context(), bytes.NewReader(encodeProbeRequest(test, request)), &output, 0, "501", collect); status != 0 {
		test.Fatalf("complete probe exited %d", status)
	}
	var report probeReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		test.Fatal(err)
	}
	if report.Version != 1 || report.Challenge != request.Challenge || report.EffectiveUID != 0 || !report.Complete || !report.ActiveMatched || !report.EnumerationComplete || report.RootCount != len(request.Roots) {
		test.Fatal("report lost request binding, complete evidence or privilege identity")
	}
	if output.Len() > 16384 || bytes.Contains(output.Bytes(), []byte(request.ActiveRoot)) || bytes.Contains(output.Bytes(), []byte(request.ActiveCreatedAt.Format(time.RFC3339Nano))) {
		test.Fatal("report is unbounded or exposes raw fixture/process evidence")
	}
}

func TestProbeFailsEveryPartialCollection(test *testing.T) {
	request := validProbeRequest()
	marker := "private-host-data-must-not-escape"
	for name, mutate := range map[string]func(*process.Collection) []error{
		"enumeration":    func(collection *process.Collection) []error { collection.Complete = false; return nil },
		"returned-error": func(*process.Collection) []error { return []error{errors.New(marker)} },
		"retained-error": func(collection *process.Collection) []error { collection.Errors = []string{marker}; return nil },
		"uninspectable": func(collection *process.Collection) []error {
			collection.Uninspectable = map[int32]domain.ProcessEvidence{999: {State: domain.EvidenceUnknown, Error: marker}}
			return nil
		},
		"global-unknown": func(collection *process.Collection) []error {
			collection.GlobalUnknown = []domain.ProcessEvidence{{State: domain.EvidenceUnknown, Error: marker}}
			return nil
		},
		"scoped-unknown": func(collection *process.Collection) []error {
			collection.ByWorktree[request.Roots[0]] = []domain.ProcessEvidence{{State: domain.EvidenceUnknown, Error: marker}}
			return nil
		},
		"reused-pid": func(collection *process.Collection) []error {
			collection.ByWorktree[request.ActiveRoot][0].CreatedAt = request.ActiveCreatedAt.Add(time.Microsecond)
			return nil
		},
		"missing-active": func(collection *process.Collection) []error {
			collection.ByWorktree[request.ActiveRoot] = nil
			return nil
		},
		"unproven-active": func(collection *process.Collection) []error {
			collection.ByWorktree[request.ActiveRoot][0].State = domain.EvidenceUnknown
			return nil
		},
		"missing-root": func(collection *process.Collection) []error {
			delete(collection.ByWorktree, request.Roots[0])
			return nil
		},
		"additional-root": func(collection *process.Collection) []error { collection.ByWorktree["/other"] = nil; return nil },
	} {
		test.Run(name, func(test *testing.T) {
			var output bytes.Buffer
			collect := func(context.Context, []domain.Worktree) (process.Collection, []error) {
				collection := completeProbeCollection(request)
				failures := mutate(&collection)
				return collection, failures
			}
			status := runProbe(test.Context(), bytes.NewReader(encodeProbeRequest(test, request)), &output, 0, "501", collect)
			var report probeReport
			if err := json.Unmarshal(output.Bytes(), &report); err != nil {
				test.Fatal(err)
			}
			if status == 0 || report.Complete || bytes.Contains(output.Bytes(), []byte(marker)) {
				test.Fatal("partial collection passed or leaked raw evidence")
			}
		})
	}
}

func TestProbePropagatesCancellationAndOutputFailure(test *testing.T) {
	request := validProbeRequest()
	collect := func(context.Context, []domain.Worktree) (process.Collection, []error) {
		return completeProbeCollection(request), nil
	}
	ctx, cancel := context.WithCancel(test.Context())
	cancel()
	if runProbe(ctx, bytes.NewReader(encodeProbeRequest(test, request)), io.Discard, 0, "501", collect) == 0 {
		test.Fatal("canceled probe passed")
	}
	if runProbe(test.Context(), bytes.NewReader(encodeProbeRequest(test, request)), probeFailedWriter{}, 0, "501", collect) == 0 {
		test.Fatal("failed output passed")
	}
}

func TestProbeCountsErrorsWithoutExposingDetails(test *testing.T) {
	request := validProbeRequest()
	var output bytes.Buffer
	collect := func(context.Context, []domain.Worktree) (process.Collection, []error) {
		return completeProbeCollection(request), []error{
			errors.New("private-path: operation not permitted"),
			errors.New("private-path: no such file or directory"),
			errors.New("private-command: unexpected failure"),
		}
	}
	if runProbe(test.Context(), bytes.NewReader(encodeProbeRequest(test, request)), &output, 0, "501", collect) != 1 {
		test.Fatal("collection errors did not fail the probe")
	}
	var report probeReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		test.Fatal(err)
	}
	if report.ErrorCount != 3 || report.DeniedErrorCount != 1 || report.MissingPathCount != 1 || report.OtherErrorCount != 1 || bytes.Contains(output.Bytes(), []byte("private-")) {
		test.Fatal("probe lost aggregate error counts or exposed raw details")
	}
}

func TestProbeReportsOnlyFixedFailureStages(test *testing.T) {
	request := validProbeRequest()
	marker := "private-host-data-must-not-escape"
	for _, fixture := range []struct {
		message         string
		stage           string
		invalidArgument int
		nativeRead      int
	}{
		{"enumerate processes: " + marker, "enumeration", 0, 0},
		{"worktree path \"/" + marker + "\": unavailable", "worktree-path", 0, 0},
		{"process 123: creation time is unavailable or invalid; " + marker, "creation-time", 0, 0},
		{"process 123: executable: unknown error: proc_pidpath returned 0; " + marker, "executable", 0, 1},
		{"process 123: executable is not a regular file; " + marker, "executable", 0, 0},
		{"process 123: cwd: unknown error: proc_pidinfo returned 0; " + marker, "cwd", 0, 1},
		{"process 123: cwd is not a valid absolute path; " + marker, "cwd", 0, 0},
		{"process 123: command line: invalid argument; " + marker, "command-line", 1, 0},
		{"process 123: owner contains a NUL byte; " + marker, "owner", 0, 0},
		{"process 123: current owner: " + marker, "owner", 0, 0},
		{"process 123: name is unavailable or malformed; " + marker, "name", 0, 0},
		{"process 123: inspection: " + marker, "inspection", 0, 0},
		{"process 123: containment: " + marker, "containment", 0, 0},
		{"process 123: cwd: " + marker + "; command line: unavailable", "cwd", 0, 0},
		{"process " + marker + ": cwd: unavailable", "other", 0, 0},
		{marker, "other", 0, 0},
	} {
		test.Run(fixture.stage+"/"+fixture.message[:min(20, len(fixture.message))], func(test *testing.T) {
			var output bytes.Buffer
			collect := func(context.Context, []domain.Worktree) (process.Collection, []error) {
				return completeProbeCollection(request), []error{errors.New(fixture.message)}
			}
			if status := runProbe(test.Context(), bytes.NewReader(encodeProbeRequest(test, request)), &output, 0, "501", collect); status != 1 {
				test.Fatalf("diagnosed error exited %d, want failure", status)
			}
			var report struct {
				FirstFailureStages   map[string]int `json:"first_failure_stages"`
				InvalidArgumentCount int            `json:"invalid_argument_count"`
				NativeReadErrorCount int            `json:"native_read_error_count"`
			}
			if err := json.Unmarshal(output.Bytes(), &report); err != nil {
				test.Fatal(err)
			}
			if len(report.FirstFailureStages) != 1 || report.FirstFailureStages[fixture.stage] != 1 || report.InvalidArgumentCount != fixture.invalidArgument || report.NativeReadErrorCount != fixture.nativeRead {
				test.Fatal("missing or incorrect fixed diagnostic category")
			}
			if bytes.Contains(output.Bytes(), []byte(marker)) || bytes.Contains(output.Bytes(), []byte("process 123")) {
				test.Fatal("diagnostics exposed raw process/error data")
			}
		})
	}
}

func TestProbeReportsOnlyFixedNativePathErrnos(test *testing.T) {
	request := validProbeRequest()
	marker := "private-host-data-must-not-escape"
	for code, label := range map[string]string{
		"0": "unavailable",
		"1": "EPERM", "2": "ENOENT", "3": "ESRCH", "5": "EIO",
		"9": "EBADF", "12": "ENOMEM", "13": "EACCES", "16": "EBUSY",
		"20": "ENOTDIR", "22": "EINVAL", "35": "EAGAIN", "45": "ENOTSUP",
		"63": "ENAMETOOLONG", "84": "EOVERFLOW", "999": "other", marker: "other",
	} {
		test.Run(label, func(test *testing.T) {
			var output bytes.Buffer
			collect := func(context.Context, []domain.Worktree) (process.Collection, []error) {
				return completeProbeCollection(request), []error{errors.New("process 123: executable: proc_pidpath errno " + code + ": " + marker)}
			}
			if runProbe(test.Context(), bytes.NewReader(encodeProbeRequest(test, request)), &output, 0, "501", collect) != 1 {
				test.Fatal("native error diagnostics hid incomplete collection")
			}
			var report struct {
				NativePathErrnos map[string]int `json:"native_path_errnos"`
			}
			if err := json.Unmarshal(output.Bytes(), &report); err != nil {
				test.Fatal(err)
			}
			if len(report.NativePathErrnos) != 1 || report.NativePathErrnos[label] != 1 || bytes.Contains(output.Bytes(), []byte(marker)) {
				test.Fatal("native errno category is missing, incorrect or exposes raw data")
			}
		})
	}
}

func TestProbeReportsUninspectableHarnessActorsWithoutPIDs(test *testing.T) {
	request := validProbeRequest()
	for name, pid := range map[string]int32{
		"probe": int32(os.Getpid()), "parent": int32(os.Getppid()), "driver": request.DriverPID,
	} {
		test.Run(name, func(test *testing.T) {
			var output bytes.Buffer
			collect := func(context.Context, []domain.Worktree) (process.Collection, []error) {
				collection := completeProbeCollection(request)
				collection.Uninspectable = map[int32]domain.ProcessEvidence{pid: {State: domain.EvidenceUnknown, Error: "private-host-data"}}
				return collection, nil
			}
			if runProbe(test.Context(), bytes.NewReader(encodeProbeRequest(test, request)), &output, 0, "501", collect) != 1 {
				test.Fatal("actor diagnostics accepted unknown evidence")
			}
			var report struct {
				ProbeUninspectable  bool `json:"probe_uninspectable"`
				ParentUninspectable bool `json:"parent_uninspectable"`
				DriverUninspectable bool `json:"driver_uninspectable"`
			}
			if err := json.Unmarshal(output.Bytes(), &report); err != nil {
				test.Fatal(err)
			}
			if report.ProbeUninspectable != (pid == int32(os.Getpid())) || report.ParentUninspectable != (pid == int32(os.Getppid())) || report.DriverUninspectable != (pid == request.DriverPID) || bytes.Contains(output.Bytes(), []byte("private-host-data")) || bytes.Contains(output.Bytes(), []byte("\"driver_pid\"")) {
				test.Fatal("actor diagnostic was lost, incorrect or exposed raw data")
			}
		})
	}
}

func TestProbeRetainsFullCollectorPathValidation(test *testing.T) {
	root, err := filepath.EvalSymlinks(test.TempDir())
	if err != nil {
		test.Fatal(err)
	}
	request := validProbeRequest()
	request.Roots, request.ActiveRoot = []string{root}, root
	executable := filepath.Join(root, "fixture-executable")
	if err := os.WriteFile(executable, nil, 0o700); err != nil {
		test.Fatal(err)
	}
	for _, validExecutable := range []bool{true, false} {
		info := process.Info{PID: request.ActivePID, CreatedAt: request.ActiveCreatedAt, CWD: root, Executable: executable, Inspectable: true}
		if !validExecutable {
			info.Executable = root
		}
		collector := process.Collector{Source: probeFixtureSource{info: info}}
		var output bytes.Buffer
		status := runProbe(test.Context(), bytes.NewReader(encodeProbeRequest(test, request)), &output, 0, "501", collector.Collect)
		if (status == 0) != validExecutable {
			test.Fatal("probe skipped collector file-type/path validation or rejected a valid fixture")
		}
	}
}

type probeFixtureSource struct {
	info process.Info
}

func (source probeFixtureSource) List(context.Context) ([]process.Info, error) {
	return []process.Info{source.info}, nil
}

type probeFailedWriter struct{}

func (probeFailedWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}
