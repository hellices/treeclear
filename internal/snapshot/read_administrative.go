package snapshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/hellices/treeclear/internal/pathutil"
)

type administrativeReadOperations struct {
	openRootMetadata func(string) (*os.File, error)
	openRoot         func(string) (*os.Root, error)
	openDirectory    func(*os.Root, string) (*os.Root, error)
	openFile         func(*os.Root, string) (*os.File, error)
	lstat            func(*os.Root, string) (fs.FileInfo, error)
	stat             func(*os.File) (fs.FileInfo, error)
	readNames        func(*os.File, int) ([]string, error)
	read             func(*os.File, []byte) (int, error)
	closeFile        func(*os.File) error
	closeRoot        func(*os.Root) error
}

type administrativeReadObservation struct {
	path        string
	name        string
	parent      *os.Root
	directory   *os.Root
	information fs.FileInfo
}

type administrativeReader struct {
	ctx          context.Context
	operations   administrativeReadOperations
	roots        []*os.Root
	observations []administrativeReadObservation
	entries      []AdminEntry
	identities   map[string]bool
	reserved     int
	bytes        int
}

func ReadAdministrative(ctx context.Context, directory string) ([]AdminEntry, error) {
	return readAdministrative(ctx, directory, defaultAdministrativeReadOperations())
}

func defaultAdministrativeReadOperations() administrativeReadOperations {
	return administrativeReadOperations{
		openRootMetadata: openAdministrativeReadRootMetadata,
		openRoot:         openAdministrativeReadRoot,
		openDirectory:    openAdministrativeReadDirectory,
		openFile:         openAdministrativeReadFile,
		lstat:            (*os.Root).Lstat,
		stat:             (*os.File).Stat,
		readNames:        (*os.File).Readdirnames,
		read:             (*os.File).Read,
		closeFile:        (*os.File).Close,
		closeRoot:        (*os.Root).Close,
	}
}

func readAdministrative(ctx context.Context, directory string, operations administrativeReadOperations) (entries []AdminEntry, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(directory) > 32<<10 {
		return nil, manifestLimit("administrative root path")
	}
	if err := pathutil.ValidateAbsoluteForm(directory); err != nil {
		return nil, fmt.Errorf("administrative root: %w", err)
	}
	reader := administrativeReader{ctx: ctx, operations: operations, identities: map[string]bool{".": true}, reserved: 1}
	defer func() {
		for rootIndex := len(reader.roots) - 1; rootIndex >= 0; rootIndex-- {
			root := reader.roots[rootIndex]
			if err := operations.closeRoot(root); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("close administrative directory %q: %w", root.Name(), err))
			}
		}
		resultErr = reader.check(resultErr)
		if resultErr != nil {
			entries = nil
		}
	}()
	initial, err := reader.rootInfo(directory)
	if err != nil {
		return nil, err
	}
	if !initial.IsDir() {
		return nil, fmt.Errorf("administrative root is not a directory")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := operations.openRoot(directory)
	if root != nil {
		reader.roots = append(reader.roots, root)
	}
	if err := reader.check(err); err != nil {
		return nil, fmt.Errorf("open administrative root: %w", err)
	}
	if err := reader.checkPath(root, ".", initial); err != nil {
		return nil, err
	}
	if err := reader.checkRootPath(directory, initial); err != nil {
		return nil, err
	}
	reader.entries = append(reader.entries, AdminEntry{Path: ".", Kind: "directory", Mode: initial.Mode()})
	reader.observations = append(reader.observations, administrativeReadObservation{path: ".", name: ".", parent: root, directory: root, information: initial})
	if err := reader.walk(root, ".", initial); err != nil {
		return nil, err
	}
	if err := reader.check(validateAdministrativeEntries(reader.entries)); err != nil {
		return nil, err
	}
	for _, observation := range reader.observations {
		if err := reader.checkPath(observation.parent, observation.name, observation.information); err != nil {
			return nil, fmt.Errorf("revalidate administrative entry %q: %w", observation.path, err)
		}
		if observation.directory != nil && observation.directory != observation.parent {
			if err := reader.checkPath(observation.directory, ".", observation.information); err != nil {
				return nil, err
			}
		}
	}
	if err := reader.checkRootPath(directory, initial); err != nil {
		return nil, err
	}
	slices.SortFunc(reader.entries, func(left, right AdminEntry) int { return strings.Compare(left.Path, right.Path) })
	return reader.entries, nil
}

func (reader *administrativeReader) walk(root *os.Root, directoryPath string, directoryInfo fs.FileInfo) error {
	names, err := reader.directoryNames(root, directoryPath, directoryInfo)
	if err != nil {
		return err
	}
	for _, name := range names {
		entryPath := administrativeReadChildPath(directoryPath, name)
		information, err := reader.pathInfo(root, name)
		if err != nil {
			return err
		}
		observation := administrativeReadObservation{path: entryPath, name: name, parent: root, information: information}
		entry := AdminEntry{Path: entryPath, Mode: information.Mode(), Kind: "file"}
		if information.IsDir() {
			if err := reader.ctx.Err(); err != nil {
				return err
			}
			child, err := reader.operations.openDirectory(root, name)
			if child != nil {
				reader.roots = append(reader.roots, child)
			}
			if err := reader.check(err); err != nil {
				return fmt.Errorf("open administrative directory %q: %w", entryPath, err)
			}
			if err := reader.checkPath(child, ".", information); err != nil {
				return err
			}
			if err := reader.checkPath(root, name, information); err != nil {
				return err
			}
			observation.directory = child
			entry.Kind = "directory"
			reader.observations = append(reader.observations, observation)
			reader.entries = append(reader.entries, entry)
			if err := reader.walk(child, entryPath, information); err != nil {
				return err
			}
		} else {
			data, err := reader.fileData(root, name, information)
			if err != nil {
				return fmt.Errorf("read administrative file %q: %w", entryPath, err)
			}
			entry.Data = data
			reader.bytes += len(data)
			reader.observations = append(reader.observations, observation)
			reader.entries = append(reader.entries, entry)
		}
	}
	return nil
}

func (reader *administrativeReader) directoryNames(root *os.Root, directoryPath string, information fs.FileInfo) (names []string, resultErr error) {
	if err := reader.ctx.Err(); err != nil {
		return nil, err
	}
	file, err := reader.operations.openFile(root, ".")
	if file != nil {
		defer func() {
			resultErr = reader.closeFile(file, resultErr)
			if resultErr != nil {
				names = nil
			}
		}()
	}
	if err := reader.check(err); err != nil {
		return nil, err
	}
	if err := reader.checkFile(file, information); err != nil {
		return nil, err
	}
	for {
		if err := reader.ctx.Err(); err != nil {
			return nil, err
		}
		requested := min(64, maximumAdministrativeEntries-reader.reserved+1)
		batch, readErr := reader.operations.readNames(file, requested)
		if err := reader.ctx.Err(); err != nil {
			return nil, errors.Join(readErr, err)
		}
		if readErr != nil && readErr != io.EOF {
			return nil, readErr
		}
		if len(batch) > requested || len(batch) > maximumAdministrativeEntries-reader.reserved {
			return nil, manifestLimit("administrative entry count")
		}
		reader.reserved += len(batch)
		for _, name := range batch {
			if name == "." || name == ".." || strings.ContainsAny(name, `/\`) || !validAdministrativePath(name) {
				return nil, fmt.Errorf("%w: invalid administrative entry name", ErrManifestInvalid)
			}
			entryPath := administrativeReadChildPath(directoryPath, name)
			if !validAdministrativePath(entryPath) {
				return nil, fmt.Errorf("%w: invalid administrative entry path", ErrManifestInvalid)
			}
			identity := foldAdministrativePath(entryPath)
			if reader.identities[identity] {
				return nil, fmt.Errorf("%w: conflicting administrative entry names", ErrManifestInvalid)
			}
			reader.identities[identity] = true
		}
		names = append(names, batch...)
		if readErr == io.EOF {
			break
		}
		if len(batch) == 0 {
			return nil, io.ErrNoProgress
		}
	}
	if err := reader.checkFile(file, information); err != nil {
		return nil, err
	}
	if err := reader.checkPath(root, ".", information); err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}

func (reader *administrativeReader) fileData(root *os.Root, name string, information fs.FileInfo) (data []byte, resultErr error) {
	if information.Size() > int64(maximumAdministrativeBytes-reader.bytes) {
		return nil, manifestLimit("aggregate administrative bytes")
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
		buffer := data[offset:min(len(data), offset+(32<<10))]
		count, readErr := reader.read(file, buffer)
		offset += count
		if readErr != nil {
			if readErr != io.EOF {
				return nil, readErr
			}
			if offset != len(data) {
				return nil, io.ErrUnexpectedEOF
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
			if reader.bytes+len(data)+count > maximumAdministrativeBytes {
				return nil, manifestLimit("aggregate administrative bytes")
			}
			return nil, fmt.Errorf("administrative file grew during read")
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

func (reader *administrativeReader) read(file *os.File, buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	count, err := reader.operations.read(file, buffer)
	if count < 0 || count > len(buffer) {
		return 0, reader.check(errors.Join(err, fmt.Errorf("invalid administrative read count")))
	}
	if contextErr := reader.ctx.Err(); contextErr != nil {
		return count, errors.Join(err, contextErr)
	}
	if count == 0 && err == nil {
		return 0, io.ErrNoProgress
	}
	return count, err
}

func (reader *administrativeReader) rootInfo(directory string) (information fs.FileInfo, resultErr error) {
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
		return nil, fmt.Errorf("observe administrative root: %w", err)
	}
	return reader.fileInfo(file)
}

func (reader *administrativeReader) checkRootPath(directory string, initial fs.FileInfo) error {
	current, err := reader.rootInfo(directory)
	if err != nil {
		return err
	}
	return compareAdministrativeReadInfo(initial, current)
}

func (reader *administrativeReader) pathInfo(root *os.Root, name string) (fs.FileInfo, error) {
	if err := reader.ctx.Err(); err != nil {
		return nil, err
	}
	information, err := reader.operations.lstat(root, name)
	if err := reader.check(err); err != nil {
		return nil, err
	}
	if err := validateAdministrativeReadInfo(information); err != nil {
		return nil, err
	}
	return information, nil
}

func (reader *administrativeReader) fileInfo(file *os.File) (fs.FileInfo, error) {
	if err := reader.ctx.Err(); err != nil {
		return nil, err
	}
	information, err := reader.operations.stat(file)
	if err := reader.check(err); err != nil {
		return nil, err
	}
	if err := validateAdministrativeReadInfo(information); err != nil {
		return nil, err
	}
	return information, nil
}

func (reader *administrativeReader) checkPath(root *os.Root, name string, initial fs.FileInfo) error {
	current, err := reader.pathInfo(root, name)
	if err != nil {
		return err
	}
	return compareAdministrativeReadInfo(initial, current)
}

func (reader *administrativeReader) checkFile(file *os.File, initial fs.FileInfo) error {
	current, err := reader.fileInfo(file)
	if err != nil {
		return err
	}
	return compareAdministrativeReadInfo(initial, current)
}

func (reader *administrativeReader) closeFile(file *os.File, previous error) error {
	if err := reader.operations.closeFile(file); err != nil {
		previous = errors.Join(previous, fmt.Errorf("close administrative file %q: %w", file.Name(), err))
	}
	return reader.check(previous)
}

func (reader *administrativeReader) check(err error) error {
	return errors.Join(err, reader.ctx.Err())
}

func compareAdministrativeReadInfo(initial, current fs.FileInfo) error {
	if !os.SameFile(initial, current) || initial.Mode() != current.Mode() || initial.Size() != current.Size() || !initial.ModTime().Equal(current.ModTime()) {
		return fmt.Errorf("administrative object changed during read")
	}
	return nil
}

func validateAdministrativeReadInfo(information fs.FileInfo) error {
	if information == nil {
		return fmt.Errorf("administrative object information is missing")
	}
	if err := validateAdministrativeReadNativeInfo(information); err != nil {
		return err
	}
	mode := information.Mode() &^ administrativePermissionBits
	if mode != 0 && mode != fs.ModeDir || information.Size() < 0 {
		return fmt.Errorf("unsupported administrative object type, mode, or size")
	}
	return nil
}

func administrativeReadChildPath(parent, name string) string {
	if parent == "." {
		return name
	}
	return parent + "/" + name
}
