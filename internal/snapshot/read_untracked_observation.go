package snapshot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

func (reader *untrackedReader) rootInfo(directory string) (information fs.FileInfo, resultErr error) {
	if err := reader.ctx.Err(); err != nil {
		return nil, err
	}
	file, err := reader.operations.openRootMetadata(directory)
	if file != nil {
		defer func() {
			resultErr = reader.closeFile(file, resultErr)
			if resultErr != nil {
				information = nil
			}
		}()
	}
	if err := reader.check(err); err != nil {
		return nil, fmt.Errorf("observe untracked root: %w", err)
	}
	return reader.fileInfo(file)
}

func (reader *untrackedReader) checkRootPath(directory string, initial fs.FileInfo) error {
	current, err := reader.rootInfo(directory)
	if err != nil {
		return err
	}
	return compareUntrackedReadInfo(initial, current)
}

func (reader *untrackedReader) pathInfo(root *os.Root, name string) (fs.FileInfo, error) {
	if err := reader.ctx.Err(); err != nil {
		return nil, err
	}
	information, err := reader.operations.lstat(root, name)
	if err := reader.check(err); err != nil {
		return nil, err
	}
	if err := validateUntrackedReadInfo(information); err != nil {
		return nil, err
	}
	return information, nil
}

func (reader *untrackedReader) fileInfo(file *os.File) (fs.FileInfo, error) {
	if err := reader.ctx.Err(); err != nil {
		return nil, err
	}
	if file == nil {
		return nil, fmt.Errorf("%w: untracked file handle is missing", ErrUntrackedInvalid)
	}
	information, err := reader.operations.stat(file)
	if err := reader.check(err); err != nil {
		return nil, err
	}
	if err := validateUntrackedReadInfo(information); err != nil {
		return nil, err
	}
	return information, nil
}

func (reader *untrackedReader) checkPath(root *os.Root, name string, initial fs.FileInfo) error {
	current, err := reader.pathInfo(root, name)
	if err != nil {
		return err
	}
	return compareUntrackedReadInfo(initial, current)
}

func (reader *untrackedReader) checkFile(file *os.File, initial fs.FileInfo) error {
	current, err := reader.fileInfo(file)
	if err != nil {
		return err
	}
	return compareUntrackedReadInfo(initial, current)
}

func (reader *untrackedReader) closeFile(file *os.File, previous error) error {
	if err := reader.operations.closeFile(file); err != nil {
		previous = errors.Join(previous, fmt.Errorf("close untracked file %q: %w", file.Name(), err))
	}
	return reader.check(previous)
}

func (reader *untrackedReader) check(err error) error {
	return errors.Join(err, reader.ctx.Err())
}

func compareUntrackedReadInfo(initial, current fs.FileInfo) error {
	if !os.SameFile(initial, current) || initial.Mode() != current.Mode() || initial.Size() != current.Size() || !initial.ModTime().Equal(current.ModTime()) {
		return fmt.Errorf("%w: untracked object changed during read", ErrUntrackedInvalid)
	}
	return nil
}

func validateUntrackedReadInfo(information fs.FileInfo) error {
	if information == nil {
		return fmt.Errorf("%w: untracked object information is missing", ErrUntrackedInvalid)
	}
	if err := validateAdministrativeReadNativeInfo(information); err != nil {
		return fmt.Errorf("%w: unsupported untracked native information: %w", ErrUntrackedInvalid, err)
	}
	mode := information.Mode() &^ untrackedPermissionBits
	if mode != 0 && mode != fs.ModeDir && mode != fs.ModeSymlink || information.Size() < 0 {
		return fmt.Errorf("%w: unsupported untracked object type, mode or size", ErrUntrackedInvalid)
	}
	return nil
}
