package pathutil

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestCanonicalResolvesRelativePaths(test *testing.T) {
	root := test.TempDir()
	name := "space and 한글"
	if runtime.GOOS != "windows" {
		name += "\nnewline"
	}
	directory := makeDirectory(test, filepath.Join(root, name))
	expected, err := filepath.EvalSymlinks(directory)
	if err != nil {
		test.Fatal(err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		test.Fatal(err)
	}
	relative, err := filepath.Rel(workingDirectory, directory)
	if err != nil {
		test.Fatal(err)
	}
	for _, input := range []string{directory, relative, directory + string(filepath.Separator) + "."} {
		actual, err := Canonical(input)
		if err != nil || actual != expected {
			test.Errorf("Canonical(%q) = %q, %v; want %q", input, actual, err, expected)
		}
	}
}

func TestContainsUsesPathComponents(test *testing.T) {
	root := test.TempDir()
	parent := makeDirectory(test, filepath.Join(root, "wt"))
	child := makeDirectory(test, filepath.Join(parent, "subdir"))
	sibling := makeDirectory(test, filepath.Join(root, "wt-other"))
	tests := []struct {
		name   string
		parent string
		child  string
		want   bool
	}{
		{"equal", parent, parent, true},
		{"child", parent, child, true},
		{"sibling prefix", parent, sibling, false},
		{"ancestor", parent, root, false},
		{"dot components", parent, child + string(filepath.Separator) + "..", true},
		{"parent traversal", parent, parent + string(filepath.Separator) + "..", false},
		{"empty parent", "", child, false},
		{"empty child", parent, "", false},
		{"missing child", parent, filepath.Join(parent, "missing"), false},
		{"missing parent", filepath.Join(root, "missing"), child, false},
		{"malformed child", parent, child + "\x00", false},
	}
	for _, entry := range tests {
		test.Run(entry.name, func(test *testing.T) {
			if actual := Contains(entry.parent, entry.child); actual != entry.want {
				test.Errorf("Contains(%q, %q) = %v, want %v", entry.parent, entry.child, actual, entry.want)
			}
		})
	}
}

func TestContainsCheckedDistinguishesOutsideFromReadFailure(test *testing.T) {
	root := test.TempDir()
	parent := makeDirectory(test, filepath.Join(root, "wt"))
	child := makeDirectory(test, filepath.Join(parent, "child"))
	outside := makeDirectory(test, filepath.Join(root, "outside"))
	if contained, err := ContainsChecked(parent, outside); err != nil || contained {
		test.Fatalf("ContainsChecked(outside) = %v, %v; want false without an error", contained, err)
	}
	canonicalParent, err := Canonical(parent)
	if err != nil {
		test.Fatal(err)
	}
	canonicalChild, err := Canonical(child)
	if err != nil {
		test.Fatal(err)
	}
	if err := os.Remove(child); err != nil {
		test.Fatal(err)
	}
	if contained, err := ContainsChecked(canonicalParent, canonicalChild); contained || !errors.Is(err, os.ErrNotExist) {
		test.Fatalf("ContainsChecked(disappeared) = %v, %v; want a retained read failure", contained, err)
	}
}

func TestCanonicalAndContainsResolveSymlinksBeforeParentTraversal(test *testing.T) {
	root := test.TempDir()
	parent := makeDirectory(test, filepath.Join(root, "wt"))
	child := makeDirectory(test, filepath.Join(parent, "child"))
	outside := makeDirectory(test, filepath.Join(root, "outside"))
	outsideChild := makeDirectory(test, filepath.Join(outside, "child"))
	outsideTarget := makeDirectory(test, filepath.Join(outside, "target"))
	makeDirectory(test, filepath.Join(parent, "target"))
	alias := filepath.Join(root, "alias")
	makeSymlink(test, parent, alias)
	escape := filepath.Join(parent, "escape")
	makeSymlink(test, outsideChild, escape)
	aliasChild := filepath.Join(alias, "child")
	want, err := Canonical(child)
	if err != nil {
		test.Fatal(err)
	}
	if actual, err := Canonical(aliasChild); err != nil || actual != want {
		test.Fatalf("Canonical(alias) = %q, %v; want %q", actual, err, want)
	}
	if !Contains(parent, aliasChild) || !Contains(alias, child) {
		test.Fatal("symlink alias must retain containment")
	}
	if Contains(parent, escape) {
		test.Fatal("symlink escape must not be contained")
	}
	traversal := escape + string(filepath.Separator) + ".." + string(filepath.Separator) + "target"
	want, err = Canonical(outsideTarget)
	if err != nil {
		test.Fatal(err)
	}
	if actual, err := Canonical(traversal); err != nil || actual != want {
		test.Fatalf("Canonical(symlink/../target) = %q, %v; want %q", actual, err, want)
	}
	if Contains(parent, traversal) {
		test.Fatal("cleaning before symlink resolution hid an escape")
	}
}

func TestCanonicalRejectsUnresolvedPaths(test *testing.T) {
	root := test.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		test.Fatal(err)
	}
	for _, input := range []string{"", "bad\x00path", filepath.Join(root, "missing"), filepath.Join(file, "child")} {
		if actual, err := Canonical(input); err == nil || actual != "" {
			test.Errorf("Canonical(%q) = %q, %v; want empty path and error", input, actual, err)
		}
	}
}

func TestCanonicalRejectsDanglingAndLoopingSymlinks(test *testing.T) {
	root := test.TempDir()
	dangling := filepath.Join(root, "dangling")
	loop := filepath.Join(root, "loop")
	makeSymlink(test, filepath.Join(root, "missing"), dangling)
	makeSymlink(test, loop, loop)
	for _, input := range []string{dangling, loop} {
		if actual, err := Canonical(input); err == nil || actual != "" {
			test.Errorf("Canonical(%q) = %q, %v; want error", input, actual, err)
		}
		if Contains(root, input) {
			test.Errorf("Contains(root, %q) accepted unresolved identity", input)
		}
	}
}

func TestCanonicalPreservesCaseSensitiveIdentity(test *testing.T) {
	if runtime.GOOS == "windows" {
		test.Skip("Unix case-sensitive path semantics")
	}
	root := test.TempDir()
	upper := makeDirectory(test, filepath.Join(root, "CaseSensitive"))
	lower := makeDirectory(test, filepath.Join(root, "casesensitive"))
	upperInfo, err := os.Stat(upper)
	if err != nil {
		test.Fatal(err)
	}
	lowerInfo, err := os.Stat(lower)
	if err != nil {
		test.Fatal(err)
	}
	canonicalUpper, err := Canonical(upper)
	if err != nil {
		test.Fatal(err)
	}
	if filepath.Base(canonicalUpper) != "CaseSensitive" {
		test.Fatalf("Canonical() folded Unix case: %q", canonicalUpper)
	}
	if os.SameFile(upperInfo, lowerInfo) {
		test.Skip("temporary filesystem is case-insensitive; case preservation checked")
	}
	canonicalLower, err := Canonical(lower)
	if err != nil {
		test.Fatal(err)
	}
	if canonicalUpper == canonicalLower || Contains(upper, lower) || Contains(lower, upper) {
		test.Fatal("distinct case-sensitive directories were conflated")
	}
}

func TestContainsRecognizesFilesystemCaseAliases(test *testing.T) {
	root := test.TempDir()
	parent := makeDirectory(test, filepath.Join(root, "MixedCase"))
	child := makeDirectory(test, filepath.Join(parent, "child"))
	alias := filepath.Join(root, strings.ToLower(filepath.Base(parent)))
	aliasInfo, err := os.Stat(alias)
	if errors.Is(err, os.ErrNotExist) {
		test.Skip("temporary filesystem is case-sensitive")
	}
	if err != nil {
		test.Fatal(err)
	}
	parentInfo, err := os.Stat(parent)
	if err != nil {
		test.Fatal(err)
	}
	if !os.SameFile(aliasInfo, parentInfo) {
		test.Fatal("unexpected distinct alias fixture")
	}
	if !Contains(alias, child) || !Contains(parent, filepath.Join(alias, "child")) {
		test.Fatal("filesystem case aliases must not hide an active cwd")
	}
}

func TestCanonicalFailsClosedOnInaccessibleComponents(test *testing.T) {
	if runtime.GOOS == "windows" {
		test.Skip("POSIX permission fixture")
	}
	root := test.TempDir()
	blocked := makeDirectory(test, filepath.Join(root, "blocked"))
	child := makeDirectory(test, filepath.Join(blocked, "child"))
	if err := os.Chmod(blocked, 0); err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() {
		if err := os.Chmod(blocked, 0o700); err != nil {
			test.Error(err)
		}
	})
	if _, err := os.Stat(child); err == nil {
		test.Skip("current user bypasses directory permissions")
	}
	if actual, err := Canonical(child); err == nil || actual != "" {
		test.Fatalf("Canonical(inaccessible) = %q, %v", actual, err)
	}
	if Contains(root, child) {
		test.Fatal("inaccessible path must not be accepted as contained")
	}
}

func TestUnixBackslashIsNotASeparator(test *testing.T) {
	if runtime.GOOS == "windows" {
		test.Skip("backslash is a Windows separator")
	}
	root := test.TempDir()
	parent := makeDirectory(test, filepath.Join(root, "wt"))
	sibling := makeDirectory(test, filepath.Join(root, `wt\child`))
	if Contains(parent, sibling) {
		test.Fatal("Unix backslash filename was treated as a child component")
	}
}

func makeDirectory(test *testing.T, directory string) string {
	test.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		test.Fatal(err)
	}
	return directory
}

func makeSymlink(test *testing.T, target, link string) {
	test.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" && (errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.Errno(1314))) {
			test.Skipf("symlink privilege unavailable: %v", err)
		}
		test.Fatal(err)
	}
}
