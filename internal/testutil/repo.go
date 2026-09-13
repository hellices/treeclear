package testutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type Repository struct {
	Root          string
	ownedRoot     string
	privateRoot   string
	gitExecutable string
	environment   []string
}

func NewRepository(test testing.TB) *Repository {
	test.Helper()
	ownedRoot, err := filepath.EvalSymlinks(test.TempDir())
	if err != nil {
		test.Fatalf("resolve fixture temporary root: %v", err)
	}
	gitExecutable, err := exec.LookPath("git")
	if err != nil {
		test.Fatalf("Git is required for repository fixtures: %v", err)
	}
	repository := &Repository{
		Root:          filepath.Join(ownedRoot, "primary"),
		ownedRoot:     ownedRoot,
		privateRoot:   filepath.Join(ownedRoot, ".fixture"),
		gitExecutable: gitExecutable,
	}
	for _, directory := range []string{repository.Root, repository.privateRoot, repository.privatePath("home"), repository.privatePath("config"), repository.privatePath("data"), repository.privatePath("cache"), repository.privatePath("tmp"), repository.privatePath("empty")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			test.Fatalf("create fixture directory %q: %v", directory, err)
		}
	}
	if err := os.WriteFile(repository.privatePath("gitconfig"), nil, 0o600); err != nil {
		test.Fatalf("create empty fixture Git config: %v", err)
	}
	repository.environment = repository.isolatedEnvironment()
	repository.Git(test, "init", "--initial-branch=main", "--object-format=sha1")
	if err := os.MkdirAll(filepath.Join(repository.Root, ".git", "info"), 0o700); err != nil {
		test.Fatalf("create fixture Git info directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repository.Root, ".git", "info", "attributes"), []byte("* -filter\n"), 0o600); err != nil {
		test.Fatalf("disable fixture executable filters: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repository.Root, "seed.txt"), []byte("treeclear fixture seed\n"), 0o600); err != nil {
		test.Fatalf("write deterministic fixture seed: %v", err)
	}
	repository.Git(test, "add", "--", "seed.txt")
	repository.Git(test, "commit", "-m", "Initial fixture commit")
	return repository
}

func (repository *Repository) Git(test testing.TB, arguments ...string) string {
	test.Helper()
	output, err := repository.runGit(context.Background(), arguments...)
	if err != nil {
		test.Fatal(err)
	}
	return output
}

func (repository *Repository) AddWorktree(test testing.TB, name, branch string) string {
	test.Helper()
	destination, err := repository.validateWorktree(name, branch)
	if err != nil {
		test.Fatalf("invalid fixture worktree: %v", err)
	}
	repository.Git(test, "worktree", "add", "-b", branch, "--", destination, "HEAD")
	return destination
}

func (repository *Repository) validateWorktree(name, branch string) (string, error) {
	if !filepath.IsLocal(name) || name == "." || strings.ContainsAny(name, "/\\:\x00") {
		return "", fmt.Errorf("worktree name must be a single relative directory: %q", name)
	}
	if strings.TrimSpace(branch) == "" || strings.HasPrefix(strings.TrimSpace(branch), "-") || strings.ContainsRune(branch, '\x00') {
		return "", fmt.Errorf("branch must be nonempty and not option-like: %q", branch)
	}
	if err := repository.validateOwnedRoot(); err != nil {
		return "", err
	}
	destination := filepath.Join(repository.ownedRoot, name)
	if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
		if err != nil {
			return "", fmt.Errorf("inspect fixture worktree %q: %w", destination, err)
		}
		return "", fmt.Errorf("fixture worktree path already exists (including symlinks): %q", destination)
	}
	return destination, nil
}

func (repository *Repository) runGit(parent context.Context, arguments ...string) (string, error) {
	directory, gitDirectory, commandArguments, err := repository.gitLocation(arguments)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	options := []string{"-C", directory, "--git-dir=" + gitDirectory, "--work-tree=" + directory}
	for _, setting := range []string{
		"core.hooksPath=" + repository.privatePath("empty"),
		"core.fsmonitor=false",
		"core.attributesFile=" + repository.privatePath("gitconfig"),
		"core.autocrlf=false",
		"core.eol=lf",
		"core.safecrlf=false",
		"core.quotePath=false",
		"core.pager=",
		"core.askPass=",
		"commit.gpgSign=false",
		"tag.gpgSign=false",
		"log.showSignature=false",
		"credential.helper=",
		"credential.interactive=false",
		"init.templateDir=" + repository.privatePath("empty"),
		"protocol.allow=never",
		"maintenance.auto=false",
		"gc.auto=0",
	} {
		options = append(options, "-c", setting)
	}
	command := exec.CommandContext(ctx, repository.gitExecutable, append(options, commandArguments...)...)
	command.Dir = directory
	command.Env = repository.environment
	command.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return "", fmt.Errorf("git %q in %q: %w\nstdout:\n%s\nstderr:\n%s", commandArguments, directory, err, stdout.String(), stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (repository *Repository) gitLocation(arguments []string) (string, string, []string, error) {
	directory := repository.Root
	if len(arguments) >= 2 && arguments[0] == "-C" {
		directory = arguments[1]
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(repository.Root, directory)
		}
		arguments = arguments[2:]
	}
	if len(arguments) == 0 || strings.HasPrefix(arguments[0], "-") {
		return "", "", nil, errors.New("Git requires a subcommand; only one fixture-local -C routing option is allowed")
	}
	if err := repository.validateOwnedRoot(); err != nil {
		return "", "", nil, err
	}
	directory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve Git fixture directory: %w", err)
	}
	if filepath.Dir(directory) != repository.ownedRoot || directory == repository.privateRoot {
		return "", "", nil, fmt.Errorf("Git directory is not an owned fixture worktree: %q", directory)
	}
	gitDirectory := filepath.Join(directory, ".git")
	metadata, err := os.Lstat(gitDirectory)
	if errors.Is(err, fs.ErrNotExist) && directory == repository.Root && arguments[0] == "init" {
		return directory, gitDirectory, arguments, nil
	}
	if err != nil {
		return "", "", nil, fmt.Errorf("inspect fixture Git metadata: %w", err)
	}
	if metadata.Mode()&os.ModeSymlink != 0 {
		return "", "", nil, fmt.Errorf("fixture Git metadata must not be a symlink: %q", gitDirectory)
	}
	if !metadata.IsDir() {
		content, err := os.ReadFile(gitDirectory)
		if err != nil {
			return "", "", nil, fmt.Errorf("read fixture gitfile: %w", err)
		}
		location, found := strings.CutPrefix(string(content), "gitdir: ")
		if !found {
			return "", "", nil, fmt.Errorf("invalid fixture gitfile: %q", gitDirectory)
		}
		gitDirectory = strings.TrimSuffix(strings.TrimSuffix(location, "\n"), "\r")
		if !filepath.IsAbs(gitDirectory) {
			gitDirectory = filepath.Join(directory, gitDirectory)
		}
	}
	gitDirectory, err = filepath.EvalSymlinks(gitDirectory)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve fixture Git metadata: %w", err)
	}
	relative, err := filepath.Rel(repository.ownedRoot, gitDirectory)
	if err != nil || !filepath.IsLocal(relative) {
		return "", "", nil, fmt.Errorf("Git metadata escapes fixture root: %q", gitDirectory)
	}
	return directory, gitDirectory, arguments, nil
}

func (repository *Repository) validateOwnedRoot() error {
	resolved, err := filepath.EvalSymlinks(repository.ownedRoot)
	if err != nil {
		return fmt.Errorf("resolve owned fixture root: %w", err)
	}
	if resolved != repository.ownedRoot {
		return fmt.Errorf("owned fixture root changed: %q", repository.ownedRoot)
	}
	return nil
}

func (repository *Repository) privatePath(name string) string {
	return filepath.Join(repository.privateRoot, name)
}

func (repository *Repository) isolatedEnvironment() []string {
	var environment []string
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		switch strings.ToUpper(name) {
		case "PATH", "SYSTEMROOT", "WINDIR", "SYSTEMDRIVE", "COMSPEC", "PATHEXT":
			environment = append(environment, entry)
		}
	}
	return append(environment,
		"HOME="+repository.privatePath("home"),
		"USERPROFILE="+repository.privatePath("home"),
		"XDG_CONFIG_HOME="+repository.privatePath("config"),
		"XDG_DATA_HOME="+repository.privatePath("data"),
		"XDG_CACHE_HOME="+repository.privatePath("cache"),
		"APPDATA="+repository.privatePath("config"),
		"LOCALAPPDATA="+repository.privatePath("data"),
		"TMPDIR="+repository.privatePath("tmp"),
		"TMP="+repository.privatePath("tmp"),
		"TEMP="+repository.privatePath("tmp"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM="+repository.privatePath("gitconfig"),
		"GIT_CONFIG_GLOBAL="+repository.privatePath("gitconfig"),
		"GIT_ATTR_NOSYSTEM=1",
		"GIT_TEMPLATE_DIR="+repository.privatePath("empty"),
		"GIT_CEILING_DIRECTORIES="+repository.ownedRoot,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"SSH_ASKPASS=",
		"GCM_INTERACTIVE=never",
		"GIT_AUTHOR_NAME=Treeclear Fixture",
		"GIT_AUTHOR_EMAIL=fixture@treeclear.invalid",
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z",
		"GIT_COMMITTER_NAME=Treeclear Fixture",
		"GIT_COMMITTER_EMAIL=fixture@treeclear.invalid",
		"GIT_COMMITTER_DATE=2000-01-01T00:00:00Z",
		"LC_ALL=C",
		"LANG=C",
		"LANGUAGE=C",
		"TZ=UTC",
	)
}
