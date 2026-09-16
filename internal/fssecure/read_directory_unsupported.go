//go:build !darwin

package fssecure

import (
	"context"
	"errors"
)

func readPrivateDirectory(ctx context.Context, path string, limits []PrivateFileLimit, maximumBytes int64) ([]PrivateFile, error) {
	return nil, errors.ErrUnsupported
}
