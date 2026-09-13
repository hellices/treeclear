//go:build darwin || linux

package snapshot

import (
	"io/fs"
	"os"
	"strings"
	"syscall"
)

func openAdministrativeReadRootMetadata(directory string) (*os.File, error) {
	return os.OpenFile(directory, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_DIRECTORY, 0)
}

func openAdministrativeReadRoot(directory string) (*os.Root, error) {
	if !strings.HasSuffix(directory, "/") {
		directory += "/"
	}
	return os.OpenRoot(directory)
}

func openAdministrativeReadDirectory(parent *os.Root, name string) (*os.Root, error) {
	return parent.OpenRoot(name + "/.")
}

func openAdministrativeReadFile(parent *os.Root, name string) (*os.File, error) {
	return parent.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}

func validateAdministrativeReadNativeInfo(information fs.FileInfo) error {
	return nil
}
