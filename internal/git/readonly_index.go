package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var ErrIndexPreflightLimit = errors.New("Git index preflight limit exceeded")

type readonlyIndexOperations struct {
	open      func(string) (*os.File, error)
	lstat     func(string) (fs.FileInfo, error)
	stat      func(*os.File) (fs.FileInfo, error)
	readNames func(*os.File, int) ([]string, error)
	close     func(*os.File) error
}

func defaultReadonlyIndexOperations() readonlyIndexOperations {
	return readonlyIndexOperations{
		open: func(directory string) (*os.File, error) {
			return os.Open(directory + string(filepath.Separator) + ".")
		},
		lstat:     readonlyIndexDirectoryInfo,
		stat:      (*os.File).Stat,
		readNames: (*os.File).Readdirnames,
		close:     (*os.File).Close,
	}
}

func readonlyIndexDirectoryInfo(directory string) (fs.FileInfo, error) {
	information, err := os.Lstat(directory)
	if err != nil {
		return information, err
	}
	if err := validateReadNativeInfo(information); err != nil {
		return nil, err
	}
	if information.Mode().Type() != fs.ModeDir {
		return information, nil
	}
	file, err := os.Open(directory + string(filepath.Separator) + ".")
	if err != nil {
		return nil, err
	}
	information, err = file.Stat()
	if err == nil {
		err = validateReadNativeInfo(information)
	}
	if err := errors.Join(err, file.Close()); err != nil {
		return nil, err
	}
	return information, nil
}

func (client *Client) rejectSplitIndex(ctx context.Context, directory string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	administrative, err := client.gitDirectory(ctx, directory, "--absolute-git-dir")
	if err := errors.Join(err, ctx.Err()); err != nil {
		return err
	}
	return rejectSplitIndexDirectory(ctx, administrative, defaultReadonlyIndexOperations())
}

func rejectSplitIndexDirectory(ctx context.Context, directory string, operations readonlyIndexOperations) (resultErr error) {
	initial, err := readonlyIndexRootInfo(ctx, directory, operations)
	if err != nil {
		return err
	}
	file, err := operations.open(directory)
	if file != nil {
		defer func() {
			resultErr = errors.Join(resultErr, operations.close(file), ctx.Err())
		}()
	}
	if err := errors.Join(err, ctx.Err()); err != nil {
		return err
	}
	if file == nil {
		return fmt.Errorf("Git administrative directory handle is missing: %w", fs.ErrInvalid)
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		opened, err := operations.stat(file)
		if err := errors.Join(err, ctx.Err()); err != nil {
			return err
		}
		if err := validateReadNativeInfo(opened); err != nil {
			return err
		}
		if !sameReadonlyIndexDirectory(initial, opened) {
			return fmt.Errorf("%w: Git administrative directory identity changed during split-index preflight: %q", ErrWorktreeChanged, directory)
		}
		current, err := readonlyIndexRootInfo(ctx, directory, operations)
		if err != nil {
			return err
		}
		if !sameReadonlyIndexDirectory(initial, current) {
			return fmt.Errorf("%w: Git administrative directory path changed during split-index preflight: %q", ErrWorktreeChanged, directory)
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		limit := min(128, maxAdminEntries-len(seen)+1)
		names, readErr := operations.readNames(file, limit)
		finished := readErr == io.EOF
		if finished {
			readErr = nil
		}
		readErr = errors.Join(readErr, ctx.Err())
		if len(names) > limit {
			return errors.Join(readErr, fmt.Errorf("Git administrative enumeration exceeded its requested bound: %w", ErrIndexPreflightLimit))
		}
		if len(names) > maxAdminEntries-len(seen) {
			readErr = errors.Join(readErr, fmt.Errorf("Git administrative directory exceeds %d entries: %w", maxAdminEntries, ErrIndexPreflightLimit))
		}
		for _, name := range names {
			if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.IndexByte(name, 0) >= 0 || seen[name] {
				return errors.Join(readErr, fmt.Errorf("malformed or repeated Git administrative entry %q: %w", name, fs.ErrInvalid))
			}
			prefix, _, dotted := strings.Cut(name, ".")
			if dotted && strings.EqualFold(prefix, "sharedindex") {
				return errors.Join(readErr, fmt.Errorf("Git shared-index backing entry %q prevents read-only collection: %w", name, errors.ErrUnsupported))
			}
			seen[name] = true
		}
		if readErr != nil {
			return readErr
		}
		if finished {
			break
		}
		if len(names) == 0 {
			return io.ErrNoProgress
		}
	}
	return check()
}

func readonlyIndexRootInfo(ctx context.Context, directory string, operations readonlyIndexOperations) (fs.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateReadonlyIndexDirectoryPath(directory); err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(directory)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	if canonical != directory {
		return nil, fmt.Errorf("%w: Git administrative directory path changed during split-index preflight: %q", ErrWorktreeChanged, directory)
	}
	information, err := operations.lstat(directory)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	if err := validateReadNativeInfo(information); err != nil {
		return nil, err
	}
	if information.Mode().Type() != fs.ModeDir {
		return nil, fmt.Errorf("Git administrative root is not a native directory: %w", fs.ErrInvalid)
	}
	return information, nil
}

func validateReadNativeInfo(information fs.FileInfo) error {
	if information == nil || information.Sys() == nil {
		return fmt.Errorf("native file metadata is unavailable: %w", fs.ErrInvalid)
	}
	return nil
}

func validateReadonlyIndexDirectoryPath(directory string) error {
	if !filepath.IsAbs(directory) || strings.IndexByte(directory, 0) >= 0 {
		return fmt.Errorf("invalid Git administrative directory path: %w", fs.ErrInvalid)
	}
	if len(directory) > 32<<10 {
		return fmt.Errorf("Git administrative directory path exceeds %d bytes: %w", 32<<10, ErrIndexPreflightLimit)
	}
	return nil
}

func sameReadonlyIndexDirectory(initial, current fs.FileInfo) bool {
	return current != nil && os.SameFile(initial, current) && initial.Mode() == current.Mode() && initial.Size() == current.Size() && initial.ModTime().Equal(current.ModTime())
}
