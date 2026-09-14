//go:build darwin || linux || windows

package snapshot

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/git"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestVerifyBundleNativeGitComposition(test *testing.T) {
	for _, scenario := range []struct {
		name    string
		symlink bool
	}{
		{"portable", false},
		{"native_symlink", true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			if scenario.symlink && runtime.GOOS == "windows" {
				test.Skip("native symlink fixture requires Unix semantics; portable bundle coverage runs separately on Windows")
			}
			const bundleBudget = 1 << 20
			repository := testutil.NewRepository(test)
			for name, contents := range map[string][]byte{
				".gitignore":  []byte("ignored.tmp\nignored-tree/\n"),
				"tracked.bin": bytes.Repeat([]byte{0, 1, 0xff, '\r', '\n'}, 257),
			} {
				if err := os.WriteFile(filepath.Join(repository.Root, name), contents, 0o600); err != nil {
					test.Fatal(err)
				}
			}
			repository.Git(test, "add", "--", ".gitignore", "tracked.bin")
			repository.Git(test, "commit", "-m", "Add native bundle fixtures")
			worktree := repository.AddWorktree(test, "bundle verification", "topic/bundle-verification")
			if err := os.WriteFile(filepath.Join(worktree, "tracked.bin"), bytes.Repeat([]byte{0, 2, 0xfe, '\n'}, 257), 0o600); err != nil {
				test.Fatal(err)
			}
			repository.Git(test, "-C", worktree, "add", "--", "tracked.bin")
			for _, directory := range []string{"assets/nested", "ignored-tree"} {
				if err := os.MkdirAll(filepath.Join(worktree, filepath.FromSlash(directory)), 0o750); err != nil {
					test.Fatal(err)
				}
			}
			payload := bytes.Repeat([]byte{0, 4, 0xfc, '\r', '\n'}, 129)
			notes := []byte("owned Task8G fixture notes\r\n")
			for name, contents := range map[string][]byte{
				"tracked.bin":                        bytes.Repeat([]byte{0, 3, 0xfd, '\r'}, 257),
				"assets/nested/original payload.bin": payload,
				"notes.txt":                          notes,
				"ignored.tmp":                        []byte("ignored fixture\n"),
				"assets/ignored.tmp":                 []byte("ignored nested fixture\n"),
				"ignored-tree/cache.bin":             {0, 5, 0xfb},
			} {
				if err := os.WriteFile(filepath.Join(worktree, filepath.FromSlash(name)), contents, 0o600); err != nil {
					test.Fatal(err)
				}
			}
			for _, name := range []string{"ignored.tmp", "assets/ignored.tmp", "ignored-tree/cache.bin"} {
				if actual := repository.Git(test, "-C", worktree, "check-ignore", "--", name); actual != name {
					test.Fatalf("ignored control %q is not ignored: %q", name, actual)
				}
			}
			wantedPaths := []string{"assets/nested/original payload.bin", "notes.txt"}
			if scenario.symlink {
				if err := os.Symlink("../missing.bin", filepath.Join(worktree, "assets", "nested", "missing-link")); err != nil {
					test.Fatal(err)
				}
				wantedPaths = append(wantedPaths, "assets/nested/missing-link")
				slices.Sort(wantedPaths)
			}
			wantedNames := append(slices.Clone(wantedPaths), "assets", "assets/nested")
			wanted := untrackedReadIntegrationEntries(test, worktree, wantedNames)
			untrackedBytes := int64(len(payload) + len(notes))
			sourcesBefore := verifyBundleNativeSourceEntries(test, worktree)
			sourceManifest := gitManifestFixture(test, repository, worktree)
			indexPath := filepath.Join(sourceManifest.AdminDir, "index")
			indexBefore, err := os.ReadFile(indexPath)
			if err != nil {
				test.Fatal(err)
			}
			test.Cleanup(func() {
				if after := verifyBundleNativeSourceEntries(test, worktree); !reflect.DeepEqual(after, sourcesBefore) {
					test.Error("bundle composition changed source paths, bytes, modes or symlink targets, including ignored files")
				}
				if after := gitManifestFixture(test, repository, worktree); !reflect.DeepEqual(after, sourceManifest) {
					test.Error("bundle composition changed Git identities or administrative paths, modes or bytes")
				}
				if after, err := os.ReadFile(indexPath); err != nil || !bytes.Equal(after, indexBefore) {
					test.Errorf("bundle composition changed the original Git index: %v", err)
				}
			})
			runner := &statusSelectionNativeRunner{}
			client := git.NewClient(runner)
			worktrees, worktreeRaw, err := client.ListWorktreesRaw(test.Context(), repository.Root)
			if err != nil || len(worktrees) != 2 {
				test.Fatalf("native worktree collection = %#v, %v", worktrees, err)
			}
			listed := worktrees[1]
			if !worktrees[0].Primary || worktrees[0].Path != repository.Root || listed.Primary || listed.Path != worktree || listed.Head != sourceManifest.Head || listed.Branch != sourceManifest.Branch || listed.CommonGitDir != sourceManifest.CommonGitDir {
				test.Fatalf("native worktree identities differ from owned fixture: %#v", worktrees)
			}
			selection, err := client.StatusSnapshot(test.Context(), worktree)
			wantedStatus := domain.GitStatus{Staged: 1, Unstaged: 1, Untracked: len(wantedPaths)}
			if err != nil || selection.Status != wantedStatus || !slices.Equal(selection.UntrackedPaths, wantedPaths) {
				test.Fatalf("native status selection = %#v, %v; want %#v and %q", selection, err, wantedStatus, wantedPaths)
			}
			if len(runner.statusOutputs) != 1 || !bytes.Equal(selection.Raw, runner.statusOutputs[0]) {
				test.Fatal("status selection did not preserve the single native Git output")
			}
			staged, err := client.Diff(test.Context(), worktree, true)
			if err != nil || !bytes.Contains(staged, []byte("GIT binary patch")) {
				test.Fatalf("staged binary patch = %q, %v", staged, err)
			}
			unstaged, err := client.Diff(test.Context(), worktree, false)
			if err != nil || !bytes.Contains(unstaged, []byte("GIT binary patch")) || bytes.Equal(staged, unstaged) {
				test.Fatalf("distinct unstaged binary patch = %q, %v", unstaged, err)
			}
			var collected []string
			for _, command := range runner.commands {
				if command == "worktree" || command == "status" || command == "diff" {
					collected = append(collected, command)
				}
			}
			if !slices.Equal(collected, []string{"worktree", "status", "diff", "diff"}) {
				test.Fatalf("native payload collection commands = %q", collected)
			}
			entries, err := ReadUntracked(test.Context(), worktree, selection.UntrackedPaths, untrackedBytes)
			if err != nil || !reflect.DeepEqual(entries, wanted) {
				test.Fatalf("Git-selected bytes, native modes, parents or symlink targets changed: got %#v; want %#v; error %v", entries, wanted, err)
			}
			archive, err := EncodeUntracked(entries, bundleBudget)
			if err != nil {
				test.Fatal(err)
			}
			assertUntrackedStandardArchive(test, archive, wanted, bundleBudget)
			if decoded, err := DecodeUntracked(archive, bundleBudget); err != nil || !reflect.DeepEqual(decoded, wanted) {
				test.Fatalf("native archive round trip changed collected entries: %#v, %v", decoded, err)
			}
			payloads := map[string][]byte{
				"worktree-list.bin": worktreeRaw,
				"status.bin":        selection.Raw,
				"staged.patch":      staged,
				"unstaged.patch":    unstaged,
				"untracked.tar.gz":  archive,
			}
			value := sourceManifest
			value.UntrackedFiles = len(wantedPaths)
			value.UntrackedBytes = untrackedBytes
			value.Files = make(map[string]string, len(payloads))
			for name, contents := range payloads {
				value.Files[name] = fmt.Sprintf("sha256:%x", sha256.Sum256(contents))
			}
			corrupt := bytes.Clone(archive)
			corrupt[len(corrupt)-1] ^= 0xff
			if decoded, err := DecodeUntracked(corrupt, bundleBudget); !errors.Is(err, ErrUntrackedInvalid) || decoded != nil {
				test.Fatalf("corrupt archive control = %#v, %v", decoded, err)
			}
			corruptPayloads := maps.Clone(payloads)
			corruptPayloads["untracked.tar.gz"] = corrupt
			corruptManifest := value
			corruptManifest.Files = maps.Clone(value.Files)
			corruptManifest.Files["untracked.tar.gz"] = fmt.Sprintf("sha256:%x", sha256.Sum256(corrupt))
			directoryCount := value
			directoryCount.UntrackedFiles = len(wanted)
			byteCount := value
			byteCount.UntrackedBytes++
			for _, bundle := range []struct {
				name      string
				manifest  Manifest
				payloads  map[string][]byte
				wantError error
			}{
				{"valid", value, payloads, nil},
				{"rehashed_corrupt_archive", corruptManifest, corruptPayloads, ErrUntrackedInvalid},
				{"directories_counted_as_leaves", directoryCount, payloads, ErrBundleInvalid},
				{"contradictory_regular_bytes", byteCount, payloads, ErrBundleInvalid},
			} {
				test.Run(bundle.name, func(test *testing.T) {
					contents, err := EncodeManifest(bundle.manifest)
					if err != nil {
						test.Fatalf("prepare canonical bundle manifest: %v", err)
					}
					loaded, err := DecodeManifest(contents)
					if err != nil || !reflect.DeepEqual(loaded, bundle.manifest) {
						test.Fatalf("native manifest observations did not round trip: %v", err)
					}
					if err := VerifyPayloads(loaded, bundle.payloads); err != nil {
						test.Fatalf("matching-hash control must pass before whole-bundle verification: %v", err)
					}
					manifestBefore := bytes.Clone(contents)
					payloadsBefore := make(map[string][]byte, len(bundle.payloads))
					for name, payload := range bundle.payloads {
						payloadsBefore[name] = bytes.Clone(payload)
					}
					err = VerifyBundle(contents, bundle.payloads, bundleBudget)
					if bundle.wantError == nil {
						if err != nil {
							test.Errorf("valid native bundle rejected: %v", err)
						}
					} else if !errors.Is(err, ErrBundleInvalid) || !errors.Is(err, bundle.wantError) || errors.Is(err, ErrBundleLimit) {
						test.Errorf("VerifyBundle error = %v; want ErrBundleInvalid and %v, not ErrBundleLimit", err, bundle.wantError)
					}
					if !bytes.Equal(contents, manifestBefore) || !reflect.DeepEqual(bundle.payloads, payloadsBefore) {
						test.Error("whole-bundle verification mutated caller-owned manifest or native payloads")
					}
				})
			}
		})
	}
}

func verifyBundleNativeSourceEntries(test *testing.T, directory string) []UntrackedEntry {
	test.Helper()
	var names []string
	err := filepath.WalkDir(directory, func(filename string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		relative, err := filepath.Rel(directory, filename)
		if err != nil {
			return err
		}
		names = append(names, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		test.Fatal(err)
	}
	return untrackedReadIntegrationEntries(test, directory, names)
}
