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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
)

const (
	maxGitBytes     = 16 << 20
	maxAdminEntries = 4096
	gitReadTimeout  = 30 * time.Second
)

var (
	ErrWorktreeChanged = errors.New("Git worktree state changed")
	ErrReadLimit       = errors.New("Git read limit exceeded")
)

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
	if directory == "" && !(len(arguments) == 1 && arguments[0] == "--version") {
		return execx.Result{}, errors.New("Git working directory is required")
	}
	if len(arguments) != 0 {
		switch arguments[0] {
		case "ls-files", "status", "diff":
			if err := client.rejectSplitIndex(ctx, directory); err != nil {
				return execx.Result{}, fmt.Errorf("Git read-only index preflight: %w", err)
			}
		}
	}
	environment := execx.SanitizedEnvironment(os.Environ(), map[string]string{
		"LC_ALL": "C", "LANG": "C", "GIT_OPTIONAL_LOCKS": "0",
		"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.DevNull,
		"GIT_ATTR_NOSYSTEM":   "1",
		"GIT_TERMINAL_PROMPT": "0", "GCM_INTERACTIVE": "never", "GIT_NO_LAZY_FETCH": "1",
		"GIT_CONFIG_COUNT": "5",
		"GIT_CONFIG_KEY_0": "core.fsmonitor", "GIT_CONFIG_VALUE_0": "false",
		"GIT_CONFIG_KEY_1": "protocol.allow", "GIT_CONFIG_VALUE_1": "never",
		"GIT_CONFIG_KEY_2": "log.showSignature", "GIT_CONFIG_VALUE_2": "false",
		"GIT_CONFIG_KEY_3": "core.attributesFile", "GIT_CONFIG_VALUE_3": os.DevNull,
		"GIT_CONFIG_KEY_4": "diff.autoRefreshIndex", "GIT_CONFIG_VALUE_4": "false",
	})
	result, err := client.Runner.Run(ctx, execx.Request{
		Directory: directory, Name: "git", Args: arguments, Env: environment,
		Timeout: gitReadTimeout, MaxBytes: maxGitBytes,
	})
	if err == nil && result.ExitCode != 0 {
		err = commandExitStatus(result.ExitCode)
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

func (client *Client) WorktreeRoot(ctx context.Context, directory string) (string, error) {
	return client.gitDirectory(ctx, directory, "--path-format=absolute", "--show-toplevel")
}

func (client *Client) verifyWorktreeRoot(ctx context.Context, worktree string) error {
	root, err := client.WorktreeRoot(ctx, worktree)
	if err != nil {
		return err
	}
	registered, err := os.Stat(worktree)
	if err != nil {
		return err
	}
	effective, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !registered.IsDir() || !effective.IsDir() || !os.SameFile(registered, effective) {
		return fmt.Errorf("%w: effective Git worktree %q differs from registered path %q", ErrWorktreeChanged, root, worktree)
	}
	return nil
}

func (client *Client) verifyCommonGitDir(ctx context.Context, worktree, expected string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	effective, err := client.CommonGitDir(ctx, worktree)
	if err != nil {
		return err
	}
	registeredInfo, err := os.Stat(expected)
	if err != nil {
		return err
	}
	effectiveInfo, err := os.Stat(effective)
	if err != nil {
		return err
	}
	if !registeredInfo.IsDir() || !effectiveInfo.IsDir() || !os.SameFile(registeredInfo, effectiveInfo) {
		return fmt.Errorf("%w: effective common Git directory %q differs from repository identity %q", ErrWorktreeChanged, effective, expected)
	}
	return ctx.Err()
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
	worktrees, _, err := client.ListWorktreesRaw(ctx, repository)
	return worktrees, err
}

func (client *Client) ListWorktreesRaw(ctx context.Context, repository string) ([]domain.Worktree, []byte, error) {
	result, err := client.run(ctx, repository, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, nil, err
	}
	records, err := parseWorktreePorcelainZ(result.Stdout)
	if err != nil {
		return nil, nil, err
	}
	if records[0].Bare {
		return nil, nil, errors.New("bare repositories are not supported as discovery anchors")
	}
	commonDirectory, err := client.CommonGitDir(ctx, repository)
	if err != nil {
		return nil, nil, err
	}
	worktrees := make([]domain.Worktree, 0, len(records))
	for index, record := range records {
		worktrees = append(worktrees, domain.Worktree{
			Path: filepath.FromSlash(record.Path), RepositoryRoot: filepath.FromSlash(records[0].Path), CommonGitDir: commonDirectory,
			Head: record.Head, Branch: record.Branch, Primary: index == 0,
			Detached: record.Detached, Locked: record.Locked, LockReason: record.LockReason,
			Prunable: record.Prunable,
		})
	}
	return worktrees, result.Stdout, nil
}

func (client *Client) rejectExecutableFilters(ctx context.Context, directory string) error {
	result, err := client.run(ctx, directory, "config", "--null", "--get-regexp", `^filter\..*\.(clean|smudge|process)$`)
	if isQuietCommandExit(result, err, 1) {
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

func (client *Client) rejectUnsafeIndex(ctx context.Context, directory string) error {
	result, err := client.run(ctx, directory, "ls-files", "--cached", "--stage", "-v", "-z", "--no-recurse-submodules")
	if err != nil {
		return err
	}
	remaining := string(result.Stdout)
	for remaining != "" {
		record, rest, terminated := strings.Cut(remaining, "\x00")
		metadata, path, separated := strings.Cut(record, "\t")
		fields := strings.Fields(metadata)
		if !terminated || !separated || path == "" || len(fields) != 4 {
			return errors.New("malformed Git index entry")
		}
		if fields[0] != "H" && fields[0] != "M" {
			return errors.New("hidden or unsupported Git index flags prevent read-only collection")
		}
		if fields[1] == "160000" {
			return errors.New("submodule entries prevent read-only collection until guarded submodule inspection is supported")
		}
		if fields[1] != "100644" && fields[1] != "100755" && fields[1] != "120000" {
			return errors.New("unsupported Git index mode")
		}
		if _, err := hex.DecodeString(fields[2]); err != nil || (len(fields[2]) != 40 && len(fields[2]) != 64) {
			return errors.New("invalid Git index object ID")
		}
		if fields[3] != "0" && fields[3] != "1" && fields[3] != "2" && fields[3] != "3" {
			return errors.New("invalid Git index stage")
		}
		remaining = rest
	}
	return nil
}

func (client *Client) Status(ctx context.Context, worktree string) (domain.GitStatus, error) {
	status, _, err := client.StatusRaw(ctx, worktree)
	return status, err
}

func (client *Client) StatusRaw(ctx context.Context, worktree string) (domain.GitStatus, []byte, error) {
	contents, err := client.readStatusRaw(ctx, worktree)
	if err != nil {
		return domain.GitStatus{}, nil, err
	}
	status, err := parseStatusPorcelainZ(contents)
	if err != nil {
		return domain.GitStatus{}, nil, err
	}
	return status, contents, nil
}

func (client *Client) Diff(ctx context.Context, worktree string, staged bool) ([]byte, error) {
	if err := client.rejectExecutableFilters(ctx, worktree); err != nil {
		return nil, err
	}
	if err := client.rejectUnsafeIndex(ctx, worktree); err != nil {
		return nil, err
	}
	arguments := []string{"diff", "--binary", "--full-index", "--no-color", "--no-ext-diff", "--no-textconv"}
	if staged {
		arguments = append(arguments, "--cached")
	}
	result, err := client.run(ctx, worktree, append(arguments, "--")...)
	if err != nil {
		return nil, err
	}
	return result.Stdout, nil
}

func (client *Client) RemoveWorktree(ctx context.Context, repository, path string) error {
	if path == "" {
		return errors.New("worktree path is required")
	}
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
	if repository == "" {
		record("repository", errors.New("path is required"))
	}
	if worktree.Path == "" {
		record("worktree", errors.New("path is required"))
	}
	if len(failures) != 0 {
		return worktree, errors.Join(failures...)
	}
	if err := client.verifyWorktreeRoot(ctx, worktree.Path); err != nil {
		worktree.PathSafe = false
		record("worktree root", err)
		return worktree, errors.Join(failures...)
	}
	var err error
	commonDirectory, err := client.CommonGitDir(ctx, repository)
	if err != nil {
		worktree.PathSafe = false
	}
	record("common directory", err)
	worktree.CommonGitDir = commonDirectory
	if err == nil {
		if err := client.verifyCommonGitDir(ctx, worktree.Path, commonDirectory); err != nil {
			worktree.PathSafe = false
			record("effective common directory", err)
		}
	}
	if len(failures) != 0 {
		return worktree, errors.Join(failures...)
	}
	worktree.AdminDir, err = client.gitDirectory(ctx, worktree.Path, "--absolute-git-dir")
	if err != nil {
		worktree.PathSafe = false
	}
	record("administrative directory", err)
	if worktree.AdminDir != "" && commonDirectory != "" {
		relative, relErr := filepath.Rel(commonDirectory, worktree.AdminDir)
		if relErr != nil || !filepath.IsLocal(relative) {
			worktree.PathSafe = false
			record("administrative directory", fmt.Errorf("%w: metadata is outside the repository common directory", ErrWorktreeChanged))
		} else {
			if err := rejectSplitIndexDirectory(ctx, worktree.AdminDir, defaultReadonlyIndexOperations()); err != nil {
				record("read-only index preflight", err)
				return worktree, errors.Join(failures...)
			}
			hashContext, cancel := context.WithTimeout(ctx, gitReadTimeout)
			worktree.IndexHash, _, err = hashFile(hashContext, filepath.Join(worktree.AdminDir, "index"), maxGitBytes)
			record("index hash", err)
			worktree.AdminHash, worktree.MetadataModifiedAt, err = hashAdmin(hashContext, worktree.AdminDir, worktree.Primary)
			cancel()
			record("administrative metadata", err)
		}
	}
	if len(failures) != 0 {
		return worktree, errors.Join(failures...)
	}
	worktree.Status, err = client.Status(ctx, worktree.Path)
	record("status", err)
	headResult, err := client.run(ctx, worktree.Path, "rev-parse", "--verify", "HEAD")
	record("HEAD", err)
	if err == nil {
		head := outputLine(headResult.Stdout)
		_, decodeErr := hex.DecodeString(head)
		if decodeErr != nil || (len(head) != 40 && len(head) != 64) {
			record("HEAD", errors.New("invalid object ID"))
		} else if worktree.Head != "" && worktree.Head != head {
			record("HEAD", fmt.Errorf("%w: HEAD changed during collection", ErrWorktreeChanged))
		}
		worktree.Head = head
	}
	branchResult, branchErr := client.run(ctx, worktree.Path, "symbolic-ref", "--quiet", "HEAD")
	if isQuietCommandExit(branchResult, branchErr, 1) {
		if !worktree.Detached {
			record("branch", fmt.Errorf("%w: HEAD became detached: %w", ErrWorktreeChanged, branchErr))
		}
	} else {
		record("branch", branchErr)
		if branchErr == nil {
			branch, valid := strings.CutPrefix(outputLine(branchResult.Stdout), "refs/heads/")
			if !valid || branch == "" {
				record("branch", errors.New("branch identity is invalid"))
			} else if worktree.Detached || (worktree.Branch != "" && branch != worktree.Branch) {
				record("branch", fmt.Errorf("%w: branch identity changed during collection", ErrWorktreeChanged))
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

func hashFile(ctx context.Context, path string, maxBytes int64) (string, int64, error) {
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	metadata, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if !metadata.Mode().IsRegular() {
		return "", 0, fmt.Errorf("metadata is not a regular file: %q", path)
	}
	if metadata.Size() > maxBytes {
		return "", 0, fmt.Errorf("Git metadata exceeds collection limit: %w", ErrReadLimit)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	digest := sha256.New()
	reader := io.LimitReader(file, maxBytes+1)
	var buffer [32 << 10]byte
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
		count, readErr := reader.Read(buffer[:])
		total += int64(count)
		if total > maxBytes {
			return "", 0, fmt.Errorf("Git metadata exceeds collection limit: %w", ErrReadLimit)
		}
		digest.Write(buffer[:count])
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return "", 0, readErr
			}
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(digest.Sum(nil)), total, nil
}

func hashAdmin(ctx context.Context, directory string, primary bool) (string, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return "", time.Time{}, err
	}
	root, err := os.Lstat(directory)
	if err != nil {
		return "", time.Time{}, err
	}
	digest := sha256.New()
	var latest time.Time
	var total int64
	remainingEntries := maxAdminEntries - 1
	var visit func(string, string, fs.DirEntry) error
	visit = func(path, relative string, entry fs.DirEntry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if primary && relative != "." && entry.IsDir() && relative != "info" {
			return nil
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
			entries, err := readAdminEntries(ctx, path, remainingEntries)
			if err != nil {
				return err
			}
			remainingEntries -= len(entries)
			for _, child := range entries {
				if err := visit(filepath.Join(path, child.Name()), filepath.Join(relative, child.Name()), child); err != nil {
					return err
				}
			}
			return nil
		}
		fileHash, count, err := hashFile(ctx, path, maxGitBytes-total)
		if err == nil {
			total += count
			fmt.Fprintln(digest, fileHash)
		}
		return err
	}
	err = visit(directory, ".", fs.FileInfoToDirEntry(root))
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return "", time.Time{}, err
	}
	return hex.EncodeToString(digest.Sum(nil)), latest, nil
}

func readAdminEntries(ctx context.Context, directory string, limit int) ([]os.DirEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(directory)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var entries []os.DirEntry
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batch, readErr := file.ReadDir(min(128, limit-len(entries)+1))
		entries = append(entries, batch...)
		if len(entries) > limit {
			return nil, fmt.Errorf("Git administrative metadata exceeds entry limit: %w", ErrReadLimit)
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, readErr
			}
			break
		}
	}
	sort.Slice(entries, func(first, second int) bool { return entries[first].Name() < entries[second].Name() })
	return entries, nil
}
