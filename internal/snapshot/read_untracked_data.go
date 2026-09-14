package snapshot

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

func (reader *untrackedReader) fileData(root *os.Root, name string, information fs.FileInfo) (data []byte, resultErr error) {
	if information.Size() > reader.remaining {
		return nil, fmt.Errorf("%w: aggregate untracked file bytes", ErrUntrackedLimit)
	}
	if err := reader.ctx.Err(); err != nil {
		return nil, err
	}
	file, err := reader.operations.openFile(root, name)
	if file != nil {
		defer func() {
			resultErr = reader.closeFile(file, resultErr)
			if resultErr != nil {
				data = nil
			}
		}()
	}
	if err := reader.check(err); err != nil {
		return nil, err
	}
	if err := reader.checkFile(file, information); err != nil {
		return nil, err
	}
	if err := reader.checkPath(root, name, information); err != nil {
		return nil, err
	}
	data = make([]byte, int(information.Size()))
	ended := false
	for offset := 0; offset < len(data); {
		buffer := data[offset : offset+min(len(data)-offset, 32<<10)]
		count, readErr := reader.read(file, buffer)
		offset += count
		if readErr != nil {
			if readErr != io.EOF {
				return nil, readErr
			}
			if offset != len(data) {
				return nil, fmt.Errorf("%w: untracked file shortened: %w", ErrUntrackedInvalid, io.ErrUnexpectedEOF)
			}
			ended = true
			break
		}
	}
	if !ended {
		var extra [1]byte
		count, readErr := reader.read(file, extra[:])
		if readErr != nil && readErr != io.EOF {
			return nil, readErr
		}
		if count != 0 {
			if int64(len(data))+int64(count) > reader.remaining {
				return nil, fmt.Errorf("%w: aggregate untracked file bytes", ErrUntrackedLimit)
			}
			return nil, fmt.Errorf("%w: untracked file grew during read", ErrUntrackedInvalid)
		}
	}
	if err := reader.checkFile(file, information); err != nil {
		return nil, err
	}
	if err := reader.checkPath(root, name, information); err != nil {
		return nil, err
	}
	return data, nil
}

func (reader *untrackedReader) linkText(root *os.Root, name string, information fs.FileInfo) (string, error) {
	if err := reader.checkPath(root, name, information); err != nil {
		return "", err
	}
	if err := reader.ctx.Err(); err != nil {
		return "", err
	}
	target, err := reader.operations.readlink(root, name)
	if err := reader.check(err); err != nil {
		return "", err
	}
	if len(target) > maximumUntrackedText {
		return "", fmt.Errorf("%w: untracked link text bytes", ErrUntrackedLimit)
	}
	if err := reader.checkPath(root, name, information); err != nil {
		return "", err
	}
	return target, nil
}

func (reader *untrackedReader) read(file *os.File, buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	count, err := reader.operations.read(file, buffer)
	if count < 0 || count > len(buffer) {
		return 0, reader.check(errors.Join(err, fmt.Errorf("%w: invalid untracked read count", ErrUntrackedInvalid)))
	}
	if contextErr := reader.ctx.Err(); contextErr != nil {
		return count, errors.Join(err, contextErr)
	}
	if count == 0 && err == nil {
		return 0, io.ErrNoProgress
	}
	return count, err
}
