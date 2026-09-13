package git

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

type rawWorktree struct {
	Path       string
	Head       string
	Branch     string
	Detached   bool
	Locked     bool
	LockReason string
	Prunable   bool
	Bare       bool
}

func parseWorktreePorcelainZ(contents []byte) ([]rawWorktree, error) {
	if !bytes.HasSuffix(contents, []byte{0, 0}) {
		return nil, errors.New("worktree inventory is empty or missing a record terminator")
	}
	var result []rawWorktree
	paths := make(map[string]bool)
	for _, record := range strings.Split(string(contents[:len(contents)-2]), "\x00\x00") {
		worktree := rawWorktree{}
		seen := make(map[string]bool)
		for index, field := range strings.Split(record, "\x00") {
			name, value, _ := strings.Cut(field, " ")
			if seen[name] || (index == 0 && name != "worktree") {
				return nil, fmt.Errorf("ambiguous worktree field %q", name)
			}
			seen[name] = true
			switch name {
			case "worktree":
				worktree.Path = value
			case "HEAD":
				worktree.Head = value
			case "branch":
				branch, found := strings.CutPrefix(value, "refs/heads/")
				if !found || branch == "" {
					return nil, fmt.Errorf("invalid worktree branch %q", value)
				}
				worktree.Branch = branch
			case "detached", "bare":
				if value != "" {
					return nil, fmt.Errorf("unexpected value for %s", name)
				}
				worktree.Detached = name == "detached" || worktree.Detached
				worktree.Bare = name == "bare" || worktree.Bare
			case "locked":
				worktree.Locked, worktree.LockReason = true, value
			case "prunable":
				worktree.Prunable = true
			default:
				return nil, fmt.Errorf("unknown worktree field %q", name)
			}
		}
		if worktree.Path == "" || paths[worktree.Path] {
			return nil, errors.New("missing or duplicate worktree path")
		}
		if worktree.Bare {
			if worktree.Head != "" || worktree.Branch != "" || worktree.Detached {
				return nil, errors.New("bare worktree has conflicting branch metadata")
			}
		} else if !validPorcelainObjectID(worktree.Head) || (worktree.Branch == "") == !worktree.Detached {
			return nil, errors.New("worktree HEAD or branch state is ambiguous")
		}
		paths[worktree.Path] = true
		result = append(result, worktree)
	}
	return result, nil
}
