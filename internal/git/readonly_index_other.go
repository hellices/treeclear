//go:build !windows

package git

import (
	"fmt"
	"io/fs"
)

func validateReadNativeInfo(information fs.FileInfo) error {
	if information == nil || information.Sys() == nil {
		return fmt.Errorf("native file metadata is unavailable: %w", fs.ErrInvalid)
	}
	return nil
}
