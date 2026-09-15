package process

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

func nativeDarwinTable(ctx context.Context) ([]unix.KinfoProc, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	library, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	defer purego.Dlclose(library)
	sysctlAddress, err := purego.Dlsym(library, "sysctl")
	if err != nil {
		return nil, err
	}
	errnoAddress, err := purego.Dlsym(library, "__error")
	if err != nil {
		return nil, err
	}
	var sysctl func(*int32, uint32, unsafe.Pointer, *uintptr, unsafe.Pointer, uintptr) int32
	var errnoLocation func() *int32
	purego.RegisterFunc(&sysctl, sysctlAddress)
	purego.RegisterFunc(&errnoLocation, errnoAddress)
	return readDarwinTable(ctx, func(buffer []unix.KinfoProc, size *uintptr) error {
		mib := [...]int32{unix.CTL_KERN, 14, 0, 0}
		var contents unsafe.Pointer
		if len(buffer) != 0 {
			contents = unsafe.Pointer(&buffer[0])
		}
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		errno := errnoLocation()
		status := sysctl(&mib[0], uint32(len(mib)), contents, size, nil, 0)
		var failure error
		if status != 0 {
			failure = unix.EIO
			if *errno != 0 {
				failure = unix.Errno(*errno)
			}
		}
		runtime.KeepAlive(buffer)
		return failure
	})
}

func readDarwinTable(ctx context.Context, read func([]unix.KinfoProc, *uintptr) error) ([]unix.KinfoProc, error) {
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var size uintptr
		if err := read(nil, &size); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if size == 0 || size%unix.SizeofKinfoProc != 0 || size/unix.SizeofKinfoProc > darwinProcessLimit {
			return nil, errors.New("native process table reported an invalid or excessive allocation size")
		}
		capacity := size
		buffer := make([]unix.KinfoProc, size/unix.SizeofKinfoProc)
		err := read(buffer, &size)
		if cancellation := ctx.Err(); cancellation != nil {
			return nil, cancellation
		}
		if errors.Is(err, unix.ENOMEM) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if size == 0 || size > capacity || size%unix.SizeofKinfoProc != 0 {
			return nil, errors.New("native process table returned an invalid byte count")
		}
		return buffer[:size/unix.SizeofKinfoProc], nil
	}
	return nil, fmt.Errorf("native process table sizing exhausted after 3 attempts: %w", unix.ENOMEM)
}
