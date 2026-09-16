package fssecure

import (
	"context"
	"fmt"
	"io/fs"
	"math"
	"path/filepath"
	"strings"
)

type PrivateFile struct {
	Name     string
	Contents []byte
}

func PublishPrivateDirectory(ctx context.Context, path string, files []PrivateFile) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("nil private directory context: %w", fs.ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	trimmed := strings.TrimRight(filepath.FromSlash(path), string(filepath.Separator))
	_, leaf := filepath.Split(trimmed)
	if leaf == "" || leaf == "." || leaf == ".." || strings.ContainsRune(path, 0) || len(files) == 0 {
		return "", fmt.Errorf("invalid private directory destination or empty file set: %w", fs.ErrInvalid)
	}
	names := make(map[string]bool, len(files))
	for _, file := range files {
		folded := strings.ToLower(file.Name)
		if !validPrivateDirectoryLeaf(file.Name) || names[folded] || int64(len(file.Contents)) == math.MaxInt64 {
			return "", fmt.Errorf("invalid or conflicting private directory file %q: %w", file.Name, fs.ErrInvalid)
		}
		names[folded] = true
	}
	return publishPrivateDirectory(ctx, path, files)
}

func validPrivateDirectoryLeaf(name string) bool {
	if name == "" || len(name) > 255 || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") || strings.ContainsAny(name, `/\<>:"|?*`) {
		return false
	}
	for _, character := range name {
		if character < 32 || character >= 127 {
			return false
		}
	}
	stem, _, _ := strings.Cut(name, ".")
	stem = strings.ToUpper(strings.TrimRight(stem, " "))
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return false
	}
	return !((strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && len(stem) == 4 && stem[3] >= '1' && stem[3] <= '9')
}
