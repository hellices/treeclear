//go:build !darwin && !linux && !windows

package git

import (
	"errors"
	"os"
)

func openInspectionPointer(root *os.Root, name string) (*os.File, error) {
	return nil, errors.ErrUnsupported
}
