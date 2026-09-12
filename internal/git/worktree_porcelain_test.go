package git

import (
	"reflect"
	"testing"
)

func TestParseWorktreePorcelainZ(test *testing.T) {
	input := []byte("worktree /tmp/main repo\x00HEAD abc123\x00branch refs/heads/main\x00\x00" +
		"worktree /tmp/feature\nname\x00HEAD def456\x00branch refs/heads/feature\x00locked agent active\x00\x00")
	actual, err := parseWorktreePorcelainZ(input)
	want := []rawWorktree{
		{Path: "/tmp/main repo", Head: "abc123", Branch: "main"},
		{Path: "/tmp/feature\nname", Head: "def456", Branch: "feature", Locked: true, LockReason: "agent active"},
	}
	if err != nil || !reflect.DeepEqual(actual, want) {
		test.Fatalf("parsed = %#v, error = %v", actual, err)
	}
}

func TestParseWorktreeDetachedAndPrunable(test *testing.T) {
	actual, err := parseWorktreePorcelainZ([]byte("worktree /tmp/wt\x00HEAD abc123\x00detached\x00prunable missing path\x00\x00"))
	if err != nil || len(actual) != 1 || !actual[0].Detached || !actual[0].Prunable {
		test.Fatalf("parsed = %#v, error = %v", actual, err)
	}
}

func TestParseWorktreeRejectsAmbiguousRecords(test *testing.T) {
	for _, input := range []string{
		"worktree /tmp/wt\x00HEAD abc123\x00branch refs/heads/topic\x00",
		"HEAD abc123\x00\x00",
		"worktree /tmp/wt\x00HEAD abc123\x00locked\x00locked\x00\x00",
		"worktree /tmp/wt\x00HEAD abc123\x00unknown x\x00\x00",
		"worktree /tmp/wt\x00HEAD abc123\x00branch refs/heads/topic\x00detached\x00\x00",
	} {
		if actual, err := parseWorktreePorcelainZ([]byte(input)); err == nil {
			test.Fatalf("accepted %q as %#v", input, actual)
		}
	}
}
