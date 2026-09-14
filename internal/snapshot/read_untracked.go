package snapshot

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/hellices/treeclear/internal/pathutil"
)

type untrackedReadOperations struct {
	administrativeReadOperations
	readlink func(*os.Root, string) (string, error)
}

type untrackedReadObservation struct {
	path        string
	name        string
	parent      *os.Root
	directory   *os.Root
	information fs.FileInfo
	linkTarget  string
}

type untrackedReader struct {
	ctx          context.Context
	operations   untrackedReadOperations
	roots        []*os.Root
	observations []untrackedReadObservation
	entries      []UntrackedEntry
	remaining    int64
}

func ReadUntracked(ctx context.Context, directory string, paths []string, maximumBytes int64) ([]UntrackedEntry, error) {
	return readUntracked(ctx, directory, paths, maximumBytes, defaultUntrackedReadOperations())
}

func defaultUntrackedReadOperations() untrackedReadOperations {
	return untrackedReadOperations{
		administrativeReadOperations: defaultAdministrativeReadOperations(),
		readlink:                     (*os.Root).Readlink,
	}
}

func readUntracked(ctx context.Context, directory string, paths []string, maximumBytes int64, operations untrackedReadOperations) (entries []UntrackedEntry, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateUntrackedBudget(maximumBytes); err != nil {
		return nil, err
	}
	if len(directory) > 32<<10 {
		return nil, fmt.Errorf("%w: untracked root path bytes", ErrUntrackedLimit)
	}
	if err := pathutil.ValidateAbsoluteForm(directory); err != nil {
		return nil, fmt.Errorf("%w: untracked root: %w", ErrUntrackedInvalid, err)
	}
	selection, err := selectUntrackedReadPaths(ctx, paths)
	if err != nil {
		return nil, err
	}
	reader := untrackedReader{ctx: ctx, operations: operations, remaining: maximumBytes}
	defer func() {
		for rootIndex := len(reader.roots) - 1; rootIndex >= 0; rootIndex-- {
			root := reader.roots[rootIndex]
			if err := operations.closeRoot(root); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("close untracked directory %q: %w", root.Name(), err))
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
		return nil, fmt.Errorf("%w: untracked root is not a directory", ErrUntrackedInvalid)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := operations.openRoot(directory)
	if root != nil {
		reader.roots = append(reader.roots, root)
	}
	if err := reader.check(err); err != nil {
		return nil, fmt.Errorf("open untracked root: %w", err)
	}
	if root == nil {
		return nil, fmt.Errorf("%w: untracked root handle is missing", ErrUntrackedInvalid)
	}
	if err := reader.checkPath(root, ".", initial); err != nil {
		return nil, err
	}
	if err := reader.checkRootPath(directory, initial); err != nil {
		return nil, err
	}
	reader.observations = append(reader.observations, untrackedReadObservation{path: ".", name: ".", parent: root, directory: root, information: initial})
	if err := reader.collect(root, selection); err != nil {
		return nil, err
	}
	if err := reader.check(validateUntrackedEntries(reader.entries, maximumBytes)); err != nil {
		return nil, err
	}
	for _, observation := range reader.observations {
		if err := reader.checkPath(observation.parent, observation.name, observation.information); err != nil {
			return nil, fmt.Errorf("revalidate untracked entry %q: %w", observation.path, err)
		}
		if observation.directory != nil && observation.directory != observation.parent {
			if err := reader.checkPath(observation.directory, ".", observation.information); err != nil {
				return nil, err
			}
		}
		if observation.linkTarget != "" {
			target, err := reader.linkText(observation.parent, observation.name, observation.information)
			if err != nil {
				return nil, err
			}
			if target != observation.linkTarget {
				return nil, fmt.Errorf("%w: untracked link text changed", ErrUntrackedInvalid)
			}
		}
	}
	if err := reader.checkRootPath(directory, initial); err != nil {
		return nil, err
	}
	slices.SortFunc(reader.entries, func(left, right UntrackedEntry) int { return strings.Compare(left.Path, right.Path) })
	return reader.entries, nil
}

func (reader *untrackedReader) collect(parent *os.Root, selection *untrackedReadNode) error {
	for _, node := range selection.children {
		information, err := reader.pathInfo(parent, node.name)
		if err != nil {
			return fmt.Errorf("observe untracked entry %q: %w", node.path, err)
		}
		observation := untrackedReadObservation{path: node.path, name: node.name, parent: parent, information: information}
		entry := UntrackedEntry{Path: node.path, Mode: information.Mode()}
		if len(node.children) != 0 {
			if !information.IsDir() {
				return fmt.Errorf("%w: untracked parent %q is not a directory", ErrUntrackedInvalid, node.path)
			}
			if err := reader.ctx.Err(); err != nil {
				return err
			}
			child, err := reader.operations.openDirectory(parent, node.name)
			if child != nil {
				reader.roots = append(reader.roots, child)
			}
			if err := reader.check(err); err != nil {
				return fmt.Errorf("open untracked parent %q: %w", node.path, err)
			}
			if child == nil {
				return fmt.Errorf("%w: untracked parent handle is missing", ErrUntrackedInvalid)
			}
			if err := reader.checkPath(child, ".", information); err != nil {
				return err
			}
			if err := reader.checkPath(parent, node.name, information); err != nil {
				return err
			}
			entry.Kind = "directory"
			observation.directory = child
			reader.entries = append(reader.entries, entry)
			reader.observations = append(reader.observations, observation)
			if err := reader.collect(child, node); err != nil {
				return err
			}
			continue
		}
		switch {
		case information.Mode().IsRegular():
			entry.Kind = "file"
			entry.Data, err = reader.fileData(parent, node.name, information)
			if err != nil {
				return fmt.Errorf("read untracked file %q: %w", node.path, err)
			}
			reader.remaining -= int64(len(entry.Data))
		case information.Mode()&fs.ModeSymlink != 0:
			entry.Kind = "symlink"
			entry.LinkTarget, err = reader.linkText(parent, node.name, information)
			if err != nil {
				return fmt.Errorf("read untracked link %q: %w", node.path, err)
			}
			observation.linkTarget = entry.LinkTarget
		default:
			return fmt.Errorf("%w: requested untracked leaf %q is not a file or supported link", ErrUntrackedInvalid, node.path)
		}
		if err := reader.check(validateUntrackedEntry(entry)); err != nil {
			return err
		}
		reader.entries = append(reader.entries, entry)
		reader.observations = append(reader.observations, observation)
	}
	return nil
}
