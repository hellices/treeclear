package main

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"golang.org/x/sys/unix"
)

type probeProcessRecord struct {
	PID       int32
	ParentPID int32
	CreatedAt time.Time
	Name      string
}

type probeProcessRead func(context.Context, int32) (probeProcessRecord, error)

func readProbeProcessRecord(ctx context.Context, pid int32) (probeProcessRecord, error) {
	if err := ctx.Err(); err != nil {
		return probeProcessRecord{}, err
	}
	if pid <= 0 {
		return probeProcessRecord{}, errors.New("invalid metadata PID")
	}
	native, err := unix.SysctlKinfoProc("kern.proc.pid", int(pid))
	if err != nil {
		return probeProcessRecord{}, err
	}
	if err := ctx.Err(); err != nil {
		return probeProcessRecord{}, err
	}
	if native.Proc.P_pid != pid || native.Proc.P_starttime.Sec <= 0 || native.Proc.P_starttime.Usec < 0 || native.Proc.P_starttime.Usec >= 1000000 {
		return probeProcessRecord{}, errors.New("invalid native process metadata")
	}
	return probeProcessRecord{
		PID: native.Proc.P_pid, ParentPID: native.Eproc.Ppid,
		CreatedAt: time.Unix(native.Proc.P_starttime.Sec, int64(native.Proc.P_starttime.Usec)*int64(time.Microsecond)).UTC(),
		Name:      unix.ByteSliceToString(native.Proc.P_comm[:]),
	}, nil
}

func probeProcessRole(record probeProcessRecord) string {
	switch record.Name {
	case "Runner.Listener":
		return "ci-listener"
	case "Runner.Worker":
		return "ci-worker"
	case "go":
		return "go-tool"
	case "node":
		return "node-tool"
	case "bash", "zsh", "sh", "pwsh":
		return "shell"
	case "launchd":
		if record.PID == 1 {
			return "system-init"
		}
	}
	return "other"
}

func sampleProbeProcessRoles(ctx context.Context, unknown map[int32]domain.ProcessEvidence, read probeProcessRead) (map[string]int, map[string]int) {
	roles, parents := make(map[string]int), make(map[string]int)
	add := func(role, parent string, count int) {
		if count > 0 {
			roles[role] += count
			parents[parent] += count
		}
	}
	if len(unknown) > 65536 {
		add("not-sampled", "not-sampled", len(unknown))
		return roles, parents
	}
	if read == nil {
		add("not-sampled", "not-sampled", len(unknown))
		return roles, parents
	}
	pids := make([]int32, 0, len(unknown))
	for pid := range unknown {
		pids = append(pids, pid)
	}
	slices.Sort(pids)
	for index, pid := range pids {
		if index >= 16 || ctx.Err() != nil {
			add("not-sampled", "not-sampled", len(pids)-index)
			break
		}
		evidence := unknown[pid]
		if pid <= 0 || evidence.PID != pid || !evidence.CreatedAt.After(time.Unix(0, 0)) || evidence.CreatedAt.Nanosecond()%1000 != 0 {
			add("unavailable", "unavailable", 1)
			continue
		}
		before, err := read(ctx, pid)
		if err != nil || ctx.Err() != nil {
			add("unavailable", "unavailable", 1)
			continue
		}
		if before.PID != pid || !before.CreatedAt.Equal(evidence.CreatedAt) {
			add("identity-changed", "identity-changed", 1)
			continue
		}
		var parent probeProcessRecord
		var parentErr error
		if before.ParentPID > 0 && before.ParentPID != pid {
			parent, parentErr = read(ctx, before.ParentPID)
		}
		if ctx.Err() != nil {
			add("unavailable", "unavailable", 1)
			continue
		}
		after, err := read(ctx, pid)
		if err != nil || ctx.Err() != nil {
			add("unavailable", "unavailable", 1)
			continue
		}
		if before != after {
			add("identity-changed", "identity-changed", 1)
			continue
		}
		parentRole := "unavailable"
		if parentErr == nil && parent.PID == before.ParentPID && parent.Name != "" && parent.CreatedAt.After(time.Unix(0, 0)) && parent.CreatedAt.Before(before.CreatedAt) {
			parentRole = probeProcessRole(parent)
		}
		add(probeProcessRole(before), parentRole, 1)
	}
	return roles, parents
}
