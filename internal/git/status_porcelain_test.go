package git

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
)

func TestParseStatusPorcelainZ(test *testing.T) {
	objectID := strings.Repeat("a", 40)
	otherID := strings.Repeat("b", 40)
	input := "1 M. N... 100644 100644 100644 " + objectID + " " + otherID + " staged file\x00" +
		"1 .M N... 100644 100644 100644 " + objectID + " " + otherID + " changed\nfile\x00" +
		"2 R. N... 100644 100644 100644 " + objectID + " " + otherID + " R100 new name\x00old name\x00" +
		"u UU N... 100644 100644 100644 100644 " + objectID + " " + otherID + " " + objectID + " conflict\x00" +
		"? untracked\x00! ignored\x00"
	actual, err := parseStatusPorcelainZ([]byte(input))
	if err != nil || actual.Staged != 2 || actual.Unstaged != 1 || actual.Unmerged != 1 || actual.Untracked != 1 {
		test.Fatalf("status = %#v, error = %v", actual, err)
	}
}

func TestParseStatusEmptyIsClean(test *testing.T) {
	actual, err := parseStatusPorcelainZ(nil)
	if err != nil || !actual.Clean() {
		test.Fatalf("status = %#v, error = %v", actual, err)
	}
}

func TestParseStatusProtectsDirtySubmodules(test *testing.T) {
	objectID := strings.Repeat("a", 40)
	actual, err := parseStatusPorcelainZ([]byte("1 .. S.MU 160000 160000 160000 " + objectID + " " + objectID + " module\x00"))
	if err != nil || actual.Clean() {
		test.Fatalf("dirty submodule status = %#v, error = %v", actual, err)
	}
}

func TestParseStatusRejectsMalformedRecords(test *testing.T) {
	objectID := strings.Repeat("a", 40)
	for _, input := range []string{
		"? missing terminator", "? \x00", "\x00", "unexpected\x00", "1 M. short\x00",
		"1 XX N... 100644 100644 100644 " + objectID + " " + objectID + " name\x00",
		"2 R. N... 100644 100644 100644 " + objectID + " " + objectID + " R100 new\x00",
	} {
		if actual, err := parseStatusPorcelainZ([]byte("? earlier\x00" + input)); err == nil || actual != (domain.GitStatus{}) {
			test.Fatalf("malformed %q returned %#v, error = %v", input, actual, err)
		}
	}
}

func TestParseStatusRejectsMalformedMetadata(test *testing.T) {
	objectID := strings.Repeat("a", 40)
	formats := []struct {
		name       string
		fields     []string
		modeFields []int
		idFields   []int
		suffix     string
	}{
		{"ordinary", []string{"1", "M.", "N...", "100644", "100644", "100644", objectID, objectID, "name"}, []int{3, 4, 5}, []int{6, 7}, "\x00"},
		{"rename", []string{"2", "R.", "N...", "100644", "100644", "100644", objectID, objectID, "R100", "name"}, []int{3, 4, 5}, []int{6, 7}, "\x00old name\x00"},
		{"unmerged", []string{"u", "UU", "N...", "100644", "100644", "100644", "100644", objectID, objectID, objectID, "name"}, []int{3, 4, 5, 6}, []int{7, 8, 9}, "\x00"},
	}
	for _, format := range formats {
		test.Run(format.name, func(test *testing.T) {
			for _, fieldIndex := range format.modeFields {
				for _, mode := range []string{"", "0", "10064", "1006440", "100648", "100abc", "+00644", "100600", "100664", "140000", "040755", "160001", "120644"} {
					test.Run(fmt.Sprintf("mode-%d/%q", fieldIndex, mode), func(test *testing.T) {
						fields := append([]string(nil), format.fields...)
						fields[fieldIndex] = mode
						input := "? earlier\x00" + strings.Join(fields, " ") + format.suffix
						if actual, err := parseStatusPorcelainZ([]byte(input)); err == nil || actual != (domain.GitStatus{}) {
							test.Fatalf("invalid mode %q returned %#v, error = %v", mode, actual, err)
						}
					})
				}
			}
			for _, fieldIndex := range format.idFields {
				for _, invalidID := range []string{
					"", "abc", strings.Repeat("a", 39), strings.Repeat("a", 41), strings.Repeat("a", 63), strings.Repeat("a", 65),
					strings.Repeat("a", 39) + "g", strings.Repeat("b", 63) + "g", strings.Repeat("a", 39) + "\xff", strings.Repeat("é", 20),
					strings.Repeat("b", 64),
				} {
					test.Run(fmt.Sprintf("object-%d/%q", fieldIndex, invalidID), func(test *testing.T) {
						fields := append([]string(nil), format.fields...)
						fields[fieldIndex] = invalidID
						input := "? earlier\x00" + strings.Join(fields, " ") + format.suffix
						if actual, err := parseStatusPorcelainZ([]byte(input)); err == nil || actual != (domain.GitStatus{}) {
							test.Fatalf("invalid object ID %q returned %#v, error = %v", invalidID, actual, err)
						}
					})
				}
			}
		})
	}
}

func TestParseStatusValidatesDirectoryModeRoles(test *testing.T) {
	for _, width := range []int{40, 64} {
		objectID := strings.Repeat("a", width)
		for _, format := range []struct {
			name      string
			fields    []string
			modeRoles []string
			suffix    string
			want      domain.GitStatus
		}{
			{"ordinary", []string{"1", ".T", "N...", "100644", "100644", "100644", objectID, objectID, "name"}, []string{"head", "index", "worktree"}, "\x00", domain.GitStatus{Unstaged: 1}},
			{"rename", []string{"2", "RT", "N...", "100644", "100644", "100644", objectID, objectID, "R100", "name"}, []string{"head", "index", "worktree"}, "\x00old name\x00", domain.GitStatus{Staged: 1, Unstaged: 1}},
			{"copy", []string{"2", "CT", "N...", "100644", "100644", "100644", objectID, objectID, "C100", "name"}, []string{"head", "index", "worktree"}, "\x00old name\x00", domain.GitStatus{Staged: 1, Unstaged: 1}},
			{"unmerged", []string{"u", "UU", "N...", "100644", "100644", "100644", "100644", objectID, objectID, objectID, "name"}, []string{"stage-1", "stage-2", "stage-3", "worktree"}, "\x00", domain.GitStatus{Unmerged: 1}},
		} {
			for modeOffset, role := range format.modeRoles {
				test.Run(fmt.Sprintf("object-width-%d/%s/%s", width, format.name, role), func(test *testing.T) {
					fields := append([]string(nil), format.fields...)
					fields[3+modeOffset] = "040000"
					input := "? earlier\x00" + strings.Join(fields, " ") + format.suffix
					actual, err := parseStatusPorcelainZ([]byte(input))
					if role != "worktree" {
						if err == nil || actual != (domain.GitStatus{}) {
							test.Fatalf("directory mode in %s returned %#v, error = %v", role, actual, err)
						}
						return
					}
					want := format.want
					want.Untracked++
					if err != nil || actual != want {
						test.Fatalf("worktree directory mode returned %#v, want %#v, error = %v", actual, want, err)
					}
				})
			}
		}
	}
}

func TestParseStatusRejectsMalformedRecordGrammar(test *testing.T) {
	objectID := strings.Repeat("a", 40)
	metadata := "N... 100644 100644 100644 " + objectID + " " + objectID
	unmerged := "N... 100644 100644 100644 100644 " + objectID + " " + objectID + " " + objectID
	for _, entry := range []struct {
		name  string
		input string
	}{
		{"ordinary-unmerged", "1 UU " + metadata + " name\x00"},
		{"ordinary-rename", "1 R. " + metadata + " name\x00"},
		{"ordinary-worktree-rename", "1 .R " + metadata + " name\x00"},
		{"ordinary-copy", "1 C. " + metadata + " name\x00"},
		{"ordinary-worktree-copy", "1 .C " + metadata + " name\x00"},
		{"ordinary-clean", "1 .. " + metadata + " name\x00"},
		{"ordinary-clean-submodule", "1 .. S... 160000 160000 160000 " + objectID + " " + objectID + " module\x00"},
		{"rename-without-rename-status", "2 M. " + metadata + " R100 name\x00old\x00"},
		{"rename-clean", "2 .. " + metadata + " R100 name\x00old\x00"},
		{"rename-wrong-score-kind", "2 R. " + metadata + " C100 name\x00old\x00"},
		{"copy-wrong-score-kind", "2 C. " + metadata + " R100 name\x00old\x00"},
		{"worktree-rename-wrong-score-kind", "2 .R " + metadata + " C100 name\x00old\x00"},
		{"rename-double-status", "2 RR " + metadata + " R100 name\x00old\x00"},
		{"rename-conflicting-status", "2 RC " + metadata + " R100 name\x00old\x00"},
		{"rename-unmerged-status", "2 RU " + metadata + " R100 name\x00old\x00"},
		{"rename-invalid-score-kind", "2 R. " + metadata + " X100 name\x00old\x00"},
		{"rename-empty-source", "2 R. " + metadata + " R100 name\x00\x00"},
		{"unmerged-ordinary-status", "u M. " + unmerged + " name\x00"},
		{"unmerged-rename-status", "u R. " + unmerged + " name\x00"},
		{"unmerged-clean-status", "u .. " + unmerged + " name\x00"},
		{"unmerged-incomplete-status", "u .U " + unmerged + " name\x00"},
		{"unmerged-impossible-status", "u MA " + unmerged + " name\x00"},
		{"invalid-submodule", "1 M. S.X. 160000 160000 160000 " + objectID + " " + objectID + " name\x00"},
		{"invalid-non-submodule", "1 M. N..U 100644 100644 100644 " + objectID + " " + objectID + " name\x00"},
	} {
		test.Run(entry.name, func(test *testing.T) {
			if actual, err := parseStatusPorcelainZ([]byte("? earlier\x00" + entry.input)); err == nil || actual != (domain.GitStatus{}) {
				test.Fatalf("malformed record returned %#v, error = %v", actual, err)
			}
		})
	}
}

func TestParseStatusRejectsMalformedRenameScores(test *testing.T) {
	objectID := strings.Repeat("a", 40)
	for _, kind := range []string{"R", "C"} {
		for _, score := range []string{"", "+0", "+1", "-0", "-1", "101", "999", "01", "000", "001", "1000", "1.0", "1e1", "1\t", "10\n", "one", "١", "999999999999999999999"} {
			test.Run(fmt.Sprintf("%s%q", kind, score), func(test *testing.T) {
				input := "? earlier\x00" + "2 " + kind + ". N... 100644 100644 100644 " + objectID + " " + objectID + " " + kind + score + " new\x00old\x00"
				if actual, err := parseStatusPorcelainZ([]byte(input)); err == nil || actual != (domain.GitStatus{}) {
					test.Fatalf("invalid score %q returned %#v, error = %v", kind+score, actual, err)
				}
			})
		}
	}
}

func TestParseStatusAcceptsValidMetadata(test *testing.T) {
	for _, width := range []int{40, 64} {
		test.Run(fmt.Sprintf("object-width-%d", width), func(test *testing.T) {
			objectID := strings.Repeat("a", width)
			otherID := strings.Repeat("ABcd0123", width/8)
			zeroID := strings.Repeat("0", width)
			for _, entry := range []struct {
				name     string
				metadata string
				want     domain.GitStatus
			}{
				{"ordinary", "M. N... 100644 100644 100644 " + objectID + " " + otherID, domain.GitStatus{Staged: 1}},
				{"executable", ".M N... 100644 100644 100755 " + objectID + " " + objectID, domain.GitStatus{Unstaged: 1}},
				{"symlink", "T. N... 100644 120000 120000 " + objectID + " " + otherID, domain.GitStatus{Staged: 1}},
				{"directory-mode", ".T N... 100644 100644 040000 " + objectID + " " + objectID, domain.GitStatus{Unstaged: 1}},
				{"added", "A. N... 000000 100644 100644 " + zeroID + " " + objectID, domain.GitStatus{Staged: 1}},
				{"deleted", "D. N... 100644 000000 000000 " + objectID + " " + zeroID, domain.GitStatus{Staged: 1}},
				{"unborn-intent-to-add", ".A N... 000000 000000 100644 " + zeroID + " " + zeroID, domain.GitStatus{Unstaged: 1}},
				{"tracked-intent-to-add", "DA N... 100644 000000 100644 " + objectID + " " + zeroID, domain.GitStatus{Staged: 1, Unstaged: 1}},
				{"missing-intent-to-add", "DD N... 100644 100644 000000 " + objectID + " " + otherID, domain.GitStatus{Staged: 1, Unstaged: 1}},
			} {
				test.Run(entry.name, func(test *testing.T) {
					actual, err := parseStatusPorcelainZ([]byte("1 " + entry.metadata + " name\x00"))
					if err != nil || actual != entry.want {
						test.Fatalf("status = %#v, want %#v, error = %v", actual, entry.want, err)
					}
				})
			}
			for _, code := range []string{"R.", ".R", "RM", "C.", ".C", "CM"} {
				for _, score := range []string{"0", "1", "9", "10", "50", "99", "100"} {
					test.Run(code+score, func(test *testing.T) {
						kind := strings.Trim(code, ".M")
						input := "2 " + code + " N... 100644 100644 100644 " + objectID + " " + otherID + " " + kind + score + " new\x00old\x00"
						want := domain.GitStatus{}
						if code[0] != '.' {
							want.Staged = 1
						}
						if code[1] != '.' {
							want.Unstaged = 1
						}
						if actual, err := parseStatusPorcelainZ([]byte(input)); err != nil || actual != want {
							test.Fatalf("status = %#v, want %#v, error = %v", actual, want, err)
						}
					})
				}
			}
		})
	}
}

func TestParseStatusAcceptsUnmergedStages(test *testing.T) {
	for _, width := range []int{40, 64} {
		for _, entry := range []struct {
			code string
			mask int
		}{
			{"DD", 1},
			{"AU", 2},
			{"UD", 3},
			{"UA", 4},
			{"DU", 5},
			{"AA", 6},
			{"UU", 7},
		} {
			test.Run(fmt.Sprintf("object-width-%d/%s", width, entry.code), func(test *testing.T) {
				zeroID := strings.Repeat("0", width)
				fields := []string{"u", entry.code, "N...", "000000", "000000", "000000", "100644", zeroID, zeroID, zeroID, "conflict"}
				for stage := 0; stage < 3; stage++ {
					if entry.mask&(1<<stage) != 0 {
						fields[3+stage] = "100644"
						fields[7+stage] = strings.Repeat("a", width)
					}
				}
				for _, worktreeMode := range []string{"100644", "000000"} {
					fields[6] = worktreeMode
					if actual, err := parseStatusPorcelainZ([]byte(strings.Join(fields, " ") + "\x00")); err != nil || actual != (domain.GitStatus{Unmerged: 1}) {
						test.Fatalf("unmerged status = %#v, error = %v", actual, err)
					}
				}
			})
		}
	}
}

func TestParseStatusAcceptsSubmoduleCodes(test *testing.T) {
	objectID := strings.Repeat("a", 40)
	for _, code := range []string{"S...", "SC..", "S.M.", "S..U", "SCM.", "SC.U", "S.MU", "SCMU"} {
		test.Run(code, func(test *testing.T) {
			statusCode := ".."
			want := domain.GitStatus{Unstaged: 1}
			if code == "S..." {
				statusCode = "M."
				want = domain.GitStatus{Staged: 1}
			}
			input := "1 " + statusCode + " " + code + " 160000 160000 160000 " + objectID + " " + objectID + " module\x00"
			if actual, err := parseStatusPorcelainZ([]byte(input)); err != nil || actual != want {
				test.Fatalf("submodule status = %#v, want %#v, error = %v", actual, want, err)
			}
		})
	}
}

func TestParseStatusPreservesNULPathFraming(test *testing.T) {
	objectID := strings.Repeat("a", 40)
	metadata := "N... 100644 100644 100644 " + objectID + " " + objectID
	path := " \tname\n\xff\xfe trailing "
	for _, entry := range []struct {
		name  string
		input string
		want  domain.GitStatus
	}{
		{"ordinary", "1 M. " + metadata + " " + path + "\x00", domain.GitStatus{Staged: 1}},
		{"whitespace-only", "1 M. " + metadata + "  \x00", domain.GitStatus{Staged: 1}},
		{"rename", "2 R. " + metadata + " R100 " + path + "\x00? source\n\xff\t \x00", domain.GitStatus{Staged: 1}},
		{"copy", "2 C. " + metadata + " C100 " + path + "\x00! source\n\xfe\t \x00", domain.GitStatus{Staged: 1}},
		{"unmerged", "u UU N... 100644 100644 100644 100644 " + objectID + " " + objectID + " " + objectID + " " + path + "\x00", domain.GitStatus{Unmerged: 1}},
		{"untracked", "? " + path + "\x00", domain.GitStatus{Untracked: 1}},
		{"ignored", "! " + path + "\x00", domain.GitStatus{}},
	} {
		test.Run(entry.name, func(test *testing.T) {
			want := entry.want
			want.Untracked++
			if actual, err := parseStatusPorcelainZ([]byte(entry.input + "? following\x00")); err != nil || actual != want {
				test.Fatalf("status = %#v, want %#v, error = %v", actual, want, err)
			}
		})
	}
}
