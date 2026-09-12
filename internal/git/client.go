package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
)

const maxGitBytes = 16 << 20

type Client struct {
	Runner       execx.Runner
	BaseBranches []string
}

func NewClient(runner execx.Runner) *Client {
	if runner == nil {
		runner = execx.OSRunner{}
	}
	return &Client{Runner: runner, BaseBranches: []string{"main", "master"}}
}

func (client *Client) run(ctx context.Context, directory string, arguments ...string) (execx.Result, error) {
	environment := execx.SanitizedEnvironment(os.Environ(), map[string]string{
		"LC_ALL": "C", "LANG": "C", "GIT_OPTIONAL_LOCKS": "0",
		"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.DevNull,
		"GIT_ATTR_NOSYSTEM":   "1",
		"GIT_TERMINAL_PROMPT": "0", "GCM_INTERACTIVE": "never", "GIT_NO_LAZY_FETCH": "1",
		"GIT_CONFIG_COUNT": "4",
		"GIT_CONFIG_KEY_0": "core.fsmonitor", "GIT_CONFIG_VALUE_0": "false",
		"GIT_CONFIG_KEY_1": "protocol.allow", "GIT_CONFIG_VALUE_1": "never",
		"GIT_CONFIG_KEY_2": "log.showSignature", "GIT_CONFIG_VALUE_2": "false",
		"GIT_CONFIG_KEY_3": "core.attributesFile", "GIT_CONFIG_VALUE_3": os.DevNull,
	})
	result, err := client.Runner.Run(ctx, execx.Request{
		Directory: directory, Name: "git", Args: arguments, Env: environment,
		Timeout: 30 * time.Second, MaxBytes: maxGitBytes,
	})
	if err == nil && result.ExitCode != 0 {
		err = fmt.Errorf("exit status %d", result.ExitCode)
	}
	if err != nil {
		diagnostic := result.Stderr
		if len(diagnostic) > 4096 {
			diagnostic = diagnostic[:4096]
		}
		return result, fmt.Errorf("git %q in %q: %w: %s", arguments, directory, err, diagnostic)
	}
	return result, nil
}

func (client *Client) CheckVersion(ctx context.Context) error {
	result, err := client.run(ctx, "", "--version")
	if err != nil {
		return err
	}
	fields := strings.Fields(string(result.Stdout))
	if len(fields) < 3 || fields[0] != "git" || fields[1] != "version" {
		return errors.New("unrecognized Git version output")
	}
	components := strings.Split(fields[2], ".")
	if len(components) < 2 {
		return errors.New("unrecognized Git version number")
	}
	major, majorErr := strconv.Atoi(components[0])
	minor, minorErr := strconv.Atoi(components[1])
	if majorErr != nil || minorErr != nil || major < 2 || (major == 2 && minor < 36) {
		return errors.New("Treeclear requires Git 2.36 or newer")
	}
	return nil
}

func (client *Client) CommonGitDir(ctx context.Context, repository string) (string, error) {
	return client.gitDirectory(ctx, repository, "--path-format=absolute", "--git-common-dir")
}

func (client *Client) gitDirectory(ctx context.Context, repository string, arguments ...string) (string, error) {
	result, err := client.run(ctx, repository, append([]string{"rev-parse"}, arguments...)...)
	if err != nil {
		return "", err
	}
	path := outputLine(result.Stdout)
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("Git returned a non-absolute metadata path %q", path)
	}
	return filepath.EvalSymlinks(path)
}

func (client *Client) ListWorktrees(ctx context.Context, repository string) ([]domain.Worktree, error) {
	result, err := client.run(ctx, repository, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	records, err := parseWorktreePorcelainZ(result.Stdout)
	if err != nil {
		return nil, err
	}
	if records[0].Bare {
		return nil, errors.New("bare repositories are not supported as discovery anchors")
	}
	commonDirectory, err := client.CommonGitDir(ctx, repository)
	if err != nil {
		return nil, err
	}
	worktrees := make([]domain.Worktree, 0, len(records))
	for index, record := range records {
		worktrees = append(worktrees, domain.Worktree{
			Path: record.Path, RepositoryRoot: records[0].Path, CommonGitDir: commonDirectory,
			Head: record.Head, Branch: record.Branch, Primary: index == 0,
			Detached: record.Detached, Locked: record.Locked, LockReason: record.LockReason,
			Prunable: record.Prunable,
		})
	}
	return worktrees, nil
}

func (client *Client) rejectExecutableFilters(ctx context.Context, directory string) error {
	result, err := client.run(ctx, directory, "config", "--null", "--get-regexp", `^filter\..*\.(clean|smudge|process)$`)
	if err != nil && result.ExitCode == 1 && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, execx.ErrOutputLimit) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(result.Stdout) != 0 {
		return errors.New("executable Git filters prevent read-only status collection")
	}
	return nil
}

func (client *Client) Status(ctx context.Context, worktree string) (domain.GitStatus, error) {
	if err := client.rejectExecutableFilters(ctx, worktree); err != nil {
		return domain.GitStatus{}, err
	}
	result, err := client.run(ctx, worktree, "status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return domain.GitStatus{}, err
	}
	return parseStatusPorcelainZ(result.Stdout)
}

func (client *Client) Diff(ctx context.Context, worktree string, staged bool) ([]byte, error) {
	if err := client.rejectExecutableFilters(ctx, worktree); err != nil {
		return nil, err
	}
	arguments := []string{"diff", "--binary", "--full-index", "--no-color", "--no-ext-diff", "--no-textconv"}
	if staged {
		arguments = append(arguments, "--cached")
	}
	result, err := client.run(ctx, worktree, append(arguments, "--")...)
	return result.Stdout, err
}

func (client *Client) RemoveWorktree(ctx context.Context, repository, path string) error {
	_, err := client.run(ctx, repository, "worktree", "remove", "--", path)
	return err
}

func (client *Client) InspectWorktree(ctx context.Context, repository string, worktree domain.Worktree) (domain.Worktree, error) {
	worktree.GitStateKnown = false
	worktree.CollectionErrors = append([]string(nil), worktree.CollectionErrors...)
	var failures []error
	record := func(field string, err error) {
		if err != nil {
			failure := fmt.Errorf("%s: %w", field, err)
			failures = append(failures, failure)
			worktree.CollectionErrors = append(worktree.CollectionErrors, failure.Error())
		}
	}
	var err error
	worktree.Status, err = client.Status(ctx, worktree.Path)
	record("status", err)
	commonDirectory, err := client.CommonGitDir(ctx, repository)
	record("common directory", err)
	worktree.CommonGitDir = commonDirectory
	worktree.AdminDir, err = client.gitDirectory(ctx, worktree.Path, "--absolute-git-dir")
	record("administrative directory", err)
	if worktree.AdminDir != "" && commonDirectory != "" {
		relative, relErr := filepath.Rel(commonDirectory, worktree.AdminDir)
		if relErr != nil || !filepath.IsLocal(relative) {
			record("administrative directory", errors.New("metadata is outside the repository common directory"))
		} else {
			worktree.IndexHash, err = hashFile(filepath.Join(worktree.AdminDir, "index"))
			record("index hash", err)
			worktree.AdminHash, worktree.MetadataModifiedAt, err = hashAdmin(worktree.AdminDir, worktree.Primary)
			record("administrative metadata", err)
		}
	}
	headResult, err := client.run(ctx, worktree.Path, "rev-parse", "--verify", "HEAD")
	record("HEAD", err)
	if err == nil {
		head := outputLine(headResult.Stdout)
		_, decodeErr := hex.DecodeString(head)
		if decodeErr != nil || (len(head) != 40 && len(head) != 64) {
			record("HEAD", errors.New("invalid object ID"))
		} else if worktree.Head != "" && worktree.Head != head {
			record("HEAD", errors.New("HEAD changed during collection"))
		}
		worktree.Head = head
	}
	branchResult, branchErr := client.run(ctx, worktree.Path, "symbolic-ref", "--quiet", "HEAD")
	if !(worktree.Detached && branchResult.ExitCode == 1) {
		record("branch", branchErr)
		if branchErr == nil {
			branch, valid := strings.CutPrefix(outputLine(branchResult.Stdout), "refs/heads/")
			if !valid || branch == "" || worktree.Detached || (worktree.Branch != "" && branch != worktree.Branch) {
				record("branch", errors.New("branch identity changed or is invalid"))
			}
			worktree.Branch = branch
		}
	}
	if worktree.Branch != "" {
		upstream, upstreamErr := client.run(ctx, worktree.Path, "for-each-ref", "--format=%(upstream)", "--", "refs/heads/"+worktree.Branch)
		record("upstream", upstreamErr)
		worktree.Upstream = outputLine(upstream.Stdout)
	}
	worktree.Recoverable, err = client.recoverable(ctx, worktree.Path, worktree.Head)
	record("recoverability", err)
	commitTime, err := client.run(ctx, worktree.Path, "show", "--no-patch", "--no-show-signature", "--format=%ct", "HEAD", "--")
	record("commit time", err)
	if err == nil {
		seconds, parseErr := strconv.ParseInt(outputLine(commitTime.Stdout), 10, 64)
		if parseErr != nil || seconds < 0 {
			record("commit time", errors.New("invalid commit timestamp"))
		} else {
			worktree.LastCommitAt = time.Unix(seconds, 0).UTC()
		}
	}
	worktree.GitStateKnown = len(failures) == 0
	return worktree, errors.Join(failures...)
}

func (client *Client) recoverable(ctx context.Context, worktree, head string) (bool, error) {
	if head == "" {
		return false, errors.New("HEAD is unavailable")
	}
	result, err := client.run(ctx, worktree, "for-each-ref", "--format=%(refname)", "--contains="+head, "--", "refs/remotes/", "refs/heads/")
	if err != nil {
		return false, err
	}
	for _, reference := range strings.Split(outputLine(result.Stdout), "\n") {
		if strings.HasPrefix(reference, "refs/remotes/") {
			return true, nil
		}
		for _, base := range client.BaseBranches {
			if reference == "refs/heads/"+base {
				return true, nil
			}
		}
	}
	return false, nil
}

func outputLine(contents []byte) string {
	value := strings.TrimSuffix(string(contents), "\n")
	if runtime.GOOS == "windows" {
		value = strings.TrimSuffix(value, "\r")
	}
	return value
}

func hashFile(path string) (string, error) {
	metadata, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !metadata.Mode().IsRegular() {
		return "", fmt.Errorf("metadata is not a regular file: %q", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	count, err := io.Copy(digest, io.LimitReader(file, maxGitBytes+1))
	if err != nil {
		return "", err
	}
	if count > maxGitBytes {
		return "", errors.New("Git metadata exceeds collection limit")
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func hashAdmin(directory string, primary bool) (string, time.Time, error) {
	digest := sha256.New()
	var latest time.Time
	var total int64
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		if primary && relative != "." && entry.IsDir() && relative != "info" {
			return fs.SkipDir
		}
		metadata, err := entry.Info()
		if err != nil {
			return err
		}
		if metadata.Mode()&os.ModeSymlink != 0 || (!metadata.Mode().IsRegular() && !metadata.IsDir()) {
			return fmt.Errorf("unsafe administrative entry %q", path)
		}
		if metadata.ModTime().After(latest) {
			latest = metadata.ModTime().UTC()
		}
		fmt.Fprintf(digest, "%q\x00%o\x00", filepath.ToSlash(relative), metadata.Mode())
		if metadata.IsDir() {
			return nil
		}
		total += metadata.Size()
		if total > maxGitBytes {
			return errors.New("Git administrative metadata exceeds collection limit")
		}
		fileHash, err := hashFile(path)
		if err == nil {
			fmt.Fprintln(digest, fileHash)
		}
		return err
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return hex.EncodeToString(digest.Sum(nil)), latest, nil
}
