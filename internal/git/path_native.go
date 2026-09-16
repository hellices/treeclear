package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func resolveGitPath(ctx context.Context, path string) (resolved string, resultErr error) {
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			resolved = ""
		}
	}()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validateGitPath(path); err != nil {
		return "", err
	}
	resolved, err := resolveNativeGitPath(path)
	if err != nil {
		return "", err
	}
	if err := validateGitPath(resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func validateGitPath(path string) error {
	if len(path) > maxInspectionPointerBytes {
		return fmt.Errorf("Git path exceeds limit: %w", ErrReadLimit)
	}
	if !utf8.ValidString(path) || strings.IndexByte(path, 0) >= 0 || !filepath.IsAbs(path) {
		return fmt.Errorf("invalid Git path: %w", fs.ErrInvalid)
	}
	return nil
}

func boundedNativeGitPath(buffer []uint16, length uint32) (string, error) {
	if length == 0 {
		return "", fmt.Errorf("native Git path is empty: %w", fs.ErrInvalid)
	}
	if uint64(length) >= uint64(len(buffer)) {
		return "", fmt.Errorf("native Git path exceeds buffer: %w", ErrReadLimit)
	}
	if buffer[length] != 0 {
		return "", fmt.Errorf("native Git path is not terminated: %w", fs.ErrInvalid)
	}
	units := buffer[:length]
	path := string(utf16.Decode(units))
	if err := validateGitPath(path); err != nil {
		return "", err
	}
	if !slices.Equal(units, utf16.Encode([]rune(path))) {
		return "", fmt.Errorf("native Git path encoding is invalid: %w", fs.ErrInvalid)
	}
	return path, nil
}
