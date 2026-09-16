package git

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"

	"golang.org/x/sys/windows"
)

func lstatReadonlyIndexDirectory(directory string) (fs.FileInfo, error) {
	file, err := openNativeGitPath(directory, true)
	if err != nil {
		return nil, err
	}
	information, statErr := file.Stat()
	if err := errors.Join(statErr, file.Close()); err != nil {
		return nil, err
	}
	return information, nil
}

func validateReadNativeInfo(information fs.FileInfo) error {
	if information == nil {
		return fmt.Errorf("native file metadata is unavailable: %w", fs.ErrInvalid)
	}
	attributes, ok := information.Sys().(*syscall.Win32FileAttributeData)
	if !ok || attributes == nil {
		return fmt.Errorf("native file metadata is unavailable: %w", fs.ErrInvalid)
	}
	if attributes.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DEVICE) != 0 {
		return fmt.Errorf("native reparse or device records are unsupported: %w", errors.ErrUnsupported)
	}
	return nil
}
