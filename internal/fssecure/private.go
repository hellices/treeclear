package fssecure

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func EnsurePrivateDirectory(path string) error {
	path, err := privatePath(path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return secureExistingDirectory(path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := ensureMissingAncestors(filepath.Dir(path)); err != nil {
		return err
	}
	if err := makePrivateDirectory(path); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return secureExistingDirectory(path)
}

func WritePrivateFile(path string, contents []byte) error {
	path, err := privatePath(path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return &os.PathError{Op: "publish private file", Path: path, Err: fs.ErrExist}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(path)
	if resolved, err := filepath.EvalSymlinks(parent); err == nil {
		parent = resolved
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := EnsurePrivateDirectory(parent); err != nil {
		return err
	}
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	if err := verifyPrivateDirectory(parent); err != nil {
		return err
	}
	for range 10 {
		stagingPath := filepath.Join(parent, ".treeclear-"+rand.Text()+".tmp")
		staged, err := createPrivateFile(stagingPath)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		return publishPrivateFile(staged, filepath.Join(parent, filepath.Base(path)), contents)
	}
	return fmt.Errorf("create private staging file: too many name collisions")
}

func ReadPrivateFile(path string, maximumBytes int64) (contents []byte, result error) {
	if maximumBytes <= 0 || uint64(maximumBytes) >= uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("invalid private file size limit %d: %w", maximumBytes, fs.ErrInvalid)
	}
	path, err := privatePath(path)
	if err != nil {
		return nil, err
	}
	file, err := openPrivateFile(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			contents = nil
			result = errors.Join(result, err)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() < 0 || info.Size() > maximumBytes {
		return nil, fmt.Errorf("private file %q exceeds size limit %d: %w", path, maximumBytes, fs.ErrInvalid)
	}
	contents, err = io.ReadAll(io.LimitReader(file, maximumBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > maximumBytes {
		return nil, fmt.Errorf("private file %q grew beyond size limit %d: %w", path, maximumBytes, fs.ErrInvalid)
	}
	return contents, nil
}

func privatePath(path string) (string, error) {
	if path == "" {
		return "", &os.PathError{Op: "private path", Path: path, Err: fs.ErrInvalid}
	}
	return preparePrivatePath(path)
}

func ensureMissingAncestors(path string) error {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("private directory ancestor %q is not a directory", path)
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return err
	}
	if err := ensureMissingAncestors(parent); err != nil {
		return err
	}
	if err := makePrivateDirectory(path); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
		return verifyPrivateDirectory(path)
	}
	return nil
}

func publishPrivateFile(staged *os.File, path string, contents []byte) (result error) {
	closed := false
	defer func() {
		if !closed {
			result = errors.Join(result, staged.Close())
		}
		if err := os.Remove(staged.Name()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("remove private staging file: %w", err))
		}
	}()
	written, err := staged.Write(contents)
	if err != nil {
		return err
	}
	if written != len(contents) {
		return io.ErrShortWrite
	}
	if err := staged.Sync(); err != nil {
		return err
	}
	err = staged.Close()
	closed = true
	if err != nil {
		return err
	}
	if err := os.Link(staged.Name(), path); err != nil {
		return fmt.Errorf("publish private file: %w", err)
	}
	return nil
}
