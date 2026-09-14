package git

import (
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
)

func FuzzParseStatusPorcelainZ(fuzz *testing.F) {
	objectID := strings.Repeat("a", 40)
	for _, input := range []string{
		"", "? new\n\xff name \x00", "! ignored\x00", "? earlier\x00broken\x00",
		"1 M. N... 100644 100644 100644 " + objectID + " " + objectID + " file\x00",
		"2 R. N... 100644 100644 100644 " + objectID + " " + objectID + " R100 new\x00old\x00",
		"2 C. N... 100644 100644 100644 " + objectID + " " + objectID + " C100 new\x00? source\x00? genuine\x00",
		"u UU N... 100644 100644 100644 100644 " + objectID + " " + objectID + " " + objectID + " file\x00",
	} {
		fuzz.Add([]byte(input))
	}
	fuzz.Fuzz(func(test *testing.T, contents []byte) {
		status, err := parseStatusPorcelainZ(contents)
		if err != nil && status != (domain.GitStatus{}) {
			test.Fatalf("failed status parse returned partial counts: %#v, %v", status, err)
		}
		selectedStatus, paths, selectedErr := parseStatusPorcelainZPaths(contents, true)
		if selectedErr != nil && (selectedStatus != (domain.GitStatus{}) || paths != nil) {
			test.Fatalf("failed selection returned partial counts/paths: %#v, %d, %v", selectedStatus, len(paths), selectedErr)
		}
		if err != nil || status.Untracked > 4096 {
			if selectedErr == nil {
				test.Fatal("selection accepted malformed or over-limit status")
			}
		} else if selectedErr != nil || selectedStatus != status || len(paths) != status.Untracked {
			test.Fatalf("selection disagrees with legacy summary: %#v, %d paths, %v; legacy %#v", selectedStatus, len(paths), selectedErr, status)
		}
	})
}

func FuzzParseWorktreePorcelainZ(fuzz *testing.F) {
	objectID := strings.Repeat("a", 40)
	for _, input := range []string{
		"", "worktree /tmp/main\x00bare\x00\x00", "worktree /tmp/main\x00HEAD partial\x00\x00",
		"worktree /tmp/main\x00HEAD " + objectID + "\x00branch refs/heads/main\x00\x00",
		"worktree /tmp/\n\xff path \x00HEAD " + objectID + "\x00detached\x00locked active\nagent\x00\x00",
		"worktree /tmp/unborn\x00HEAD " + strings.Repeat("0", 64) + "\x00branch refs/heads/unborn\x00\x00",
	} {
		fuzz.Add([]byte(input))
	}
	fuzz.Fuzz(func(test *testing.T, contents []byte) {
		worktrees, err := parseWorktreePorcelainZ(contents)
		if err != nil && worktrees != nil {
			test.Fatalf("failed inventory parse returned partial records: %#v, %v", worktrees, err)
		}
		if err == nil && len(worktrees) == 0 {
			test.Fatal("empty inventory was accepted")
		}
	})
}
