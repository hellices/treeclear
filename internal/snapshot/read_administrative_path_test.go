package snapshot

import (
	"strings"
	"testing"
)

func TestReadAdministrativeFocusedWindowsPathConversion(test *testing.T) {
	longSuffix := strings.Repeat(`segment\`, 48) + "admin"
	for _, scenario := range []struct {
		name      string
		directory string
		expected  string
	}{
		{"drive_root", `C:\`, `\\?\C:\`},
		{"local", `C:\work\admin`, `\\?\C:\work\admin`},
		{"long_local", `C:\` + longSuffix, `\\?\C:\` + longSuffix},
		{"unicode_local", `C:\작업\관리`, `\\?\C:\작업\관리`},
		{"unc_root", `\\server\share`, `\\?\UNC\server\share`},
		{"unc", `\\server\share\admin`, `\\?\UNC\server\share\admin`},
		{"long_unc", `\\server\share\` + longSuffix, `\\?\UNC\server\share\` + longSuffix},
		{"extended_local", `\\?\C:\work\admin`, `\\?\C:\work\admin`},
		{"extended_unc", `\\?\UNC\server\share\admin`, `\\?\UNC\server\share\admin`},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			actual := administrativeReadWindowsPath(scenario.directory)
			if actual != scenario.expected {
				test.Fatalf("native path: got %q, want %q", actual, scenario.expected)
			}
			if repeated := administrativeReadWindowsPath(actual); repeated != actual {
				test.Fatalf("native path is not idempotent: got %q, want %q", repeated, actual)
			}
		})
	}
}
