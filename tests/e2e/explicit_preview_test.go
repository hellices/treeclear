package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/cli"
	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/plan"
	"github.com/hellices/treeclear/internal/process"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestExplicitPreviewPreservesDirtyIgnoredAndProtectedWorktrees(test *testing.T) {
	for _, scenario := range []struct {
		name                string
		flags               []string
		inventory           bool
		skipDirty           bool
		selectProtected     bool
		incompleteProcesses bool
	}{
		{name: "inventory", inventory: true},
		{name: "default disposal"},
		{name: "skip dirty", flags: []string{"--skip-dirty"}, skipDirty: true},
		{name: "explicit false defaults", flags: []string{"--skip-dirty=false", "--backup=false"}},
		{name: "selection is not eligibility", selectProtected: true},
		{name: "incomplete processes", incompleteProcesses: true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			fixture := newExplicitPreviewFixture(test)
			before := installedFixtureDigest(test, filepath.Dir(fixture.repository.Root))
			settings := test.TempDir()
			clock := testutil.NewClock(time.Now().UTC().Add(30 * 24 * time.Hour))
			executable, err := os.Executable()
			if err != nil {
				test.Fatal(err)
			}
			source := syntheticProcesses{infos: []process.Info{{
				PID: 42, CreatedAt: clock.Now().Add(-time.Hour), Executable: executable,
				CWD: fixture.active, CommandLine: []string{executable}, Inspectable: true, Owner: "fixture", OwnerRelation: process.OwnerSame,
			}}}
			if scenario.incompleteProcesses {
				source.err = errors.New("synthetic explicit preview enumeration failure")
			}
			var stdout, stderr bytes.Buffer
			dependencies := cli.Dependencies{
				Stdout: &stdout, Stderr: &stderr, BuildVersion: "explicit-preview-e2e", WorkingDirectory: fixture.current,
				UserConfigPath: filepath.Join(settings, "user.toml"), RepositoryConfigPath: filepath.Join(settings, "repository.toml"),
				DataDirectory: filepath.Join(settings, "state"), Processes: process.Collector{Source: source}, Now: clock.Now,
			}
			arguments := []string{"plan", "--root", fixture.repository.Root, "--format", "json"}
			selectedPaths := []string{}
			if !scenario.inventory {
				selectedPaths = append(selectedPaths, fixture.dirty, fixture.ignored)
			}
			if scenario.selectProtected {
				selectedPaths = append(selectedPaths, fixture.current, fixture.locked, fixture.active)
			}
			for _, path := range selectedPaths {
				relative, err := filepath.Rel(fixture.current, path)
				if err != nil {
					test.Fatal(err)
				}
				arguments = append(arguments, "--worktree", relative+string(filepath.Separator)+".")
			}
			command := cli.NewRootCommand(dependencies)
			command.SetArgs(append(arguments, scenario.flags...))
			err = command.ExecuteContext(test.Context())
			if (err != nil) != scenario.incompleteProcesses || (scenario.incompleteProcesses && !errors.Is(err, source.err)) {
				test.Fatalf("explicit preview error = %v, stdout = %s, stderr = %s", err, stdout.String(), stderr.String())
			}
			var value domain.Plan
			if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
				test.Fatalf("explicit preview JSON: %v; %s", err, stdout.String())
			}
			var skippedPaths []string
			if scenario.skipDirty {
				skippedPaths = []string{fixture.dirty}
			}
			checkReadOnlyPreview(test, value, selectedPaths, scenario.skipDirty, skippedPaths...)
			checkInstalledCandidates(test, value.Candidates, fixture.protections(), 42, true)
			if scenario.incompleteProcesses {
				checkIncompletePreviewProtection(test, value)
			} else if value.Summary != (domain.PlanSummary{Safe: 2, Protected: 5}) || len(value.Warnings) != 1 {
				test.Fatalf("selection changed ordinary classification counts or collection completeness: %#v, warnings=%v", value.Summary, value.Warnings)
			}
			for _, candidate := range value.Candidates {
				if candidate.Worktree.Path == fixture.dirty && (!candidate.Worktree.GitStateKnown || candidate.Worktree.Status.Staged < 1 || candidate.Worktree.Status.Unstaged < 1 || candidate.Worktree.Status.Untracked < 1) {
					test.Fatalf("dirty fixture lost staged, unstaged or untracked evidence: %#v", candidate.Worktree)
				}
				if candidate.Worktree.Path == fixture.ignored && (!candidate.Worktree.GitStateKnown || !candidate.Worktree.Status.Clean()) {
					test.Fatalf("ignored-only fixture was treated as dirty or unknown: %#v", candidate.Worktree)
				}
				stdout.Reset()
				stderr.Reset()
				command = cli.NewRootCommand(dependencies)
				command.SetArgs([]string{"explain", candidate.ID, "--plan", value.ID, "--format", "json"})
				if err := command.ExecuteContext(test.Context()); err != nil {
					test.Fatalf("explain selected/skipped/unselected candidate: %v", err)
				}
				checkPreviewExplanation(test, stdout.Bytes(), value, candidate)
			}
			checkPreviewOnlyState(test, dependencies.DataDirectory, value.ID)
			for _, path := range []string{dependencies.UserConfigPath, dependencies.RepositoryConfigPath} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					test.Fatalf("preview created configuration %q: %v", path, err)
				}
			}
			if !reflect.DeepEqual(before, installedFixtureDigest(test, filepath.Dir(fixture.repository.Root))) {
				test.Fatal("preview changed sentinels, indexes, registrations or branch refs, or created recovery artifacts")
			}
		})
	}
}

func TestExplicitPreviewRefusesBackupAndMutationBeforeWrites(test *testing.T) {
	for _, scenario := range []struct {
		name       string
		arguments  []string
		diagnostic string
	}{
		{name: "backup", arguments: []string{"plan", "--backup"}, diagnostic: "backup is not implemented"},
		{name: "force", arguments: []string{"plan", "--force"}, diagnostic: "unknown flag: --force"},
		{name: "yes", arguments: []string{"plan", "--yes"}, diagnostic: "unknown flag: --yes"},
		{name: "apply", arguments: []string{"apply"}, diagnostic: `unknown command "apply"`},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			linked := repository.AddWorktree(test, "selected, with spaces", "selected-fixture")
			before := installedFixtureDigest(test, filepath.Dir(repository.Root))
			settings := test.TempDir()
			userConfig := filepath.Join(settings, "user.toml")
			if err := os.WriteFile(userConfig, []byte("invalid fixture configuration [\n"), 0o600); err != nil {
				test.Fatal(err)
			}
			settingsBefore := installedFixtureDigest(test, settings)
			var stdout, stderr bytes.Buffer
			command := cli.NewRootCommand(cli.Dependencies{
				Stdout: &stdout, Stderr: &stderr, WorkingDirectory: linked,
				UserConfigPath: userConfig, RepositoryConfigPath: filepath.Join(settings, "repository.toml"),
				DataDirectory: filepath.Join(settings, "state"), Processes: process.Collector{Source: syntheticProcesses{}},
			})
			arguments := append([]string{}, scenario.arguments...)
			if arguments[0] == "plan" {
				arguments = append(arguments, "--root", repository.Root, "--worktree", linked, "--format", "json", "--output", filepath.Join(settings, "export.json"))
			}
			command.SetArgs(arguments)
			if err := command.ExecuteContext(test.Context()); err == nil || !strings.Contains(err.Error(), scenario.diagnostic) || stdout.Len() != 0 {
				test.Fatalf("unsupported request was not explicitly refused: %v, stdout=%q, stderr=%q", err, stdout.String(), stderr.String())
			}
			if !reflect.DeepEqual(settingsBefore, installedFixtureDigest(test, settings)) || !reflect.DeepEqual(before, installedFixtureDigest(test, filepath.Dir(repository.Root))) {
				test.Fatal("rejected request changed config, state, exports or disposable worktree contents")
			}
		})
	}
}

type explicitPreviewFixture struct {
	repository *testutil.Repository
	current    string
	dirty      string
	ignored    string
	unselected string
	locked     string
	active     string
}

func newExplicitPreviewFixture(test *testing.T) explicitPreviewFixture {
	test.Helper()
	repository := testutil.NewRepository(test)
	if err := os.WriteFile(filepath.Join(repository.Root, ".gitignore"), []byte(".env\nnode_modules/\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	repository.Git(test, "add", "--", ".gitignore")
	repository.Git(test, "commit", "-m", "Ignore synthetic preview sentinels")
	fixture := explicitPreviewFixture{
		repository: repository,
		current:    repository.AddWorktree(test, "current with spaces", "current-fixture"),
		dirty:      repository.AddWorktree(test, "z dirty,한글 with spaces", "dirty-fixture"),
		ignored:    repository.AddWorktree(test, "a ignored,only with spaces", "ignored-fixture"),
		unselected: repository.AddWorktree(test, "unselected", "unselected-fixture"),
		locked:     repository.AddWorktree(test, "locked", "locked-fixture"),
		active:     repository.AddWorktree(test, "active-process", "active-fixture"),
	}
	for path := range fixture.protections() {
		dependency := filepath.Join(path, "node_modules", "fixture", "sentinel.txt")
		if err := os.MkdirAll(filepath.Dir(dependency), 0o700); err != nil {
			test.Fatal(err)
		}
		for name, contents := range map[string]string{
			filepath.Join(path, ".env"): "TREECLEAR_SYNTHETIC_FIXTURE=not-a-secret\n" + filepath.Base(path) + "\n",
			dependency:                  "synthetic dependency bytes\x00\xff\n" + filepath.Base(path) + "\n",
		} {
			if err := os.WriteFile(name, []byte(contents), 0o600); err != nil {
				test.Fatal(err)
			}
		}
	}
	tracked := filepath.Join(fixture.dirty, "seed.txt")
	if err := os.WriteFile(tracked, []byte("staged fixture contents\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	repository.Git(test, "-C", fixture.dirty, "add", "--", "seed.txt")
	for path, contents := range map[string]string{
		tracked: "unstaged fixture contents, distinct from the index\n",
		filepath.Join(fixture.dirty, "untracked.txt"): "untracked fixture contents\n",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			test.Fatal(err)
		}
	}
	repository.Git(test, "worktree", "lock", "--reason", "explicit preview sentinel", fixture.locked)
	if status := repository.Git(test, "-C", fixture.ignored, "status", "--porcelain", "--untracked-files=all"); status != "" {
		test.Fatalf("ignored-only fixture is not clean: %s", status)
	}
	ignored := repository.Git(test, "-C", fixture.ignored, "check-ignore", "--", ".env", "node_modules/fixture/sentinel.txt")
	if ignored != ".env\nnode_modules/fixture/sentinel.txt" {
		test.Fatalf("sentinels are not ignored by committed fixture rules: %q", ignored)
	}
	return fixture
}

func (fixture explicitPreviewFixture) protections() map[string]string {
	return map[string]string{
		fixture.repository.Root: "primary_worktree", fixture.current: "current_worktree", fixture.dirty: "dirty",
		fixture.locked: "locked", fixture.active: "active_process", fixture.ignored: "", fixture.unselected: "",
	}
}

func checkReadOnlyPreview(test *testing.T, value domain.Plan, selectedPaths []string, skipDirty bool, skippedPaths ...string) {
	test.Helper()
	paths := append([]string{}, selectedPaths...)
	sort.Strings(paths)
	intent := "inventory-preview"
	if len(paths) != 0 {
		intent = "explicit-worktree-removal"
	}
	wanted := &domain.RemovalPlan{
		Intent: intent, ContentDisposition: "discard-all", BackupMode: "none", SkipDirty: skipDirty,
		SelectedPaths: paths, Execution: "preview-only",
	}
	if value.SchemaVersion != 2 || value.ID == "" || !reflect.DeepEqual(value.Removal, wanted) {
		test.Fatalf("preview policy = schema %d, %#v; want schema 2, %#v", value.SchemaVersion, value.Removal, wanted)
	}
	selected := make(map[string]bool)
	for _, path := range paths {
		selected[path] = true
	}
	skipped := make(map[string]string)
	for _, path := range skippedPaths {
		skipped[path] = "dirty"
	}
	var summary domain.PlanSummary
	for _, candidate := range value.Candidates {
		wantedSelection := &domain.CandidateSelection{Selected: selected[candidate.Worktree.Path], SkipReason: skipped[candidate.Worktree.Path]}
		if candidate.Action != "none" || candidate.Snapshot != (domain.SnapshotPlan{}) || !reflect.DeepEqual(candidate.Selection, wantedSelection) {
			test.Fatalf("preview candidate is actionable, requests backup or changed selection: %#v", candidate)
		}
		delete(selected, candidate.Worktree.Path)
		fingerprint, err := plan.CandidateFingerprint(candidate)
		if err != nil || candidate.ID == "" || candidate.Fingerprint != fingerprint {
			test.Fatalf("preview candidate lost its authenticated identity: %v; %#v", err, candidate)
		}
		switch candidate.Decision.Classification {
		case domain.Safe:
			summary.Safe++
		case domain.Review:
			summary.Review++
		case domain.Protected:
			summary.Protected++
		default:
			test.Fatalf("unknown preview classification: %q", candidate.Decision.Classification)
		}
	}
	if len(selected) != 0 || value.Summary != summary {
		test.Fatalf("preview lost selected paths or changed inventory counts/reclaimable bytes: missing=%v, summary=%#v, want=%#v", selected, value.Summary, summary)
	}
}

func checkPreviewExplanation(test *testing.T, contents []byte, value domain.Plan, candidate domain.Candidate) {
	test.Helper()
	var explained struct {
		SchemaVersion int                 `json:"schemaVersion"`
		PlanID        string              `json:"planId"`
		Removal       *domain.RemovalPlan `json:"removal"`
		Candidate     domain.Candidate    `json:"candidate"`
	}
	if err := json.Unmarshal(contents, &explained); err != nil || explained.SchemaVersion != 2 || explained.PlanID != value.ID || !reflect.DeepEqual(explained.Removal, value.Removal) || !reflect.DeepEqual(explained.Candidate, candidate) {
		test.Fatalf("explain changed recorded evidence or disposal policy: %v; %#v", err, explained)
	}
}

func checkIncompletePreviewProtection(test *testing.T, value domain.Plan) {
	test.Helper()
	if len(value.Warnings) < 2 {
		test.Fatal("incomplete preview omitted collection diagnostics")
	}
	for _, candidate := range value.Candidates {
		unknown := false
		for _, evidence := range candidate.Evidence.Processes {
			if evidence.State == domain.EvidenceUnknown {
				unknown = true
			}
		}
		if candidate.Decision.Classification != domain.Protected || candidate.Action != "none" || !unknown {
			test.Fatalf("incomplete process collection was treated as inactivity: %#v", candidate)
		}
	}
}

func checkPreviewOnlyState(test *testing.T, directory string, planIDs ...string) {
	test.Helper()
	wanted := map[string]bool{".": true, "integrity.key": true, "plans": true}
	for _, identifier := range planIDs {
		wanted[filepath.Join("plans", identifier+".json")] = true
	}
	actual := installedFixtureDigest(test, directory)
	if len(actual) != len(wanted) {
		test.Fatalf("preview state contains missing control files or unexpected backup/recovery artifacts: %v", actual)
	}
	for path := range actual {
		if !wanted[path] {
			test.Fatalf("preview created a non-plan artifact: %q", path)
		}
	}
}
