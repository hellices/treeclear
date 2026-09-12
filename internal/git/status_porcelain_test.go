package git

import "testing"

func TestParseStatusPorcelainZ(test *testing.T) {
	input := "1 M. N... 100644 100644 100644 abc def staged file\x00" +
		"1 .M N... 100644 100644 100644 abc def changed\nfile\x00" +
		"2 R. N... 100644 100644 100644 abc def R100 new name\x00old name\x00" +
		"u UU N... 100644 100644 100644 100644 abc def ghi conflict\x00" +
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
	actual, err := parseStatusPorcelainZ([]byte("1 .. S.MU 160000 160000 160000 abc def module\x00"))
	if err != nil || actual.Clean() {
		test.Fatalf("dirty submodule status = %#v, error = %v", actual, err)
	}
}

func TestParseStatusRejectsMalformedRecords(test *testing.T) {
	for _, input := range []string{
		"? missing terminator", "? \x00", "\x00", "unexpected\x00", "1 M. short\x00",
		"1 XX N... 100644 100644 100644 abc def name\x00",
		"2 R. N... 100644 100644 100644 abc def R100 new\x00",
	} {
		if actual, err := parseStatusPorcelainZ([]byte(input)); err == nil {
			test.Fatalf("accepted %q as %#v", input, actual)
		}
	}
}
