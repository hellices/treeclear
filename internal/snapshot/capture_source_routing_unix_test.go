//go:build darwin || linux

package snapshot

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hellices/treeclear/internal/git"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestCaptureSourceNativeRejectsBacklinkTraversal(test *testing.T) {
	for _, scenario := range []string{"ordinary-misroute", "absolute-traversal", "relative-traversal"} {
		test.Run(scenario, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			target := repository.AddWorktree(test, "registered-target", "topic/target")
			other := repository.AddWorktree(test, "registered-other", "topic/other")
			for _, worktree := range []string{target, other} {
				repository.Git(test, "-C", worktree, "checkout", "--detach", "HEAD")
			}
			otherExpected := captureNativeInspect(test, repository, other)
			pivot := filepath.Join(other, "owned-pivot")
			if err := os.Mkdir(pivot, 0o700); err != nil {
				test.Fatal(err)
			}
			if err := os.Symlink(pivot, filepath.Join(target, "owned-hop")); err != nil {
				test.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(otherExpected.CommonGitDir, "info", "exclude"), []byte("owned-hop\nowned-pivot\n"), 0o600); err != nil {
				test.Fatal(err)
			}
			expected := captureNativeInspect(test, repository, target)
			client := git.NewClient(nil)
			if control, err := CaptureSource(test.Context(), client, expected, 1<<20); err != nil || reflect.DeepEqual(control, SourceCapture{}) {
				test.Fatalf("legitimate source control: %v", err)
			}
			backlink := filepath.Join(other, ".git")
			if scenario != "ordinary-misroute" {
				backlink = target + "/owned-hop/../.git"
				if scenario == "relative-traversal" {
					base, err := filepath.Rel(otherExpected.AdminDir, target)
					if err != nil {
						test.Fatal(err)
					}
					backlink = base + "/owned-hop/../.git"
				}
			}
			absolute := backlink
			if !filepath.IsAbs(absolute) {
				absolute = otherExpected.AdminDir + "/" + absolute
			}
			physical, err := filepath.EvalSymlinks(absolute)
			if err != nil || physical != filepath.Join(other, ".git") {
				test.Fatalf("fixture backlink must physically identify the other worktree: path=%q error=%v", physical, err)
			}
			if err := os.WriteFile(filepath.Join(otherExpected.AdminDir, "gitdir"), []byte(backlink+"\n"), 0o600); err != nil {
				test.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, ".git"), []byte("gitdir: "+otherExpected.AdminDir+"\n"), 0o600); err != nil {
				test.Fatal(err)
			}
			repository.Git(test, "-C", target, "update-index", "--refresh")
			indexPath := filepath.Join(otherExpected.AdminDir, "index")
			before, err := os.ReadFile(indexPath)
			if err != nil {
				test.Fatal(err)
			}
			beforeInfo, err := os.Stat(indexPath)
			if err != nil {
				test.Fatal(err)
			}
			defer func() {
				after, readErr := os.ReadFile(indexPath)
				afterInfo, statErr := os.Stat(indexPath)
				if err := errors.Join(readErr, statErr); err != nil {
					test.Fatal(err)
				}
				if !bytes.Equal(before, after) || !os.SameFile(beforeInfo, afterInfo) || beforeInfo.Mode() != afterInfo.Mode() || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
					test.Error("backlink refusal changed borrowed index bytes, identity, mode or mtime")
				}
			}()
			listed, err := client.ListWorktrees(test.Context(), repository.Root)
			if err != nil {
				test.Fatal(err)
			}
			found := false
			for _, record := range listed {
				if record.Path != target {
					continue
				}
				if record.Prunable {
					test.Fatal("fixture target must remain registered and non-prunable")
				}
				found = true
				record.PathSafe = true
				inspected, inspectErr := client.InspectWorktree(test.Context(), repository.Root, record)
				if inspectErr == nil {
					expected = inspected
					test.Error("backlink traversal passed fresh administrative inspection")
				} else if !errors.Is(inspectErr, git.ErrWorktreeChanged) || inspected.GitStateKnown || inspected.PathSafe || inspected.IndexHash != "" || inspected.AdminHash != "" {
					test.Fatalf("backlink traversal was rejected for the wrong reason or after hashing: %v", inspectErr)
				}
				break
			}
			if !found {
				test.Fatal("fixture target is absent from the native Git registration")
			}
			actual, err := CaptureSource(test.Context(), client, expected, 1<<20)
			if !errors.Is(err, ErrSourceInvalid) || !errors.Is(err, ErrSourceChanged) || !reflect.DeepEqual(actual, SourceCapture{}) {
				test.Errorf("wrong-admin source capture succeeded or lost its changed category: bytes=%d error=%v", captureNativeBytes(actual), err)
			}
		})
	}
}
