//go:build darwin || linux

package git

import (
	"os"
	"syscall"
)

func openInspectionPointer(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
