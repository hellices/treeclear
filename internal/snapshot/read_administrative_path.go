package snapshot

import "strings"

func administrativeReadWindowsPath(directory string) string {
	if strings.HasPrefix(directory, `\\?\`) {
		return directory
	}
	if strings.HasPrefix(directory, `\\`) {
		return `\\?\UNC\` + directory[2:]
	}
	return `\\?\` + directory
}
