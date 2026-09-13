package plan

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hellices/treeclear/internal/correlate"
	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/policy"
	"github.com/hellices/treeclear/internal/process"
)

type Request struct {
	Roots             []string
	Settings          domain.PolicySettings
	IntendedApplyMode domain.ApplyMode
	AdapterLockDigest string
}

type InventoryLoader interface {
	Load(context.Context, []string) ([]domain.Worktree, []error)
}

type ProcessCollector interface {
	Collect(context.Context, []domain.Worktree) (process.Collection, []error)
}

type Builder struct {
	Inventory InventoryLoader
	Processes ProcessCollector
	Now       func() time.Time
	Version   string
}

func (builder Builder) Build(ctx context.Context, request Request) (domain.Plan, error) {
	if nilBuildValue(ctx) {
		return domain.Plan{}, fmt.Errorf("%w: context is required", ErrPlanInvalid)
	}
	if err := ctx.Err(); err != nil {
		return domain.Plan{}, err
	}
	if err := builder.validateRequest(request); err != nil {
		return domain.Plan{}, err
	}
	request.Roots = slices.Clone(request.Roots)
	request.Settings.BaseBranches = slices.Clone(request.Settings.BaseBranches)
	now := builder.Now
	if now == nil {
		now = time.Now
	}
	generatedAt := now().UTC()
	if generatedAt.IsZero() {
		return domain.Plan{}, fmt.Errorf("%w: current time is unknown", ErrPlanInvalid)
	}
	digest, err := PolicyDigest(request.Settings)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("%w: %w", ErrPlanInvalid, err)
	}
	value := domain.Plan{
		SchemaVersion: 1, GeneratedAt: generatedAt, ExpiresAt: generatedAt.Add(request.Settings.PlanExpiry),
		ToolVersion: builder.Version, IntendedApplyMode: request.IntendedApplyMode,
		PolicyDigest: digest, AdapterLockDigest: request.AdapterLockDigest,
		Scope: domain.PlanScope{Roots: request.Roots}, Candidates: []domain.Candidate{},
		Warnings: []string{"Core-only plan: agent-provider adapters are not implemented; this is not authorization to remove worktrees."},
	}
	if _, err := canonicalPlanJSON(value); err != nil {
		return domain.Plan{}, fmt.Errorf("%w: %w", ErrPlanInvalid, err)
	}
	if err := ctx.Err(); err != nil {
		return domain.Plan{}, err
	}
	var failures []error
	var globalWarnings []string
	report := func(source string, failure error, global bool) string {
		if nilBuildValue(failure) {
			return ""
		}
		failure = fmt.Errorf("%s: %w", source, failure)
		failures = append(failures, failure)
		warning := failure.Error()
		value.Warnings = append(value.Warnings, warning)
		if global {
			globalWarnings = append(globalWarnings, warning)
		}
		return warning
	}
	abort := func(failure error) (domain.Plan, error) {
		return domain.Plan{}, errors.Join(ctx.Err(), failure, errors.Join(failures...))
	}
	worktrees, inventoryErrors := builder.Inventory.Load(ctx, slices.Clone(request.Roots))
	worktrees = cloneBuildWorktrees(worktrees)
	for _, failure := range inventoryErrors {
		report("inventory collection", failure, true)
	}
	worktreeWarnings := make(map[string][]string, len(worktrees))
	for _, worktree := range worktrees {
		for _, failure := range buildWorktreeProblems(worktree) {
			warning := report(fmt.Sprintf("worktree %q", worktree.Path), failure, false)
			worktreeWarnings[worktree.Path] = append(worktreeWarnings[worktree.Path], warning)
		}
	}
	if err := ctx.Err(); err != nil {
		return abort(err)
	}
	if err := validateBuildIdentities(worktrees); err != nil {
		return abort(err)
	}
	processes := process.Collection{Complete: true}
	if len(worktrees) != 0 {
		processContext, cancel := context.WithTimeout(ctx, 30*time.Second)
		var processErrors []error
		processes, processErrors = builder.Processes.Collect(processContext, cloneBuildWorktrees(worktrees))
		deadlineError := processContext.Err()
		cancel()
		for _, failure := range processErrors {
			report("process collection", failure, true)
		}
		report("process collection", deadlineError, true)
	}
	for _, failure := range buildProcessProblems(processes) {
		report("process collection", failure, true)
	}
	if err := ctx.Err(); err != nil {
		return abort(err)
	}
	globalWarnings = sortedBuildWarnings(globalWarnings)
	grouped := correlate.Group(worktrees, processes, nil)
	sort.Slice(worktrees, func(first, second int) bool { return worktrees[first].Path < worktrees[second].Path })
	for _, worktree := range worktrees {
		if err := ctx.Err(); err != nil {
			return abort(err)
		}
		evidence := grouped[worktree.Path]
		evidence.Warnings = sortedBuildWarnings(append(slices.Clone(globalWarnings), worktreeWarnings[worktree.Path]...))
		identifier, err := buildCandidateID(worktree)
		if err != nil {
			return abort(err)
		}
		candidate := domain.Candidate{
			ID: identifier, Worktree: worktree, Evidence: evidence, Action: "none",
			Decision: policy.Evaluate(worktree, evidence, domain.Policy{Now: generatedAt, Settings: request.Settings}),
			Snapshot: domain.SnapshotPlan{MaximumBytes: request.Settings.SnapshotMaxBytes, UntrackedFiles: worktree.Status.Untracked},
		}
		switch candidate.Decision.Classification {
		case domain.Safe:
			candidate.Action, candidate.Snapshot.Required = "remove", true
			value.Summary.Safe++
			if worktree.EstimatedBytes < 0 || worktree.EstimatedBytes > math.MaxInt64-value.Summary.ReclaimableBytes {
				return abort(fmt.Errorf("%w: reclaimable bytes overflow", ErrPlanInvalid))
			}
			value.Summary.ReclaimableBytes += worktree.EstimatedBytes
		case domain.Review:
			value.Summary.Review++
		case domain.Protected:
			value.Summary.Protected++
		}
		candidate.Fingerprint, err = CandidateFingerprint(candidate)
		if err != nil {
			return abort(fmt.Errorf("fingerprint worktree %q: %w", worktree.Path, err))
		}
		value.Candidates = append(value.Candidates, candidate)
	}
	value.Warnings = sortedBuildWarnings(value.Warnings)
	encoded, err := canonicalPlanJSON(value)
	if err != nil {
		return abort(fmt.Errorf("encode plan: %w", err))
	}
	var randomness [32]byte
	if _, err := rand.Read(randomness[:]); err != nil {
		return abort(fmt.Errorf("generate plan identity: %w", err))
	}
	value.ID = fmt.Sprintf("plan_%x", sha256.Sum256(append(encoded, randomness[:]...)))
	if err := ctx.Err(); err != nil {
		return abort(err)
	}
	if len(failures) != 0 {
		return value, fmt.Errorf("plan collection incomplete: %w", errors.Join(failures...))
	}
	return value, nil
}

func (builder Builder) validateRequest(request Request) error {
	if nilBuildValue(builder.Inventory) || nilBuildValue(builder.Processes) {
		return fmt.Errorf("%w: inventory and process collectors are required", ErrPlanInvalid)
	}
	if len(request.Roots) == 0 {
		return fmt.Errorf("%w: at least one root is required", ErrPlanInvalid)
	}
	for _, root := range request.Roots {
		if !canonicalBuildPath(root) {
			return fmt.Errorf("%w: root %q is not a canonical absolute path", ErrPlanInvalid, root)
		}
	}
	if request.IntendedApplyMode != domain.ApplyInteractive && request.IntendedApplyMode != domain.ApplyScheduled {
		return fmt.Errorf("%w: unknown intended apply mode %q", ErrPlanInvalid, request.IntendedApplyMode)
	}
	settings := request.Settings
	if settings.InactivityThreshold <= 0 || settings.PlanExpiry <= 0 || settings.SnapshotMaxBytes <= 0 {
		return fmt.Errorf("%w: policy durations and snapshot maximum bytes must be positive", ErrPlanInvalid)
	}
	switch settings.MinimumTrustGrade {
	case domain.TrustSupportedAPI, domain.TrustSupportedAppServer, domain.TrustSupportedCLI,
		domain.TrustExperimentalAPI, domain.TrustVersionedPrivate, domain.TrustUnversionedPrivate, domain.TrustProcessOnly:
	default:
		return fmt.Errorf("%w: unknown minimum trust grade %q", ErrPlanInvalid, settings.MinimumTrustGrade)
	}
	return nil
}

func nilBuildValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func canonicalBuildPath(path string) bool {
	return utf8.ValidString(path) && !strings.ContainsRune(path, 0) && filepath.IsAbs(path) && filepath.Clean(path) == path
}

func buildPathKey(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func validateBuildIdentities(worktrees []domain.Worktree) error {
	paths := make(map[string]bool, len(worktrees))
	admins := make(map[string]bool, len(worktrees))
	for _, worktree := range worktrees {
		if !canonicalBuildPath(worktree.Path) {
			return fmt.Errorf("%w: worktree %q is not a canonical absolute path", ErrPlanInvalid, worktree.Path)
		}
		path := buildPathKey(worktree.Path)
		if paths[path] {
			return fmt.Errorf("%w: duplicate worktree path %q", ErrPlanInvalid, worktree.Path)
		}
		paths[path] = true
		if canonicalBuildPath(worktree.AdminDir) {
			admin := buildPathKey(worktree.AdminDir)
			if admins[admin] {
				return fmt.Errorf("%w: duplicate worktree administrative identity %q", ErrPlanInvalid, worktree.AdminDir)
			}
			admins[admin] = true
		}
	}
	return nil
}

func cloneBuildWorktrees(worktrees []domain.Worktree) []domain.Worktree {
	cloned := slices.Clone(worktrees)
	for index := range cloned {
		cloned[index].CollectionErrors = slices.Clone(cloned[index].CollectionErrors)
	}
	return cloned
}

func buildWorktreeProblems(worktree domain.Worktree) []error {
	var failures []error
	for _, diagnostic := range worktree.CollectionErrors {
		if diagnostic == "" {
			diagnostic = "Git collection failed without a diagnostic"
		}
		failures = append(failures, errors.New(diagnostic))
	}
	for _, identity := range []struct{ name, path string }{
		{"repository root", worktree.RepositoryRoot}, {"common Git directory", worktree.CommonGitDir}, {"administrative directory", worktree.AdminDir},
	} {
		if !canonicalBuildPath(identity.path) {
			failures = append(failures, fmt.Errorf("%s %q is not a canonical absolute path", identity.name, identity.path))
		}
	}
	if !worktree.Primary && (buildPathKey(worktree.Path) == buildPathKey(worktree.RepositoryRoot) || worktree.AdminDir != "" && buildPathKey(worktree.AdminDir) == buildPathKey(worktree.CommonGitDir)) {
		failures = append(failures, errors.New("linked worktree identity conflicts with the primary worktree"))
	}
	if !worktree.GitStateKnown || worktree.LastCommitAt.IsZero() || worktree.MetadataModifiedAt.IsZero() {
		failures = append(failures, errors.New("Git collection is incomplete"))
	}
	if worktree.EstimatedBytes < 0 || worktree.Status.Staged < 0 || worktree.Status.Unstaged < 0 || worktree.Status.Unmerged < 0 || worktree.Status.Untracked < 0 {
		failures = append(failures, errors.New("Git collection contains a negative size or status count"))
	}
	return failures
}

func buildProcessProblems(collection process.Collection) []error {
	var failures []error
	if !collection.Complete {
		failures = append(failures, errors.New("process enumeration is incomplete"))
	}
	for _, diagnostic := range collection.Errors {
		if diagnostic == "" {
			diagnostic = "process collection failed without a diagnostic"
		}
		failures = append(failures, errors.New(diagnostic))
	}
	inspect := func(evidence domain.ProcessEvidence, unknown bool) {
		if evidence.Error != "" {
			failures = append(failures, fmt.Errorf("process %d: %s", evidence.PID, evidence.Error))
		} else if unknown || (evidence.State != domain.EvidenceActive && evidence.State != domain.EvidenceInactive && evidence.State != domain.EvidenceNotApplicable) {
			failures = append(failures, fmt.Errorf("unknown process evidence for PID %d", evidence.PID))
		}
	}
	for _, evidence := range collection.GlobalUnknown {
		inspect(evidence, true)
	}
	for _, evidence := range collection.Uninspectable {
		inspect(evidence, true)
	}
	for _, records := range collection.ByWorktree {
		for _, evidence := range records {
			inspect(evidence, false)
		}
	}
	sort.Slice(failures, func(first, second int) bool { return failures[first].Error() < failures[second].Error() })
	return failures
}

func sortedBuildWarnings(warnings []string) []string {
	sort.Strings(warnings)
	return slices.Compact(warnings)
}

func buildCandidateID(worktree domain.Worktree) (string, error) {
	encoded, err := encodePreconditions([4]string{worktree.Path, worktree.RepositoryRoot, worktree.CommonGitDir, worktree.AdminDir})
	if err != nil {
		return "", fmt.Errorf("encode worktree identity: %w", err)
	}
	return fmt.Sprintf("candidate_%x", sha256.Sum256(encoded)), nil
}
