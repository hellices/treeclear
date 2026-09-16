package fssecure

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

type PrivateFileLimit struct {
	Name         string
	MaximumBytes int64
}

var ErrPrivateDirectoryLimit = errors.New("private directory byte limit exceeded")

func ReadPrivateDirectory(ctx context.Context, path string, limits []PrivateFileLimit, maximumBytes int64) ([]PrivateFile, error) {
	if ctx == nil {
		return nil, fmt.Errorf("nil private directory context: %w", fs.ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	nativeMaximum := int64(int(^uint(0) >> 1))
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path || strings.ContainsRune(path, 0) {
		return nil, fmt.Errorf("invalid private directory path: %w", fs.ErrInvalid)
	}
	if len(limits) == 0 || int64(len(limits)) == nativeMaximum || maximumBytes <= 0 || maximumBytes >= nativeMaximum {
		return nil, fmt.Errorf("invalid private directory limits: %w", fs.ErrInvalid)
	}
	names := make(map[string]bool, len(limits))
	for _, limit := range limits {
		folded := strings.ToLower(limit.Name)
		if !validPrivateDirectoryLeaf(limit.Name) || names[folded] || limit.MaximumBytes <= 0 || limit.MaximumBytes >= nativeMaximum {
			return nil, fmt.Errorf("invalid private directory file limit %q: %w", limit.Name, fs.ErrInvalid)
		}
		names[folded] = true
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return readPrivateDirectory(ctx, path, limits, maximumBytes)
}
