package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/process"
)

func TestProbeProcessNamesRequireExplicitOptIn(test *testing.T) {
	for _, includeNames := range []bool{false, true} {
		test.Run(map[bool]string{false: "private-default", true: "explicit-names"}[includeNames], func(test *testing.T) {
			request := validProbeRequest()
			collection := completeProbeCollection(request)
			collection.Uninspectable = map[int32]domain.ProcessEvidence{
				290013: {PID: 290013, CreatedAt: request.ActiveCreatedAt, State: domain.EvidenceUnknown, Error: "private-error"},
			}
			calls := 0
			read := func(ctx context.Context, pid int32) (probeProcessRecord, error) {
				if _, bounded := ctx.Deadline(); !bounded {
					test.Fatal("name sampling lost its deadline")
				}
				calls++
				if pid == 290013 {
					return probeProcessRecord{PID: pid, ParentPID: 290001, CreatedAt: request.ActiveCreatedAt, Name: "Runner.Worker"}, nil
				}
				if pid == 290001 {
					return probeProcessRecord{PID: pid, CreatedAt: request.ActiveCreatedAt.Add(-time.Second), Name: "Runner.Listener"}, nil
				}
				test.Fatal("name sampling inspected an unrelated process")
				return probeProcessRecord{}, nil
			}
			contents := encodeProbeRequest(test, request)
			if includeNames {
				contents = append(contents[:len(contents)-1], []byte(`,"include_process_names":true}`)...)
			}
			collect := func(context.Context, []domain.Worktree) (process.Collection, []error) { return collection, nil }
			var output bytes.Buffer
			if runProbe(test.Context(), bytes.NewReader(contents), &output, 0, "501", collect, read) != 1 {
				test.Fatal("valid diagnostic request was rejected or qualified unknown evidence")
			}
			var report struct {
				probeReport
				Names []struct {
					Process string `json:"process"`
					Parent  string `json:"parent"`
				} `json:"uninspectable_names"`
			}
			if err := json.Unmarshal(output.Bytes(), &report); err != nil {
				test.Fatal("diagnostic report is malformed")
			}
			if report.Complete || report.UninspectableCount != 1 || calls != 3 {
				test.Fatal("name disclosure changed completeness or added native reads")
			}
			if includeNames {
				if len(report.Names) != 1 || report.Names[0].Process != "Runner.Worker" || report.Names[0].Parent != "Runner.Listener" {
					test.Fatal("explicitly approved stable names were not reported")
				}
			} else if len(report.Names) != 0 || strings.Contains(output.String(), "uninspectable_names") || strings.Contains(output.String(), "Runner.") {
				test.Fatal("default diagnostics disclosed process names")
			}
			for _, private := range []string{"290013", "290001", "private-error", "/fixture/", request.ActiveCreatedAt.Format(time.RFC3339Nano)} {
				if strings.Contains(output.String(), private) {
					test.Fatal("diagnostic report disclosed unapproved metadata")
				}
			}
		})
	}
}

func TestProbeProcessNamesWithholdUnsafeMetadata(test *testing.T) {
	for _, scenario := range []struct {
		name        string
		processName string
		parentName  string
		wantProcess string
		wantParent  string
	}{
		{"safe", "Tool_1.2-test", "Runner.Listener", "Tool_1.2-test", "Runner.Listener"},
		{"boundary", "abcdefghijklmnop", "abcdefghijklmnop", "abcdefghijklmnop", "abcdefghijklmnop"},
		{"empty", "", "parent", "", ""},
		{"long", "abcdefghijklmnopq", "parent", "", ""},
		{"path", "/private/tool", "parent", "", ""},
		{"space", "private tool", "parent", "", ""},
		{"newline", "private\nname", "parent", "", ""},
		{"escape", "\x1b[31mname", "parent", "", ""},
		{"nul", "private\x00name", "parent", "", ""},
		{"unicode", "이름", "parent", "", ""},
		{"quote", "private\"name", "parent", "", ""},
		{"unsafe-parent", "target", "/private/parent", "target", ""},
		{"empty-parent", "target", "", "target", ""},
		{"long-parent", "target", "abcdefghijklmnopq", "target", ""},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			request := validProbeRequest()
			collection := completeProbeCollection(request)
			collection.Uninspectable = map[int32]domain.ProcessEvidence{
				123: {PID: 123, CreatedAt: request.ActiveCreatedAt, State: domain.EvidenceUnknown},
			}
			read := func(_ context.Context, pid int32) (probeProcessRecord, error) {
				if pid == 123 {
					return probeProcessRecord{PID: pid, ParentPID: 1, CreatedAt: request.ActiveCreatedAt, Name: scenario.processName}, nil
				}
				if pid == 1 {
					return probeProcessRecord{PID: pid, CreatedAt: request.ActiveCreatedAt.Add(-time.Second), Name: scenario.parentName}, nil
				}
				return probeProcessRecord{}, errors.New("unrelated synthetic process")
			}
			contents := encodeProbeRequest(test, request)
			contents = append(contents[:len(contents)-1], []byte(`,"include_process_names":true}`)...)
			collect := func(context.Context, []domain.Worktree) (process.Collection, []error) { return collection, nil }
			var output bytes.Buffer
			if runProbe(test.Context(), bytes.NewReader(contents), &output, 0, "501", collect, read) != 1 {
				test.Fatal("name diagnostic request did not retain unknown evidence")
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(output.Bytes(), &fields); err != nil {
				test.Fatal("diagnostic report is malformed")
			}
			names, present := fields["uninspectable_names"]
			if scenario.wantProcess == "" {
				if present {
					test.Fatal("unsafe target name was disclosed")
				}
				return
			}
			var pairs []map[string]string
			if !present || json.Unmarshal(names, &pairs) != nil || len(pairs) != 1 || pairs[0]["process"] != scenario.wantProcess || pairs[0]["parent"] != scenario.wantParent {
				test.Fatal("safe name pair was lost or unsafe parent name was disclosed")
			}
		})
	}
}

func TestProbeProcessNamesNotReportedForCompleteCollection(test *testing.T) {
	request := validProbeRequest()
	contents := encodeProbeRequest(test, request)
	contents = append(contents[:len(contents)-1], []byte(`,"include_process_names":true}`)...)
	collect := func(context.Context, []domain.Worktree) (process.Collection, []error) {
		return completeProbeCollection(request), nil
	}
	read := func(context.Context, int32) (probeProcessRecord, error) {
		test.Fatal("complete collection sampled process names")
		return probeProcessRecord{}, nil
	}
	var output bytes.Buffer
	if runProbe(test.Context(), bytes.NewReader(contents), &output, 0, "501", collect, read) != 0 || strings.Contains(output.String(), "uninspectable_names") {
		test.Fatal("complete collection failed or disclosed process names")
	}
}
