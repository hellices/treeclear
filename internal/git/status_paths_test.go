package git

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
)

func TestParseStatusPathsPreservesExactSelection(test *testing.T) {
	contents, wantStatus, wantPaths := statusPathsMixedFixture()
	before := bytes.Clone(contents)
	status, paths, err := parseStatusPorcelainZPaths(contents, true)
	if err != nil || status != wantStatus || !reflect.DeepEqual(paths, wantPaths) {
		test.Fatalf("status = %#v, paths = %q, error = %v; want %#v, %q", status, paths, err, wantStatus, wantPaths)
	}
	if !bytes.Equal(contents, before) {
		test.Fatal("parser changed its input")
	}
	legacy, omittedPaths, err := parseStatusPorcelainZPaths(contents, false)
	if err != nil || legacy != wantStatus || omittedPaths != nil {
		test.Fatalf("summary mode = %#v, paths = %q, error = %v", legacy, omittedPaths, err)
	}
}

func TestParseStatusPathsDoesNotApplySnapshotPolicy(test *testing.T) {
	want := []string{".env", "../outside", ".git/config", "back\\slash", "duplicate", "duplicate", "Case", "case", " nested/name "}
	contents := []byte("? " + strings.Join(want, "\x00? ") + "\x00")
	status, paths, err := parseStatusPorcelainZPaths(contents, true)
	if err != nil || status != (domain.GitStatus{Untracked: len(want)}) || !reflect.DeepEqual(paths, want) {
		test.Fatalf("path evidence was filtered or normalized: status %#v, paths %q, error %v", status, paths, err)
	}
}

func TestParseStatusPathsRejectsPartialSelection(test *testing.T) {
	objectID := strings.Repeat("a", 40)
	rename := "2 R. N... 100644 100644 100644 " + objectID + " " + objectID + " R100 new\x00"
	for _, suffix := range []string{
		"? missing terminator", "? \x00", "\x00", "unexpected\x00", "# branch.head main\x00",
		"1 M. N... 100648 100644 100644 " + objectID + " " + objectID + " file\x00",
		"1 M. N... 100644 100644 100644 invalid " + objectID + " file\x00",
		rename, rename + "\x00", strings.Replace(rename, "R100", "R101", 1) + "old\x00",
		"u invalid\x00",
	} {
		test.Run(fmt.Sprintf("%q", suffix), func(test *testing.T) {
			status, paths, err := parseStatusPorcelainZPaths([]byte("? earlier\x00"+suffix), true)
			if err == nil || status != (domain.GitStatus{}) || paths != nil {
				test.Fatalf("malformed input returned partial status %#v, paths %q, error %v", status, paths, err)
			}
		})
	}
}

func TestParseStatusPathsBoundsOnlyRetainedPaths(test *testing.T) {
	for _, count := range []int{0, 4096, 4097} {
		test.Run(fmt.Sprint(count), func(test *testing.T) {
			contents := statusPathsCountFixture(count)
			status, paths, err := parseStatusPorcelainZPaths(contents, true)
			if count > 4096 {
				if err == nil || status != (domain.GitStatus{}) || paths != nil {
					test.Fatalf("over-limit parse returned status %#v, %d paths, error %v", status, len(paths), err)
				}
			} else if err != nil || status.Untracked != count || len(paths) != count {
				test.Fatalf("bounded parse = %#v, %d paths, error %v", status, len(paths), err)
			}
			legacy, omittedPaths, err := parseStatusPorcelainZPaths(contents, false)
			if err != nil || legacy.Untracked != count || omittedPaths != nil {
				test.Fatalf("legacy mode inherited path limit/retention: %#v, %d paths, %v", legacy, len(omittedPaths), err)
			}
			legacy, err = parseStatusPorcelainZ(contents)
			if err != nil || legacy.Untracked != count {
				test.Fatalf("legacy parser = %#v, error %v", legacy, err)
			}
		})
	}
}

func statusPathsCountFixture(count int) []byte {
	var contents strings.Builder
	for index := 0; index < count; index++ {
		fmt.Fprintf(&contents, "? leaf-%04d\x00", index)
	}
	return []byte(contents.String())
}

func statusPathsMixedFixture() ([]byte, domain.GitStatus, []string) {
	objectID := strings.Repeat("a", 40)
	contents := "? zebra\n\t\xff \x00" +
		"2 R. N... 100644 100644 100644 " + objectID + " " + objectID + " R100 renamed destination\x00? not an untracked record\x00" +
		"! ignored/file\x00" +
		"1 .M N... 100644 100644 100644 " + objectID + " " + objectID + " tracked\x00" +
		"u UU N... 100644 100644 100644 100644 " + objectID + " " + objectID + " " + objectID + " conflict\x00" +
		"2 C. N... 100644 100644 100644 " + objectID + " " + objectID + " C100 copied destination\x00? also not untracked\x00" +
		"? .env\x00? Alpha/한글 file\x00"
	return []byte(contents), domain.GitStatus{Staged: 2, Unstaged: 1, Unmerged: 1, Untracked: 3}, []string{"zebra\n\t\xff ", ".env", "Alpha/한글 file"}
}
