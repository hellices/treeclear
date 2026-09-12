package pathutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Canonical(path string) (string, error) {
	if path == "" || strings.IndexByte(path, 0) >= 0 {
		return "", fmt.Errorf("invalid path %q", path)
	}
	prepared, err := preparePath(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(prepared) {
		workingDirectory, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("absolute path %q: %w", path, err)
		}
		prepared = workingDirectory + string(filepath.Separator) + prepared
	}
	resolved, err := filepath.EvalSymlinks(prepared)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}
	normalized, err := preparePath(resolved)
	if err != nil {
		return "", err
	}
	return filepath.Clean(normalized), nil
}

func Contains(parent, child string) bool {
	contained, err := ContainsChecked(parent, child)
	return err == nil && contained
}

func ContainsChecked(parent, child string) (bool, error) {
	canonicalParent, err := Canonical(parent)
	if err != nil {
		return false, fmt.Errorf("containment parent %q: %w", parent, err)
	}
	canonicalChild, err := Canonical(child)
	if err != nil {
		return false, fmt.Errorf("containment child %q: %w", child, err)
	}
	parentInfo, err := os.Stat(canonicalParent)
	if err != nil {
		return false, fmt.Errorf("inspect containment parent %q: %w", canonicalParent, err)
	}
	relative, err := filepath.Rel(canonicalParent, canonicalChild)
	if err != nil {
		return false, nil
	}
	if filepath.IsLocal(relative) {
		ancestor := canonicalChild
		if relative != "." {
			for range strings.Split(relative, string(filepath.Separator)) {
				ancestor = filepath.Dir(ancestor)
			}
		}
		ancestorInfo, err := os.Stat(ancestor)
		if err != nil {
			return false, fmt.Errorf("inspect containment ancestor %q: %w", ancestor, err)
		}
		return os.SameFile(parentInfo, ancestorInfo), nil
	}
	for ancestor := canonicalChild; ; {
		ancestorInfo, err := os.Stat(ancestor)
		if err != nil {
			return false, fmt.Errorf("inspect containment ancestor %q: %w", ancestor, err)
		}
		if os.SameFile(parentInfo, ancestorInfo) {
			return true, nil
		}
		next := filepath.Dir(ancestor)
		if next == ancestor {
			return false, nil
		}
		ancestor = next
	}
}
