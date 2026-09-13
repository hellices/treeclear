package pathutil

import (
	"runtime"
	"testing"
)

func TestValidateAbsoluteFormIsPureAndRequiresCanonicalSyntax(test *testing.T) {
	valid := []string{"/", "/treeclear-synthetic/nonexistent", "/treeclear-synthetic/한글\nname"}
	invalid := []string{"", ".", "relative/path", "/treeclear-synthetic/../other", "/treeclear-synthetic/./other", "/treeclear-synthetic//other", "/treeclear-synthetic/", "/bad\x00path", "/bad\xffpath"}
	if runtime.GOOS == "windows" {
		valid = []string{`C:\`, `C:\treeclear-synthetic\nonexistent`, `C:\treeclear-synthetic\한글`, `\\server\share\nonexistent`}
		invalid = []string{"", ".", `relative\path`, `C:relative`, `\rooted`, `c:\treeclear-synthetic`, `C:/treeclear-synthetic`, `C:\treeclear-synthetic\..\other`, `C:\treeclear-synthetic\.\other`, `C:\treeclear-synthetic\\other`, `C:\treeclear-synthetic\`, `\\SERVER\Share\other`, `\\?\C:\treeclear-synthetic`, `\\.\PhysicalDrive0`, `C:\treeclear-synthetic\NUL`, `C:\treeclear-synthetic\file:stream`, `C:\treeclear-synthetic\trailing.`, "C:\\bad\x00path", "C:\\bad\xffpath"}
	}
	for _, value := range valid {
		if err := ValidateAbsoluteForm(value); err != nil {
			test.Errorf("ValidateAbsoluteForm(%q) = %v", value, err)
		}
	}
	for _, value := range invalid {
		if err := ValidateAbsoluteForm(value); err == nil {
			test.Errorf("ValidateAbsoluteForm(%q) accepted noncanonical syntax", value)
		}
	}
}
