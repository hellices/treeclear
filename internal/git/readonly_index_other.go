//go:build !windows

package git

import (
	"fmt"
	"io/fs"
	"os"
)

func lstatReadonlyIndexDirectory(directory string) (fs.FileInfo, error) {
	return os.Lstat(directory)
}

func validateReadNativeInfo(information fs.FileInfo) error {
	if information == nil || information.Sys() == nil {
		return fmt.Errorf("native file metadata is unavailable: %w", fs.ErrInvalid)
	}
	return nil
}
