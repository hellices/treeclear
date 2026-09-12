//go:build !windows

package pathutil

func preparePath(path string) (string, error) {
	return path, nil
}
