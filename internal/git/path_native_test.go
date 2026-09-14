package git

import (
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestBoundedNativeGitPath(test *testing.T) {
	directory := readonlyIndexCanonicalTemporaryDirectory(test)
	path := filepath.Join(directory, "source")
	units := utf16.Encode([]rune(path))
	unicodePath := filepath.Join(directory, "source-한글-🚀")
	unicodeUnits := utf16.Encode([]rune(unicodePath))
	overflow := filepath.VolumeName(directory) + string(filepath.Separator) + strings.Repeat("界", maxInspectionPointerBytes/3+1)
	overflowUnits := utf16.Encode([]rune(overflow))
	for _, scenario := range []struct {
		name   string
		buffer []uint16
		length uint32
		want   string
		cause  error
	}{
		{name: "ordinary", buffer: append(slices.Clone(units), 0), length: uint32(len(units)), want: path},
		{name: "Unicode", buffer: append(slices.Clone(unicodeUnits), 0), length: uint32(len(unicodeUnits)), want: unicodePath},
		{name: "empty", cause: fs.ErrInvalid},
		{name: "buffer boundary", buffer: units, length: uint32(len(units)), cause: ErrReadLimit},
		{name: "reported overflow", buffer: units, length: ^uint32(0), cause: ErrReadLimit},
		{name: "decoded byte overflow", buffer: append(slices.Clone(overflowUnits), 0), length: uint32(len(overflowUnits)), cause: ErrReadLimit},
		{name: "missing terminator", buffer: append(slices.Clone(units), 'x'), length: uint32(len(units)), cause: fs.ErrInvalid},
		{name: "embedded NUL", buffer: append(slices.Clone(units), 0, 'x', 0), length: uint32(len(units) + 2), cause: fs.ErrInvalid},
		{name: "unpaired high surrogate", buffer: append(slices.Clone(units), 0xd800, 0), length: uint32(len(units) + 1), cause: fs.ErrInvalid},
		{name: "unpaired low surrogate", buffer: append(slices.Clone(units), 0xdc00, 0), length: uint32(len(units) + 1), cause: fs.ErrInvalid},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			before := slices.Clone(scenario.buffer)
			actual, err := boundedNativeGitPath(scenario.buffer, scenario.length)
			if !slices.Equal(before, scenario.buffer) {
				test.Fatal("native path decoding changed the input buffer")
			}
			if scenario.cause != nil {
				if !errors.Is(err, scenario.cause) || actual != "" {
					test.Errorf("invalid native path returned %q, error %v; want %v", actual, err, scenario.cause)
				}
			} else if err != nil || actual != scenario.want {
				test.Errorf("native path returned %q, error %v; want %q", actual, err, scenario.want)
			}
		})
	}
}
