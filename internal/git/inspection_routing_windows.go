package git

import (
	"os"

	"golang.org/x/sys/windows"
)

func openInspectionPointer(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|int(windows.FILE_FLAG_OPEN_REPARSE_POINT), 0)
}
