package pathutil

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareWindowsPaths(test *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`c:\Mixed/Child`, `C:\Mixed\Child`},
		{`\\?\c:\Mixed\Child`, `C:\Mixed\Child`},
		{`\\SERVER\Share\Mixed`, `\\server\share\Mixed`},
		{`//SERVER/Share/Mixed`, `\\server\share\Mixed`},
		{`\\?\UNC\SERVER\Share\Mixed`, `\\server\share\Mixed`},
		{`\\?\unc\SERVER\Share\Mixed`, `\\server\share\Mixed`},
		{`relative/path`, `relative\path`},
		{`C:\repo\COMLPT1`, `C:\repo\COMLPT1`},
		{`C:\repo\COMLPT2`, `C:\repo\COMLPT2`},
		{`C:\repo\COMLPT3`, `C:\repo\COMLPT3`},
		{`C:\repo\COMLPT4`, `C:\repo\COMLPT4`},
		{`C:\repo\COMLPT5`, `C:\repo\COMLPT5`},
		{`C:\repo\COMLPT6`, `C:\repo\COMLPT6`},
		{`C:\repo\COMLPT7`, `C:\repo\COMLPT7`},
		{`C:\repo\COMLPT8`, `C:\repo\COMLPT8`},
		{`C:\repo\COMLPT9`, `C:\repo\COMLPT9`},
		{`C:\repo\comlpt1.txt`, `C:\repo\comlpt1.txt`},
		{`\\?\C:\repo\COMLPT1`, `C:\repo\COMLPT1`},
		{`\\server\share\COMLPT1`, `\\server\share\COMLPT1`},
	}
	for _, entry := range tests {
		actual, err := preparePath(entry.input)
		if err != nil || actual != entry.want {
			test.Errorf("preparePath(%q) = %q, %v; want %q", entry.input, actual, err, entry.want)
		}
	}
}

func TestPrepareWindowsPathsRejectsAmbiguousIdentity(test *testing.T) {
	for _, input := range []string{
		`C:relative`, `\rooted`, `\\server`, `\\server\`, `\\\share\child`,
		`\\?\relative`, `\\?\UNC\server`, `\\.\PhysicalDrive0`, `\??\C:\wt`,
		`\\?\GLOBALROOT\Device\HarddiskVolume1\wt`,
		`C:\wt\trailing.`, `C:\wt\trailing `, `C:\wt\file:stream`,
		`C:\wt\NUL`, `C:\wt\con.txt`, `C:\wt\COM1`, `C:\wt\COM9`,
		`C:\wt\LPT1`, `C:\wt\LPT9`, `C:\wt\com1.txt`, `C:\wt\lpt1.txt`,
		`C:\wt\COM¹`, `C:\wt\LPT¹`, `C:\wt\bad*name`,
		"C:\\wt\\bad\nname", `\\?\C:\wt\..\other`, `\\?\C:\wt\.\child`,
	} {
		if actual, err := preparePath(input); err == nil || actual != "" {
			test.Errorf("preparePath(%q) = %q, %v; want empty path and error", input, actual, err)
		}
	}
}

func TestCanonicalWindowsAliasesAndLongPaths(test *testing.T) {
	parent := makeDirectory(test, filepath.Join(test.TempDir(), "MixedCase"))
	child := parent
	for range 18 {
		child = filepath.Join(child, "long-path-component")
	}
	makeDirectory(test, child)
	want, err := Canonical(child)
	if err != nil {
		test.Fatal(err)
	}
	for _, input := range []string{
		strings.ToLower(child), filepath.ToSlash(child), `\\?\` + child,
		strings.ToLower(filepath.VolumeName(child)) + child[len(filepath.VolumeName(child)):],
	} {
		actual, err := Canonical(input)
		if err != nil || actual != want {
			test.Errorf("Canonical(%q) = %q, %v; want %q", input, actual, err, want)
		}
		if !Contains(parent, input) {
			test.Errorf("Contains(parent, %q) = false", input)
		}
	}
}
