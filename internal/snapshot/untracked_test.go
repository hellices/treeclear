package snapshot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestUntrackedCodecRoundTrip(test *testing.T) {
	entries := []UntrackedEntry{
		{Path: "binary.bin", Kind: "file", Mode: 0o600, Data: []byte{0, 1, 0xff}},
		{Path: "folder", Kind: "directory", Mode: fs.ModeDir | 0o750},
		{Path: "folder/link", Kind: "symlink", Mode: fs.ModeSymlink | 0o777, LinkTarget: "../binary.bin"},
	}
	encoded, err := EncodeUntracked(entries, 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	decoded, err := DecodeUntracked(encoded, 1<<20)
	if err != nil || !reflect.DeepEqual(decoded, entries) {
		test.Fatalf("archive round trip: %#v, %v", decoded, err)
	}
}

func TestUntrackedCodecPortableEntries(test *testing.T) {
	entries := []UntrackedEntry{
		{Path: ".env", Kind: "file", Mode: 0o600, Data: []byte("codec does not select paths")},
		{Path: "empty", Kind: "file", Mode: 0},
		{Path: "empty-data", Kind: "file", Mode: 0o444, Data: []byte{}},
		{Path: "folder", Kind: "directory", Mode: fs.ModeDir | fs.ModeSticky | fs.ModeSetgid | fs.ModeSetuid | 0o751},
		{Path: "folder/root-link", Kind: "symlink", Mode: fs.ModeSymlink | fs.ModeSticky | fs.ModeSetgid | fs.ModeSetuid | 0o777, LinkTarget: ".."},
		{Path: "implicit/child", Kind: "file", Mode: fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky | 0o654, Data: []byte("special bits")},
		{Path: "links/relative", Kind: "symlink", Mode: fs.ModeSymlink | 0o700, LinkTarget: "../folder/./../empty"},
		{Path: "links/unicode", Kind: "symlink", Mode: fs.ModeSymlink | 0o777, LinkTarget: "../한글/" + strings.Repeat("long", 70)},
		{Path: strings.Repeat("long/", 50) + "name", Kind: "file", Mode: 0o640, Data: []byte("long name")},
		{Path: "한글/자료.txt", Kind: "file", Mode: 0o644, Data: []byte("unicode")},
	}
	encoded, err := EncodeUntracked(entries, 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	decoded, err := DecodeUntracked(encoded, 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	untrackedCodecEqualEntries(test, decoded, entries)
}

func TestUntrackedCodecDeterminismAndOwnership(test *testing.T) {
	entries := []UntrackedEntry{
		{Path: "z-file", Kind: "file", Mode: 0o644, Data: []byte{3, 2, 1}},
		{Path: "a-directory", Kind: "directory", Mode: fs.ModeDir | 0o750},
		{Path: "b-file", Kind: "file", Mode: 0o600, Data: []byte{4, 5}},
	}
	wantedInput := slices.Clone(entries)
	for index := range wantedInput {
		wantedInput[index].Data = slices.Clone(wantedInput[index].Data)
	}
	encoded, err := EncodeUntracked(entries, 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	if !reflect.DeepEqual(entries, wantedInput) {
		test.Fatal("encoding modified caller entries")
	}
	for offset := range len(entries) {
		permuted := append(slices.Clone(entries[offset:]), entries[:offset]...)
		slices.Reverse(permuted)
		again, err := EncodeUntracked(permuted, 1<<20)
		if err != nil || !bytes.Equal(encoded, again) {
			test.Fatalf("nondeterministic encoding at permutation %d: %v", offset, err)
		}
	}
	wantedEncoded := slices.Clone(encoded)
	first, err := DecodeUntracked(encoded, 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	second, err := DecodeUntracked(encoded, 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	if !bytes.Equal(encoded, wantedEncoded) {
		test.Fatal("decoding modified encoded input")
	}
	first[1].Data[0] ^= 0xff
	first[0].Path = "changed"
	untrackedCodecEqualEntries(test, second, wantedInput)
	clear(encoded)
	entries[0].Data[0] ^= 0xff
	untrackedCodecEqualEntries(test, second, wantedInput)
}

func TestUntrackedCodecEmptyArchive(test *testing.T) {
	for _, entries := range [][]UntrackedEntry{nil, {}} {
		encoded, err := EncodeUntracked(entries, 1024)
		if err != nil || len(encoded) == 0 {
			test.Fatalf("empty archive encoding: %d bytes, %v", len(encoded), err)
		}
		expanded := untrackedCodecExpand(test, encoded)
		if !bytes.Equal(expanded, make([]byte, 1024)) {
			test.Fatal("empty archive must contain both tar end blocks")
		}
		decoded, err := DecodeUntracked(encoded, 1024)
		if err != nil || len(decoded) != 0 {
			test.Fatalf("empty archive decoding: %#v, %v", decoded, err)
		}
	}
}

func TestUntrackedCodecStandardMetadata(test *testing.T) {
	entries := []UntrackedEntry{
		{Path: "a", Kind: "file", Mode: fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky | 0o751, Data: []byte("contents")},
		{Path: "한글-" + strings.Repeat("x", 150), Kind: "symlink", Mode: fs.ModeSymlink | 0o777, LinkTarget: strings.Repeat("target", 30)},
	}
	encoded, err := EncodeUntracked(entries, 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	compressed, err := gzip.NewReader(bytes.NewReader(encoded))
	if err != nil {
		test.Fatal(err)
	}
	defer compressed.Close()
	if !compressed.ModTime.IsZero() || compressed.Name != "" || compressed.Comment != "" || len(compressed.Extra) != 0 || compressed.OS != 255 {
		test.Fatalf("nonconstant gzip metadata: %#v", compressed.Header)
	}
	archive := tar.NewReader(compressed)
	for index, entry := range entries {
		header, err := archive.Next()
		if err != nil {
			test.Fatal(err)
		}
		if header.Name != entry.Path || header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" || !header.ModTime.Equal(time.Unix(0, 0)) || !header.AccessTime.IsZero() || !header.ChangeTime.IsZero() {
			test.Fatalf("unexpected tar metadata: %#v", header)
		}
		if header.Format != tar.FormatUSTAR && header.Format != tar.FormatPAX {
			test.Fatalf("unsupported encoding format %v", header.Format)
		}
		if index == 0 && header.Mode != 0o7751 {
			test.Fatalf("special mode bits lost: %o", header.Mode)
		}
		data, err := io.ReadAll(archive)
		if err != nil || !bytes.Equal(data, entry.Data) || header.Linkname != entry.LinkTarget {
			test.Fatalf("stdlib payload mismatch: %q, %v", data, err)
		}
	}
	if _, err := archive.Next(); err != io.EOF {
		test.Fatalf("unexpected tar suffix: %v", err)
	}
	if _, err := io.Copy(io.Discard, compressed); err != nil {
		test.Fatal(err)
	}
}

func untrackedCodecEqualEntries(test testing.TB, actual, wanted []UntrackedEntry) {
	test.Helper()
	wanted = slices.Clone(wanted)
	slices.SortFunc(wanted, func(left, right UntrackedEntry) int { return strings.Compare(left.Path, right.Path) })
	if len(actual) != len(wanted) {
		test.Fatalf("entry count = %d, want %d", len(actual), len(wanted))
	}
	for index, entry := range actual {
		expected := wanted[index]
		if entry.Path != expected.Path || entry.Kind != expected.Kind || entry.Mode != expected.Mode || entry.LinkTarget != expected.LinkTarget || !bytes.Equal(entry.Data, expected.Data) {
			test.Fatalf("entry %d = %#v, want %#v", index, entry, expected)
		}
		if entry.Kind != "file" && entry.Data != nil {
			test.Fatalf("non-file entry has non-nil data: %#v", entry)
		}
	}
}

type untrackedCodecRecord struct {
	header tar.Header
	data   []byte
}

func untrackedCodecTar(test testing.TB, records ...untrackedCodecRecord) []byte {
	test.Helper()
	var contents bytes.Buffer
	archive := tar.NewWriter(&contents)
	for _, record := range records {
		if err := archive.WriteHeader(&record.header); err != nil {
			test.Fatalf("tar fixture header: %v", err)
		}
		if _, err := archive.Write(record.data); err != nil {
			test.Fatalf("tar fixture data: %v", err)
		}
	}
	if err := archive.Close(); err != nil {
		test.Fatalf("tar fixture close: %v", err)
	}
	return contents.Bytes()
}

func untrackedCodecGzip(test testing.TB, contents []byte) []byte {
	test.Helper()
	var encoded bytes.Buffer
	compressed := gzip.NewWriter(&encoded)
	if _, err := compressed.Write(contents); err != nil {
		test.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		test.Fatal(err)
	}
	return encoded.Bytes()
}

func untrackedCodecExpand(test testing.TB, contents []byte) []byte {
	test.Helper()
	compressed, err := gzip.NewReader(bytes.NewReader(contents))
	if err != nil {
		test.Fatal(err)
	}
	defer compressed.Close()
	expanded, err := io.ReadAll(io.LimitReader(compressed, 8<<20))
	if err != nil {
		test.Fatal(err)
	}
	return expanded
}
