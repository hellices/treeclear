package snapshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/git"
)

var (
	ErrSourceInvalid = errors.New("invalid snapshot source")
	ErrSourceChanged = errors.New("snapshot source changed")
	ErrSourceLimit   = errors.New("snapshot source limit exceeded")
)

type SourceCapture struct {
	WorktreeList          []byte
	Status                git.StatusSnapshot
	StagedPatch           []byte
	UnstagedPatch         []byte
	AdministrativeEntries []AdminEntry
	UntrackedEntries      []UntrackedEntry
}

type sourceCaptureGit interface {
	ListWorktreesRaw(context.Context, string) ([]domain.Worktree, []byte, error)
	InspectWorktree(context.Context, string, domain.Worktree) (domain.Worktree, error)
	StatusSnapshot(context.Context, string) (git.StatusSnapshot, error)
	Diff(context.Context, string, bool) ([]byte, error)
}

type sourceCaptureReaders struct {
	administrative func(context.Context, string) ([]AdminEntry, error)
	untracked      func(context.Context, string, []string, int64) ([]UntrackedEntry, error)
	roots          sourceRootOperations
}

func defaultSourceCaptureReaders() sourceCaptureReaders {
	return sourceCaptureReaders{
		administrative: ReadAdministrative, untracked: ReadUntracked,
		roots: defaultSourceRootOperations(),
	}
}

func CaptureSource(ctx context.Context, client *git.Client, expected domain.Worktree, maximumBytes int64) (SourceCapture, error) {
	if client == nil || client.Runner == nil {
		err := errors.New("source Git client is required")
		if ctx != nil {
			err = errors.Join(err, ctx.Err())
		}
		return SourceCapture{}, sourceCaptureError(err)
	}
	return captureSource(ctx, client, expected, maximumBytes, defaultSourceCaptureReaders())
}

func captureSource(ctx context.Context, client sourceCaptureGit, expected domain.Worktree, maximumBytes int64, readers sourceCaptureReaders) (capture SourceCapture, resultErr error) {
	if ctx == nil {
		return SourceCapture{}, sourceCaptureError(errors.New("source context is required"))
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			capture = SourceCapture{}
			resultErr = sourceCaptureError(resultErr)
		}
	}()
	if err := ctx.Err(); err != nil {
		return SourceCapture{}, err
	}
	if err := validateUntrackedBudget(maximumBytes); err != nil {
		return SourceCapture{}, fmt.Errorf("%w: %w", ErrSourceLimit, err)
	}
	if client == nil {
		return SourceCapture{}, errors.New("source Git client is required")
	}
	if err := validateSourceWorktree(expected); err != nil {
		return SourceCapture{}, err
	}
	roots := sourceRootGuard{ctx: ctx, operations: readers.roots}
	defer func() { resultErr = errors.Join(resultErr, roots.close()) }()
	if err := roots.bind([]string{expected.RepositoryRoot, expected.CommonGitDir, expected.Path, expected.AdminDir}); err != nil {
		return SourceCapture{}, err
	}
	first, firstState, err := collectSource(ctx, client, expected, maximumBytes, readers)
	if err != nil {
		return SourceCapture{}, err
	}
	if err := roots.check(); err != nil {
		return SourceCapture{}, err
	}
	second, secondState, err := collectSource(ctx, client, expected, maximumBytes, readers)
	if err != nil {
		return SourceCapture{}, err
	}
	if err := roots.check(); err != nil {
		return SourceCapture{}, err
	}
	if !equalSourceWorktree(firstState, secondState) || !equalSourceCapture(first, second) {
		return SourceCapture{}, fmt.Errorf("%w: source collections differ", ErrSourceChanged)
	}
	return first, nil
}

func collectSource(ctx context.Context, client sourceCaptureGit, expected domain.Worktree, maximumBytes int64, readers sourceCaptureReaders) (SourceCapture, domain.Worktree, error) {
	var capture SourceCapture
	remaining := sourceByteBudget(maximumBytes)
	if err := ctx.Err(); err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	listed, raw, err := client.ListWorktreesRaw(ctx, expected.RepositoryRoot)
	if err := sourceCallError(ctx, err); err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	if err := remaining.take(len(raw)); err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	capture.WorktreeList = bytes.Clone(raw)
	target, err := selectSourceWorktree(listed, expected)
	if err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	target.PathSafe = true
	target.CollectionErrors = slices.Clone(target.CollectionErrors)
	if err := ctx.Err(); err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	inspected, err := client.InspectWorktree(ctx, expected.RepositoryRoot, target)
	if err := sourceCallError(ctx, err); err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	if err := validateSourceWorktree(inspected); err != nil {
		return SourceCapture{}, domain.Worktree{}, fmt.Errorf("%w: inspected source: %w", ErrSourceChanged, err)
	}
	if !equalSourceWorktree(expected, inspected) {
		return SourceCapture{}, domain.Worktree{}, fmt.Errorf("%w: inspected source disagrees with expected worktree", ErrSourceChanged)
	}
	inspected.CollectionErrors = slices.Clone(inspected.CollectionErrors)
	if err := ctx.Err(); err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	status, err := client.StatusSnapshot(ctx, expected.Path)
	if err := sourceCallError(ctx, err); err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	if err := remaining.take(len(status.Raw)); err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	if status.Status != inspected.Status || status.Status.Untracked != len(status.UntrackedPaths) {
		return SourceCapture{}, domain.Worktree{}, fmt.Errorf("%w: status or selected leaf count disagrees with inspection", ErrSourceChanged)
	}
	selection, err := selectUntrackedReadPaths(ctx, status.UntrackedPaths)
	if err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	capture.Status = git.StatusSnapshot{Status: status.Status, Raw: bytes.Clone(status.Raw), UntrackedPaths: slices.Clone(status.UntrackedPaths)}
	for _, staged := range []bool{true, false} {
		if err := ctx.Err(); err != nil {
			return SourceCapture{}, domain.Worktree{}, err
		}
		patch, err := client.Diff(ctx, expected.Path, staged)
		if err := sourceCallError(ctx, err); err != nil {
			return SourceCapture{}, domain.Worktree{}, err
		}
		if err := remaining.take(len(patch)); err != nil {
			return SourceCapture{}, domain.Worktree{}, err
		}
		if staged {
			capture.StagedPatch = bytes.Clone(patch)
		} else {
			capture.UnstagedPatch = bytes.Clone(patch)
		}
	}
	if err := ctx.Err(); err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	administrative, err := readers.administrative(ctx, expected.AdminDir)
	if err := sourceCallError(ctx, err); err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	if err := validateSourceAdministrative(administrative, inspected.IndexHash, &remaining); err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	capture.AdministrativeEntries = slices.Clone(administrative)
	for index := range capture.AdministrativeEntries {
		capture.AdministrativeEntries[index].Data = bytes.Clone(capture.AdministrativeEntries[index].Data)
	}
	if err := ctx.Err(); err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	untracked, err := readers.untracked(ctx, expected.Path, slices.Clone(capture.Status.UntrackedPaths), max(1, int64(remaining)))
	if err := sourceCallError(ctx, err); err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	if err := validateSourceUntracked(untracked, selection, &remaining); err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	capture.UntrackedEntries = slices.Clone(untracked)
	for index := range capture.UntrackedEntries {
		capture.UntrackedEntries[index].Data = bytes.Clone(capture.UntrackedEntries[index].Data)
	}
	if err := ctx.Err(); err != nil {
		return SourceCapture{}, domain.Worktree{}, err
	}
	return capture, inspected, nil
}

func sourceCallError(ctx context.Context, err error) error {
	return errors.Join(err, ctx.Err())
}

func sourceCaptureError(err error) error {
	if errors.Is(err, git.ErrWorktreeChanged) {
		err = fmt.Errorf("%w: %w", ErrSourceChanged, err)
	}
	if errors.Is(err, ErrManifestLimit) || errors.Is(err, ErrUntrackedLimit) || errors.Is(err, execx.ErrOutputLimit) || errors.Is(err, git.ErrIndexPreflightLimit) {
		return fmt.Errorf("%w: %w: %w", ErrSourceInvalid, ErrSourceLimit, err)
	}
	return fmt.Errorf("%w: %w", ErrSourceInvalid, err)
}
