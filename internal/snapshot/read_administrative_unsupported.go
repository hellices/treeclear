//go:build !darwin && !linux && !windows

package snapshot

import (
	"errors"
	"io/fs"
	"os"
)

func openAdministrativeReadRootMetadata(directory string) (*os.File, error) {
	return nil, errors.ErrUnsupported
}

func openAdministrativeReadRoot(directory string) (*os.Root, error) {
	return nil, errors.ErrUnsupported
}

func openAdministrativeReadDirectory(parent *os.Root, name string) (*os.Root, error) {
	return nil, errors.ErrUnsupported
}

func openAdministrativeReadFile(parent *os.Root, name string) (*os.File, error) {
	return nil, errors.ErrUnsupported
}

func validateAdministrativeReadNativeInfo(information fs.FileInfo) error {
	return errors.ErrUnsupported
}
