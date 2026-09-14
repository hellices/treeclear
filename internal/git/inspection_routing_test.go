package git

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestClientInspectRequiresAdministrativeBacklink(test *testing.T) {
	for _, scenario := range []string{"missing", "empty", "directory", "oversized", "wrong-marker", "outside-registration"} {
		test.Run(scenario, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			worktree := repository.AddWorktree(test, "backlink-target", "topic")
			client := NewClient(nil)
			record := inspectionIdentityRecord(test, client, repository.Root, worktree)
			known, err := client.InspectWorktree(test.Context(), repository.Root, record)
			if err != nil || !known.GitStateKnown {
				test.Fatalf("ordinary registration control: %v", err)
			}
			backlink := filepath.Join(known.AdminDir, "gitdir")
			var wantCause error
			switch scenario {
			case "missing", "directory":
				if err := os.Remove(backlink); err != nil {
					test.Fatal(err)
				}
				if scenario == "directory" {
					if err := os.Mkdir(backlink, 0o700); err != nil {
						test.Fatal(err)
					}
				} else {
					wantCause = fs.ErrNotExist
				}
			case "empty":
				if err := os.WriteFile(backlink, nil, 0o600); err != nil {
					test.Fatal(err)
				}
			case "oversized":
				if err := os.WriteFile(backlink, []byte(strings.Repeat("x", (32<<10)+1)), 0o600); err != nil {
					test.Fatal(err)
				}
				wantCause = ErrReadLimit
			case "wrong-marker":
				marker := filepath.Join(worktree, "not-dotgit")
				if err := os.WriteFile(marker, []byte("owned unrelated marker"), 0o600); err != nil {
					test.Fatal(err)
				}
				if err := os.WriteFile(backlink, []byte(filepath.ToSlash(marker)+"\n"), 0o600); err != nil {
					test.Fatal(err)
				}
			case "outside-registration":
				copied := filepath.Join(known.CommonGitDir, "unregistered-admin")
				if err := os.CopyFS(copied, os.DirFS(known.AdminDir)); err != nil {
					test.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(copied, "commondir"), []byte(filepath.ToSlash(known.CommonGitDir)+"\n"), 0o600); err != nil {
					test.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+filepath.ToSlash(copied)+"\n"), 0o600); err != nil {
					test.Fatal(err)
				}
				wantCause = ErrWorktreeChanged
			}
			before := readonlyIndexEvidence(test, known.AdminDir)
			defer func() { assertReadonlyIndexEvidence(test, before, readonlyIndexEvidence(test, known.AdminDir)) }()
			indexCommands := 0
			client = NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
				if slices.Contains([]string{"ls-files", "status", "diff"}, request.Args[0]) {
					indexCommands++
				}
				return (execx.OSRunner{}).Run(ctx, request)
			}))
			actual, err := client.InspectWorktree(test.Context(), repository.Root, record)
			if err == nil || wantCause != nil && !errors.Is(err, wantCause) || actual.GitStateKnown || actual.PathSafe || actual.IndexHash != "" || actual.AdminHash != "" || indexCommands != 0 {
				test.Fatalf("invalid administrative routing reached inspection: known=%t safe=%t indexReads=%d indexHash=%q adminHash=%q error=%v", actual.GitStateKnown, actual.PathSafe, indexCommands, actual.IndexHash, actual.AdminHash, err)
			}
		})
	}
}

func TestClientInspectAllowsRelativeAdministrativePointers(test *testing.T) {
	for _, scenario := range []string{"forward", "backward", "both"} {
		test.Run(scenario, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			worktree := repository.AddWorktree(test, "relative pointer target", "topic")
			client := NewClient(nil)
			record := inspectionIdentityRecord(test, client, repository.Root, worktree)
			known, err := client.InspectWorktree(test.Context(), repository.Root, record)
			if err != nil {
				test.Fatal(err)
			}
			if scenario != "backward" {
				relative, err := filepath.Rel(worktree, known.AdminDir)
				if err != nil {
					test.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+filepath.ToSlash(relative)+"\n"), 0o600); err != nil {
					test.Fatal(err)
				}
			}
			if scenario != "forward" {
				relative, err := filepath.Rel(known.AdminDir, filepath.Join(worktree, ".git"))
				if err != nil {
					test.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(known.AdminDir, "gitdir"), []byte(filepath.ToSlash(relative)+"\n"), 0o600); err != nil {
					test.Fatal(err)
				}
			}
			before := readonlyIndexEvidence(test, known.AdminDir)
			defer func() { assertReadonlyIndexEvidence(test, before, readonlyIndexEvidence(test, known.AdminDir)) }()
			actual, err := client.InspectWorktree(test.Context(), repository.Root, record)
			if err != nil || !actual.GitStateKnown || !actual.PathSafe || actual.AdminDir != known.AdminDir || actual.Head != known.Head {
				test.Fatalf("legitimate relative pointer layout rejected: %v", err)
			}
		})
	}
}
