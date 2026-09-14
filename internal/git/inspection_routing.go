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

	"github.com/hellices/treeclear/internal/domain"
)

const maxInspectionPointerBytes = 32 << 10

func verifyInspectionRouting(ctx context.Context, worktree domain.Worktree) error {
	common, err := inspectionDirectoryInfo(ctx, worktree.CommonGitDir)
	if err != nil {
		return err
	}
	administrative, err := inspectionDirectoryInfo(ctx, worktree.AdminDir)
	if err != nil {
		return err
	}
	if os.SameFile(common, administrative) != worktree.Primary {
		return fmt.Errorf("%w: primary and linked administrative identities conflict", ErrWorktreeChanged)
	}
	if !worktree.Primary {
		registered, err := inspectionDirectoryInfo(ctx, filepath.Join(worktree.CommonGitDir, "worktrees"))
		if err != nil {
			return err
		}
		parent, err := inspectionDirectoryInfo(ctx, filepath.Dir(worktree.AdminDir))
		if err != nil {
			return err
		}
		if !os.SameFile(registered, parent) {
			return fmt.Errorf("%w: administrative directory is not a linked registration", ErrWorktreeChanged)
		}
	}
	root, err := inspectionDirectoryInfo(ctx, worktree.Path)
	if err != nil {
		return err
	}
	marker := filepath.Join(worktree.Path, ".git")
	markerInfo, err := os.Lstat(marker)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return err
	}
	if err := validateReadNativeInfo(markerInfo); err != nil {
		return err
	}
	if markerInfo.Mode().Type() == fs.ModeDir {
		markerInfo, err = inspectionDirectoryInfo(ctx, marker)
		if err != nil {
			return err
		}
		if !worktree.Primary || !os.SameFile(markerInfo, administrative) {
			return fmt.Errorf("%w: Git directory marker conflicts with registration", ErrWorktreeChanged)
		}
		return ctx.Err()
	}
	forward, err := readInspectionPointer(ctx, worktree.Path, ".git")
	if err != nil {
		return err
	}
	pointer, ok := strings.CutPrefix(strings.TrimRight(string(forward), "\r\n"), "gitdir: ")
	if !ok {
		return fmt.Errorf("invalid Git directory marker: %w", fs.ErrInvalid)
	}
	target, err := inspectionPointerPath(ctx, worktree.Path, pointer)
	if err != nil {
		return err
	}
	targetInfo, err := inspectionDirectoryInfo(ctx, target)
	if err != nil {
		return err
	}
	if !os.SameFile(targetInfo, administrative) {
		return fmt.Errorf("%w: Git directory marker changed its administrative target", ErrWorktreeChanged)
	}
	if worktree.Primary {
		return ctx.Err()
	}
	backward, err := readInspectionPointer(ctx, worktree.AdminDir, "gitdir")
	if err != nil {
		return err
	}
	backlink := strings.TrimRight(string(backward), " \t\r\n\v\f")
	if backlink == "" || strings.IndexByte(backlink, 0) >= 0 {
		return fmt.Errorf("invalid administrative backlink: %w", fs.ErrInvalid)
	}
	if !strings.HasSuffix(backlink, "/.git") {
		return fmt.Errorf("%w: administrative backlink does not name a Git marker", ErrWorktreeChanged)
	}
	backlinkDirectory, err := inspectionPointerPath(ctx, worktree.AdminDir, strings.TrimSuffix(backlink, ".git"))
	if err != nil {
		return err
	}
	backlinkRoot, err := inspectionDirectoryInfo(ctx, backlinkDirectory)
	if err != nil {
		return err
	}
	if !os.SameFile(root, backlinkRoot) {
		return fmt.Errorf("%w: administrative backlink identifies another worktree", ErrWorktreeChanged)
	}
	return ctx.Err()
}

func inspectionPointerPath(ctx context.Context, directory, value string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if value == "" || strings.IndexByte(value, 0) >= 0 {
		return "", fmt.Errorf("invalid administrative pointer: %w", fs.ErrInvalid)
	}
	value = filepath.FromSlash(value)
	if !filepath.IsAbs(value) {
		if filepath.VolumeName(value) != "" {
			return "", fmt.Errorf("volume-relative administrative pointer: %w", fs.ErrInvalid)
		}
		value = directory + string(filepath.Separator) + value
	}
	if len(value) > maxInspectionPointerBytes {
		return "", fmt.Errorf("administrative pointer path exceeds limit: %w", ErrReadLimit)
	}
	resolved, err := filepath.EvalSymlinks(value)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return "", err
	}
	if len(resolved) > maxInspectionPointerBytes {
		return "", fmt.Errorf("resolved administrative pointer path exceeds limit: %w", ErrReadLimit)
	}
	return resolved, nil
}

func inspectionDirectoryInfo(ctx context.Context, directory string) (fs.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateReadonlyIndexDirectoryPath(directory); err != nil {
		return nil, err
	}
	information, err := readonlyIndexDirectoryInfo(directory)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	if information.Mode().Type() != fs.ModeDir {
		return nil, fmt.Errorf("administrative routing path is not a directory: %w", fs.ErrInvalid)
	}
	return information, nil
}

func readInspectionPointer(ctx context.Context, directory, name string) (data []byte, resultErr error) {
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			data = nil
		}
	}()
	initialRoot, err := inspectionDirectoryInfo(ctx, directory)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory + string(filepath.Separator) + ".")
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	rootFile, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	openedRoot, statErr := rootFile.Stat()
	if err := errors.Join(statErr, rootFile.Close(), ctx.Err()); err != nil {
		return nil, err
	}
	if err := compareInspectionInfo(initialRoot, openedRoot); err != nil {
		return nil, err
	}
	initial, err := root.Lstat(name)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	if err := validateInspectionPointerInfo(initial); err != nil {
		return nil, err
	}
	file, err := openInspectionPointer(root, name)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	opened, err := file.Stat()
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	if err := validateInspectionPointerInfo(opened); err != nil {
		return nil, err
	}
	if err := compareInspectionInfo(initial, opened); err != nil {
		return nil, err
	}
	data, err = io.ReadAll(io.LimitReader(file, maxInspectionPointerBytes+1))
	if len(data) > maxInspectionPointerBytes {
		err = errors.Join(err, fmt.Errorf("administrative pointer exceeds read limit: %w", ErrReadLimit))
	}
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	current, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if err := compareInspectionInfo(opened, current); err != nil {
		return nil, err
	}
	current, err = root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if err := compareInspectionInfo(opened, current); err != nil {
		return nil, err
	}
	currentRoot, err := inspectionDirectoryInfo(ctx, directory)
	if err != nil {
		return nil, err
	}
	if err := compareInspectionInfo(initialRoot, currentRoot); err != nil {
		return nil, err
	}
	return data, nil
}

func validateInspectionPointerInfo(information fs.FileInfo) error {
	if err := validateReadNativeInfo(information); err != nil {
		return err
	}
	if !information.Mode().IsRegular() || information.Size() < 0 {
		return fmt.Errorf("administrative pointer is not a regular file: %w", fs.ErrInvalid)
	}
	if information.Size() > maxInspectionPointerBytes {
		return fmt.Errorf("administrative pointer exceeds read limit: %w", ErrReadLimit)
	}
	return nil
}

func compareInspectionInfo(initial, current fs.FileInfo) error {
	if err := validateReadNativeInfo(current); err != nil {
		return err
	}
	if !os.SameFile(initial, current) || initial.Mode() != current.Mode() || initial.Size() != current.Size() || !initial.ModTime().Equal(current.ModTime()) {
		return fmt.Errorf("%w: administrative routing metadata changed", ErrWorktreeChanged)
	}
	return nil
}
