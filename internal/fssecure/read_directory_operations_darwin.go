package fssecure

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

type privateDirectoryReadOperations struct {
	openat  func(int, string, int, uint32) (*os.File, error)
	read    func(*os.File, []byte) (int, error)
	readDir func(*os.File, int) ([]os.DirEntry, error)
	close   func(*os.File) error
}

func nativePrivateDirectoryReadOperations() privateDirectoryReadOperations {
	return privateDirectoryReadOperations{
		openat:  openDirectoryObjectAt,
		read:    (*os.File).Read,
		readDir: (*os.File).ReadDir,
		close:   (*os.File).Close,
	}
}

type privateDirectoryFileReader struct {
	directory *privateDirectoryReader
	context   context.Context
	file      *os.File
	identity  unix.Stat_t
}

func (reader privateDirectoryFileReader) Read(buffer []byte) (int, error) {
	if err := reader.directory.check(reader.context); err != nil {
		return 0, err
	}
	if err := verifyPrivateDirectoryReadHandle(reader.file, reader.identity, false); err != nil {
		return 0, err
	}
	count, readErr := reader.directory.operations.read(reader.file, buffer)
	checkErr := errors.Join(reader.directory.check(reader.context), verifyPrivateDirectoryReadHandle(reader.file, reader.identity, false))
	if count < 0 || count > len(buffer) {
		return 0, errors.Join(readErr, checkErr, fmt.Errorf("invalid private read length: %w", fs.ErrInvalid))
	}
	if checkErr != nil {
		return count, errors.Join(readErr, checkErr)
	}
	if count == 0 && readErr == nil && len(buffer) != 0 {
		return 0, io.ErrNoProgress
	}
	return count, readErr
}
