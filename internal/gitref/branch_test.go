package gitref

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidBranchNameAcceptsSupportedNames(test *testing.T) {
	for _, entry := range []struct{ name, branch string }{
		{"simple", "main"},
		{"nested", "release/topic"},
		{"at", "@"},
		{"unicode", "topic/한글-café"},
		{"unicode space", "topic\u00a0name"},
		{"punctuation", "topic/a-b_c.d+e,f@g"},
		{"nested dash", "topic/-child"},
		{"nested HEAD", "topic/HEAD"},
		{"lowercase head", "head"},
		{"component trailing dot", "topic./child"},
		{"uppercase lock suffix", "topic.LOCK"},
		{"lock prefix", "topic.locked"},
		{"ASCII byte limit", strings.Repeat("a", 1024)},
		{"Unicode byte limit", strings.Repeat("é", 512)},
	} {
		test.Run(entry.name, func(test *testing.T) {
			if !ValidBranchName(entry.branch) {
				test.Fatalf("supported branch %q was rejected", entry.branch)
			}
		})
	}
}

func TestValidBranchNameRejectsUnsupportedNames(test *testing.T) {
	for _, entry := range []struct{ name, branch string }{
		{"empty", ""},
		{"reserved HEAD", "HEAD"},
		{"leading dash", "-name"},
		{"trailing dot", "topic."},
		{"consecutive dots", "foo..bar"},
		{"reflog expression", "topic@{0}"},
		{"checkout expression", "@{-1}"},
		{"tilde", "topic~1"},
		{"caret", "topic^"},
		{"colon", "topic:name"},
		{"question mark", "topic?"},
		{"asterisk", "topic*"},
		{"opening bracket", "topic[1]"},
		{"backslash", `topic\name`},
		{"leading slash", "/topic"},
		{"trailing slash", "topic/"},
		{"empty component", "topic//child"},
		{"hidden branch", ".topic"},
		{"hidden component", "topic/.child"},
		{"lock suffix", "topic.lock"},
		{"lock component", "topic.lock/child"},
		{"invalid UTF-8", "topic\xff"},
		{"ASCII over byte limit", strings.Repeat("a", 1025)},
		{"Unicode over byte limit", strings.Repeat("é", 512) + "a"},
	} {
		test.Run(entry.name, func(test *testing.T) {
			if ValidBranchName(entry.branch) {
				test.Fatalf("unsupported branch %q was accepted", entry.branch)
			}
		})
	}
}

func TestValidBranchNameRejectsASCIIControlsAndSpace(test *testing.T) {
	for codePoint := 0; codePoint <= 127; codePoint++ {
		if codePoint > 32 && codePoint != 127 {
			continue
		}
		test.Run(fmt.Sprintf("byte-%02x", codePoint), func(test *testing.T) {
			branch := "topic" + string(rune(codePoint)) + "name"
			if ValidBranchName(branch) {
				test.Fatalf("unsupported branch %q was accepted", branch)
			}
		})
	}
}
