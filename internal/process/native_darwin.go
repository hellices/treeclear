package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"time"

	gprocess "github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/unix"
)

const (
	darwinSystemProcess     = 0x200
	darwinRunning           = 2
	darwinZombie            = 5
	darwinProcessLimit      = 65536
	DarwinCollectionTimeout = 20 * time.Second
)

type darwinProcess struct {
	pid       int32
	parentPID int32
	created   time.Time
	uid       uint32
	name      string
	state     int8
	flags     int32
}

type DarwinSource struct {
	snapshot   func(context.Context) ([]darwinProcess, error)
	reader     func(darwinProcess) processReader
	currentUID func() int
}

func NativeSource() Source {
	return DarwinSource{}
}

func (source DarwinSource) List(parent context.Context) ([]Info, error) {
	ctx, cancel := context.WithTimeout(parent, DarwinCollectionTimeout)
	defer cancel()
	snapshot := source.snapshot
	if snapshot == nil {
		snapshot = readDarwinSnapshot
	}
	reader := source.reader
	if reader == nil {
		reader = func(record darwinProcess) processReader {
			return darwinReader{Process: &gprocess.Process{Pid: record.pid}, record: record}
		}
	}
	currentUID := source.currentUID
	if currentUID == nil {
		currentUID = os.Geteuid
	}
	uid := currentUID()
	if uid < 0 || uint64(uid) > uint64(^uint32(0)) {
		return nil, errors.New("current process owner is unavailable or malformed")
	}
	owner := strconv.Itoa(uid)
	var infos []Info
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return infos, err
		}
		before, err := snapshot(ctx)
		if err == nil {
			err = validateDarwinSnapshot(before)
		}
		if err != nil {
			return infos, fmt.Errorf("before native process enumeration: %w", err)
		}
		var handles []processHandle
		var pids []int32
		creation := make(map[int32]time.Time, len(before))
		for _, record := range before {
			if record.pid == 0 || record.state == darwinZombie {
				continue
			}
			handles = append(handles, processHandle{pid: record.pid, reader: reader(record)})
			pids = append(pids, record.pid)
			creation[record.pid] = record.created
		}
		if len(handles) != 0 {
			inspection := GopsutilSource{
				processes:   func(context.Context) ([]processHandle, error) { return handles, nil },
				pids:        func(context.Context) ([]int32, error) { return pids, nil },
				currentUser: func() (*user.User, error) { return &user.User{Username: owner}, nil },
			}
			infos, err = inspection.List(ctx)
			for index := range infos {
				infos[index].CreatedAt = creation[infos[index].PID]
			}
			if err != nil {
				return infos, err
			}
		} else {
			infos = nil
		}
		after, err := snapshot(ctx)
		if err == nil {
			err = validateDarwinSnapshot(after)
		}
		if err != nil {
			return infos, fmt.Errorf("after native process enumeration: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return infos, err
		}
		if sameDarwinProcesses(before, after) {
			return infos, nil
		}
	}
	return infos, errors.New("native process identities changed during inspection; complete resampling exhausted after 3 attempts")
}

func readDarwinSnapshot(ctx context.Context) ([]darwinProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	records, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	if len(records) > darwinProcessLimit {
		return nil, errors.New("native process enumeration exceeds the record limit")
	}
	processes := make([]darwinProcess, 0, len(records))
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var created time.Time
		if record.Proc.P_starttime.Sec > 0 && record.Proc.P_starttime.Usec >= 0 && record.Proc.P_starttime.Usec < 1000000 {
			created = time.Unix(record.Proc.P_starttime.Sec, int64(record.Proc.P_starttime.Usec)*int64(time.Microsecond)).UTC()
		}
		processes = append(processes, darwinProcess{
			pid: record.Proc.P_pid, parentPID: record.Eproc.Ppid, created: created,
			uid: record.Eproc.Ucred.Uid, name: unix.ByteSliceToString(record.Proc.P_comm[:]),
			state: record.Proc.P_stat, flags: record.Proc.P_flag,
		})
	}
	return processes, nil
}

func validateDarwinSnapshot(records []darwinProcess) error {
	if len(records) < 2 || len(records) > darwinProcessLimit {
		return errors.New("native process enumeration has an invalid record count")
	}
	seen := make(map[int32]bool, len(records))
	for _, record := range records {
		if record.pid < 0 || seen[record.pid] || !record.created.After(time.Unix(0, 0)) || !validText(record.name) || record.state < 1 || record.state > darwinZombie {
			return fmt.Errorf("native process %d has invalid or conflicting identity", record.pid)
		}
		seen[record.pid] = true
		if record.pid == 0 && (record.parentPID != 0 || record.uid != 0 || record.flags&darwinSystemProcess == 0 || record.state != darwinRunning || record.name != "kernel_task") {
			return errors.New("native PID 0 is not a proven kernel-only record")
		}
	}
	if !seen[0] {
		return errors.New("native process enumeration is missing its kernel identity")
	}
	return nil
}

func sameDarwinProcesses(before, after []darwinProcess) bool {
	if len(before) != len(after) {
		return false
	}
	identities := make(map[int32]darwinProcess, len(before))
	for _, record := range before {
		identities[record.pid] = record
	}
	for _, current := range after {
		previous, found := identities[current.pid]
		if !found || !previous.created.Equal(current.created) || previous.uid != current.uid || previous.parentPID != current.parentPID || previous.name != current.name || previous.flags&darwinSystemProcess != current.flags&darwinSystemProcess || (previous.state == darwinZombie) != (current.state == darwinZombie) {
			return false
		}
	}
	return true
}

type darwinReader struct {
	*gprocess.Process
	record darwinProcess
}

func (reader darwinReader) CreateTimeWithContext(context.Context) (int64, error) {
	return reader.record.created.UnixMilli(), nil
}

func (reader darwinReader) UsernameWithContext(context.Context) (string, error) {
	return strconv.FormatUint(uint64(reader.record.uid), 10), nil
}

func (reader darwinReader) NameWithContext(context.Context) (string, error) {
	return reader.record.name, nil
}
