package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/process"
)

func TestProbeProcessRolesUseFixedLabels(test *testing.T) {
	for name, expected := range map[string]string{
		"Runner.Listener": "ci-listener", "Runner.Worker": "ci-worker",
		"go": "go-tool", "node": "node-tool", "bash": "shell",
		"zsh": "shell", "sh": "shell", "pwsh": "shell",
		"launchd": "other", "private-host-data": "other", "": "other",
	} {
		if actual := probeProcessRole(probeProcessRecord{PID: 123, Name: name}); actual != expected {
			test.Errorf("fixed role classification = %q, want %q", actual, expected)
		}
	}
	if probeProcessRole(probeProcessRecord{PID: 1, Name: "launchd"}) != "system-init" {
		test.Fatal("native init name/PID hint was not classified")
	}
}

func TestProbeRoleSamplingPreservesUnknownsAndBoundsReads(test *testing.T) {
	created := time.Unix(1700000000, 123000).UTC()
	unknown := make(map[int32]domain.ProcessEvidence)
	for pid := int32(1); pid <= 20; pid++ {
		unknown[pid] = domain.ProcessEvidence{PID: pid, CreatedAt: created, State: domain.EvidenceUnknown, Error: "private-host-data"}
	}
	before := maps.Clone(unknown)
	calls := 0
	read := func(ctx context.Context, pid int32) (probeProcessRecord, error) {
		if ctx != test.Context() {
			test.Fatal("metadata read lost its deadline context")
		}
		calls++
		if pid == 1000 {
			return probeProcessRecord{PID: pid, CreatedAt: created.Add(-time.Second), Name: "Runner.Worker"}, nil
		}
		if pid > 16 {
			test.Fatal("sample selection was not bounded and deterministic")
		}
		return probeProcessRecord{PID: pid, ParentPID: 1000, CreatedAt: created, Name: "sample-process"}, nil
	}
	roles, parents, names := sampleProbeProcessRoles(test.Context(), unknown, read, true)
	if !reflect.DeepEqual(roles, map[string]int{"other": 16, "not-sampled": 4}) || !reflect.DeepEqual(parents, map[string]int{"ci-worker": 16, "not-sampled": 4}) || calls != 48 || len(names) != 16 {
		test.Fatal("diagnostic sampling lost counts or exceeded its lookup bound")
	}
	for _, pair := range names {
		if pair.Process != "sample-process" || pair.Parent != "Runner.Worker" {
			test.Fatal("diagnostic sampling lost a validated name pair")
		}
	}
	if !reflect.DeepEqual(unknown, before) {
		test.Fatal("diagnostic sampling mutated retained unknown evidence")
	}
}

func TestProbeRoleSamplingRejectsChangedOrUnavailableIdentity(test *testing.T) {
	created := time.Unix(1700000000, 123000).UTC()
	unknown := map[int32]domain.ProcessEvidence{123: {PID: 123, CreatedAt: created, State: domain.EvidenceUnknown}}
	for name, mutate := range map[string]func(int, *probeProcessRecord) error{
		"read-error": func(int, *probeProcessRecord) error { return errors.New("private-host-data") },
		"wrong-pid":  func(_ int, record *probeProcessRecord) error { record.PID++; return nil },
		"reused-pid": func(_ int, record *probeProcessRecord) error {
			record.CreatedAt = record.CreatedAt.Add(time.Second)
			return nil
		},
		"changed-name": func(call int, record *probeProcessRecord) error {
			if call > 1 && record.PID == 123 {
				record.Name = "private-changed-name"
			}
			return nil
		},
		"changed-parent": func(call int, record *probeProcessRecord) error {
			if call > 1 && record.PID == 123 {
				record.ParentPID++
			}
			return nil
		},
	} {
		test.Run(name, func(test *testing.T) {
			calls := 0
			read := func(_ context.Context, pid int32) (probeProcessRecord, error) {
				calls++
				record := probeProcessRecord{PID: pid, ParentPID: 1, CreatedAt: created, Name: "Runner.Listener"}
				if pid == 1 {
					record.CreatedAt, record.Name = created.Add(-time.Second), "launchd"
				}
				err := mutate(calls, &record)
				return record, err
			}
			roles, parents, names := sampleProbeProcessRoles(test.Context(), unknown, read, true)
			expected := "identity-changed"
			if name == "read-error" {
				expected = "unavailable"
			}
			if !reflect.DeepEqual(roles, map[string]int{expected: 1}) || !reflect.DeepEqual(parents, map[string]int{expected: 1}) || calls > 3 || len(names) != 0 {
				test.Fatal("unavailable or changed identity became an attributed process role")
			}
		})
	}
}

func TestProbeRoleSamplingStopsAfterCancellation(test *testing.T) {
	ctx, cancel := context.WithCancel(test.Context())
	defer cancel()
	created := time.Unix(1700000000, 123000).UTC()
	unknown := map[int32]domain.ProcessEvidence{
		123: {PID: 123, CreatedAt: created, State: domain.EvidenceUnknown},
		124: {PID: 124, CreatedAt: created, State: domain.EvidenceUnknown},
	}
	calls := 0
	read := func(context.Context, int32) (probeProcessRecord, error) {
		calls++
		cancel()
		return probeProcessRecord{PID: 123, ParentPID: 1, CreatedAt: created, Name: "private-host-data"}, nil
	}
	roles, parents, names := sampleProbeProcessRoles(ctx, unknown, read, true)
	if calls != 1 || !reflect.DeepEqual(roles, map[string]int{"unavailable": 1, "not-sampled": 1}) || !reflect.DeepEqual(roles, parents) || len(names) != 0 {
		test.Fatal("cancelled sampling continued native reads or lost retained unknown counts")
	}
	for role := range roles {
		if strings.Contains(role, "private") {
			test.Fatal("diagnostic exposed a raw process name")
		}
	}
}

func TestProbeRoleSamplingWithoutReaderIsNotSampled(test *testing.T) {
	unknown := map[int32]domain.ProcessEvidence{123: {State: domain.EvidenceUnknown}}
	roles, parents, names := sampleProbeProcessRoles(test.Context(), unknown, nil, true)
	if !reflect.DeepEqual(roles, map[string]int{"not-sampled": 1}) || !reflect.DeepEqual(roles, parents) || len(names) != 0 {
		test.Fatal("missing metadata reader lost unknown diagnostic counts")
	}
}

func TestProbeRoleSamplingDoesNotAttributeAnUnverifiedParent(test *testing.T) {
	created := time.Unix(1700000000, 123000).UTC()
	unknown := map[int32]domain.ProcessEvidence{123: {PID: 123, CreatedAt: created, State: domain.EvidenceUnknown}}
	for _, problem := range []string{"unavailable", "wrong-pid", "later-birth", "zero-birth"} {
		test.Run(problem, func(test *testing.T) {
			read := func(_ context.Context, pid int32) (probeProcessRecord, error) {
				if pid == 123 {
					return probeProcessRecord{PID: pid, ParentPID: 1, CreatedAt: created, Name: "go"}, nil
				}
				parent := probeProcessRecord{PID: pid, CreatedAt: created.Add(-time.Second), Name: "launchd"}
				switch problem {
				case "unavailable":
					return probeProcessRecord{}, errors.New("private-host-data")
				case "wrong-pid":
					parent.PID++
				case "later-birth":
					parent.CreatedAt = created.Add(time.Second)
				case "zero-birth":
					parent.CreatedAt = time.Time{}
				}
				return parent, nil
			}
			roles, parents, names := sampleProbeProcessRoles(test.Context(), unknown, read, true)
			if !reflect.DeepEqual(roles, map[string]int{"go-tool": 1}) || !reflect.DeepEqual(parents, map[string]int{"unavailable": 1}) || len(names) != 1 || names[0].Process != "go" || names[0].Parent != "" {
				test.Fatal("unverified parent metadata became a role attribution")
			}
		})
	}
}

func TestProbeRoleSamplingWithholdsNamesForOversizedInput(test *testing.T) {
	unknown := make(map[int32]domain.ProcessEvidence, 65537)
	for pid := int32(1); pid <= 65537; pid++ {
		unknown[pid] = domain.ProcessEvidence{PID: pid, State: domain.EvidenceUnknown}
	}
	read := func(context.Context, int32) (probeProcessRecord, error) {
		test.Fatal("oversized diagnostic input reached native metadata reads")
		return probeProcessRecord{}, nil
	}
	roles, parents, names := sampleProbeProcessRoles(test.Context(), unknown, read, true)
	if !reflect.DeepEqual(roles, map[string]int{"not-sampled": 65537}) || !reflect.DeepEqual(roles, parents) || len(names) != 0 {
		test.Fatal("oversized input disclosed names or lost unknown counts")
	}
}

func TestProbeProcessRecordReadsOnlyOwnedIdentity(test *testing.T) {
	if os.Geteuid() == 0 || os.Geteuid() != os.Getuid() {
		test.Fatal("native identity regression requires an ordinary test driver")
	}
	record, err := readProbeProcessRecord(test.Context(), int32(os.Getpid()))
	if err != nil || record.PID != int32(os.Getpid()) || record.ParentPID != int32(os.Getppid()) || !record.CreatedAt.After(time.Unix(0, 0)) || record.CreatedAt.Nanosecond()%1000 != 0 || record.Name == "" {
		test.Fatal("owned process metadata was unavailable or malformed")
	}
	ctx, cancel := context.WithCancel(test.Context())
	cancel()
	if _, err := readProbeProcessRecord(ctx, int32(os.Getpid())); !errors.Is(err, context.Canceled) {
		test.Fatal("cancelled metadata query did not stop")
	}
	if _, err := readProbeProcessRecord(test.Context(), 0); err == nil {
		test.Fatal("invalid metadata PID was accepted")
	}
}

func TestProbeIncludesRoleDiagnosticsWithoutQualifyingUnknowns(test *testing.T) {
	request := validProbeRequest()
	collection := completeProbeCollection(request)
	collection.Uninspectable = map[int32]domain.ProcessEvidence{
		290013: {PID: 290013, CreatedAt: request.ActiveCreatedAt, State: domain.EvidenceUnknown, Error: "private-host-data"},
	}
	collect := func(context.Context, []domain.Worktree) (process.Collection, []error) { return collection, nil }
	calls := 0
	read := func(ctx context.Context, pid int32) (probeProcessRecord, error) {
		if _, ok := ctx.Deadline(); !ok {
			test.Fatal("diagnostic native read has no deadline")
		}
		calls++
		switch pid {
		case 290013:
			return probeProcessRecord{PID: pid, ParentPID: 290001, CreatedAt: request.ActiveCreatedAt, Name: "Runner.Worker"}, nil
		case 290001:
			return probeProcessRecord{PID: pid, CreatedAt: request.ActiveCreatedAt.Add(-time.Second), Name: "Runner.Listener"}, nil
		default:
			test.Fatal("diagnostics inspected an unrelated process")
			return probeProcessRecord{}, nil
		}
	}
	var output bytes.Buffer
	if runProbe(test.Context(), bytes.NewReader(encodeProbeRequest(test, request)), &output, 0, "501", collect, read) != 1 {
		test.Fatal("diagnostic role hints made unknown evidence qualify")
	}
	var report probeReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		test.Fatal(err)
	}
	if report.Complete || report.UninspectableCount != 1 || report.UninspectableRoles["ci-worker"] != 1 || report.UninspectableParentRoles["ci-listener"] != 1 || calls != 3 {
		test.Fatal("role diagnostics were lost or changed collection completeness")
	}
	for _, private := range []string{"290013", "290001", "Runner.Worker", "Runner.Listener", "private-host-data"} {
		if strings.Contains(output.String(), private) {
			test.Fatal("role report exposed raw metadata")
		}
	}
}
