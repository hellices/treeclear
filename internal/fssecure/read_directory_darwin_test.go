package fssecure

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReadPrivateDirectoryReturnsRequestedOrder(test *testing.T) {
	want := []PrivateFile{
		{Name: "manifest.json", Contents: []byte("{\"stored\":true}\n")},
		{Name: "z-payload.bin", Contents: bytes.Repeat([]byte{0, 255, '\r', '\n'}, 4096)},
		{Name: "a-empty", Contents: nil},
	}
	path := readDirectoryFixture(test, want)
	before := readDirectoryFixtureState(test, path)
	limits := []PrivateFileLimit{{Name: want[0].Name, MaximumBytes: int64(len(want[0].Contents))}, {Name: want[1].Name, MaximumBytes: int64(len(want[1].Contents))}, {Name: want[2].Name, MaximumBytes: 1}}
	maximum := int64(len(want[0].Contents) + len(want[1].Contents))
	actual, err := ReadPrivateDirectory(context.Background(), path, limits, maximum)
	if err != nil {
		test.Fatalf("valid private directory read: %v", err)
	}
	assertReadDirectoryFiles(test, actual, want)
	assertReadDirectoryUnchanged(test, path, before)
	actual[0].Contents[0] ^= 255
	assertContents(test, filepath.Join(path, want[0].Name), want[0].Contents)
	again, err := ReadPrivateDirectory(context.Background(), path, limits, maximum)
	if err != nil {
		test.Fatal(err)
	}
	assertReadDirectoryFiles(test, again, want)
}

func TestReadPrivateDirectoryByteBoundaries(test *testing.T) {
	cases := []struct {
		name    string
		files   []PrivateFile
		limits  []PrivateFileLimit
		maximum int64
		want    error
	}{
		{name: "exact file and total", files: []PrivateFile{{Name: "payload", Contents: []byte("1234")}}, limits: []PrivateFileLimit{{Name: "payload", MaximumBytes: 4}}, maximum: 4},
		{name: "file overflow", files: []PrivateFile{{Name: "payload", Contents: []byte("1234")}}, limits: []PrivateFileLimit{{Name: "payload", MaximumBytes: 3}}, maximum: 8, want: ErrPrivateDirectoryLimit},
		{name: "aggregate overflow", files: []PrivateFile{{Name: "first", Contents: []byte("12")}, {Name: "second", Contents: []byte("34")}}, limits: []PrivateFileLimit{{Name: "first", MaximumBytes: 4}, {Name: "second", MaximumBytes: 4}}, maximum: 3, want: ErrPrivateDirectoryLimit},
		{name: "empty after exhausted total", files: []PrivateFile{{Name: "first", Contents: []byte("12")}, {Name: "empty"}, {Name: "also-empty", Contents: []byte{}}}, limits: []PrivateFileLimit{{Name: "first", MaximumBytes: 8}, {Name: "empty", MaximumBytes: 8}, {Name: "also-empty", MaximumBytes: 8}}, maximum: 2},
		{name: "nonempty after exhausted total", files: []PrivateFile{{Name: "first", Contents: []byte("12")}, {Name: "second", Contents: []byte("3")}}, limits: []PrivateFileLimit{{Name: "first", MaximumBytes: 8}, {Name: "second", MaximumBytes: 8}}, maximum: 2, want: ErrPrivateDirectoryLimit},
		{name: "all empty", files: []PrivateFile{{Name: "first"}, {Name: "second"}}, limits: []PrivateFileLimit{{Name: "first", MaximumBytes: 1}, {Name: "second", MaximumBytes: 1}}, maximum: 1},
		{name: "representable maximum", files: []PrivateFile{{Name: "payload", Contents: []byte("1234")}}, limits: []PrivateFileLimit{{Name: "payload", MaximumBytes: int64(int(^uint(0)>>1)) - 1}}, maximum: int64(int(^uint(0)>>1)) - 1},
	}
	for _, fixture := range cases {
		test.Run(fixture.name, func(test *testing.T) {
			path := readDirectoryFixture(test, fixture.files)
			before := readDirectoryFixtureState(test, path)
			actual, err := ReadPrivateDirectory(context.Background(), path, fixture.limits, fixture.maximum)
			if fixture.want != nil {
				if actual != nil || !errors.Is(err, fixture.want) {
					test.Fatalf("bounded read = %v, %v; want nil, %v", actual, err, fixture.want)
				}
			} else {
				if err != nil {
					test.Fatal(err)
				}
				assertReadDirectoryFiles(test, actual, fixture.files)
			}
			assertReadDirectoryUnchanged(test, path, before)
		})
	}
}

func readDirectoryFixture(test *testing.T, files []PrivateFile) string {
	test.Helper()
	parent := filepath.Join(privateDirectoryFixture(test), "store")
	if err := os.Mkdir(parent, 0o700); err != nil {
		test.Fatal(err)
	}
	path := filepath.Join(parent, "snapshot")
	if err := os.Mkdir(path, 0o700); err != nil {
		test.Fatal(err)
	}
	for _, file := range files {
		writePrivateFixture(test, filepath.Join(path, file.Name), file.Contents)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		test.Fatal(err)
	}
	return canonical
}

func assertReadDirectoryFiles(test *testing.T, actual, expected []PrivateFile) {
	test.Helper()
	if len(actual) != len(expected) {
		test.Fatalf("returned %d files; want %d", len(actual), len(expected))
	}
	for index, file := range actual {
		if file.Name != expected[index].Name || !bytes.Equal(file.Contents, expected[index].Contents) {
			test.Fatalf("file %d = %q (%d bytes); want %q (%d exact bytes)", index, file.Name, len(file.Contents), expected[index].Name, len(expected[index].Contents))
		}
	}
}

func readDirectoryFixtureState(test *testing.T, path string) map[string]unix.Stat_t {
	test.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		test.Fatal(err)
	}
	names := []string{path, filepath.Dir(path)}
	for _, entry := range entries {
		names = append(names, filepath.Join(path, entry.Name()))
	}
	state := make(map[string]unix.Stat_t, len(names))
	for _, name := range names {
		identity, err := directoryStatAt(unix.AT_FDCWD, name)
		if err != nil {
			test.Fatal(err)
		}
		state[name] = identity
	}
	return state
}

func assertReadDirectoryUnchanged(test *testing.T, path string, expected map[string]unix.Stat_t) {
	test.Helper()
	actual := readDirectoryFixtureState(test, path)
	if len(actual) != len(expected) {
		test.Fatalf("stored entry count changed from %d to %d", len(expected), len(actual))
	}
	for name, identity := range expected {
		if !sameDirectoryFileSnapshot(actual[name], identity) {
			test.Fatalf("stored identity/metadata changed for %q", name)
		}
	}
}
