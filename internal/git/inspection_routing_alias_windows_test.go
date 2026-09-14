package git

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/testutil"
	"golang.org/x/sys/windows"
)

func TestClientInspectAllowsWindowsAdministrativeDirectoryAliases(test *testing.T) {
	for _, scenario := range []string{"ordinary", "terminal junction", "intermediate junction"} {
		test.Run(scenario, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			worktree := repository.AddWorktree(test, "alias target", "topic")
			client := NewClient(nil)
			record := inspectionIdentityRecord(test, client, repository.Root, worktree)
			ctx, cancel := context.WithTimeout(test.Context(), 30*time.Second)
			defer cancel()
			known, err := client.InspectWorktree(ctx, repository.Root, record)
			if err != nil || !known.GitStateKnown || !known.PathSafe || known.IndexHash == "" || known.AdminHash == "" {
				test.Fatalf("ordinary registration precondition: %#v, %v", known, err)
			}
			before := readonlyIndexEvidence(test, known.AdminDir)
			defer func() { assertReadonlyIndexEvidence(test, before, readonlyIndexEvidence(test, known.AdminDir)) }()
			administrative := inspectionRoutingWindowsDirectoryIdentity(test, known.AdminDir)
			common := inspectionRoutingWindowsDirectoryIdentity(test, known.CommonGitDir)
			ownedRoot := filepath.Dir(repository.Root)
			pointer := known.AdminDir
			switch scenario {
			case "terminal junction":
				pointer = filepath.Join(ownedRoot, "terminal alias")
				inspectionRoutingWindowsJunction(test, ownedRoot, pointer, known.AdminDir)
			case "intermediate junction":
				junction := filepath.Join(ownedRoot, "intermediate alias")
				inspectionRoutingWindowsJunction(test, ownedRoot, junction, filepath.Dir(known.AdminDir))
				pointer = filepath.Join(junction, filepath.Base(known.AdminDir))
			}
			canonicalPointer, err := filepath.EvalSymlinks(pointer)
			if err != nil || !strings.EqualFold(canonicalPointer, known.AdminDir) || !os.SameFile(administrative, inspectionRoutingWindowsDirectoryIdentity(test, pointer)) {
				test.Fatalf("directory alias does not identify the original administrative directory: pointer=%q resolved=%q expected=%q error=%v", pointer, canonicalPointer, known.AdminDir, err)
			}
			marker := filepath.Join(worktree, ".git")
			if err := os.WriteFile(marker, []byte("gitdir: "+filepath.ToSlash(pointer)+"\n"), 0o600); err != nil {
				test.Fatal(err)
			}
			markerInfo, err := os.Lstat(marker)
			if err != nil || !markerInfo.Mode().IsRegular() {
				test.Fatalf("fixture Git pointer leaf is not an ordinary file: %v", err)
			}
			for _, target := range []struct {
				option      string
				path        string
				information fs.FileInfo
			}{
				{option: "--absolute-git-dir", path: known.AdminDir, information: administrative},
				{option: "--git-common-dir", path: known.CommonGitDir, information: common},
			} {
				result, err := client.run(ctx, worktree, "rev-parse", "--path-format=absolute", target.option)
				if err != nil {
					test.Fatalf("native Git directory alias precondition %s: %v", target.option, err)
				}
				observed := filepath.FromSlash(strings.TrimRight(string(result.Stdout), "\r\n"))
				if !filepath.IsAbs(observed) {
					test.Fatalf("native Git returned a non-absolute %s: %q", target.option, observed)
				}
				canonical, err := filepath.EvalSymlinks(observed)
				if err != nil || !strings.EqualFold(canonical, target.path) || !os.SameFile(target.information, inspectionRoutingWindowsDirectoryIdentity(test, observed)) {
					test.Fatalf("native Git selects a different %s: observed=%q canonical=%q expected=%q error=%v", target.option, observed, canonical, target.path, err)
				}
			}
			actual, err := client.InspectWorktree(ctx, repository.Root, record)
			if err != nil || !actual.GitStateKnown || !actual.PathSafe || len(actual.CollectionErrors) != 0 || actual.AdminDir != known.AdminDir || actual.CommonGitDir != known.CommonGitDir || actual.Head != known.Head || actual.Branch != known.Branch || actual.IndexHash != known.IndexHash || actual.AdminHash == "" {
				test.Fatalf("legitimate same-admin directory alias rejected or changed inspection: %#v, %v", actual, err)
			}
		})
	}
}

func TestClientInspectAllowsWindowsCanonicalCommonDirectoryAliases(test *testing.T) {
	for _, scenario := range []string{"terminal junction", "intermediate junction"} {
		test.Run(scenario, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			worktree := repository.AddWorktree(test, "common alias target", "topic")
			client := NewClient(nil)
			record := inspectionIdentityRecord(test, client, repository.Root, worktree)
			known, err := client.InspectWorktree(test.Context(), repository.Root, record)
			if err != nil || !known.GitStateKnown || !known.PathSafe {
				test.Fatalf("ordinary common-store precondition: %#v, %v", known, err)
			}
			common := inspectionRoutingWindowsDirectoryIdentity(test, known.CommonGitDir)
			ownedRoot := filepath.Dir(repository.Root)
			junction := filepath.Join(ownedRoot, "owned common alias")
			target := known.CommonGitDir
			if scenario == "intermediate junction" {
				target = filepath.Dir(known.CommonGitDir)
			}
			inspectionRoutingWindowsJunction(test, ownedRoot, junction, target)
			pointer := junction
			if scenario == "intermediate junction" {
				pointer = filepath.Join(pointer, filepath.Base(known.CommonGitDir))
			}
			if !os.SameFile(common, inspectionRoutingWindowsDirectoryIdentity(test, pointer)) {
				test.Fatal("fixture alias identifies a different common store")
			}
			if err := os.WriteFile(filepath.Join(known.AdminDir, "commondir"), []byte(filepath.ToSlash(pointer)+"\n"), 0o600); err != nil {
				test.Fatal(err)
			}
			effective, err := client.CommonGitDir(test.Context(), worktree)
			if err != nil || !strings.EqualFold(effective, known.CommonGitDir) || !os.SameFile(common, inspectionRoutingWindowsDirectoryIdentity(test, effective)) {
				test.Fatalf("native Git must retain the original canonical common store: %q, %v", effective, err)
			}
			before := readonlyIndexEvidence(test, known.AdminDir)
			defer func() { assertReadonlyIndexEvidence(test, before, readonlyIndexEvidence(test, known.AdminDir)) }()
			actual, err := client.InspectWorktree(test.Context(), repository.Root, record)
			if err != nil || !actual.GitStateKnown || !actual.PathSafe || len(actual.CollectionErrors) != 0 || actual.CommonGitDir != known.CommonGitDir || actual.AdminDir != known.AdminDir || actual.IndexHash != known.IndexHash {
				test.Fatalf("supported canonical common-store alias rejected: %#v, %v", actual, err)
			}
		})
	}
}

func TestClientInspectRechecksWindowsCanonicalCommonBeforeHashes(test *testing.T) {
	repository := testutil.NewRepository(test)
	worktree := repository.AddWorktree(test, "common recheck target", "topic")
	record := inspectionIdentityRecord(test, NewClient(nil), repository.Root, worktree)
	known, err := NewClient(nil).InspectWorktree(test.Context(), repository.Root, record)
	if err != nil || !known.GitStateKnown || !known.PathSafe {
		test.Fatalf("ordinary common-store precondition: %#v, %v", known, err)
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
			moved := filepath.Join(repository.Root, "owned moved common")
			if err := os.Rename(known.CommonGitDir, moved); err != nil {
				test.Fatal(err)
			}
			inspectionRoutingWindowsJunction(test, repository.Root, known.CommonGitDir, moved)
			changed = true
		}
		return result, err
	}))
	actual, err := client.InspectWorktree(test.Context(), repository.Root, record)
	if !changed || err == nil || actual.GitStateKnown || actual.PathSafe || actual.IndexHash != "" || actual.AdminHash != "" || indexCommands != 0 {
		test.Fatalf("an earlier common comparison authorized an unsafe later root: changed=%t known=%t safe=%t indexReads=%d error=%v", changed, actual.GitStateKnown, actual.PathSafe, indexCommands, err)
	}
}

func inspectionRoutingWindowsDirectoryIdentity(test *testing.T, directory string) fs.FileInfo {
	test.Helper()
	file, err := os.Open(directory)
	if err != nil {
		test.Fatal(err)
	}
	information, statErr := file.Stat()
	if err := errors.Join(statErr, file.Close()); err != nil {
		test.Fatal(err)
	}
	if information.Mode().Type() != fs.ModeDir {
		test.Fatalf("native identity fixture is not a directory: %q", directory)
	}
	return information
}

func inspectionRoutingWindowsJunction(test *testing.T, ownedRoot, junction, target string) {
	test.Helper()
	canonicalTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		test.Fatal(err)
	}
	for _, path := range []string{junction, target, canonicalTarget} {
		relative, err := filepath.Rel(ownedRoot, path)
		if err != nil || !filepath.IsAbs(path) || !filepath.IsLocal(relative) || relative == "." || strings.ContainsAny(path, "\x00\r\n\"%!&|<>^()") {
			test.Fatalf("junction command path is not a shell-safe owned fixture descendant: %q, %v", path, err)
		}
	}
	if filepath.Dir(junction) != ownedRoot {
		test.Fatalf("junction parent is not the owned fixture root: %q", junction)
	}
	if _, err := os.Lstat(junction); !errors.Is(err, fs.ErrNotExist) {
		test.Fatalf("junction fixture already exists or is inaccessible: %q, %v", junction, err)
	}
	result, err := (execx.OSRunner{}).Run(test.Context(), execx.Request{
		Directory: ownedRoot,
		Name:      "cmd.exe",
		Args:      []string{"/d", "/v:off", "/c", "mklink", "/J", junction, canonicalTarget},
		Env:       execx.SanitizedEnvironment(os.Environ(), nil),
		Timeout:   10 * time.Second,
		MaxBytes:  16 << 10,
	})
	if err != nil || result.ExitCode != 0 {
		test.Fatalf("create fixture-local directory junction: %v, exit %d, stdout %q, stderr %q", err, result.ExitCode, result.Stdout, result.Stderr)
	}
	test.Cleanup(func() {
		if err := os.Remove(junction); err != nil {
			test.Errorf("remove owned junction: %v", err)
		}
	})
	information, err := os.Lstat(junction)
	if err != nil {
		test.Fatal(err)
	}
	attributes, ok := information.Sys().(*syscall.Win32FileAttributeData)
	if !ok || attributes == nil || attributes.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		test.Fatalf("mklink did not create a native reparse-point fixture: %q", junction)
	}
}
