package fssecure

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadPrivateDirectoryRejectsInvalidArguments(test *testing.T) {
	parent := test.TempDir()
	path := filepath.Join(parent, "missing", "snapshot")
	limits := []PrivateFileLimit{{Name: "manifest.json", MaximumBytes: 16}}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, expire := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer expire()
	nativeMaximum := int64(int(^uint(0) >> 1))
	cases := []struct {
		name    string
		context context.Context
		path    string
		limits  []PrivateFileLimit
		maximum int64
		want    error
	}{
		{name: "nil context", path: path, limits: limits, maximum: 16, want: fs.ErrInvalid},
		{name: "canceled", context: canceled, path: path, limits: limits, maximum: 16, want: context.Canceled},
		{name: "expired", context: expired, path: path, limits: limits, maximum: 16, want: context.DeadlineExceeded},
		{name: "empty path", context: context.Background(), limits: limits, maximum: 16, want: fs.ErrInvalid},
		{name: "relative", context: context.Background(), path: "snapshot", limits: limits, maximum: 16, want: fs.ErrInvalid},
		{name: "root", context: context.Background(), path: filepath.VolumeName(parent) + string(filepath.Separator), limits: limits, maximum: 16, want: fs.ErrInvalid},
		{name: "trailing separator", context: context.Background(), path: path + string(filepath.Separator), limits: limits, maximum: 16, want: fs.ErrInvalid},
		{name: "unclean path", context: context.Background(), path: parent + string(filepath.Separator) + "." + string(filepath.Separator) + "snapshot", limits: limits, maximum: 16, want: fs.ErrInvalid},
		{name: "dot dot", context: context.Background(), path: parent + string(filepath.Separator) + "unused" + string(filepath.Separator) + ".." + string(filepath.Separator) + "snapshot", limits: limits, maximum: 16, want: fs.ErrInvalid},
		{name: "nul path", context: context.Background(), path: path + "\x00", limits: limits, maximum: 16, want: fs.ErrInvalid},
		{name: "nil limits", context: context.Background(), path: path, maximum: 16, want: fs.ErrInvalid},
		{name: "empty limits", context: context.Background(), path: path, limits: []PrivateFileLimit{}, maximum: 16, want: fs.ErrInvalid},
		{name: "zero total", context: context.Background(), path: path, limits: limits, want: fs.ErrInvalid},
		{name: "negative total", context: context.Background(), path: path, limits: limits, maximum: -1, want: fs.ErrInvalid},
		{name: "maximum total", context: context.Background(), path: path, limits: limits, maximum: nativeMaximum, want: fs.ErrInvalid},
		{name: "zero file limit", context: context.Background(), path: path, limits: []PrivateFileLimit{{Name: "manifest.json"}}, maximum: 16, want: fs.ErrInvalid},
		{name: "negative file limit", context: context.Background(), path: path, limits: []PrivateFileLimit{{Name: "manifest.json", MaximumBytes: -1}}, maximum: 16, want: fs.ErrInvalid},
		{name: "maximum file limit", context: context.Background(), path: path, limits: []PrivateFileLimit{{Name: "manifest.json", MaximumBytes: nativeMaximum}}, maximum: 16, want: fs.ErrInvalid},
		{name: "duplicate", context: context.Background(), path: path, limits: []PrivateFileLimit{{Name: "manifest.json", MaximumBytes: 16}, {Name: "manifest.json", MaximumBytes: 16}}, maximum: 16, want: fs.ErrInvalid},
		{name: "case duplicate", context: context.Background(), path: path, limits: []PrivateFileLimit{{Name: "manifest.json", MaximumBytes: 16}, {Name: "MANIFEST.JSON", MaximumBytes: 16}}, maximum: 16, want: fs.ErrInvalid},
	}
	for _, name := range []string{"", ".", "..", "../escape", "sub/file", `sub\file`, "/absolute", `C:drive`, "nul\x00", "line\nfeed", "tab\t", "delete\x7f", "trailing.", "trailing ", "CON", "con.txt", "NUL", "aux.log", "PRN", "COM1", "lpt9.txt", "CONIN$", "CONOUT$", "wild*", "query?", "pipe|", "quote\"", "left<", "right>", "\xff", "é", "e\u0301", strings.Repeat("a", 256)} {
		test.Run("leaf "+name, func(test *testing.T) {
			files, err := ReadPrivateDirectory(context.Background(), path, []PrivateFileLimit{{Name: name, MaximumBytes: 16}}, 16)
			if files != nil || !errors.Is(err, fs.ErrInvalid) {
				test.Fatalf("invalid leaf = %v, %v; want nil, ErrInvalid", files, err)
			}
		})
	}
	for _, fixture := range cases {
		test.Run(fixture.name, func(test *testing.T) {
			files, err := ReadPrivateDirectory(fixture.context, fixture.path, fixture.limits, fixture.maximum)
			if files != nil || !errors.Is(err, fixture.want) {
				test.Fatalf("invalid arguments = %v, %v; want nil, %v", files, err, fixture.want)
			}
		})
	}
	assertOnlyNames(test, parent)
}
