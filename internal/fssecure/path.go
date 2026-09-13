package fssecure

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func ResolvePrivatePath(path string) (string, error) {
	return privatePath(path)
}

func anchorPrivatePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	volume := filepath.VolumeName(path)
	relative := strings.TrimPrefix(path, volume)
	if volume != "" && !strings.EqualFold(volume, filepath.VolumeName(workingDirectory)) {
		workingDirectory, err = filepath.Abs(volume + ".")
		if err != nil {
			return "", err
		}
	}
	if strings.HasPrefix(relative, string(filepath.Separator)) {
		workingDirectory = filepath.VolumeName(workingDirectory) + string(filepath.Separator)
		relative = strings.TrimLeft(relative, string(filepath.Separator))
	}
	workingDirectory, err = filepath.EvalSymlinks(workingDirectory)
	if err != nil {
		return "", err
	}
	return workingDirectory + string(filepath.Separator) + relative, nil
}

func resolvePrivateParents(path string) (string, error) {
	path = filepath.FromSlash(path)
	path, err := anchorPrivatePath(path)
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimRight(path, string(filepath.Separator))
	if trimmed == "" || trimmed == filepath.VolumeName(path) {
		return path, nil
	}
	parent, name := filepath.Split(trimmed)
	if name == "." {
		if parent == "" {
			return path, nil
		}
		return resolvePrivateParents(parent)
	}
	if name == ".." {
		return filepath.EvalSymlinks(trimmed)
	}
	resolved, err := resolvePrivateAncestors(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, name), nil
}

func resolvePrivateAncestors(path string) (string, error) {
	if path == "" {
		path = "."
	}
	resolved, err := resolveExistingPrivateAncestor(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	trimmed := strings.TrimRight(path, string(filepath.Separator))
	parent, name := filepath.Split(trimmed)
	if name == "" || name == "." || name == ".." || trimmed == filepath.VolumeName(path) {
		return "", err
	}
	if info, lookupErr := os.Lstat(trimmed); lookupErr == nil {
		if info.IsDir() {
			return resolveExistingPrivateAncestor(path)
		}
		return "", err
	} else if !errors.Is(lookupErr, fs.ErrNotExist) {
		return "", lookupErr
	}
	resolved, err = resolvePrivateAncestors(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, name), nil
}

func resolveExistingPrivateAncestor(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("private path ancestor %q is not a directory: %w", path, fs.ErrInvalid)
	}
	return resolved, nil
}
