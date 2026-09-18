package plan

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"

	"github.com/hellices/treeclear/internal/domain"
)

const (
	LegacySchemaVersion  = 1
	PreviewSchemaVersion = 2
)

var ErrPlanNotExecutable = errors.New("plan is not executable")

func (store Store) LoadForApply(ctx context.Context, reference string) (domain.Plan, error) {
	value, err := store.Load(ctx, reference)
	if err != nil {
		return domain.Plan{}, err
	}
	return domain.Plan{}, fmt.Errorf("%w: schema %d plans are inspection-only and cannot authorize removal", ErrPlanNotExecutable, value.SchemaVersion)
}

func validatePreviewRequest(request Request) error {
	if request.BackupRequested {
		return fmt.Errorf("%w: backup is not implemented; no plan was created", ErrPlanInvalid)
	}
	if request.SkipDirty && len(request.SelectedPaths) == 0 {
		return fmt.Errorf("%w: skip-dirty requires explicit worktree selection", ErrPlanInvalid)
	}
	if len(request.SelectedPaths) != 0 && request.IntendedApplyMode != domain.ApplyInteractive {
		return fmt.Errorf("%w: explicit selection is not available for scheduled cleanup", ErrPlanInvalid)
	}
	return validateSelectedPaths(request.SelectedPaths)
}

func validateSelectedPaths(paths []string) error {
	identities := make(map[string]bool, len(paths))
	for _, path := range paths {
		if !canonicalBuildPath(path) {
			return fmt.Errorf("%w: selected path %q is not a canonical absolute path", ErrPlanInvalid, path)
		}
		key := buildPathKey(path)
		if identities[key] {
			return fmt.Errorf("%w: duplicate selected worktree %q", ErrPlanInvalid, path)
		}
		identities[key] = true
	}
	for _, path := range paths {
		for parent := filepath.Dir(path); parent != path; parent = filepath.Dir(parent) {
			if identities[buildPathKey(parent)] {
				return fmt.Errorf("%w: overlapping selected worktrees %q and %q", ErrPlanInvalid, parent, path)
			}
			if parent == filepath.Dir(parent) {
				break
			}
		}
	}
	return nil
}

func matchSelectedPaths(paths []string, worktrees []domain.Worktree) ([]string, error) {
	registered := make(map[string]domain.Worktree, len(worktrees))
	for _, worktree := range worktrees {
		registered[buildPathKey(worktree.Path)] = worktree
	}
	matched := make([]string, 0, len(paths))
	for _, path := range paths {
		worktree, found := registered[buildPathKey(path)]
		if !found {
			return nil, fmt.Errorf("%w: selected worktree %q is not an exact registered root in the collected scope", ErrPlanInvalid, path)
		}
		if worktree.Primary {
			return nil, fmt.Errorf("%w: primary worktree %q cannot be selected", ErrPlanInvalid, path)
		}
		matched = append(matched, worktree.Path)
	}
	sort.Strings(matched)
	return matched, nil
}

func previewSelection(worktree domain.Worktree, selected, skipDirty bool) *domain.CandidateSelection {
	selection := &domain.CandidateSelection{Selected: selected}
	status := worktree.Status
	knownStatus := worktree.GitStateKnown && len(worktree.CollectionErrors) == 0 && status.Staged >= 0 && status.Unstaged >= 0 && status.Unmerged >= 0 && status.Untracked >= 0
	if selected && skipDirty && knownStatus && !status.Clean() {
		selection.SkipReason = "dirty"
	}
	return selection
}

func validatePreviewPlan(value domain.Plan) error {
	removal := value.Removal
	if removal == nil || removal.ContentDisposition != domain.ContentDiscardAll || removal.BackupMode != domain.BackupNone || removal.Execution != domain.ExecutionPreviewOnly {
		return fmt.Errorf("%w: v2 requires discard-all, no-backup, preview-only policy", ErrPlanInvalid)
	}
	if value.GeneratedAt.IsZero() || removal.SelectedPaths == nil || !slices.IsSorted(removal.SelectedPaths) {
		return fmt.Errorf("%w: v2 requires generation time and a sorted explicit selection record", ErrPlanInvalid)
	}
	if value.IntendedApplyMode != domain.ApplyInteractive && value.IntendedApplyMode != domain.ApplyScheduled {
		return fmt.Errorf("%w: unknown intended apply mode", ErrPlanInvalid)
	}
	if err := validatePreviewRequest(Request{SelectedPaths: removal.SelectedPaths, SkipDirty: removal.SkipDirty, IntendedApplyMode: value.IntendedApplyMode}); err != nil {
		return err
	}
	wantIntent := domain.RemovalIntentInventory
	if len(removal.SelectedPaths) != 0 {
		wantIntent = domain.RemovalIntentExplicit
	}
	if removal.Intent != wantIntent {
		return fmt.Errorf("%w: removal intent conflicts with selected paths", ErrPlanInvalid)
	}
	worktrees := make([]domain.Worktree, len(value.Candidates))
	for index, candidate := range value.Candidates {
		worktrees[index] = candidate.Worktree
	}
	if err := validateBuildIdentities(worktrees); err != nil {
		return err
	}
	matched, err := matchSelectedPaths(removal.SelectedPaths, worktrees)
	if err != nil {
		return err
	}
	if !slices.Equal(matched, removal.SelectedPaths) {
		return fmt.Errorf("%w: selected paths differ from recorded registered roots", ErrPlanInvalid)
	}
	selected := make(map[string]bool, len(matched))
	for _, path := range matched {
		selected[path] = true
	}
	var summary domain.PlanSummary
	for _, candidate := range value.Candidates {
		identifier, err := buildCandidateID(candidate.Worktree)
		if err != nil || candidate.ID != identifier {
			return fmt.Errorf("%w: candidate identity does not match its worktree", ErrPlanInvalid)
		}
		selection := previewSelection(candidate.Worktree, selected[candidate.Worktree.Path], removal.SkipDirty)
		if candidate.Selection == nil || *candidate.Selection != *selection {
			return fmt.Errorf("%w: candidate selection conflicts with the removal policy", ErrPlanInvalid)
		}
		if candidate.Action != "none" || candidate.Snapshot != (domain.SnapshotPlan{}) {
			return fmt.Errorf("%w: v2 previews cannot authorize removal or backup", ErrPlanInvalid)
		}
		fingerprint, err := CandidateFingerprint(candidate)
		if err != nil || candidate.Fingerprint != fingerprint {
			return fmt.Errorf("%w: candidate fingerprint does not match its preconditions", ErrPlanInvalid)
		}
		switch candidate.Decision.Classification {
		case domain.Safe:
			summary.Safe++
		case domain.Review:
			summary.Review++
		case domain.Protected:
			summary.Protected++
		default:
			return fmt.Errorf("%w: unknown candidate classification", ErrPlanInvalid)
		}
	}
	if value.Summary != summary {
		return fmt.Errorf("%w: preview summary conflicts with recorded candidates", ErrPlanInvalid)
	}
	return nil
}
