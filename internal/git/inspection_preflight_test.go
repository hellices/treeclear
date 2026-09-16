package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestClientInspectPreflightsSplitIndexBeforeHashes(test *testing.T) {
	for _, linked := range []bool{false, true} {
		for _, missingIndex := range []bool{false, true} {
			test.Run(fmt.Sprintf("linked-%t/missing-index-%t", linked, missingIndex), func(test *testing.T) {
				repository := testutil.NewRepository(test)
				worktree := repository.Root
				if linked {
					worktree = repository.AddWorktree(test, "preflight-order", "topic")
				}
				indexCommands := 0
				client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
					if slices.Contains([]string{"ls-files", "status", "diff"}, request.Args[0]) {
						indexCommands++
					}
					return (execx.OSRunner{}).Run(ctx, request)
				}))
				record := inspectionIdentityRecord(test, client, repository.Root, worktree)
				repository.Git(test, "-C", worktree, "update-index", "--split-index")
				administrative := repository.Git(test, "-C", worktree, "rev-parse", "--absolute-git-dir")
				shared, err := filepath.Glob(filepath.Join(administrative, "sharedindex.*"))
				if err != nil || len(shared) != 1 {
					test.Fatalf("split-index fixture: %q, %v", shared, err)
				}
				indexPath := filepath.Join(administrative, "index")
				if missingIndex {
					if err := os.Remove(indexPath); err != nil {
						test.Fatal(err)
					}
				}
				before := map[string]readonlyIndexFileEvidence{
					filepath.Base(shared[0]): readonlyIndexFileObservation(test, shared[0]),
				}
				if !missingIndex {
					before["index"] = readonlyIndexFileObservation(test, indexPath)
				}
				defer func() {
					after := make(map[string]readonlyIndexFileEvidence, len(before))
					for name := range before {
						after[name] = readonlyIndexFileObservation(test, filepath.Join(administrative, name))
					}
					assertReadonlyIndexEvidence(test, before, after)
					if missingIndex {
						if _, err := os.Lstat(indexPath); !errors.Is(err, os.ErrNotExist) {
							test.Errorf("inspection recreated missing index: %v", err)
						}
					}
					afterShared, err := filepath.Glob(filepath.Join(administrative, "sharedindex.*"))
					if err != nil || !slices.Equal(shared, afterShared) {
						test.Errorf("inspection changed backing entries: %q, %v", afterShared, err)
					}
				}()
				actual, err := client.InspectWorktree(test.Context(), repository.Root, record)
				if !errors.Is(err, errors.ErrUnsupported) || actual.GitStateKnown || len(actual.CollectionErrors) == 0 || actual.IndexHash != "" || actual.AdminHash != "" || indexCommands != 0 {
					test.Errorf("split-index refusal followed hashing or Git index reads: indexHash=%q adminHash=%q known=%t commands=%d error=%v", actual.IndexHash, actual.AdminHash, actual.GitStateKnown, indexCommands, err)
				}
			})
		}
	}
}
