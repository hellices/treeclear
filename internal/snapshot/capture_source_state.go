package snapshot

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/gitref"
	"github.com/hellices/treeclear/internal/pathutil"
)

func validateSourceWorktree(value domain.Worktree) error {
	if !value.GitStateKnown || !value.PathSafe || value.Primary || value.Prunable || len(value.CollectionErrors) != 0 {
		return fmt.Errorf("source must be a known path-safe non-prunable linked worktree without collection errors")
	}
	if !(validLowerHex(value.Head, 40) || validLowerHex(value.Head, 64)) || strings.Trim(value.Head, "0") == "" {
		return fmt.Errorf("source HEAD must be a full nonzero object ID")
	}
	if value.Detached && value.Branch != "" || !value.Detached && !gitref.ValidBranchName(value.Branch) {
		return fmt.Errorf("source branch or detached identity is invalid")
	}
	if !value.Locked && value.LockReason != "" {
		return fmt.Errorf("source lock reason conflicts with unlocked state")
	}
	if !validLowerHex(value.IndexHash, 64) || !validLowerHex(value.AdminHash, 64) {
		return fmt.Errorf("source index and administrative hashes must be SHA-256 hex")
	}
	if value.Status.Staged < 0 || value.Status.Unstaged < 0 || value.Status.Unmerged < 0 || value.Status.Untracked < 0 {
		return fmt.Errorf("source status counts must be nonnegative")
	}
	for _, directory := range []string{value.RepositoryRoot, value.CommonGitDir, value.Path, value.AdminDir} {
		if len(directory) > 32<<10 {
			return manifestLimit("source root path")
		}
		if err := pathutil.ValidateAbsoluteForm(directory); err != nil {
			return err
		}
	}
	relative, err := filepath.Rel(value.CommonGitDir, value.AdminDir)
	if err != nil || relative == "." || !filepath.IsLocal(relative) {
		return fmt.Errorf("source administrative directory must be strictly inside the common Git directory")
	}
	return nil
}

func selectSourceWorktree(listed []domain.Worktree, expected domain.Worktree) (domain.Worktree, error) {
	var target domain.Worktree
	var targets, primaries int
	for _, entry := range listed {
		if entry.RepositoryRoot != expected.RepositoryRoot || entry.CommonGitDir != expected.CommonGitDir || len(entry.CollectionErrors) != 0 {
			return domain.Worktree{}, fmt.Errorf("%w: listed repository or common identity conflicts", ErrSourceChanged)
		}
		if entry.Primary != (entry.Path == expected.RepositoryRoot) {
			return domain.Worktree{}, fmt.Errorf("%w: primary path and registration disagree", ErrSourceChanged)
		}
		if entry.Primary {
			primaries++
			if entry.Prunable {
				return domain.Worktree{}, fmt.Errorf("%w: primary registration conflicts", ErrSourceChanged)
			}
		}
		if entry.Path == expected.Path {
			targets++
			target = entry
		}
	}
	if targets != 1 || primaries != 1 || !equalSourceRegistration(target, expected) {
		return domain.Worktree{}, fmt.Errorf("%w: source registration is missing, ambiguous or conflicting", ErrSourceChanged)
	}
	return target, nil
}

func equalSourceRegistration(left, right domain.Worktree) bool {
	return left.Path == right.Path && left.RepositoryRoot == right.RepositoryRoot && left.CommonGitDir == right.CommonGitDir &&
		left.Head == right.Head && left.Branch == right.Branch && left.Primary == right.Primary &&
		left.Detached == right.Detached && left.Locked == right.Locked && left.LockReason == right.LockReason && left.Prunable == right.Prunable
}

func equalSourceWorktree(left, right domain.Worktree) bool {
	return equalSourceRegistration(left, right) && left.AdminDir == right.AdminDir && left.Upstream == right.Upstream &&
		left.PathSafe == right.PathSafe && left.GitStateKnown == right.GitStateKnown && slices.Equal(left.CollectionErrors, right.CollectionErrors) &&
		left.Status == right.Status && left.Recoverable == right.Recoverable && left.LastCommitAt.Equal(right.LastCommitAt) &&
		left.MetadataModifiedAt.Equal(right.MetadataModifiedAt) && left.IndexHash == right.IndexHash && left.AdminHash == right.AdminHash
}

func equalSourceCapture(left, right SourceCapture) bool {
	return bytes.Equal(left.WorktreeList, right.WorktreeList) && left.Status.Status == right.Status.Status &&
		bytes.Equal(left.Status.Raw, right.Status.Raw) && slices.Equal(left.Status.UntrackedPaths, right.Status.UntrackedPaths) &&
		bytes.Equal(left.StagedPatch, right.StagedPatch) && bytes.Equal(left.UnstagedPatch, right.UnstagedPatch) &&
		slices.EqualFunc(left.AdministrativeEntries, right.AdministrativeEntries, func(left, right AdminEntry) bool {
			return left.Path == right.Path && left.Kind == right.Kind && left.Mode == right.Mode && bytes.Equal(left.Data, right.Data)
		}) && slices.EqualFunc(left.UntrackedEntries, right.UntrackedEntries, func(left, right UntrackedEntry) bool {
		return left.Path == right.Path && left.Kind == right.Kind && left.Mode == right.Mode && left.LinkTarget == right.LinkTarget && bytes.Equal(left.Data, right.Data)
	})
}

type sourceByteBudget int64

func (remaining *sourceByteBudget) take(size int) error {
	if int64(size) > int64(*remaining) {
		return fmt.Errorf("%w: aggregate source bytes", ErrSourceLimit)
	}
	*remaining -= sourceByteBudget(size)
	return nil
}

func validateSourceAdministrative(entries []AdminEntry, indexHash string, remaining *sourceByteBudget) error {
	if len(entries) > maximumAdministrativeEntries {
		return manifestLimit("administrative entry count")
	}
	for _, entry := range entries {
		if err := remaining.take(len(entry.Data)); err != nil {
			return err
		}
	}
	if err := validateAdministrativeEntries(entries); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Path == "index" && fmt.Sprintf("%x", sha256.Sum256(entry.Data)) == indexHash {
			return nil
		}
	}
	return fmt.Errorf("%w: administrative index bytes disagree with inspected hash", ErrSourceChanged)
}

func validateSourceUntracked(entries []UntrackedEntry, selection *untrackedReadNode, remaining *sourceByteBudget) error {
	if err := validateUntrackedEntries(entries, int64(*remaining)); err != nil {
		return err
	}
	byPath := make(map[string]UntrackedEntry, len(entries))
	for _, entry := range entries {
		if err := remaining.take(len(entry.Data)); err != nil {
			return err
		}
		if err := remaining.take(len(entry.LinkTarget)); err != nil {
			return err
		}
		byPath[entry.Path] = entry
	}
	pending := slices.Clone(selection.children)
	for len(pending) != 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		entry, exists := byPath[node.path]
		if !exists || (entry.Kind == "directory") != (len(node.children) != 0) {
			return fmt.Errorf("%w: untracked leaves or parents disagree with Git selection", ErrSourceChanged)
		}
		delete(byPath, node.path)
		pending = append(pending, node.children...)
	}
	if len(byPath) != 0 {
		return fmt.Errorf("%w: untracked entries outside Git selection", ErrSourceChanged)
	}
	return nil
}
