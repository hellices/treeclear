package fssecure

import (
	"encoding/binary"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

func verifyUnixSecuritySupport(file *os.File) error {
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(int(file.Fd()), &filesystem); err != nil {
		return err
	}
	if filesystem.Flags&unix.MNT_IGNORE_OWNERSHIP != 0 || filesystem.Flags&unix.MNT_LOCAL == 0 {
		return fmt.Errorf("filesystem cannot guarantee local Unix ownership: %w", fs.ErrPermission)
	}
	switch unix.ByteSliceToString(filesystem.Fstypename[:]) {
	case "apfs", "hfs":
	default:
		return fmt.Errorf("unsupported private filesystem security: %w", fs.ErrPermission)
	}
	attributes := unix.Attrlist{Bitmapcount: 5, Commonattr: unix.ATTR_CMN_RETURNED_ATTRS | unix.ATTR_CMN_EXTENDED_SECURITY}
	var buffer [80]byte
	_, _, errno := unix.Syscall6(unix.SYS_FGETATTRLIST, file.Fd(), uintptr(unsafe.Pointer(&attributes)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), unix.FSOPT_REPORT_FULLSIZE, 0)
	runtime.KeepAlive(file)
	if errno != 0 {
		return fmt.Errorf("inspect extended file security: %w", errno)
	}
	length := binary.NativeEndian.Uint32(buffer[:4])
	returned := binary.NativeEndian.Uint32(buffer[4:8])
	if (length == 24 || length == 32) && returned == unix.ATTR_CMN_RETURNED_ATTRS {
		return nil
	}
	if length < 32 || length > uint32(len(buffer)) || returned&unix.ATTR_CMN_EXTENDED_SECURITY == 0 {
		return fmt.Errorf("unverifiable extended file security (length %d, attributes %#x): %w", length, returned, fs.ErrPermission)
	}
	offset := int64(int32(binary.NativeEndian.Uint32(buffer[24:28]))) + 24
	size := int64(binary.NativeEndian.Uint32(buffer[28:32]))
	if offset < 32 || size < 44 || offset+size > int64(length) {
		return fmt.Errorf("malformed extended file security: %w", fs.ErrPermission)
	}
	if binary.NativeEndian.Uint32(buffer[offset:offset+4]) != 0x012cc16d {
		return fmt.Errorf("unrecognized extended file security: %w", fs.ErrPermission)
	}
	count := binary.NativeEndian.Uint32(buffer[offset+36 : offset+40])
	if count != 0 && count != ^uint32(0) {
		return fmt.Errorf("extended ACLs are not supported for private files: %w", fs.ErrPermission)
	}
	return nil
}
