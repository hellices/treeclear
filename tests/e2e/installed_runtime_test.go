package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/cli"
	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
)

func TestInstalledPreviewRunsNativeScanPlanExplain(test *testing.T) {
	installation := newInstallFixture(test)
	installation.environment["GOBIN"] = filepath.Join(test.TempDir(), "설치 with spaces", "bin")
	if output, err := installation.make(test, "install", "VERSION=installed-runtime-e2e"); err != nil {
		test.Fatalf("install native preview: %v\n%s", err, output)
	}
	binary := filepath.Join(installation.environment["GOBIN"], "treeclear")
	fixture := newExplicitPreviewFixture(test)
	repository, current := fixture.repository, fixture.current
	home := test.TempDir()
	environment := execx.SanitizedEnvironment(os.Environ(), map[string]string{
		"HOME": home, "USERPROFILE": home, "APPDATA": home, "LOCALAPPDATA": home,
		"XDG_CONFIG_HOME": filepath.Join(home, "config"), "XDG_DATA_HOME": filepath.Join(home, "data"),
		"XDG_CACHE_HOME": filepath.Join(home, "cache"), "PATH": "/usr/bin:/bin:/usr/sbin:/sbin",
		"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_SYSTEM": os.DevNull, "GIT_CONFIG_GLOBAL": os.DevNull,
	})
	sleeper := exec.CommandContext(test.Context(), "/bin/sleep", "300")
	sleeper.Dir, sleeper.Env, sleeper.WaitDelay = fixture.active, environment, time.Second
	if err := sleeper.Start(); err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() {
		_ = sleeper.Process.Kill()
		_ = sleeper.Wait()
	})
	root := filepath.Dir(repository.Root)
	before := installedFixtureDigest(test, root)
	checkUnchanged := func(stage string) {
		test.Helper()
		if !reflect.DeepEqual(before, installedFixtureDigest(test, root)) {
			test.Fatalf("installed %s changed fixture files, indexes, branches or registrations", stage)
		}
	}
	expected := fixture.protections()
	scanOutput, _, scanExit := runInstalledCommand(test, binary, current, environment, "scan", "--root", repository.Root, "--format", "json")
	var scan cli.ScanResult
	if err := json.Unmarshal(scanOutput, &scan); err != nil {
		test.Fatalf("installed scan JSON: %v", err)
	}
	if scan.ToolVersion != "installed-runtime-e2e" || (scanExit == 0) != scan.Complete {
		test.Fatal("installed scan lost its version or incomplete exit status")
	}
	for _, warning := range scan.Warnings {
		if strings.Contains(warning, "invalid process PID 0") {
			test.Fatal("installed Darwin scan misclassified the native kernel task as an invalid user process")
		}
	}
	test.Logf("native scan complete=%v warnings=%d; a partial result is not complete runtime acceptance", scan.Complete, len(scan.Warnings))
	var scanned []domain.Candidate
	for _, item := range scan.Worktrees {
		if !scan.Complete && item.Decision.Classification != domain.Protected {
			test.Error("installed partial scan did not protect every candidate")
		}
		scanned = append(scanned, domain.Candidate{Worktree: item.Worktree, Decision: item.Decision, Evidence: item.Evidence})
	}
	checkInstalledCandidates(test, scanned, expected, sleeper.Process.Pid, false)
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		test.Fatalf("installed scan created state: %v", err)
	}
	checkUnchanged("scan")
	rejectedExport := filepath.Join(test.TempDir(), "rejected-plan.json")
	homeBefore := installedFixtureDigest(test, home)
	for _, request := range []struct {
		arguments  []string
		diagnostic string
	}{
		{[]string{"plan", "--root", repository.Root, "--worktree", fixture.dirty, "--backup", "--format", "json", "--output", rejectedExport}, "backup is not implemented"},
		{[]string{"plan", "--force"}, "unknown flag: --force"},
		{[]string{"plan", "--yes"}, "unknown flag: --yes"},
		{[]string{"apply"}, `unknown command "apply"`},
	} {
		output, diagnostics, exitCode := runInstalledCommand(test, binary, current, environment, request.arguments...)
		diagnostic, err := strconv.Unquote(strings.TrimSpace(string(diagnostics)))
		if err != nil || exitCode != 1 || len(output) != 0 || !strings.Contains(diagnostic, request.diagnostic) {
			test.Fatalf("installed unsupported request was not explicitly refused: exit=%d, stdout=%q, stderr=%q", exitCode, output, diagnostics)
		}
		if !reflect.DeepEqual(homeBefore, installedFixtureDigest(test, home)) {
			test.Fatal("installed rejected request created configuration, plans or state")
		}
		if _, err := os.Stat(rejectedExport); !errors.Is(err, os.ErrNotExist) {
			test.Fatalf("installed rejected backup request wrote an export: %v", err)
		}
		checkUnchanged("rejected request")
	}
	export := filepath.Join(test.TempDir(), "plan.json")
	planOutput, _, planExit := runInstalledCommand(test, binary, current, environment, "plan", "--root", repository.Root, "--format", "json", "--output", export)
	var value domain.Plan
	if err := json.Unmarshal(planOutput, &value); err != nil {
		test.Fatalf("installed plan JSON: %v", err)
	}
	if value.ID == "" || value.ToolVersion != "installed-runtime-e2e" || (planExit == 0) != (len(value.Warnings) == 1) {
		test.Fatal("installed plan lost its identity or failed to return an error for warnings beyond the core-only limitation")
	}
	checkReadOnlyPreview(test, value, nil, false)
	if planExit != 0 {
		checkIncompletePreviewProtection(test, value)
	}
	checkInstalledCandidates(test, value.Candidates, expected, sleeper.Process.Pid, true)
	exported, err := os.ReadFile(export)
	if err != nil || !bytes.Equal(exported, bytes.TrimSuffix(planOutput, []byte("\n"))) {
		test.Fatalf("installed plan export changed canonical bytes: %v", err)
	}
	metadata, err := os.Stat(export)
	if err != nil || metadata.Mode().Perm() != 0o600 {
		test.Fatalf("installed plan export is not private: %v", err)
	}
	checkUnchanged("plan")
	for _, candidate := range value.Candidates {
		output, _, exitCode := runInstalledCommand(test, binary, current, environment, "explain", candidate.ID, "--plan", export, "--format", "json")
		if exitCode != 0 {
			test.Fatal("installed explain could not load an authenticated candidate")
		}
		checkPreviewExplanation(test, output, value, candidate)
	}
	selected := value.Candidates[0]
	if _, _, exitCode := runInstalledCommand(test, binary, current, environment, "explain", selected.ID, "--plan", value.ID, "--format", "json"); exitCode != 0 {
		test.Fatal("installed explain could not load the saved plan ID")
	}
	tampered := value
	tampered.ToolVersion = "changed-without-authentication"
	contents, err := json.Marshal(tampered)
	if err != nil {
		test.Fatal(err)
	}
	tamperedPath := filepath.Join(filepath.Dir(export), "tampered.json")
	if err := os.WriteFile(tamperedPath, contents, 0o600); err != nil {
		test.Fatal(err)
	}
	if output, _, exitCode := runInstalledCommand(test, binary, current, environment, "explain", selected.ID, "--plan", tamperedPath, "--format", "json"); exitCode != 1 || len(output) != 0 {
		test.Fatal("installed explain accepted a modified plan")
	}
	if _, _, exitCode := runInstalledCommand(test, binary, current, environment, "explain", selected.ID, "--plan", export, "--format", "json"); exitCode != 0 {
		test.Fatal("installed explain no longer accepts the original plan")
	}
	checkUnchanged("explain")
	stateDirectory := filepath.Join(home, "Library", "Application Support", "treeclear")
	planIDs := []string{value.ID}
	checkPreviewOnlyState(test, stateDirectory, planIDs...)
	for _, skipDirty := range []bool{false, true} {
		explicitExport := filepath.Join(test.TempDir(), "explicit-plan.json")
		arguments := []string{"plan", "--root", repository.Root, "--format", "json", "--output", explicitExport}
		for _, path := range []string{fixture.dirty, fixture.ignored} {
			relative, err := filepath.Rel(current, path)
			if err != nil {
				test.Fatal(err)
			}
			arguments = append(arguments, "--worktree", relative)
		}
		var skippedPaths []string
		if skipDirty {
			arguments = append(arguments, "--skip-dirty")
			skippedPaths = []string{fixture.dirty}
		}
		output, _, exitCode := runInstalledCommand(test, binary, current, environment, arguments...)
		var preview domain.Plan
		if err := json.Unmarshal(output, &preview); err != nil {
			test.Fatalf("installed explicit preview JSON: %v; %s", err, output)
		}
		if preview.ToolVersion != "installed-runtime-e2e" || (exitCode == 0) != (len(preview.Warnings) == 1) {
			test.Fatal("installed explicit preview lost its version or incomplete-collection exit status")
		}
		checkReadOnlyPreview(test, preview, []string{fixture.dirty, fixture.ignored}, skipDirty, skippedPaths...)
		checkInstalledCandidates(test, preview.Candidates, expected, sleeper.Process.Pid, true)
		if exitCode != 0 {
			checkIncompletePreviewProtection(test, preview)
		}
		test.Logf("native explicit preview skipDirty=%v complete=%v warnings=%d; selection is not removal eligibility", skipDirty, exitCode == 0, len(preview.Warnings))
		exported, err := os.ReadFile(explicitExport)
		if err != nil || !bytes.Equal(exported, bytes.TrimSuffix(output, []byte("\n"))) {
			test.Fatalf("installed explicit export changed canonical plan bytes: %v", err)
		}
		metadata, err := os.Stat(explicitExport)
		if err != nil || metadata.Mode().Perm() != 0o600 {
			test.Fatalf("installed explicit export is not private: %v", err)
		}
		checkUnchanged("explicit plan")
		for _, candidate := range preview.Candidates {
			explanation, _, exitCode := runInstalledCommand(test, binary, current, environment, "explain", candidate.ID, "--plan", preview.ID, "--format", "json")
			if exitCode != 0 {
				test.Fatal("installed explicit explain could not load the saved plan")
			}
			checkPreviewExplanation(test, explanation, preview, candidate)
		}
		planIDs = append(planIDs, preview.ID)
		checkPreviewOnlyState(test, stateDirectory, planIDs...)
		checkUnchanged("explicit explain")
	}
}

func runInstalledCommand(test *testing.T, binary, directory string, environment []string, arguments ...string) ([]byte, []byte, int) {
	test.Helper()
	ctx, cancel := context.WithTimeout(test.Context(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Dir, command.Env, command.WaitDelay = directory, environment, time.Second
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		test.Fatalf("installed %s timed out: %v", arguments[0], ctx.Err())
	}
	exitCode := 0
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
			test.Fatalf("installed %s did not return an ordinary CLI result: %v", arguments[0], err)
		}
		exitCode = exitError.ExitCode()
	}
	test.Logf("installed %s exit=%d stdout=%dB stderr=%dB", arguments[0], exitCode, stdout.Len(), stderr.Len())
	if stderr.Len() > 4096 {
		test.Fatalf("installed %s diagnostics are unbounded: %d bytes", arguments[0], stderr.Len())
	}
	return stdout.Bytes(), stderr.Bytes(), exitCode
}

func checkInstalledCandidates(test *testing.T, candidates []domain.Candidate, expected map[string]string, activePID int, planned bool) {
	test.Helper()
	if len(candidates) != len(expected) {
		test.Fatalf("installed command returned %d candidates, want %d", len(candidates), len(expected))
	}
	seen := make(map[string]bool)
	for _, candidate := range candidates {
		wanted, found := expected[candidate.Worktree.Path]
		if !found || seen[candidate.Worktree.Path] || len(candidate.Decision.Reasons) == 0 {
			test.Fatal("installed command returned duplicate/unexpected candidates or missing reasons")
		}
		seen[candidate.Worktree.Path] = true
		if wanted != "" && (candidate.Decision.Classification != domain.Protected || candidate.Decision.Reasons[0].Code != wanted) {
			test.Errorf("installed protection: got %s/%s, want protected/%s", candidate.Decision.Classification, candidate.Decision.Reasons[0].Code, wanted)
		}
		if planned && (candidate.ID == "" || candidate.Fingerprint == "" || candidate.Action != "none" || candidate.Snapshot != (domain.SnapshotPlan{})) {
			test.Error("installed preview omitted candidate identity or authorized removal or backup")
		}
		if wanted == "dirty" && (candidate.Worktree.Status.Staged < 1 || candidate.Worktree.Status.Unstaged < 1 || candidate.Worktree.Status.Untracked < 1) {
			test.Error("installed command missed staged, unstaged or untracked modifications")
		}
		if wanted == "" && (!candidate.Worktree.GitStateKnown || !candidate.Worktree.Status.Clean()) {
			test.Error("installed command treated ignored-only sentinels as dirty or unknown Git state")
		}
		if wanted == "active_process" {
			observed := false
			for _, evidence := range candidate.Evidence.Processes {
				if int(evidence.PID) == activePID && evidence.State == domain.EvidenceActive {
					observed = true
				}
			}
			if !observed {
				test.Error("installed command did not observe the owned native process")
			}
		}
	}
}

func installedFixtureDigest(test *testing.T, root string) map[string]string {
	test.Helper()
	digests := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		metadata, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		var contents []byte
		if metadata.Mode().IsRegular() {
			contents, err = os.ReadFile(path)
		} else if metadata.Mode()&os.ModeSymlink != 0 {
			var target string
			target, err = os.Readlink(path)
			contents = []byte(target)
		} else if !metadata.IsDir() {
			return fmt.Errorf("unexpected fixture file type: %s", relative)
		}
		if err != nil {
			return err
		}
		digests[relative] = fmt.Sprintf("%s %x", metadata.Mode(), sha256.Sum256(contents))
		return nil
	})
	if err != nil {
		test.Fatal(err)
	}
	return digests
}
