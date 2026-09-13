//go:build !darwin && !linux && !windows

package fssecure

import (
	"errors"
	"os"
)

func preparePrivatePath(path string) (string, error) {
	return "", errors.ErrUnsupported
}

func makePrivateDirectory(path string) error {
	return errors.ErrUnsupported
}

func secureExistingDirectory(path string) error {
	return errors.ErrUnsupported
}

func verifyPrivateDirectory(path string) error {
	return errors.ErrUnsupported
}

func createPrivateFile(path string) (*os.File, error) {
	return nil, errors.ErrUnsupported
}

func openPrivateFile(path string) (*os.File, error) {
	return nil, errors.ErrUnsupported
}
