package process

import (
	"context"
	"errors"
	"fmt"
	"os/user"
	"runtime"
	"strings"
	"time"

	gprocess "github.com/shirou/gopsutil/v4/process"
)

type GopsutilSource struct {
	processes   func(context.Context) ([]processHandle, error)
	pids        func(context.Context) ([]int32, error)
	currentUser func() (*user.User, error)
}

type processReader interface {
	CreateTimeWithContext(context.Context) (int64, error)
	ExeWithContext(context.Context) (string, error)
	CwdWithContext(context.Context) (string, error)
	CmdlineSliceWithContext(context.Context) ([]string, error)
	UsernameWithContext(context.Context) (string, error)
	NameWithContext(context.Context) (string, error)
}

type processHandle struct {
	pid    int32
	reader processReader
}

func (source GopsutilSource) List(ctx context.Context) ([]Info, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	enumerate := source.processes
	if enumerate == nil {
		enumerate = enumerateProcesses
	}
	listPIDs := source.pids
	if listPIDs == nil {
		listPIDs = gprocess.PidsWithContext
	}
	currentUser := source.currentUser
	if currentUser == nil {
		currentUser = user.Current
	}
	processes, listErr := enumerate(ctx)
	var enumerationErrors []error
	if listErr != nil {
		enumerationErrors = append(enumerationErrors, fmt.Errorf("list processes: %w", listErr))
	}
	var pids []int32
	pidErr := ctx.Err()
	if pidErr == nil {
		pids, pidErr = listPIDs(ctx)
	}
	if pidErr != nil {
		enumerationErrors = append(enumerationErrors, fmt.Errorf("verify process enumeration: %w", pidErr))
	} else if len(pids) == 0 {
		enumerationErrors = append(enumerationErrors, errors.New("PID enumeration returned no processes"))
	}
	var currentOwner string
	ownerErr := ctx.Err()
	if ownerErr == nil {
		owner, err := currentUser()
		ownerErr = err
		if owner != nil {
			currentOwner = owner.Username
		}
		if ownerErr == nil && !validText(currentOwner) {
			ownerErr = errors.New("current owner is unavailable or malformed")
		}
	}
	infos := make([]Info, 0, len(processes))
	seen := make(map[int32]bool, len(processes))
	for _, process := range processes {
		seen[process.pid] = true
		if err := ctx.Err(); err != nil {
			infos = append(infos, Info{PID: process.pid, OwnerRelation: OwnerUnknown, Error: err.Error()})
			continue
		}
		if process.reader == nil {
			err := fmt.Errorf("process %d is missing an inspection handle", process.pid)
			enumerationErrors = append(enumerationErrors, err)
			infos = append(infos, Info{PID: process.pid, OwnerRelation: OwnerUnknown, Error: err.Error()})
			continue
		}
		infos = append(infos, inspectProcess(ctx, process, currentOwner, ownerErr))
	}
	for _, pid := range pids {
		if seen[pid] {
			continue
		}
		seen[pid] = true
		err := fmt.Errorf("process %d is missing from process enumeration", pid)
		enumerationErrors = append(enumerationErrors, err)
		infos = append(infos, Info{PID: pid, OwnerRelation: OwnerUnknown, Error: err.Error()})
	}
	if err := ctx.Err(); err != nil {
		enumerationErrors = append(enumerationErrors, err)
	}
	return infos, errors.Join(enumerationErrors...)
}

func enumerateProcesses(ctx context.Context) ([]processHandle, error) {
	processes, err := gprocess.ProcessesWithContext(ctx)
	handles := make([]processHandle, 0, len(processes))
	for _, process := range processes {
		if process == nil {
			handles = append(handles, processHandle{})
		} else {
			handles = append(handles, processHandle{pid: process.Pid, reader: process})
		}
	}
	return handles, err
}

func inspectProcess(ctx context.Context, process processHandle, currentOwner string, currentOwnerErr error) Info {
	info := Info{PID: process.pid, OwnerRelation: OwnerUnknown}
	var problems []string
	record := func(field string, present bool, err error) {
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", field, err))
		} else if !present {
			problems = append(problems, field+" is unavailable or malformed")
		}
	}
	created, err := process.reader.CreateTimeWithContext(ctx)
	if created > 0 {
		info.CreatedAt = time.UnixMilli(created).UTC()
	}
	record("creation time", created > 0, err)
	info.Executable, err = process.reader.ExeWithContext(ctx)
	record("executable", absolutePath(info.Executable), err)
	info.CWD, err = process.reader.CwdWithContext(ctx)
	record("cwd", absolutePath(info.CWD), err)
	info.CommandLine, err = process.reader.CmdlineSliceWithContext(ctx)
	info.CommandLine = append([]string(nil), info.CommandLine...)
	record("command line", validCommandLine(info.CommandLine), err)
	info.Owner, err = process.reader.UsernameWithContext(ctx)
	record("owner", validText(info.Owner), err)
	if currentOwnerErr != nil {
		record("current owner", false, currentOwnerErr)
	}
	if err == nil && currentOwnerErr == nil && validText(info.Owner) && validText(currentOwner) {
		info.OwnerRelation = OwnerOther
		if info.Owner == currentOwner || runtime.GOOS == "windows" && strings.EqualFold(info.Owner, currentOwner) {
			info.OwnerRelation = OwnerSame
		}
	}
	name, err := process.reader.NameWithContext(ctx)
	record("name", validText(name), err)
	if err := ctx.Err(); err != nil {
		record("inspection", false, err)
	}
	info.Inspectable = len(problems) == 0
	info.Error = strings.Join(problems, "; ")
	return info
}

func validText(value string) bool {
	return value != "" && strings.IndexByte(value, 0) < 0
}

func validCommandLine(arguments []string) bool {
	for _, argument := range arguments {
		if strings.IndexByte(argument, 0) >= 0 {
			return false
		}
	}
	return true
}
