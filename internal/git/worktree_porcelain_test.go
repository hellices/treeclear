package git

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParseWorktreePorcelainZ(test *testing.T) {
	head := strings.Repeat("a", 40)
	otherHead := strings.Repeat("b", 40)
	input := []byte("worktree /tmp/main repo\x00HEAD " + head + "\x00branch refs/heads/main\x00\x00" +
		"worktree /tmp/feature\nname\x00HEAD " + otherHead + "\x00branch refs/heads/feature\x00locked agent active\x00\x00")
	actual, err := parseWorktreePorcelainZ(input)
	want := []rawWorktree{
		{Path: "/tmp/main repo", Head: head, Branch: "main"},
		{Path: "/tmp/feature\nname", Head: otherHead, Branch: "feature", Locked: true, LockReason: "agent active"},
	}
	if err != nil || !reflect.DeepEqual(actual, want) {
		test.Fatalf("parsed = %#v, error = %v", actual, err)
	}
}

func TestParseWorktreeDetachedAndPrunable(test *testing.T) {
	head := strings.Repeat("a", 40)
	actual, err := parseWorktreePorcelainZ([]byte("worktree /tmp/wt\x00HEAD " + head + "\x00detached\x00prunable missing path\x00\x00"))
	if err != nil || len(actual) != 1 || !actual[0].Detached || !actual[0].Prunable {
		test.Fatalf("parsed = %#v, error = %v", actual, err)
	}
}

func TestParseWorktreeRejectsAmbiguousRecords(test *testing.T) {
	head := strings.Repeat("a", 40)
	for _, input := range []string{
		"worktree /tmp/wt\x00HEAD " + head + "\x00branch refs/heads/topic\x00",
		"HEAD " + head + "\x00\x00",
		"worktree /tmp/wt\x00HEAD " + head + "\x00locked\x00locked\x00\x00",
		"worktree /tmp/wt\x00HEAD " + head + "\x00unknown x\x00\x00",
		"worktree /tmp/wt\x00HEAD " + head + "\x00branch refs/heads/topic\x00detached\x00\x00",
	} {
		prefix := "worktree /tmp/earlier\x00HEAD " + head + "\x00branch refs/heads/main\x00\x00"
		if actual, err := parseWorktreePorcelainZ([]byte(prefix + input)); err == nil || actual != nil {
			test.Fatalf("malformed %q returned %#v, error = %v", input, actual, err)
		}
	}
}

func TestParseWorktreeRejectsMalformedHEADs(test *testing.T) {
	prefix := "worktree /tmp/earlier\x00HEAD " + strings.Repeat("a", 40) + "\x00branch refs/heads/main\x00\x00"
	for _, head := range []string{
		"", "abc123", strings.Repeat("a", 39), strings.Repeat("a", 41), strings.Repeat("a", 63), strings.Repeat("a", 65),
		strings.Repeat("a", 39) + "g", strings.Repeat("b", 63) + "g", strings.Repeat("a", 39) + "\xff", strings.Repeat("é", 20),
		strings.Repeat("a", 40) + " ", " " + strings.Repeat("a", 40), strings.Repeat("a", 39) + "\n",
	} {
		for _, branchState := range []string{"branch refs/heads/topic", "detached"} {
			test.Run(fmt.Sprintf("%s/%q", branchState, head), func(test *testing.T) {
				input := prefix + "worktree /tmp/wt\x00HEAD " + head + "\x00" + branchState + "\x00\x00"
				if actual, err := parseWorktreePorcelainZ([]byte(input)); err == nil || actual != nil {
					test.Fatalf("invalid HEAD %q returned %#v, error = %v", head, actual, err)
				}
			})
		}
	}
}

func TestParseWorktreeAcceptsHEADEncodings(test *testing.T) {
	for _, width := range []int{40, 64} {
		for _, head := range []string{strings.Repeat("a", width), strings.Repeat("ABcd0123", width/8), strings.Repeat("0", width)} {
			test.Run(head, func(test *testing.T) {
				input := "worktree /tmp/main\x00HEAD " + head + "\x00branch refs/heads/main\x00\x00"
				want := []rawWorktree{{Path: "/tmp/main", Head: head, Branch: "main"}}
				if actual, err := parseWorktreePorcelainZ([]byte(input)); err != nil || !reflect.DeepEqual(actual, want) {
					test.Fatalf("inventory = %#v, want %#v, error = %v", actual, want, err)
				}
			})
		}
	}
}

func TestParseWorktreePreservesNULPathFraming(test *testing.T) {
	head := strings.Repeat("a", 40)
	path := "/tmp/ \tname\n\xff\xfe trailing "
	reason := " \tactive\n\xff trailing "
	input := "worktree " + path + "\x00HEAD " + head + "\x00branch refs/heads/topic\x00locked " + reason + "\x00\x00" +
		"worktree /tmp/bare\xff repo\x00bare\x00\x00"
	want := []rawWorktree{
		{Path: path, Head: head, Branch: "topic", Locked: true, LockReason: reason},
		{Path: "/tmp/bare\xff repo", Bare: true},
	}
	if actual, err := parseWorktreePorcelainZ([]byte(input)); err != nil || !reflect.DeepEqual(actual, want) {
		test.Fatalf("inventory = %#v, want %#v, error = %v", actual, want, err)
	}
}
