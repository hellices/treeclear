package fssecure

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

const directoryReadFlags = unix.O_RDONLY | unix.O_NOFOLLOW_ANY | unix.O_NONBLOCK | unix.O_CLOEXEC

type directoryOperations struct {
	openat   func(int, string, int, uint32) (*os.File, error)
	mkdirat  func(int, string, uint32) error
	write    func(*os.File, []byte) (int, error)
	read     func(*os.File, []byte) (int, error)
	sync     func(*os.File) error
	close    func(*os.File) error
	renameat func(int, string, int, string, uint32) error
	unlinkat func(int, string, int) error
}

func nativeDirectoryOperations() directoryOperations {
	return directoryOperations{
		openat:   openDirectoryObjectAt,
		mkdirat:  unix.Mkdirat,
		write:    (*os.File).Write,
		read:     (*os.File).Read,
		sync:     (*os.File).Sync,
		close:    (*os.File).Close,
		renameat: unix.RenameatxNp,
		unlinkat: unix.Unlinkat,
	}
}

func openDirectoryObjectAt(descriptor int, name string, flags int, mode uint32) (*os.File, error) {
	opened, err := unix.Openat(descriptor, name, flags, mode)
	if err != nil {
		return nil, &os.PathError{Op: "open private directory object", Path: name, Err: err}
	}
	return os.NewFile(uintptr(opened), name), nil
}

func directoryStatAt(descriptor int, name string) (unix.Stat_t, error) {
	var identity unix.Stat_t
	if err := unix.Fstatat(descriptor, name, &identity, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return identity, &os.PathError{Op: "inspect private directory entry", Path: name, Err: err}
	}
	return identity, nil
}

func verifyDirectoryHandle(file *os.File, identity unix.Stat_t, directory bool) error {
	if err := verifyUnixPrivacy(file, directory); err != nil {
		return err
	}
	var actual unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &actual); err != nil {
		return err
	}
	if !sameDirectoryIdentity(actual, identity) || !directory && (actual.Mode != unix.S_IFREG|0o600 || actual.Nlink != 1) {
		return fmt.Errorf("private object %q identity or permissions changed: %w", file.Name(), fs.ErrInvalid)
	}
	return nil
}

func sameDirectoryIdentity(actual, expected unix.Stat_t) bool {
	return actual.Dev == expected.Dev && actual.Ino == expected.Ino && actual.Gen == expected.Gen && actual.Mode&unix.S_IFMT == expected.Mode&unix.S_IFMT
}

func sameDirectoryFileSnapshot(actual, expected unix.Stat_t) bool {
	return sameDirectoryIdentity(actual, expected) && actual.Mode == expected.Mode && actual.Nlink == expected.Nlink && actual.Uid == expected.Uid && actual.Gid == expected.Gid && actual.Size == expected.Size && actual.Mtim == expected.Mtim && actual.Ctim == expected.Ctim && actual.Btim == expected.Btim && actual.Flags == expected.Flags
}

func verifyDirectoryParentPath(path string, identity unix.Stat_t) (result error) {
	parent, err := openDirectoryObjectAt(unix.AT_FDCWD, path, directoryReadFlags|unix.O_DIRECTORY, 0)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, parent.Close()) }()
	return verifyDirectoryHandle(parent, identity, true)
}

type directoryReader struct {
	publisher *directoryPublisher
	context   context.Context
	file      *os.File
	identity  unix.Stat_t
}

func (reader directoryReader) Read(buffer []byte) (int, error) {
	if err := reader.publisher.check(reader.context); err != nil {
		return 0, err
	}
	count, readErr := reader.publisher.operations.read(reader.file, buffer)
	if err := errors.Join(reader.publisher.check(reader.context), verifyDirectoryHandle(reader.file, reader.identity, false)); err != nil {
		return 0, errors.Join(readErr, err)
	}
	if count < 0 || count > len(buffer) {
		return 0, fmt.Errorf("invalid private read length: %w", fs.ErrInvalid)
	}
	if count == 0 && readErr == nil && len(buffer) != 0 {
		return 0, io.ErrNoProgress
	}
	return count, readErr
}
