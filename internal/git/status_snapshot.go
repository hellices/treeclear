package git

import (
	"bytes"
	"context"
	"fmt"

	"github.com/hellices/treeclear/internal/domain"
)

type StatusSnapshot struct {
	Status         domain.GitStatus
	Raw            []byte
	UntrackedPaths []string
}

func (client *Client) StatusSnapshot(ctx context.Context, worktree string) (StatusSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return StatusSnapshot{}, err
	}
	if err := client.verifyWorktreeRoot(ctx, worktree); err != nil {
		return StatusSnapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return StatusSnapshot{}, err
	}
	contents, err := client.readStatusRaw(ctx, worktree)
	if err != nil {
		return StatusSnapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return StatusSnapshot{}, err
	}
	status, paths, err := parseStatusPorcelainZPaths(contents, true)
	if err != nil {
		return StatusSnapshot{}, err
	}
	result := StatusSnapshot{Status: status, Raw: bytes.Clone(contents), UntrackedPaths: paths}
	if err := ctx.Err(); err != nil {
		return StatusSnapshot{}, err
	}
	return result, nil
}

func (client *Client) readStatusRaw(ctx context.Context, worktree string) ([]byte, error) {
	if err := client.rejectExecutableFilters(ctx, worktree); err != nil {
		return nil, err
	}
	if err := client.rejectUnsafeIndex(ctx, worktree); err != nil {
		return nil, err
	}
	result, err := client.run(ctx, worktree, "status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return nil, err
	}
	if len(result.Stderr) != 0 {
		diagnostic := result.Stderr
		if len(diagnostic) > 4096 {
			diagnostic = diagnostic[:4096]
		}
		return nil, fmt.Errorf("git status reported diagnostics: %s", diagnostic)
	}
	return result.Stdout, nil
}
