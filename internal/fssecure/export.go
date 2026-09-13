package fssecure

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func WritePrivateExport(stagingDirectory, destination string, contents []byte) error {
	destination, err := privatePath(destination)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return &os.PathError{Op: "export private file", Path: destination, Err: fs.ErrExist}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := EnsurePrivateDirectory(stagingDirectory); err != nil {
		return err
	}
	stagingDirectory, err = filepath.EvalSymlinks(stagingDirectory)
	if err != nil {
		return err
	}
	if err := verifyPrivateDirectory(stagingDirectory); err != nil {
		return err
	}
	parent := filepath.Dir(destination)
	if err := ensureMissingAncestors(parent); err != nil {
		return err
	}
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	for range 10 {
		stagingPath := filepath.Join(stagingDirectory, ".treeclear-export-"+rand.Text()+".tmp")
		staged, err := createPrivateFile(stagingPath)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		return publishPrivateFile(staged, filepath.Join(parent, filepath.Base(destination)), contents)
	}
	return fmt.Errorf("create private export staging file: too many name collisions")
}
