//go:build !windows

package git

import "path/filepath"

func resolveNativeGitPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
