package process

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/pathutil"
)

type Collector struct {
	Source   Source
	contains func(string, string) (bool, error)
}

func (collector Collector) Collect(ctx context.Context, worktrees []domain.Worktree) (Collection, []error) {
	contains := collector.contains
	if contains == nil {
		contains = pathutil.ContainsChecked
	}
	collection := Collection{
		ByWorktree:    make(map[string][]domain.ProcessEvidence, len(worktrees)),
		Uninspectable: make(map[int32]domain.ProcessEvidence),
		Complete:      true,
	}
	var errs, enumerationErrors []error
	report := func(err error) {
		errs = append(errs, err)
		collection.Errors = append(collection.Errors, err.Error())
	}
	incomplete := func(err error) {
		collection.Complete = false
		enumerationErrors = append(enumerationErrors, err)
		report(err)
	}
	finish := func() (Collection, []error) {
		if err := errors.Join(enumerationErrors...); err != nil {
			collection.GlobalUnknown = append(collection.GlobalUnknown, unknownEvidence(err))
		}
		return collection, errs
	}
	if err := ctx.Err(); err != nil {
		incomplete(err)
		return finish()
	}
	type worktreePath struct {
		key       string
		canonical string
	}
	roots := make([]worktreePath, 0, len(worktrees))
	for _, worktree := range worktrees {
		collection.ByWorktree[worktree.Path] = nil
		canonical, err := canonicalDirectory(worktree.Path)
		if err != nil {
			err = fmt.Errorf("worktree path %q: %w", worktree.Path, err)
			report(err)
			collection.ByWorktree[worktree.Path] = []domain.ProcessEvidence{unknownEvidence(err)}
			continue
		}
		roots = append(roots, worktreePath{key: worktree.Path, canonical: canonical})
	}
	if collector.Source == nil {
		incomplete(errors.New("process source is unavailable"))
		return finish()
	}
	processes, err := collector.Source.List(ctx)
	if err != nil {
		incomplete(fmt.Errorf("enumerate processes: %w", err))
	}
	invalidPIDs := make(map[int32]error)
	seen := make(map[int32]bool, len(processes))
	for _, info := range processes {
		var invalid error
		if info.PID <= 0 {
			invalid = fmt.Errorf("invalid process PID %d", info.PID)
		} else if seen[info.PID] {
			invalid = fmt.Errorf("conflicting process records for PID %d", info.PID)
		}
		if invalid != nil && invalidPIDs[info.PID] == nil {
			invalidPIDs[info.PID] = invalid
			incomplete(invalid)
		}
		seen[info.PID] = true
	}
	for _, info := range processes {
		if err := ctx.Err(); err != nil {
			incomplete(err)
			return finish()
		}
		if invalid := invalidPIDs[info.PID]; invalid != nil {
			if info.Error != "" {
				info.Error += "; "
			}
			info.Error += invalid.Error()
		}
		evidence := evidenceFor(info)
		hints := []string{evidence.CWD}
		if evidence.State == domain.EvidenceUnknown {
			hints = append(hints, processPathHints(info)...)
		}
		var matchedRoots []string
		var containmentErrors []error
		for _, root := range roots {
			for _, hint := range hints {
				if hint == "" {
					continue
				}
				contained, err := contains(root.canonical, hint)
				if err != nil {
					containmentErrors = append(containmentErrors, fmt.Errorf("worktree %q: %w", root.key, err))
					continue
				}
				if contained {
					matchedRoots = append(matchedRoots, root.key)
					break
				}
			}
		}
		if len(containmentErrors) != 0 {
			containmentErr := fmt.Errorf("containment: %w", errors.Join(containmentErrors...))
			if evidence.Error != "" {
				containmentErr = fmt.Errorf("%s; %w", evidence.Error, containmentErr)
			}
			evidence.State = domain.EvidenceUnknown
			evidence.Error = containmentErr.Error()
			collection.Uninspectable[info.PID] = evidence
			collection.GlobalUnknown = append(collection.GlobalUnknown, evidence)
			report(fmt.Errorf("process %d: %w", info.PID, containmentErr))
			continue
		}
		if evidence.State == domain.EvidenceUnknown {
			collection.Uninspectable[info.PID] = evidence
			report(fmt.Errorf("process %d: %s", info.PID, evidence.Error))
		}
		for _, root := range matchedRoots {
			collection.ByWorktree[root] = append(collection.ByWorktree[root], evidence)
		}
		if evidence.State == domain.EvidenceUnknown && len(matchedRoots) == 0 {
			collection.GlobalUnknown = append(collection.GlobalUnknown, evidence)
		}
	}
	if err := ctx.Err(); err != nil {
		incomplete(err)
	}
	return finish()
}

func evidenceFor(info Info) domain.ProcessEvidence {
	evidence := domain.ProcessEvidence{
		PID: info.PID, CreatedAt: info.CreatedAt.UTC(), Executable: info.Executable,
		State: domain.EvidenceActive,
	}
	var problems []string
	if info.Error != "" {
		problems = append(problems, info.Error)
	}
	if !info.Inspectable && info.Error == "" {
		problems = append(problems, "process inspection is incomplete")
	}
	if info.CreatedAt.IsZero() || !info.CreatedAt.After(time.Unix(0, 0)) {
		problems = append(problems, "creation time is unavailable or invalid")
	}
	if !validCommandLine(info.CommandLine) {
		problems = append(problems, "command line contains a NUL byte")
	}
	if strings.IndexByte(info.Owner, 0) >= 0 {
		problems = append(problems, "owner contains a NUL byte")
	}
	if !absolutePath(info.Executable) {
		problems = append(problems, "executable is not a valid absolute path")
	} else if canonical, err := pathutil.Canonical(info.Executable); err != nil {
		problems = append(problems, fmt.Sprintf("executable: %v", err))
	} else if executable, err := os.Stat(canonical); err != nil {
		problems = append(problems, fmt.Sprintf("executable: %v", err))
	} else if !executable.Mode().IsRegular() {
		problems = append(problems, "executable is not a regular file")
	}
	if !absolutePath(info.CWD) {
		problems = append(problems, "cwd is not a valid absolute path")
	} else {
		canonical, err := canonicalDirectory(info.CWD)
		if err != nil {
			problems = append(problems, fmt.Sprintf("cwd: %v", err))
		} else {
			evidence.CWD = canonical
		}
	}
	if len(problems) != 0 {
		evidence.State = domain.EvidenceUnknown
		evidence.Error = strings.Join(problems, "; ")
	}
	evidence.Fingerprint = fingerprint(evidence)
	return evidence
}

func processPathHints(info Info) []string {
	var hints []string
	paths := append([]string{info.Executable}, info.CommandLine...)
	for _, path := range paths {
		if !absolutePath(path) && strings.HasPrefix(path, "-") {
			_, value, found := strings.Cut(path, "=")
			if found {
				path = value
			}
		}
		if !absolutePath(path) {
			continue
		}
		canonical, err := pathutil.Canonical(path)
		if err == nil {
			hints = append(hints, canonical)
		}
	}
	return hints
}

func absolutePath(path string) bool {
	return path != "" && strings.IndexByte(path, 0) < 0 && filepath.IsAbs(path)
}

func canonicalDirectory(path string) (string, error) {
	canonical, err := pathutil.Canonical(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path %q is not a directory", path)
	}
	return canonical, nil
}

func unknownEvidence(err error) domain.ProcessEvidence {
	evidence := domain.ProcessEvidence{PID: 0, State: domain.EvidenceUnknown, Error: err.Error()}
	evidence.Fingerprint = fingerprint(evidence)
	return evidence
}

func fingerprint(evidence domain.ProcessEvidence) string {
	fields := []string{
		strconv.FormatInt(int64(evidence.PID), 10),
		evidence.CreatedAt.UTC().Format(time.RFC3339Nano),
		evidence.Executable,
		evidence.CWD,
	}
	var identity []byte
	for _, field := range fields {
		identity = binary.BigEndian.AppendUint64(identity, uint64(len(field)))
		identity = append(identity, field...)
	}
	digest := sha256.Sum256(identity)
	return hex.EncodeToString(digest[:])
}
