package snapshot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

func openAdministrativeReadRootMetadata(directory string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(administrativeReadWindowsPath(directory))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open administrative metadata", Path: directory, Err: err}
	}
	file := os.NewFile(uintptr(handle), directory)
	if file == nil {
		return nil, errors.Join(fs.ErrInvalid, windows.CloseHandle(handle))
	}
	return file, nil
}

func openAdministrativeReadRoot(directory string) (*os.Root, error) {
	parentPath := filepath.Dir(directory)
	parent, err := os.OpenRoot(parentPath)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(directory)
	if parentPath == directory {
		name = "."
	}
	root, openErr := parent.OpenRoot(name)
	return root, errors.Join(openErr, parent.Close())
}

func openAdministrativeReadDirectory(parent *os.Root, name string) (*os.Root, error) {
	return parent.OpenRoot(name)
}

func openAdministrativeReadFile(parent *os.Root, name string) (*os.File, error) {
	return parent.OpenFile(name, os.O_RDONLY|int(windows.FILE_FLAG_OPEN_REPARSE_POINT), 0)
}

func validateAdministrativeReadNativeInfo(information fs.FileInfo) error {
	attributes, ok := information.Sys().(*syscall.Win32FileAttributeData)
	if !ok || attributes == nil {
		return fmt.Errorf("administrative native attributes are unavailable")
	}
	if attributes.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DEVICE) != 0 {
		return fmt.Errorf("administrative reparse or device records are unsupported")
	}
	return nil
}
