package fssecure

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type privateDirectoryReader struct {
	operations        privateDirectoryReadOperations
	parent            *os.File
	parentPath        string
	parentIdentity    unix.Stat_t
	directory         *os.File
	directoryName     string
	directoryIdentity unix.Stat_t
	children          []privateDirectoryReadChild
}

type privateDirectoryReadChild struct {
	name     string
	identity unix.Stat_t
}

func readPrivateDirectory(ctx context.Context, path string, limits []PrivateFileLimit, maximumBytes int64) ([]PrivateFile, error) {
	reader := privateDirectoryReader{operations: nativePrivateDirectoryReadOperations()}
	return reader.read(ctx, path, limits, maximumBytes)
}

func (reader *privateDirectoryReader) read(ctx context.Context, path string, limits []PrivateFileLimit, maximumBytes int64) (files []PrivateFile, result error) {
	defer func() {
		if reader.directory != nil {
			result = errors.Join(result, reader.operations.close(reader.directory))
		}
		if reader.parent != nil {
			result = errors.Join(result, reader.operations.close(reader.parent))
		}
		result = errors.Join(result, ctx.Err())
		if result != nil {
			files = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := privatePath(path)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader.parentPath = filepath.Dir(path)
	reader.directoryName = filepath.Base(path)
	reader.parentIdentity, err = directoryStatAt(unix.AT_FDCWD, reader.parentPath)
	if err != nil {
		return nil, err
	}
	reader.parent, err = reader.operations.openat(unix.AT_FDCWD, reader.parentPath, directoryReadFlags|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	if err := reader.check(ctx); err != nil {
		return nil, err
	}
	reader.directoryIdentity, err = directoryStatAt(int(reader.parent.Fd()), reader.directoryName)
	if err != nil {
		return nil, err
	}
	reader.directory, err = reader.operations.openat(int(reader.parent.Fd()), reader.directoryName, directoryReadFlags|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	if err := reader.checkListing(ctx, limits); err != nil {
		return nil, err
	}
	for _, limit := range limits {
		if err := reader.check(ctx); err != nil {
			return nil, err
		}
		identity, err := directoryStatAt(int(reader.directory.Fd()), limit.Name)
		if err != nil {
			return nil, err
		}
		if identity.Mode != unix.S_IFREG|0o600 || identity.Nlink != 1 || identity.Size < 0 {
			return nil, fmt.Errorf("unsafe private file %q: %w", limit.Name, fs.ErrInvalid)
		}
		if identity.Uid != uint32(os.Geteuid()) {
			return nil, fmt.Errorf("private file %q is not owned by the current user: %w", limit.Name, fs.ErrPermission)
		}
		reader.children = append(reader.children, privateDirectoryReadChild{name: limit.Name, identity: identity})
	}
	remaining := maximumBytes
	for index, child := range reader.children {
		contents, err := reader.readChild(ctx, child, min(limits[index].MaximumBytes, remaining))
		if err != nil {
			return nil, err
		}
		remaining -= int64(len(contents))
		files = append(files, PrivateFile{Name: child.name, Contents: contents})
	}
	for _, child := range reader.children {
		if err := reader.verifyChild(ctx, child); err != nil {
			return nil, err
		}
	}
	if err := reader.checkListing(ctx, limits); err != nil {
		return nil, err
	}
	return files, nil
}

func (reader *privateDirectoryReader) readChild(ctx context.Context, child privateDirectoryReadChild, maximumBytes int64) (contents []byte, result error) {
	if err := reader.check(ctx); err != nil {
		return nil, err
	}
	if child.identity.Size > maximumBytes {
		return nil, fmt.Errorf("private file %q exceeds its remaining byte budget: %w", child.name, ErrPrivateDirectoryLimit)
	}
	file, err := reader.operations.openat(int(reader.directory.Fd()), child.name, directoryReadFlags, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		result = errors.Join(result, reader.operations.close(file), reader.check(ctx))
		if result != nil {
			contents = nil
		}
	}()
	if err := verifyPrivateDirectoryReadHandle(file, child.identity, false); err != nil {
		return nil, err
	}
	stream := privateDirectoryFileReader{directory: reader, context: ctx, file: file, identity: child.identity}
	contents, err = io.ReadAll(io.LimitReader(stream, maximumBytes+1))
	if int64(len(contents)) > maximumBytes {
		err = errors.Join(err, fmt.Errorf("private file %q exceeds its remaining byte budget: %w", child.name, ErrPrivateDirectoryLimit))
	}
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) != child.identity.Size {
		return nil, fmt.Errorf("private file %q length changed: %w", child.name, fs.ErrInvalid)
	}
	return contents, nil
}

func (reader *privateDirectoryReader) verifyChild(ctx context.Context, child privateDirectoryReadChild) error {
	if err := reader.check(ctx); err != nil {
		return err
	}
	file, err := reader.operations.openat(int(reader.directory.Fd()), child.name, directoryReadFlags, 0)
	if err != nil {
		return err
	}
	return errors.Join(verifyPrivateDirectoryReadHandle(file, child.identity, false), reader.operations.close(file), reader.check(ctx))
}

func (reader *privateDirectoryReader) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.Join(reader.checkStorage(), ctx.Err())
}

func (reader *privateDirectoryReader) checkStorage() error {
	if err := verifyPrivateDirectoryReadHandle(reader.parent, reader.parentIdentity, true); err != nil {
		return err
	}
	if err := reader.checkParentPath(); err != nil {
		return err
	}
	if reader.directory == nil {
		return nil
	}
	if err := verifyPrivateDirectoryReadHandle(reader.directory, reader.directoryIdentity, true); err != nil {
		return err
	}
	identity, err := directoryStatAt(int(reader.parent.Fd()), reader.directoryName)
	if err != nil {
		return err
	}
	if !sameDirectoryFileSnapshot(identity, reader.directoryIdentity) {
		return fmt.Errorf("private directory pathname identity or metadata changed: %w", fs.ErrInvalid)
	}
	for _, child := range reader.children {
		identity, err := directoryStatAt(int(reader.directory.Fd()), child.name)
		if err != nil {
			return err
		}
		if !sameDirectoryFileSnapshot(identity, child.identity) {
			return fmt.Errorf("private file %q identity or metadata changed: %w", child.name, fs.ErrInvalid)
		}
	}
	return nil
}

func (reader *privateDirectoryReader) checkParentPath() (result error) {
	parent, err := reader.operations.openat(unix.AT_FDCWD, reader.parentPath, directoryReadFlags|unix.O_DIRECTORY, 0)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, reader.operations.close(parent)) }()
	return verifyPrivateDirectoryReadHandle(parent, reader.parentIdentity, true)
}

func (reader *privateDirectoryReader) checkListing(ctx context.Context, limits []PrivateFileLimit) (result error) {
	if err := reader.check(ctx); err != nil {
		return err
	}
	listing, err := reader.operations.openat(int(reader.directory.Fd()), ".", directoryReadFlags|unix.O_DIRECTORY, 0)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, reader.operations.close(listing), reader.check(ctx)) }()
	if err := verifyPrivateDirectoryReadHandle(listing, reader.directoryIdentity, true); err != nil {
		return err
	}
	entries, err := reader.operations.readDir(listing, len(limits)+1)
	if err != nil && err != io.EOF {
		return err
	}
	if len(entries) != len(limits) {
		return fmt.Errorf("unexpected private directory entry count: %w", fs.ErrInvalid)
	}
	remaining := make(map[string]bool, len(limits))
	for _, limit := range limits {
		remaining[limit.Name] = true
	}
	for _, entry := range entries {
		if entry == nil || !remaining[entry.Name()] || !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected private directory entry: %w", fs.ErrInvalid)
		}
		delete(remaining, entry.Name())
	}
	return verifyPrivateDirectoryReadHandle(listing, reader.directoryIdentity, true)
}

func verifyPrivateDirectoryReadHandle(file *os.File, identity unix.Stat_t, directory bool) error {
	if err := verifyDirectoryHandle(file, identity, directory); err != nil {
		return err
	}
	var actual unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &actual); err != nil {
		return err
	}
	if !sameDirectoryFileSnapshot(actual, identity) {
		return fmt.Errorf("private object %q identity or metadata changed: %w", file.Name(), fs.ErrInvalid)
	}
	return nil
}
