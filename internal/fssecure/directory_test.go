package fssecure

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishPrivateDirectoryRejectsInvalidInputWithoutWrites(test *testing.T) {
	parent := test.TempDir()
	validFiles := []PrivateFile{{Name: "payload", Contents: []byte("private")}}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name    string
		context context.Context
		path    string
		files   []PrivateFile
		want    error
	}{
		{name: "nil context", path: filepath.Join(parent, "nil"), files: validFiles, want: fs.ErrInvalid},
		{name: "canceled", context: canceled, path: filepath.Join(parent, "canceled"), files: validFiles, want: context.Canceled},
		{name: "empty path", context: context.Background(), files: validFiles, want: fs.ErrInvalid},
		{name: "dot", context: context.Background(), path: ".", files: validFiles, want: fs.ErrInvalid},
		{name: "dot dot", context: context.Background(), path: "..", files: validFiles, want: fs.ErrInvalid},
		{name: "final dot", context: context.Background(), path: parent + string(filepath.Separator) + ".", files: validFiles, want: fs.ErrInvalid},
		{name: "final dot dot", context: context.Background(), path: parent + string(filepath.Separator) + "..", files: validFiles, want: fs.ErrInvalid},
		{name: "root", context: context.Background(), path: filepath.VolumeName(parent) + string(filepath.Separator), files: validFiles, want: fs.ErrInvalid},
		{name: "empty set", context: context.Background(), path: filepath.Join(parent, "empty"), want: fs.ErrInvalid},
	}
	for _, name := range []string{"", ".", "..", "../escape", "sub/file", `sub\file`, "/absolute", `C:drive`, "nul\x00", "line\nfeed", "tab\t", "delete\x7f", "trailing.", "trailing ", "CON", "con.txt", "NUL", "aux.log", "PRN", "COM1", "lpt9.txt", "CONIN$", "CONOUT$", "COM¹", "LPT²", "COM³", "wild*", "query?", "pipe|", "quote\"", "left<", "right>", "\xff", "é", "e\u0301", strings.Repeat("a", 256)} {
		cases = append(cases, struct {
			name    string
			context context.Context
			path    string
			files   []PrivateFile
			want    error
		}{name: "leaf " + name, context: context.Background(), path: filepath.Join(parent, "invalid"), files: []PrivateFile{{Name: name}}, want: fs.ErrInvalid})
	}
	for _, names := range [][2]string{{"payload", "payload"}, {"manifest.json", "MANIFEST.JSON"}, {"File", "fILE"}} {
		cases = append(cases, struct {
			name    string
			context context.Context
			path    string
			files   []PrivateFile
			want    error
		}{name: "duplicate " + names[1], context: context.Background(), path: filepath.Join(parent, "duplicate"), files: []PrivateFile{{Name: names[0]}, {Name: names[1]}}, want: fs.ErrInvalid})
	}
	for _, fixture := range cases {
		test.Run(fixture.name, func(test *testing.T) {
			path, err := PublishPrivateDirectory(fixture.context, fixture.path, fixture.files)
			if path != "" || !errors.Is(err, fixture.want) {
				test.Fatalf("PublishPrivateDirectory = %q, %v; want empty path and %v", path, err, fixture.want)
			}
		})
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		test.Fatalf("invalid inputs changed parent: %v, %v", entries, err)
	}
}
