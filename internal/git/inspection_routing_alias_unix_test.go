//go:build darwin || linux

package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestClientInspectAllowsAdministrativeDirectoryAliases(test *testing.T) {
	for _, scenario := range []string{"ordinary", "terminal", "intermediate", "absolute-traversal", "relative-traversal"} {
		test.Run(scenario, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			worktree := repository.AddWorktree(test, "alias-target", "topic")
			client := NewClient(nil)
			record := inspectionIdentityRecord(test, client, repository.Root, worktree)
			known, err := client.InspectWorktree(test.Context(), repository.Root, record)
			if err != nil {
				test.Fatal(err)
			}
			pointer := known.AdminDir
			alias := filepath.Join(worktree, "owned-alias")
			switch scenario {
			case "terminal":
				if err := os.Symlink(known.AdminDir, alias); err != nil {
					test.Fatal(err)
				}
				pointer = alias
			case "intermediate":
				if err := os.Symlink(filepath.Dir(known.AdminDir), alias); err != nil {
					test.Fatal(err)
				}
				pointer = alias + "/" + filepath.Base(known.AdminDir)
			case "absolute-traversal", "relative-traversal":
				pivot := filepath.Join(known.AdminDir, "owned-pivot")
				if err := os.Mkdir(pivot, 0o700); err != nil {
					test.Fatal(err)
				}
				if err := os.Symlink(pivot, alias); err != nil {
					test.Fatal(err)
				}
				pointer = alias + "/.."
				if scenario == "relative-traversal" {
					pointer = "owned-alias/.."
				}
			}
			if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+pointer+"\n"), 0o600); err != nil {
				test.Fatal(err)
			}
			physical, err := client.gitDirectory(test.Context(), worktree, "--absolute-git-dir")
			if err != nil || physical != known.AdminDir {
				test.Fatalf("native Git alias control: actual=%q expected=%q error=%v", physical, known.AdminDir, err)
			}
			before := readonlyIndexEvidence(test, known.AdminDir)
			defer func() { assertReadonlyIndexEvidence(test, before, readonlyIndexEvidence(test, known.AdminDir)) }()
			actual, err := client.InspectWorktree(test.Context(), repository.Root, record)
			if err != nil || !actual.GitStateKnown || !actual.PathSafe || actual.AdminDir != physical {
				test.Fatalf("native-equivalent administrative target refused: known=%t safe=%t error=%v", actual.GitStateKnown, actual.PathSafe, err)
			}
		})
	}
}

func TestClientInspectRejectsMidReadBacklinkTraversal(test *testing.T) {
	for _, relative := range []bool{false, true} {
		repository := testutil.NewRepository(test)
		target := repository.AddWorktree(test, "midread-target", "topic/target")
		other := repository.AddWorktree(test, "midread-other", "topic/other")
		client := NewClient(nil)
		record := inspectionIdentityRecord(test, client, repository.Root, target)
		known, err := client.InspectWorktree(test.Context(), repository.Root, record)
		if err != nil {
			test.Fatal(err)
		}
		pivot := filepath.Join(other, "owned-pivot")
		if err := os.Mkdir(pivot, 0o700); err != nil {
			test.Fatal(err)
		}
		if err := os.Symlink(pivot, filepath.Join(target, "owned-hop")); err != nil {
			test.Fatal(err)
		}
		backlink := target + "/owned-hop/../.git"
		if relative {
			base, err := filepath.Rel(known.AdminDir, target)
			if err != nil {
				test.Fatal(err)
			}
			backlink = base + "/owned-hop/../.git"
		}
		absolute := backlink
		if relative {
			absolute = known.AdminDir + "/" + backlink
		}
		physical, err := filepath.EvalSymlinks(absolute)
		if err != nil || physical != filepath.Join(other, ".git") {
			test.Fatalf("native backlink traversal oracle: path=%q error=%v", physical, err)
		}
		changed := false
		client = NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
			result, err := (execx.OSRunner{}).Run(ctx, request)
			if err == nil && request.Args[0] == "status" && !changed {
				if err := os.WriteFile(filepath.Join(known.AdminDir, "gitdir"), []byte(backlink+"\n"), 0o600); err != nil {
					test.Fatal(err)
				}
				changed = true
			}
			return result, err
		}))
		actual, err := client.InspectWorktree(test.Context(), repository.Root, record)
		if !changed || !errors.Is(err, ErrWorktreeChanged) || actual.GitStateKnown || actual.PathSafe {
			test.Errorf("relative=%t: persistent backlink traversal accepted: changed=%t known=%t safe=%t error=%v", relative, changed, actual.GitStateKnown, actual.PathSafe, err)
		}
	}
}

func TestClientInspectDoesNotResolveWrongBacklinkMarker(test *testing.T) {
	repository := testutil.NewRepository(test)
	worktree := repository.AddWorktree(test, "marker-target", "topic")
	client := NewClient(nil)
	record := inspectionIdentityRecord(test, client, repository.Root, worktree)
	known, err := client.InspectWorktree(test.Context(), repository.Root, record)
	if err != nil {
		test.Fatal(err)
	}
	alias := filepath.Join(worktree, "not-a-git-marker")
	if err := os.Symlink(filepath.Join(worktree, ".git"), alias); err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(known.AdminDir, "gitdir"), []byte(alias+"\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	actual, err := client.InspectWorktree(test.Context(), repository.Root, record)
	if !errors.Is(err, ErrWorktreeChanged) || actual.GitStateKnown || actual.PathSafe || actual.IndexHash != "" || actual.AdminHash != "" {
		test.Fatalf("alias file cannot substitute Git's backlink marker syntax: %v", err)
	}
}

func TestClientInspectAllowsCanonicalCommonDirectoryAliases(test *testing.T) {
	for _, terminal := range []bool{false, true} {
		repository := testutil.NewRepository(test)
		worktree := repository.AddWorktree(test, "common-alias-target", "topic")
		client := NewClient(nil)
		record := inspectionIdentityRecord(test, client, repository.Root, worktree)
		known, err := client.InspectWorktree(test.Context(), repository.Root, record)
		if err != nil {
			test.Fatal(err)
		}
		alias := filepath.Join(filepath.Dir(repository.Root), "owned-common-alias")
		target := filepath.Dir(known.CommonGitDir)
		if terminal {
			target = known.CommonGitDir
		}
		if err := os.Symlink(target, alias); err != nil {
			test.Fatal(err)
		}
		pointer := alias
		if !terminal {
			pointer += "/" + filepath.Base(known.CommonGitDir)
		}
		if err := os.WriteFile(filepath.Join(known.AdminDir, "commondir"), []byte(pointer+"\n"), 0o600); err != nil {
			test.Fatal(err)
		}
		effective, err := client.CommonGitDir(test.Context(), worktree)
		if err != nil || effective != known.CommonGitDir {
			test.Fatalf("native Git must retain the original canonical common store: %q, %v", effective, err)
		}
		before := readonlyIndexEvidence(test, known.AdminDir)
		actual, err := client.InspectWorktree(test.Context(), repository.Root, record)
		assertReadonlyIndexEvidence(test, before, readonlyIndexEvidence(test, known.AdminDir))
		if err != nil || !actual.GitStateKnown || !actual.PathSafe || actual.CommonGitDir != known.CommonGitDir || actual.AdminDir != known.AdminDir {
			test.Fatalf("terminal=%t: supported canonical common-store alias rejected: %v", terminal, err)
		}
	}
}

func TestClientInspectRechecksCanonicalCommonBeforeHashes(test *testing.T) {
	repository := testutil.NewRepository(test)
	worktree := repository.AddWorktree(test, "common-recheck-target", "topic")
	record := inspectionIdentityRecord(test, NewClient(nil), repository.Root, worktree)
	known, err := NewClient(nil).InspectWorktree(test.Context(), repository.Root, record)
	if err != nil {
		test.Fatal(err)
	}
	before := readonlyIndexEvidence(test, known.AdminDir)
	defer func() { assertReadonlyIndexEvidence(test, before, readonlyIndexEvidence(test, known.AdminDir)) }()
	changed := false
	indexCommands := 0
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		if slices.Contains([]string{"ls-files", "status", "diff"}, request.Args[0]) {
			indexCommands++
		}
		result, err := (execx.OSRunner{}).Run(ctx, request)
		if err == nil && slices.Contains(request.Args, "--absolute-git-dir") && !changed {
			moved := filepath.Join(filepath.Dir(repository.Root), "owned-moved-common")
			if err := os.Rename(known.CommonGitDir, moved); err != nil {
				test.Fatal(err)
			}
			if err := os.Symlink(moved, known.CommonGitDir); err != nil {
				test.Fatal(err)
			}
			changed = true
		}
		return result, err
	}))
	actual, err := client.InspectWorktree(test.Context(), repository.Root, record)
	if !changed || err == nil || actual.GitStateKnown || actual.PathSafe || actual.IndexHash != "" || actual.AdminHash != "" || indexCommands != 0 {
		test.Fatalf("an earlier common comparison authorized an unsafe later root: changed=%t known=%t safe=%t indexReads=%d error=%v", changed, actual.GitStateKnown, actual.PathSafe, indexCommands, err)
	}
}
