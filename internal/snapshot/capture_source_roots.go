package snapshot

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/hellices/treeclear/internal/pathutil"
)

type sourceRootOperations struct {
	open  func(string) (*os.File, error)
	stat  func(*os.File) (fs.FileInfo, error)
	close func(*os.File) error
}

func defaultSourceRootOperations() sourceRootOperations {
	return sourceRootOperations{open: openAdministrativeReadRootMetadata, stat: (*os.File).Stat, close: (*os.File).Close}
}

type sourceRootObservation struct {
	path        string
	file        *os.File
	information fs.FileInfo
}

type sourceRootGuard struct {
	ctx          context.Context
	operations   sourceRootOperations
	observations []sourceRootObservation
}

func (guard *sourceRootGuard) bind(paths []string) error {
	for _, directory := range paths {
		if err := guard.canonical(directory); err != nil {
			return err
		}
		if err := guard.ctx.Err(); err != nil {
			return err
		}
		file, err := guard.operations.open(directory)
		if file != nil {
			guard.observations = append(guard.observations, sourceRootObservation{path: directory, file: file})
		}
		if err := sourceCallError(guard.ctx, err); err != nil {
			return fmt.Errorf("open source root %q: %w", directory, err)
		}
		information, err := guard.information(file)
		if err != nil {
			return err
		}
		for _, previous := range guard.observations[:len(guard.observations)-1] {
			if os.SameFile(previous.information, information) {
				return fmt.Errorf("source roots %q and %q identify the same directory", previous.path, directory)
			}
		}
		guard.observations[len(guard.observations)-1].information = information
	}
	return guard.check()
}

func (guard *sourceRootGuard) canonical(directory string) error {
	if err := guard.ctx.Err(); err != nil {
		return err
	}
	canonical, err := pathutil.Canonical(directory)
	if err := sourceCallError(guard.ctx, err); err != nil {
		return err
	}
	if canonical != directory {
		return fmt.Errorf("%w: source root %q is not its exact canonical path", ErrSourceChanged, directory)
	}
	return nil
}

func (guard *sourceRootGuard) information(file *os.File) (fs.FileInfo, error) {
	if err := guard.ctx.Err(); err != nil {
		return nil, err
	}
	if file == nil {
		return nil, fmt.Errorf("source root handle is missing")
	}
	information, err := guard.operations.stat(file)
	if err := sourceCallError(guard.ctx, err); err != nil {
		return nil, err
	}
	if err := validateAdministrativeReadInfo(information); err != nil {
		return nil, err
	}
	if !information.IsDir() {
		return nil, fmt.Errorf("source root %q is not a directory", file.Name())
	}
	return information, nil
}

func (guard *sourceRootGuard) observe(directory string) (information fs.FileInfo, resultErr error) {
	if err := guard.ctx.Err(); err != nil {
		return nil, err
	}
	file, err := guard.operations.open(directory)
	if file != nil {
		defer func() {
			resultErr = sourceCallError(guard.ctx, errors.Join(resultErr, guard.operations.close(file)))
			if resultErr != nil {
				information = nil
			}
		}()
	}
	if err := sourceCallError(guard.ctx, err); err != nil {
		return nil, err
	}
	return guard.information(file)
}

func (guard *sourceRootGuard) check() error {
	for _, observation := range guard.observations {
		if err := guard.canonical(observation.path); err != nil {
			return err
		}
		pinned, err := guard.information(observation.file)
		if err != nil {
			return err
		}
		current, err := guard.observe(observation.path)
		if err != nil {
			return fmt.Errorf("reopen source root %q: %w", observation.path, err)
		}
		if err := compareAdministrativeReadInfo(observation.information, pinned); err != nil {
			return fmt.Errorf("%w: source root %q: %w", ErrSourceChanged, observation.path, err)
		}
		if err := compareAdministrativeReadInfo(observation.information, current); err != nil {
			return fmt.Errorf("%w: source root %q: %w", ErrSourceChanged, observation.path, err)
		}
		if err := guard.canonical(observation.path); err != nil {
			return err
		}
	}
	return guard.ctx.Err()
}

func (guard *sourceRootGuard) close() error {
	var failures []error
	for index := len(guard.observations) - 1; index >= 0; index-- {
		observation := guard.observations[index]
		if err := guard.operations.close(observation.file); err != nil {
			failures = append(failures, fmt.Errorf("close source root %q: %w", observation.path, err))
		}
	}
	return sourceCallError(guard.ctx, errors.Join(failures...))
}
