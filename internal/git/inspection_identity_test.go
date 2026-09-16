package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestClientInspectRejectsEffectiveCommonConflict(test *testing.T) {
	repository := testutil.NewRepository(test)
	worktree := repository.AddWorktree(test, "common-conflict", "topic")
	indexCommands := 0
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		if slices.Contains([]string{"ls-files", "status", "diff"}, request.Args[0]) {
			indexCommands++
		}
		return (execx.OSRunner{}).Run(ctx, request)
	}))
	record := inspectionIdentityRecord(test, client, repository.Root, worktree)
	known, err := client.InspectWorktree(test.Context(), repository.Root, record)
	if err != nil || !known.GitStateKnown {
		test.Fatalf("matching common-directory control: %v", err)
	}
	alternate := filepath.Join(test.TempDir(), "other-common")
	if err := os.CopyFS(alternate, os.DirFS(known.CommonGitDir)); err != nil {
		test.Fatal(err)
	}
	alternate, err = filepath.EvalSymlinks(alternate)
	if err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(known.AdminDir, "commondir"), []byte(alternate+"\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	effective, err := client.CommonGitDir(test.Context(), worktree)
	if err != nil || effective != alternate || effective == known.CommonGitDir {
		test.Fatalf("effective common-directory fixture: %q, %v", effective, err)
	}
	originalInfo, err := os.Stat(known.CommonGitDir)
	if err != nil {
		test.Fatal(err)
	}
	alternateInfo, err := os.Stat(alternate)
	if err != nil || os.SameFile(originalInfo, alternateInfo) {
		test.Fatalf("common-directory fixture requires distinct native identities: %v", err)
	}
	beforeHash, beforeTime, err := hashAdmin(test.Context(), known.AdminDir, false)
	if err != nil {
		test.Fatal(err)
	}
	indexCommands = 0
	actual, inspectErr := client.InspectWorktree(test.Context(), repository.Root, record)
	if !errors.Is(inspectErr, ErrWorktreeChanged) || actual.GitStateKnown || actual.PathSafe || indexCommands != 0 {
		test.Errorf("conflicting common directory accepted or reached index reads: known=%t safe=%t reads=%d error=%v", actual.GitStateKnown, actual.PathSafe, indexCommands, inspectErr)
	}
	afterHash, afterTime, err := hashAdmin(test.Context(), known.AdminDir, false)
	if err != nil || beforeHash != afterHash || !beforeTime.Equal(afterTime) {
		test.Fatalf("common-directory rejection modified metadata: %v", err)
	}
}

func TestClientInspectRejectsSameStoreAdministrativeMisrouting(test *testing.T) {
	repository := testutil.NewRepository(test)
	target := repository.AddWorktree(test, "registered-target", "topic/target")
	other := repository.AddWorktree(test, "registered-other", "topic/other")
	for _, worktree := range []string{target, other} {
		repository.Git(test, "-C", worktree, "checkout", "--detach", "HEAD")
	}
	client := NewClient(nil)
	record := inspectionIdentityRecord(test, client, repository.Root, target)
	control, err := client.InspectWorktree(test.Context(), repository.Root, record)
	if err != nil || !control.GitStateKnown {
		test.Fatalf("legitimate detached registration: %v", err)
	}
	otherAdmin := repository.Git(test, "-C", other, "rev-parse", "--absolute-git-dir")
	if err := os.WriteFile(filepath.Join(target, ".git"), []byte("gitdir: "+otherAdmin+"\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	repository.Git(test, "-C", target, "update-index", "--refresh")
	before := readonlyIndexEvidence(test, otherAdmin)
	defer func() { assertReadonlyIndexEvidence(test, before, readonlyIndexEvidence(test, otherAdmin)) }()
	indexCommands := 0
	client = NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		if slices.Contains([]string{"ls-files", "status", "diff"}, request.Args[0]) {
			indexCommands++
		}
		return (execx.OSRunner{}).Run(ctx, request)
	}))
	record = inspectionIdentityRecord(test, client, repository.Root, target)
	actual, err := client.InspectWorktree(test.Context(), repository.Root, record)
	if !errors.Is(err, ErrWorktreeChanged) || actual.GitStateKnown || actual.PathSafe || actual.IndexHash != "" || actual.AdminHash != "" || indexCommands != 0 {
		test.Fatalf("same-store administrative conflict was trusted or read: known=%t safe=%t indexReads=%d admin=%q error=%v", actual.GitStateKnown, actual.PathSafe, indexCommands, actual.AdminDir, err)
	}
}

func TestClientInspectRechecksAdministrativeRoutingAfterReads(test *testing.T) {
	repository := testutil.NewRepository(test)
	target := repository.AddWorktree(test, "routing-change-target", "topic/target")
	other := repository.AddWorktree(test, "routing-change-other", "topic/other")
	for _, worktree := range []string{target, other} {
		repository.Git(test, "-C", worktree, "checkout", "--detach", "HEAD")
	}
	record := inspectionIdentityRecord(test, NewClient(nil), repository.Root, target)
	otherAdmin := repository.Git(test, "-C", other, "rev-parse", "--absolute-git-dir")
	changed := false
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		result, err := (execx.OSRunner{}).Run(ctx, request)
		if err == nil && request.Args[0] == "status" {
			if err := os.WriteFile(filepath.Join(target, ".git"), []byte("gitdir: "+filepath.ToSlash(otherAdmin)+"\n"), 0o600); err != nil {
				test.Fatal(err)
			}
			changed = true
		}
		return result, err
	}))
	actual, err := client.InspectWorktree(test.Context(), repository.Root, record)
	if !changed || !errors.Is(err, ErrWorktreeChanged) || actual.GitStateKnown || actual.PathSafe {
		test.Fatalf("persistent mid-inspection routing change was trusted: changed=%t known=%t safe=%t error=%v", changed, actual.GitStateKnown, actual.PathSafe, err)
	}
}

func TestClientInspectClassifiesKnownStateChanges(test *testing.T) {
	for _, scenario := range []string{"head", "branch", "detach", "attach"} {
		test.Run(scenario, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			worktree := repository.AddWorktree(test, "identity-change", "topic")
			if scenario == "attach" {
				repository.Git(test, "-C", worktree, "checkout", "--detach")
			}
			client := NewClient(nil)
			record := inspectionIdentityRecord(test, client, repository.Root, worktree)
			switch scenario {
			case "head":
				repository.Git(test, "-C", worktree, "commit", "--allow-empty", "-m", "Changed fixture HEAD")
			case "branch":
				repository.Git(test, "-C", worktree, "symbolic-ref", "HEAD", "refs/heads/main")
			case "detach":
				repository.Git(test, "-C", worktree, "checkout", "--detach")
			case "attach":
				repository.Git(test, "-C", worktree, "symbolic-ref", "HEAD", "refs/heads/topic")
			}
			actual, err := client.InspectWorktree(test.Context(), repository.Root, record)
			if !errors.Is(err, ErrWorktreeChanged) || actual.GitStateKnown || len(actual.CollectionErrors) == 0 {
				test.Fatalf("known %s change lost typed cause: known=%t error=%v", scenario, actual.GitStateKnown, err)
			}
		})
	}
}

func TestClientInspectAllowsCommonDirectoryCaseAlias(test *testing.T) {
	repository := testutil.NewRepository(test)
	worktree := repository.AddWorktree(test, "common-case-alias", "topic")
	client := NewClient(nil)
	record := inspectionIdentityRecord(test, client, repository.Root, worktree)
	known, err := client.InspectWorktree(test.Context(), repository.Root, record)
	if err != nil {
		test.Fatal(err)
	}
	alias := filepath.Join(filepath.Dir(known.CommonGitDir), strings.ToUpper(filepath.Base(known.CommonGitDir)))
	aliasInfo, err := os.Stat(alias)
	if errors.Is(err, os.ErrNotExist) {
		test.Skip("filesystem has case-sensitive common-directory names")
	}
	if err != nil {
		test.Fatal(err)
	}
	originalInfo, err := os.Stat(known.CommonGitDir)
	if err != nil || !os.SameFile(originalInfo, aliasInfo) {
		test.Fatalf("common-directory case alias is not the same native object: %v", err)
	}
	if err := os.WriteFile(filepath.Join(known.AdminDir, "commondir"), []byte(alias+"\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	actual, err := client.InspectWorktree(test.Context(), repository.Root, record)
	if err != nil || !actual.GitStateKnown || !actual.PathSafe {
		test.Fatalf("same native common directory rejected through a case alias: %v", err)
	}
}

func TestClientInspectDoesNotReclassifyReadErrors(test *testing.T) {
	repository := testutil.NewRepository(test)
	worktree := repository.AddWorktree(test, "identity-error", "topic")
	record := inspectionIdentityRecord(test, NewClient(nil), repository.Root, worktree)
	failure := errors.New("HEAD changed during collection")
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		if request.Args[0] == "rev-parse" && slices.Contains(request.Args, "--verify") {
			return execx.Result{}, failure
		}
		return (execx.OSRunner{}).Run(ctx, request)
	}))
	actual, err := client.InspectWorktree(test.Context(), repository.Root, record)
	if !errors.Is(err, failure) || errors.Is(err, ErrWorktreeChanged) || actual.GitStateKnown {
		test.Fatalf("opaque read error was lost or reclassified by text: %v", err)
	}
}

func inspectionIdentityRecord(test *testing.T, client *Client, repository, path string) domain.Worktree {
	test.Helper()
	listed, err := client.ListWorktrees(test.Context(), repository)
	if err != nil {
		test.Fatal(err)
	}
	for _, record := range listed {
		if record.Path == path {
			record.PathSafe = true
			return record
		}
	}
	test.Fatal("owned linked fixture was not listed")
	return domain.Worktree{}
}
