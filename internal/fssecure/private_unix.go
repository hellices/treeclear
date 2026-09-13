//go:build darwin || linux

package fssecure

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func preparePrivatePath(path string) (string, error) {
	path, err := resolvePrivateParents(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}

func makePrivateDirectory(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil {
		return err
	}
	if err := secureExistingDirectory(path); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	return nil
}

func secureExistingDirectory(path string) (result error) {
	file, err := openUnixObject(path, true)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, file.Close()) }()
	if err := file.Chmod(0o700); err != nil {
		return err
	}
	return verifyUnixPrivacy(file, true)
}

func verifyPrivateDirectory(path string) (result error) {
	file, err := openUnixObject(path, true)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, file.Close()) }()
	return verifyUnixPrivacy(file, true)
}

func createPrivateFile(path string) (*os.File, error) {
	descriptor, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, &os.PathError{Op: "create private file", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(descriptor), path)
	if err := file.Chmod(0o600); err != nil {
		return nil, errors.Join(err, file.Close(), os.Remove(path))
	}
	if err := verifyUnixPrivacy(file, false); err != nil {
		return nil, errors.Join(err, file.Close(), os.Remove(path))
	}
	return file, nil
}

func openPrivateFile(path string) (*os.File, error) {
	file, err := openUnixObject(path, false)
	if err != nil {
		return nil, err
	}
	if err := verifyUnixPrivacy(file, false); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func openUnixObject(path string, directory bool) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unsupported private object type at %q: %w", path, fs.ErrInvalid)
	}
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	if directory {
		flags |= unix.O_DIRECTORY
	}
	descriptor, err := unix.Open(path, flags, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open private object", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(descriptor), path)
	if err := verifyUnixObject(file, directory); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func verifyUnixObject(file *os.File, directory bool) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
		return fmt.Errorf("opened private object %q has the wrong type: %w", file.Name(), fs.ErrInvalid)
	}
	if err := verifyUnixOwner(info); err != nil {
		return err
	}
	return verifyUnixSecuritySupport(file)
}

func verifyUnixOwner(info fs.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil || os.Geteuid() < 0 || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("private object ownership is not the current user: %w", fs.ErrPermission)
	}
	return nil
}

func verifyUnixPrivacy(file *os.File, directory bool) error {
	if err := verifyUnixObject(file, directory); err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	mode := info.Mode()
	if mode.Perm()&0o077 != 0 || mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || directory && mode.Perm() != 0o700 {
		return fmt.Errorf("object %q does not have private Unix permissions: %w", file.Name(), fs.ErrPermission)
	}
	return nil
}
