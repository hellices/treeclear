package fssecure

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type directoryPublisher struct {
	operations      directoryOperations
	parent          *os.File
	parentPath      string
	parentIdentity  unix.Stat_t
	staging         *os.File
	stagingName     string
	stagingIdentity unix.Stat_t
	stagingPinned   bool
	children        []directoryChild
	uncertain       bool
	published       bool
}

type directoryChild struct {
	name     string
	identity unix.Stat_t
	sealed   bool
}

func publishPrivateDirectory(ctx context.Context, path string, files []PrivateFile) (string, error) {
	publisher := directoryPublisher{operations: nativeDirectoryOperations()}
	return publisher.publish(ctx, path, files)
}

func (publisher *directoryPublisher) publish(ctx context.Context, path string, files []PrivateFile) (destination string, result error) {
	path, err := privatePath(path)
	if err != nil {
		return "", err
	}
	publisher.parentPath = filepath.Dir(path)
	defer func() {
		if !publisher.published {
			result = errors.Join(result, publisher.cleanup())
		}
		if publisher.staging != nil {
			result = errors.Join(result, publisher.operations.close(publisher.staging))
		}
		if publisher.parent != nil {
			result = errors.Join(result, publisher.operations.close(publisher.parent))
		}
		if publisher.published {
			result = errors.Join(result, publisher.checkPublished())
		}
		result = errors.Join(result, ctx.Err())
		if result != nil {
			destination = ""
		}
	}()
	publisher.parentIdentity, err = directoryStatAt(unix.AT_FDCWD, publisher.parentPath)
	if err != nil {
		return "", err
	}
	publisher.parent, err = publisher.operations.openat(unix.AT_FDCWD, publisher.parentPath, directoryReadFlags|unix.O_DIRECTORY, 0)
	if err != nil {
		return "", err
	}
	if err := publisher.check(ctx); err != nil {
		return "", err
	}
	if _, err := directoryStatAt(int(publisher.parent.Fd()), filepath.Base(path)); err == nil {
		return "", &os.PathError{Op: "publish private directory", Path: path, Err: fs.ErrExist}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := publisher.createStaging(ctx); err != nil {
		return "", err
	}
	for _, file := range files {
		if err := publisher.writeChild(ctx, file); err != nil {
			return "", err
		}
	}
	if err := publisher.verifyFiles(ctx, files); err != nil {
		return "", err
	}
	if err := publisher.sync(ctx, publisher.staging); err != nil {
		return "", err
	}
	if err := publisher.sync(ctx, publisher.parent); err != nil {
		return "", err
	}
	if err := publisher.checkListing(ctx); err != nil {
		return "", err
	}
	parentDescriptor := int(publisher.parent.Fd())
	if err := publisher.operations.renameat(parentDescriptor, publisher.stagingName, parentDescriptor, filepath.Base(path), unix.RENAME_EXCL|unix.RENAME_NOFOLLOW_ANY); err != nil {
		return "", fmt.Errorf("publish private directory: %w", err)
	}
	publisher.published = true
	publisher.stagingName = filepath.Base(path)
	if err := publisher.verifyFiles(ctx, files); err != nil {
		return "", err
	}
	if err := publisher.sync(ctx, publisher.parent); err != nil {
		return "", err
	}
	return path, nil
}

func (publisher *directoryPublisher) createStaging(ctx context.Context) error {
	for range 10 {
		if err := publisher.check(ctx); err != nil {
			return err
		}
		name := ".treeclear-directory-" + rand.Text() + ".tmp"
		parentDescriptor := int(publisher.parent.Fd())
		if err := publisher.operations.mkdirat(parentDescriptor, name, 0o700); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return fmt.Errorf("create private staging directory: %w", err)
		}
		publisher.stagingName = name
		var err error
		publisher.stagingIdentity, err = directoryStatAt(parentDescriptor, name)
		if err != nil {
			return err
		}
		publisher.staging, err = publisher.operations.openat(parentDescriptor, name, directoryReadFlags|unix.O_DIRECTORY, 0)
		if err != nil {
			return err
		}
		if err := verifyDirectoryHandle(publisher.staging, publisher.stagingIdentity, true); err != nil {
			return err
		}
		publisher.stagingPinned = true
		return publisher.check(ctx)
	}
	return fmt.Errorf("create private staging directory: too many name collisions: %w", fs.ErrExist)
}

func (publisher *directoryPublisher) writeChild(ctx context.Context, input PrivateFile) (result error) {
	if err := publisher.check(ctx); err != nil {
		return err
	}
	file, err := publisher.operations.openat(int(publisher.staging.Fd()), input.Name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			result = errors.Join(result, publisher.operations.close(file))
		}
	}()
	var identity unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &identity); err != nil {
		publisher.uncertain = true
		return err
	}
	publisher.children = append(publisher.children, directoryChild{name: input.Name, identity: identity})
	child := &publisher.children[len(publisher.children)-1]
	if err := verifyDirectoryHandle(file, child.identity, false); err != nil {
		return err
	}
	if err := publisher.check(ctx); err != nil {
		return err
	}
	written, err := publisher.operations.write(file, input.Contents)
	if err == nil && written != len(input.Contents) {
		err = io.ErrShortWrite
	}
	if err := errors.Join(err, publisher.check(ctx)); err != nil {
		return err
	}
	if err := publisher.sync(ctx, file); err != nil {
		return err
	}
	if err := verifyDirectoryHandle(file, child.identity, false); err != nil {
		return err
	}
	if err := unix.Fstat(int(file.Fd()), &child.identity); err != nil {
		return err
	}
	child.sealed = true
	err = publisher.operations.close(file)
	closed = true
	if err := errors.Join(err, publisher.check(ctx)); err != nil {
		return err
	}
	return publisher.readChild(ctx, *child, input.Contents)
}

func (publisher *directoryPublisher) readChild(ctx context.Context, child directoryChild, expected []byte) (result error) {
	if err := publisher.check(ctx); err != nil {
		return err
	}
	file, err := publisher.operations.openat(int(publisher.staging.Fd()), child.name, directoryReadFlags, 0)
	if err != nil {
		return err
	}
	defer func() {
		result = errors.Join(result, publisher.operations.close(file), publisher.check(ctx))
	}()
	if err := verifyDirectoryHandle(file, child.identity, false); err != nil {
		return err
	}
	if child.identity.Size != int64(len(expected)) {
		return fmt.Errorf("private file %q has an unexpected size: %w", child.name, fs.ErrInvalid)
	}
	reader := directoryReader{publisher: publisher, context: ctx, file: file, identity: child.identity}
	actual, err := io.ReadAll(io.LimitReader(reader, int64(len(expected))+1))
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, expected) {
		return fmt.Errorf("private file %q failed exact read-back: %w", child.name, fs.ErrInvalid)
	}
	return publisher.check(ctx)
}

func (publisher *directoryPublisher) verifyFiles(ctx context.Context, files []PrivateFile) error {
	if err := publisher.checkListing(ctx); err != nil {
		return err
	}
	for index, file := range files {
		if err := publisher.readChild(ctx, publisher.children[index], file.Contents); err != nil {
			return err
		}
	}
	return publisher.checkListing(ctx)
}

func (publisher *directoryPublisher) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.Join(publisher.checkStorage(), ctx.Err())
}

func (publisher *directoryPublisher) checkStorage() error {
	if err := verifyDirectoryHandle(publisher.parent, publisher.parentIdentity, true); err != nil {
		return err
	}
	if err := verifyDirectoryParentPath(publisher.parentPath, publisher.parentIdentity); err != nil {
		return err
	}
	if publisher.staging == nil {
		return nil
	}
	if err := verifyDirectoryHandle(publisher.staging, publisher.stagingIdentity, true); err != nil {
		return err
	}
	identity, err := directoryStatAt(int(publisher.parent.Fd()), publisher.stagingName)
	if err != nil {
		return err
	}
	if !sameDirectoryIdentity(identity, publisher.stagingIdentity) {
		return fmt.Errorf("private staging directory identity changed: %w", fs.ErrInvalid)
	}
	for _, child := range publisher.children {
		identity, err := directoryStatAt(int(publisher.staging.Fd()), child.name)
		if err != nil {
			return err
		}
		if !sameDirectoryIdentity(identity, child.identity) || identity.Mode != unix.S_IFREG|0o600 || identity.Nlink != 1 || identity.Uid != uint32(os.Geteuid()) || child.sealed && !sameDirectoryFileSnapshot(identity, child.identity) {
			return fmt.Errorf("private file %q identity or metadata changed: %w", child.name, fs.ErrInvalid)
		}
	}
	return nil
}

func (publisher *directoryPublisher) checkListing(ctx context.Context) (result error) {
	if err := publisher.check(ctx); err != nil {
		return err
	}
	listing, err := publisher.operations.openat(int(publisher.staging.Fd()), ".", directoryReadFlags|unix.O_DIRECTORY, 0)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, publisher.operations.close(listing), publisher.check(ctx)) }()
	if err := verifyDirectoryHandle(listing, publisher.stagingIdentity, true); err != nil {
		return err
	}
	entries, err := listing.ReadDir(len(publisher.children) + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) != len(publisher.children) {
		return fmt.Errorf("unexpected private directory contents: %w", fs.ErrInvalid)
	}
	known := make(map[string]bool, len(publisher.children))
	for _, child := range publisher.children {
		known[child.name] = true
	}
	for _, entry := range entries {
		if !known[entry.Name()] || !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected private directory entry %q: %w", entry.Name(), fs.ErrInvalid)
		}
	}
	return nil
}

func (publisher *directoryPublisher) sync(ctx context.Context, file *os.File) error {
	if err := publisher.check(ctx); err != nil {
		return err
	}
	return errors.Join(publisher.operations.sync(file), publisher.check(ctx))
}

func (publisher *directoryPublisher) cleanup() error {
	if publisher.stagingName == "" {
		return nil
	}
	if !publisher.stagingPinned || publisher.uncertain {
		return fmt.Errorf("leave unverified private staging directory %q: %w", publisher.stagingName, fs.ErrInvalid)
	}
	if err := publisher.checkListing(context.Background()); err != nil {
		return fmt.Errorf("leave uncertain private staging directory %q: %w", publisher.stagingName, err)
	}
	for _, child := range publisher.children {
		if !child.sealed {
			return fmt.Errorf("leave unsealed private staging file %q: %w", child.name, fs.ErrInvalid)
		}
		file, err := publisher.operations.openat(int(publisher.staging.Fd()), child.name, directoryReadFlags, 0)
		if err != nil {
			return err
		}
		err = verifyDirectoryHandle(file, child.identity, false)
		if err := errors.Join(err, publisher.operations.close(file)); err != nil {
			return fmt.Errorf("leave uncertain private staging file %q: %w", child.name, err)
		}
	}
	for len(publisher.children) > 0 {
		if err := publisher.checkStorage(); err != nil {
			return err
		}
		child := publisher.children[len(publisher.children)-1]
		if err := publisher.operations.unlinkat(int(publisher.staging.Fd()), child.name, 0); err != nil {
			return fmt.Errorf("remove owned private staging file: %w", err)
		}
		publisher.children = publisher.children[:len(publisher.children)-1]
	}
	if err := publisher.checkStorage(); err != nil {
		return err
	}
	if err := publisher.operations.unlinkat(int(publisher.parent.Fd()), publisher.stagingName, unix.AT_REMOVEDIR); err != nil {
		return fmt.Errorf("remove owned private staging directory: %w", err)
	}
	return publisher.operations.sync(publisher.parent)
}

func (publisher *directoryPublisher) checkPublished() (result error) {
	probe := *publisher
	probe.operations = nativeDirectoryOperations()
	var err error
	probe.parent, err = probe.operations.openat(unix.AT_FDCWD, probe.parentPath, directoryReadFlags|unix.O_DIRECTORY, 0)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, probe.parent.Close()) }()
	if err := verifyDirectoryHandle(probe.parent, probe.parentIdentity, true); err != nil {
		return err
	}
	probe.staging, err = probe.operations.openat(int(probe.parent.Fd()), probe.stagingName, directoryReadFlags|unix.O_DIRECTORY, 0)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, probe.staging.Close()) }()
	return probe.checkListing(context.Background())
}
