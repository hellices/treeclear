package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

const darwinProcessPathLimit = 4096

func nativeDarwinExecutable(ctx context.Context, pid int32) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	library, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return "", err
	}
	defer purego.Dlclose(library)
	pathAddress, err := purego.Dlsym(library, "proc_pidpath")
	if err != nil {
		return "", err
	}
	errnoAddress, err := purego.Dlsym(library, "__error")
	if err != nil {
		return "", err
	}
	var pathRead func(int32, unsafe.Pointer, uint32) int32
	var errnoLocation func() *int32
	purego.RegisterFunc(&pathRead, pathAddress)
	purego.RegisterFunc(&errnoLocation, errnoAddress)
	return readDarwinExecutable(ctx, pid, func(pid int32, buffer []byte) (int32, unix.Errno) {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		errno := errnoLocation()
		*errno = 0
		count := pathRead(pid, unsafe.Pointer(&buffer[0]), uint32(len(buffer)))
		var failure unix.Errno
		if count <= 0 {
			failure = unix.Errno(*errno)
		}
		runtime.KeepAlive(buffer)
		return count, failure
	})
}

func readDarwinExecutable(ctx context.Context, pid int32, read func(int32, []byte) (int32, unix.Errno)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if pid <= 0 || read == nil {
		return "", errors.New("invalid native executable lookup")
	}
	buffer := make([]byte, darwinProcessPathLimit)
	count, errno := read(pid, buffer)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if count <= 0 {
		if errno == 0 {
			return "", fmt.Errorf("proc_pidpath errno 0: missing native error: %w", unix.EIO)
		}
		return "", fmt.Errorf("proc_pidpath errno %d: %w", errno, errno)
	}
	if count >= int32(len(buffer)) || buffer[count] != 0 || bytes.IndexByte(buffer[:count], 0) >= 0 {
		return "", errors.New("native executable path has an invalid length or terminator")
	}
	path := string(buffer[:count])
	if !absolutePath(path) {
		return "", errors.New("native executable path is not absolute")
	}
	return path, nil
}
