package testutil

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRepositoryIsolatedFromHostGit(test *testing.T) {
	victim := NewRepository(test)
	host := test.TempDir()
	marker := filepath.Join(host, "executed")
	program := filepath.Join(host, "hostile-command")
	script := "#!/bin/sh\nprintf 'executed\\n' >> " + quoteFixtureShellPath(marker) + "\nexit 97\n"
	writeFixtureFile(test, program, script, 0o700)
	hooks := filepath.Join(host, "hooks")
	template := filepath.Join(host, "template")
	for _, hook := range []string{"pre-commit", "commit-msg", "post-commit", "post-checkout", "reference-transaction"} {
		writeFixtureFile(test, filepath.Join(hooks, hook), script, 0o700)
		writeFixtureFile(test, filepath.Join(template, "hooks", hook), script, 0o700)
	}
	writeFixtureFile(test, filepath.Join(template, "template-marker"), "host template\n", 0o600)
	attributes := filepath.Join(host, "attributes")
	writeFixtureFile(test, attributes, "* filter=hostile\n", 0o600)
	configValue := func(value string) string { return strconv.Quote(filepath.ToSlash(value)) }
	config := fmt.Sprintf(`[core]
	worktree = %s
	hooksPath = %s
	fsmonitor = %s
	askPass = %s
	attributesFile = %s
[user]
	name = Hostile User
	email = hostile@example.invalid
[commit]
	gpgSign = true
[tag]
	gpgSign = true
[gpg]
	program = %s
[init]
	defaultBranch = hostile
	templateDir = %s
[filter "hostile"]
	clean = %s
	smudge = %s
	process = %s
	required = true
[credential]
	helper = %s
`, configValue(victim.Root), configValue(hooks), configValue(program), configValue(program), configValue(attributes), configValue(program), configValue(template), configValue(quoteFixtureShellPath(program)), configValue(quoteFixtureShellPath(program)), configValue(quoteFixtureShellPath(program)), configValue("!"+quoteFixtureShellPath(program)))
	globalConfig := filepath.Join(host, ".gitconfig")
	xdgConfig := filepath.Join(host, "xdg-config")
	writeFixtureFile(test, globalConfig, config, 0o600)
	writeFixtureFile(test, filepath.Join(xdgConfig, "git", "config"), config, 0o600)
	for name, value := range map[string]string{
		"HOME":                             host,
		"USERPROFILE":                      host,
		"XDG_CONFIG_HOME":                  xdgConfig,
		"XDG_DATA_HOME":                    filepath.Join(host, "xdg-data"),
		"EMAIL":                            "hostile@example.invalid",
		"GIT_DIR":                          filepath.Join(victim.Root, ".git"),
		"GIT_COMMON_DIR":                   filepath.Join(victim.Root, ".git"),
		"GIT_WORK_TREE":                    victim.Root,
		"GIT_INDEX_FILE":                   filepath.Join(victim.Root, ".git", "index"),
		"GIT_OBJECT_DIRECTORY":             filepath.Join(victim.Root, ".git", "objects"),
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": filepath.Join(victim.Root, ".git", "objects"),
		"GIT_CONFIG_SYSTEM":                globalConfig,
		"GIT_CONFIG_GLOBAL":                globalConfig,
		"GIT_CONFIG_NOSYSTEM":              "0",
		"GIT_CONFIG":                       globalConfig,
		"GIT_CONFIG_COUNT":                 "2",
		"GIT_CONFIG_KEY_0":                 "core.worktree",
		"GIT_CONFIG_VALUE_0":               victim.Root,
		"GIT_CONFIG_KEY_1":                 "core.hooksPath",
		"GIT_CONFIG_VALUE_1":               hooks,
		"GIT_CONFIG_PARAMETERS":            "'commit.gpgsign'='true'",
		"GIT_AUTHOR_NAME":                  "Hostile Author",
		"GIT_AUTHOR_EMAIL":                 "author@example.invalid",
		"GIT_AUTHOR_DATE":                  "invalid inherited date",
		"GIT_COMMITTER_NAME":               "Hostile Committer",
		"GIT_COMMITTER_EMAIL":              "committer@example.invalid",
		"GIT_COMMITTER_DATE":               "invalid inherited date",
		"GIT_TEMPLATE_DIR":                 template,
		"GIT_DEFAULT_HASH":                 "sha256",
		"GIT_ATTR_NOSYSTEM":                "0",
		"GIT_ASKPASS":                      program,
		"GIT_TERMINAL_PROMPT":              "1",
		"GIT_EXTERNAL_DIFF":                program,
		"GIT_TRACE":                        filepath.Join(host, "trace"),
		"GIT_TRACE_SETUP":                  filepath.Join(host, "trace-setup"),
		"GIT_TRACE2_EVENT":                 filepath.Join(host, "trace-event"),
		"LANG":                             "hostile.invalid",
		"LC_ALL":                           "hostile.invalid",
		"TZ":                               "Pacific/Honolulu",
	} {
		test.Setenv(name, value)
	}
	hostBefore := snapshotFixtureTree(test, host)
	victimBefore := snapshotFixtureTree(test, filepath.Dir(victim.Root))
	environmentBefore := os.Environ()
	workingDirectoryBefore, err := os.Getwd()
	if err != nil {
		test.Fatal(err)
	}

	repository := NewRepository(test)
	test.Log(repository.Git(test, "version"))
	if actual := repository.Git(test, "rev-parse", "HEAD"); actual != victim.Git(test, "rev-parse", "HEAD") {
		test.Fatalf("hostile environment changed deterministic commit: %s", actual)
	}
	if actual := repository.Git(test, "log", "-1", "--format=%an|%ae|%cn|%ce"); actual != "Treeclear Fixture|fixture@treeclear.invalid|Treeclear Fixture|fixture@treeclear.invalid" {
		test.Errorf("unexpected commit identity: %s", actual)
	}
	dates := strings.Split(repository.Git(test, "log", "-1", "--format=%aI|%cI"), "|")
	if len(dates) != 2 {
		test.Fatalf("unexpected author/committer date fields: %q", dates)
	}
	for _, date := range dates {
		actual, err := time.Parse(time.RFC3339, date)
		_, offset := actual.Zone()
		if err != nil || !actual.Equal(time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)) || offset != 0 {
			test.Errorf("unexpected commit date %q: %v", date, err)
		}
	}
	if _, err := os.Stat(filepath.Join(repository.Root, ".git", "template-marker")); !errors.Is(err, fs.ErrNotExist) {
		test.Errorf("host template copied into fixture: %v", err)
	}

	for key, value := range map[string]string{
		"core.hooksPath":          hooks,
		"core.fsmonitor":          program,
		"commit.gpgSign":          "true",
		"tag.gpgSign":             "true",
		"gpg.program":             program,
		"filter.hostile.clean":    quoteFixtureShellPath(program),
		"filter.hostile.smudge":   quoteFixtureShellPath(program),
		"filter.hostile.process":  quoteFixtureShellPath(program),
		"filter.hostile.required": "true",
	} {
		repository.Git(test, "config", "--local", key, value)
	}
	writeFixtureFile(test, filepath.Join(repository.Root, ".gitattributes"), "* filter=hostile\n", 0o600)
	writeFixtureFile(test, filepath.Join(repository.Root, "filtered.txt"), "unfiltered bytes\n", 0o600)
	repository.Git(test, "add", "--", ".gitattributes", "filtered.txt")
	repository.Git(test, "commit", "-m", "Exercise isolated settings")
	repository.Git(test, "tag", "-a", "isolated-tag", "-m", "Unsigned fixture tag")
	linked := repository.AddWorktree(test, "isolated linked", "isolated-branch")
	if actual := string(readFixtureFile(test, filepath.Join(linked, "filtered.txt"))); actual != "unfiltered bytes\n" {
		test.Errorf("filter changed checkout: %q", actual)
	}
	if actual := repository.Git(test, "status", "--porcelain"); actual != "" {
		test.Errorf("fixture unexpectedly dirty: %s", actual)
	}
	if actual := snapshotFixtureTree(test, host); !reflect.DeepEqual(actual, hostBefore) {
		test.Error("host files changed or hostile command/trace executed")
	}
	if actual := snapshotFixtureTree(test, filepath.Dir(victim.Root)); !reflect.DeepEqual(actual, victimBefore) {
		test.Error("foreign temporary repository changed")
	}
	if actual := os.Environ(); !reflect.DeepEqual(actual, environmentBefore) {
		test.Error("fixture changed process-global environment")
	}
	if actual, err := os.Getwd(); err != nil || actual != workingDirectoryBefore {
		test.Errorf("fixture changed process working directory: %q, %v", actual, err)
	}

	environment := make(map[string]string)
	for _, entry := range repository.environment {
		name, value, found := strings.Cut(entry, "=")
		if found {
			environment[strings.ToUpper(name)] = value
		}
	}
	for _, name := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_TEMPLATE_DIR", "TMPDIR", "TMP", "TEMP"} {
		if !fixtureContains(filepath.Dir(repository.Root), environment[name]) {
			test.Errorf("%s is not private to the fixture: %q", name, environment[name])
		}
	}
	for name, expected := range map[string]string{"GIT_CONFIG_NOSYSTEM": "1", "GIT_ATTR_NOSYSTEM": "1", "GIT_TERMINAL_PROMPT": "0", "LC_ALL": "C", "LANG": "C", "TZ": "UTC"} {
		if actual := environment[name]; actual != expected {
			test.Errorf("%s = %q, want %q", name, actual, expected)
		}
	}
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_TRACE", "GIT_TRACE_SETUP", "GIT_TRACE2_EVENT", "GIT_EXTERNAL_DIFF", "EMAIL"} {
		if _, found := environment[name]; found {
			test.Errorf("inherited override survived: %s", name)
		}
	}
	if actual := readFixtureFile(test, environment["GIT_CONFIG_GLOBAL"]); len(actual) != 0 {
		test.Errorf("global config is not empty: %q", actual)
	}
}

func TestRepositoryWorktrees(test *testing.T) {
	repository := NewRepository(test)
	if actual := string(readFixtureFile(test, filepath.Join(repository.Root, "seed.txt"))); actual != "treeclear fixture seed\n" {
		test.Fatalf("seed bytes = %q", actual)
	}
	if actual := repository.Git(test, "branch", "--show-current"); actual != "main" {
		test.Fatalf("primary branch = %q, want main", actual)
	}
	if actual := repository.Git(test, "rev-list", "--count", "HEAD"); actual != "1" {
		test.Fatalf("commit count = %q, want 1", actual)
	}
	if actual := repository.Git(test, "status", "--porcelain"); actual != "" {
		test.Fatalf("new repository is dirty: %s", actual)
	}
	if actual := repository.Git(test, "rev-parse", "--show-toplevel"); filepath.Clean(filepath.FromSlash(actual)) != repository.Root {
		test.Fatalf("Git root = %q, fixture root = %q", actual, repository.Root)
	}
	linked := repository.AddWorktree(test, "작업 tree with spaces", "topic/한글")
	if filepath.Dir(linked) != filepath.Dir(repository.Root) || linked == repository.Root {
		test.Fatalf("linked worktree is not a sibling: %s", linked)
	}
	if actual := string(readFixtureFile(test, filepath.Join(linked, "seed.txt"))); actual != "treeclear fixture seed\n" {
		test.Errorf("linked seed bytes = %q", actual)
	}
	if actual := repository.Git(test, "rev-parse", "main"); actual != repository.Git(test, "rev-parse", "topic/한글") {
		test.Error("linked branch does not start at primary HEAD")
	}
	if actual := repository.Git(test, "-C", linked, "branch", "--show-current"); actual != "topic/한글" {
		test.Errorf("linked current branch = %q", actual)
	}
	writeFixtureFile(test, filepath.Join(linked, "seed.txt"), "dirty linked bytes\n", 0o600)
	writeFixtureFile(test, filepath.Join(linked, "untracked file.txt"), "untracked\n", 0o600)
	status := repository.Git(test, "-C", linked, "status", "--porcelain")
	if !strings.Contains(status, "M seed.txt") || !strings.Contains(status, "untracked file.txt") {
		test.Errorf("linked worktree lacks real dirty state: %q", status)
	}
	if actual := repository.Git(test, "status", "--porcelain"); actual != "" {
		test.Errorf("linked changes dirtied primary: %s", actual)
	}
	repository.Git(test, "worktree", "lock", "--reason", "fixture lock", "--", linked)
	listing := repository.Git(test, "worktree", "list", "--porcelain", "-z")
	for _, expected := range []string{"worktree " + filepath.ToSlash(repository.Root) + "\x00", "worktree " + filepath.ToSlash(linked) + "\x00", "branch refs/heads/main\x00", "branch refs/heads/topic/한글\x00", "locked fixture lock\x00"} {
		if !strings.Contains(listing, expected) {
			test.Errorf("worktree list %q does not contain %q", listing, expected)
		}
	}
}

func TestRepositoryRejectsEscapes(test *testing.T) {
	repository := NewRepository(test)
	outside := filepath.Join(test.TempDir(), "outside")
	writeFixtureFile(test, filepath.Join(filepath.Dir(repository.Root), "occupied"), "keep\n", 0o600)
	before := snapshotFixtureTree(test, filepath.Dir(repository.Root))
	cases := []struct {
		name   string
		branch string
	}{
		{name: outside, branch: "outside"},
		{name: "/absolute", branch: "absolute"},
		{name: "", branch: "empty"},
		{name: ".", branch: "dot"},
		{name: "..", branch: "parent"},
		{name: "../escape", branch: "parent"},
		{name: "nested/../../escape", branch: "traversal"},
		{name: "nested/../escape", branch: "cleaned traversal"},
		{name: `nested\..\escape`, branch: "backslash"},
		{name: `C:\outside`, branch: "drive"},
		{name: `C:outside`, branch: "drive-relative"},
		{name: `\\server\share\outside`, branch: "network"},
		{name: "invalid\x00name", branch: "nul"},
		{name: filepath.Base(repository.Root), branch: "primary"},
		{name: "occupied", branch: "occupied"},
		{name: "safe", branch: ""},
		{name: "safe", branch: "-"},
		{name: "safe", branch: "-b"},
		{name: "safe", branch: "--force"},
		{name: "safe", branch: "--detach"},
		{name: "safe", branch: "--orphan"},
		{name: "safe", branch: "invalid\x00branch"},
	}
	for _, entry := range cases {
		test.Run(fmt.Sprintf("%s/%s", entry.name, entry.branch), func(test *testing.T) {
			if destination, err := repository.validateWorktree(entry.name, entry.branch); err == nil {
				test.Errorf("accepted unsafe name %q, branch %q as %q", entry.name, entry.branch, destination)
			}
		})
	}
	for _, name := range []string{"feature", "작업 tree with spaces"} {
		destination, err := repository.validateWorktree(name, "topic/한글")
		if err != nil || destination != filepath.Join(filepath.Dir(repository.Root), name) {
			test.Errorf("valid name %q = %q, %v", name, destination, err)
		}
	}
	if actual := snapshotFixtureTree(test, filepath.Dir(repository.Root)); !reflect.DeepEqual(actual, before) {
		test.Error("validation modified fixture files")
	}
	if _, err := os.Stat(outside); !errors.Is(err, fs.ErrNotExist) {
		test.Errorf("validation created outside path: %v", err)
	}
}

func TestRepositoryRejectsSymlinkEscapes(test *testing.T) {
	repository := NewRepository(test)
	outside := test.TempDir()
	link := filepath.Join(filepath.Dir(repository.Root), "escape-link")
	if err := os.Symlink(outside, link); err != nil {
		const windowsPrivilegeNotHeld = syscall.Errno(1314)
		if runtime.GOOS == "windows" && (errors.Is(err, fs.ErrPermission) || errors.Is(err, windowsPrivilegeNotHeld)) {
			test.Skipf("native Windows symlink permission unavailable: %v", err)
		}
		test.Fatal(err)
	}
	if destination, err := repository.validateWorktree("escape-link", "escape"); err == nil {
		test.Errorf("accepted escaping symlink: %s", destination)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		test.Errorf("outside directory changed: %v, %v", entries, err)
	}
}

func TestRepositoryGitDiagnostics(test *testing.T) {
	repository := NewRepository(test)
	if _, err := repository.runGit(context.Background(), "not-a-real-fixture-command"); err == nil {
		test.Fatal("invalid Git command succeeded")
	} else {
		for _, expected := range []string{repository.Root, "not-a-real-fixture-command", "stdout", "stderr", "not a git command"} {
			if !strings.Contains(err.Error(), expected) {
				test.Errorf("diagnostic %q lacks %q", err, expected)
			}
		}
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := repository.runGit(ctx, "status"); !errors.Is(err, context.DeadlineExceeded) {
		test.Errorf("expired Git deadline = %v, want deadline exceeded", err)
	}
	for _, arguments := range [][]string{
		nil,
		{"-C"},
		{"-C", test.TempDir(), "status"},
		{"--git-dir=" + test.TempDir(), "status"},
		{"--work-tree=" + test.TempDir(), "status"},
		{"-c", "core.hooksPath=outside", "status"},
		{"-C", repository.Root, "-C", test.TempDir(), "status"},
	} {
		if _, err := repository.runGit(context.Background(), arguments...); err == nil {
			test.Errorf("accepted routing/configuration overrides: %q", arguments)
		}
	}
}

func writeFixtureFile(test *testing.T, filename, content string, mode fs.FileMode) {
	test.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), mode); err != nil {
		test.Fatal(err)
	}
}

func readFixtureFile(test *testing.T, filename string) []byte {
	test.Helper()
	content, err := os.ReadFile(filename)
	if err != nil {
		test.Fatal(err)
	}
	return content
}

func quoteFixtureShellPath(filename string) string {
	return "'" + strings.ReplaceAll(filepath.ToSlash(filename), "'", "'\\''") + "'"
}

func fixtureContains(root, filename string) bool {
	if !filepath.IsAbs(filename) {
		return false
	}
	relative, err := filepath.Rel(root, filename)
	return err == nil && filepath.IsLocal(relative)
}

func snapshotFixtureTree(test *testing.T, root string) map[string]string {
	test.Helper()
	snapshot := make(map[string]string)
	err := filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		snapshot[relative] = fmt.Sprintf("%x", sha256.Sum256(content))
		return nil
	})
	if err != nil {
		test.Fatal(err)
	}
	return snapshot
}
