package git

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/hellices/treeclear/internal/pathutil"
	"golang.org/x/sys/windows"
)

func resolveNativeGitPath(path string) (resolved string, resultErr error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	buffer := make([]uint16, maxInspectionPointerBytes+1)
	length, err := windows.GetFullPathName(pointer, uint32(len(buffer)), &buffer[0], nil)
	if err != nil {
		return "", err
	}
	absolute, err := boundedNativeGitPath(buffer, length)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	volume := filepath.VolumeName(absolute)
	volumeLength := len(volume)
	if strings.HasPrefix(volume, `\\`) {
		volume = strings.ToLower(volume)
	} else {
		volume = strings.ToUpper(volume)
	}
	absolute = volume + absolute[volumeLength:]
	file, err := openNativeGitPath(absolute, false)
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	initial, err := file.Stat()
	if err != nil {
		return "", err
	}
	if err := validateReadNativeInfo(initial); err != nil {
		return "", err
	}
	if initial.Mode().Type() != 0 && initial.Mode().Type() != fs.ModeDir {
		return "", fmt.Errorf("unsupported native Git path target: %w", errors.ErrUnsupported)
	}
	length, err = windows.GetFinalPathNameByHandle(windows.Handle(file.Fd()), &buffer[0], uint32(len(buffer)), 0)
	if err != nil {
		return "", err
	}
	resolved, err = boundedNativeGitPath(buffer, length)
	if err != nil {
		return "", err
	}
	current, err := file.Stat()
	if err != nil {
		return "", err
	}
	if err := compareInspectionInfo(initial, current); err != nil {
		return "", err
	}
	if strings.HasPrefix(resolved, `\\?\UNC\`) {
		resolved = `\\` + resolved[8:]
	} else {
		resolved = strings.TrimPrefix(resolved, `\\?\`)
	}
	resolved, err = pathutil.Canonical(resolved)
	if err != nil {
		return "", err
	}
	boundFile, err := openNativeGitPath(resolved, true)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			err = fmt.Errorf("%w: resolved Git path disappeared: %w", ErrWorktreeChanged, err)
		}
		return "", err
	}
	bound, statErr := boundFile.Stat()
	if err := errors.Join(statErr, boundFile.Close()); err != nil {
		return "", err
	}
	if err := compareInspectionInfo(initial, bound); err != nil {
		return "", err
	}
	return resolved, nil
}

func openNativeGitPath(path string, nofollow bool) (*os.File, error) {
	if err := validateGitPath(path); err != nil {
		return nil, err
	}
	if err := pathutil.ValidateAbsoluteForm(path); err != nil {
		return nil, err
	}
	extended := `\\?\` + path
	if strings.HasPrefix(path, `\\`) {
		extended = `\\?\UNC\` + path[2:]
	}
	pointer, err := windows.UTF16PtrFromString(extended)
	if err != nil {
		return nil, err
	}
	flags := uint32(windows.FILE_FLAG_BACKUP_SEMANTICS)
	if nofollow {
		flags |= windows.FILE_FLAG_OPEN_REPARSE_POINT
	}
	handle, err := windows.CreateFile(pointer, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}
