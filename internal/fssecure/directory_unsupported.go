//go:build !darwin

package fssecure

import (
	"context"
	"errors"
)

func publishPrivateDirectory(ctx context.Context, path string, files []PrivateFile) (string, error) {
	return "", errors.ErrUnsupported
}
