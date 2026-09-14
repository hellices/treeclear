package snapshot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"slices"
	"strings"
	"testing"
)

func TestUntrackedCodecRejectEntries(test *testing.T) {
	for _, name := range []string{
		"", ".", "..", "/absolute", "../escape", "a/../b", "a/./b", "a//b", "a/",
		`a\b`, "C:relative", "C:/absolute", "a<b", "a>b", "a:b", `a"b`, "a|b", "a?b", "a*b",
		"nul", "NUL.txt", "con", "CONIN$", "CONOUT$", "prn", "aux", "COM1.txt", "lpt9", "COM¹", "LPT²", "COM³",
		"con .txt", "trailing.", "trailing ", "a/COM1/b", ".git", ".GIT/config", "a/.Git/config",
		"a\x00b", "a\nb", "a\x7fb", string([]byte{0xff}),
	} {
		test.Run(fmt.Sprintf("path-%q", name), func(test *testing.T) {
			untrackedCodecEncodeError(test, []UntrackedEntry{{Path: name, Kind: "file", Mode: 0o600}}, 1<<20, ErrUntrackedInvalid)
		})
	}
	for name, entry := range map[string]UntrackedEntry{
		"unknown-kind":         {Path: "entry", Kind: "unknown", Mode: 0o600},
		"file-directory-mode":  {Path: "entry", Kind: "file", Mode: fs.ModeDir | 0o600},
		"file-link-mode":       {Path: "entry", Kind: "file", Mode: fs.ModeSymlink | 0o600},
		"file-target":          {Path: "entry", Kind: "file", Mode: 0o600, LinkTarget: "target"},
		"directory-file-mode":  {Path: "entry", Kind: "directory", Mode: 0o700},
		"directory-data":       {Path: "entry", Kind: "directory", Mode: fs.ModeDir | 0o700, Data: []byte("data")},
		"directory-empty-data": {Path: "entry", Kind: "directory", Mode: fs.ModeDir | 0o700, Data: []byte{}},
		"directory-target":     {Path: "entry", Kind: "directory", Mode: fs.ModeDir | 0o700, LinkTarget: "target"},
		"link-file-mode":       {Path: "entry", Kind: "symlink", Mode: 0o777, LinkTarget: "target"},
		"link-directory-mode":  {Path: "entry", Kind: "symlink", Mode: fs.ModeDir | fs.ModeSymlink | 0o777, LinkTarget: "target"},
		"link-data":            {Path: "entry", Kind: "symlink", Mode: fs.ModeSymlink | 0o777, Data: []byte("data"), LinkTarget: "target"},
		"link-empty-data":      {Path: "entry", Kind: "symlink", Mode: fs.ModeSymlink | 0o777, Data: []byte{}, LinkTarget: "target"},
	} {
		test.Run(name, func(test *testing.T) {
			untrackedCodecEncodeError(test, []UntrackedEntry{entry}, 1<<20, ErrUntrackedInvalid)
		})
	}
	for _, mode := range []fs.FileMode{fs.ModeAppend, fs.ModeExclusive, fs.ModeTemporary, fs.ModeDevice, fs.ModeNamedPipe, fs.ModeSocket, fs.ModeCharDevice, fs.ModeIrregular, 1 << 12, 0o1000} {
		test.Run(fmt.Sprintf("mode-%x", uint32(mode)), func(test *testing.T) {
			untrackedCodecEncodeError(test, []UntrackedEntry{{Path: "entry", Kind: "file", Mode: mode | 0o600}}, 1<<20, ErrUntrackedInvalid)
		})
	}
}

func TestUntrackedCodecLinkTargets(test *testing.T) {
	for _, target := range []string{".", "..", "../target", "./target", "missing/../target", "../folder/./../target", "../한글"} {
		test.Run("valid-"+target, func(test *testing.T) {
			entries := []UntrackedEntry{{Path: "folder/link", Kind: "symlink", Mode: fs.ModeSymlink | 0o777, LinkTarget: target}}
			encoded, err := EncodeUntracked(entries, 1<<20)
			if err != nil {
				test.Fatal(err)
			}
			decoded, err := DecodeUntracked(encoded, 1<<20)
			if err != nil {
				test.Fatal(err)
			}
			untrackedCodecEqualEntries(test, decoded, entries)
		})
	}
	for _, target := range []string{"", "/absolute", "../../escape", "a/../../../escape", `..\escape`, "C:relative", "//host/share", "a//b", "a/", ".git/config", "../.GIT/config", ".git/../target", "nul", "a/CON.txt", "a?b", "bad.", "bad ", "a\x00b", "a\nb", string([]byte{0xff})} {
		test.Run(fmt.Sprintf("invalid-%q", target), func(test *testing.T) {
			untrackedCodecEncodeError(test, []UntrackedEntry{{Path: "folder/link", Kind: "symlink", Mode: fs.ModeSymlink | 0o777, LinkTarget: target}}, 1<<20, ErrUntrackedInvalid)
		})
	}
	untrackedCodecEncodeError(test, []UntrackedEntry{{Path: "link", Kind: "symlink", Mode: fs.ModeSymlink | 0o777, LinkTarget: "../escape"}}, 1<<20, ErrUntrackedInvalid)
}

func TestUntrackedCodecPathCollisions(test *testing.T) {
	for name, entries := range map[string][]UntrackedEntry{
		"duplicate": {
			{Path: "file", Kind: "file"}, {Path: "file", Kind: "file"},
		},
		"folded-duplicate": {
			{Path: "file", Kind: "file"}, {Path: "FILE", Kind: "directory", Mode: fs.ModeDir},
		},
		"unicode-fold": {
			{Path: "K", Kind: "file"}, {Path: "K", Kind: "file"},
		},
		"file-parent": {
			{Path: "parent", Kind: "file"}, {Path: "parent/child", Kind: "file"},
		},
		"link-ancestor": {
			{Path: "parent", Kind: "symlink", Mode: fs.ModeSymlink, LinkTarget: "target"}, {Path: "parent/implicit/child", Kind: "file"},
		},
		"folded-file-parent": {
			{Path: "parent", Kind: "file"}, {Path: "PARENT/child", Kind: "file"},
		},
		"folded-directory-parent": {
			{Path: "parent", Kind: "directory", Mode: fs.ModeDir}, {Path: "parent-gap", Kind: "file"}, {Path: "PARENT/child", Kind: "file"},
		},
		"implicit-directory-alias": {
			{Path: "Parent/left", Kind: "file"}, {Path: "parent/right", Kind: "file"},
		},
		"unicode-directory-alias": {
			{Path: "K/left", Kind: "file"}, {Path: "K/right", Kind: "file"},
		},
	} {
		test.Run(name, func(test *testing.T) {
			for range 2 {
				untrackedCodecEncodeError(test, entries, 1<<20, ErrUntrackedInvalid)
				var records []untrackedCodecRecord
				for _, entry := range entries {
					typeflag := byte(tar.TypeReg)
					if entry.Kind == "directory" {
						typeflag = tar.TypeDir
					} else if entry.Kind == "symlink" {
						typeflag = tar.TypeSymlink
					}
					records = append(records, untrackedCodecRecord{header: tar.Header{Name: entry.Path, Typeflag: typeflag, Linkname: entry.LinkTarget}})
				}
				untrackedCodecDecodeError(test, untrackedCodecGzip(test, untrackedCodecTar(test, records...)), 1<<20, ErrUntrackedInvalid)
				slices.Reverse(entries)
			}
		})
	}
}

func TestUntrackedCodecByteBudgets(test *testing.T) {
	maximumInteger := int64(int(^uint(0) >> 1))
	empty := untrackedCodecGzip(test, untrackedCodecTar(test))
	for _, budget := range []int64{-1, 0, maximumInteger, math.MaxInt64} {
		test.Run(fmt.Sprintf("invalid-%d", budget), func(test *testing.T) {
			untrackedCodecEncodeError(test, nil, budget, ErrUntrackedInvalid)
			untrackedCodecDecodeError(test, empty, budget, ErrUntrackedInvalid)
		})
	}
	for _, budget := range []int64{1024, maximumInteger - 1} {
		encoded, err := EncodeUntracked(nil, budget)
		if err != nil {
			test.Fatalf("safe budget %d: %v", budget, err)
		}
		if _, err := DecodeUntracked(encoded, budget); err != nil {
			test.Fatalf("safe decode budget %d: %v", budget, err)
		}
	}
	untrackedCodecEncodeError(test, nil, 1023, ErrUntrackedLimit)
	untrackedCodecDecodeError(test, empty, 1023, ErrUntrackedLimit)
	entries := []UntrackedEntry{{Path: "file", Kind: "file", Data: []byte{1}}}
	encoded, err := EncodeUntracked(entries, 2048)
	if err != nil {
		test.Fatal(err)
	}
	if expanded := untrackedCodecExpand(test, encoded); len(expanded) != 2048 {
		test.Fatalf("full tar byte accounting: %d", len(expanded))
	}
	if _, err := DecodeUntracked(encoded, 2048); err != nil {
		test.Fatal(err)
	}
	untrackedCodecEncodeError(test, entries, 2047, ErrUntrackedLimit)
	untrackedCodecDecodeError(test, encoded, 2047, ErrUntrackedLimit)
	var larger bytes.Buffer
	compressed, err := gzip.NewWriterLevel(&larger, gzip.NoCompression)
	if err != nil {
		test.Fatal(err)
	}
	if _, err := compressed.Write(make([]byte, 1024)); err != nil {
		test.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		test.Fatal(err)
	}
	if larger.Len() <= 1024 {
		test.Fatal("fixture does not isolate compressed-byte accounting")
	}
	if _, err := DecodeUntracked(larger.Bytes(), int64(larger.Len())); err != nil {
		test.Fatal(err)
	}
	untrackedCodecDecodeError(test, larger.Bytes(), int64(larger.Len()-1), ErrUntrackedLimit)
	untrackedCodecDecodeError(test, untrackedCodecGzip(test, make([]byte, 64<<10)), 1024, ErrUntrackedLimit)
}

func TestUntrackedCodecEntryAndTextLimits(test *testing.T) {
	entries := make([]UntrackedEntry, 4097)
	records := make([]untrackedCodecRecord, len(entries))
	for index := range entries {
		entry := UntrackedEntry{Path: fmt.Sprintf("entry-%04d", index), Kind: "file", Mode: 0o700}
		header := tar.Header{Name: entry.Path, Typeflag: tar.TypeReg, Mode: 0o700}
		switch index % 3 {
		case 1:
			entry.Kind, entry.Mode, header.Typeflag = "directory", fs.ModeDir|0o700, tar.TypeDir
		case 2:
			entry.Kind, entry.Mode, entry.LinkTarget = "symlink", fs.ModeSymlink|0o700, "."
			header.Typeflag, header.Linkname = tar.TypeSymlink, "."
		}
		entries[index], records[index] = entry, untrackedCodecRecord{header: header}
	}
	encoded, err := EncodeUntracked(entries[:4096], 4<<20)
	if err != nil {
		test.Fatal(err)
	}
	decoded, err := DecodeUntracked(encoded, 4<<20)
	if err != nil || len(decoded) != 4096 {
		test.Fatalf("entry boundary: %d, %v", len(decoded), err)
	}
	untrackedCodecEqualEntries(test, decoded, entries[:4096])
	untrackedCodecEncodeError(test, entries, 4<<20, ErrUntrackedLimit)
	untrackedCodecDecodeError(test, untrackedCodecGzip(test, untrackedCodecTar(test, records...)), 4<<20, ErrUntrackedLimit)
	for _, text := range []string{strings.Repeat("x", 4096), strings.Repeat("界", 1365) + "x"} {
		for _, kind := range []string{"file", "symlink"} {
			entry := UntrackedEntry{Path: text, Kind: kind, Mode: 0o600}
			if kind == "symlink" {
				entry.Path, entry.Mode, entry.LinkTarget = "link", fs.ModeSymlink|0o777, text
			}
			encoded, err := EncodeUntracked([]UntrackedEntry{entry}, 32<<10)
			if err != nil {
				test.Fatalf("text byte boundary for %s: %v", kind, err)
			}
			decoded, err := DecodeUntracked(encoded, 32<<10)
			if err != nil {
				test.Fatal(err)
			}
			untrackedCodecEqualEntries(test, decoded, []UntrackedEntry{entry})
			header := tar.Header{Name: text + "x", Typeflag: tar.TypeReg}
			if kind == "symlink" {
				entry.LinkTarget += "x"
				header.Name, header.Typeflag, header.Linkname = "link", tar.TypeSymlink, entry.LinkTarget
			} else {
				entry.Path += "x"
			}
			untrackedCodecEncodeError(test, []UntrackedEntry{entry}, 32<<10, ErrUntrackedLimit)
			untrackedCodecDecodeError(test, untrackedCodecGzip(test, untrackedCodecTar(test, untrackedCodecRecord{header: header})), 32<<10, ErrUntrackedLimit)
		}
	}
}

func TestUntrackedCodecFileDataBudgets(test *testing.T) {
	untrackedCodecEncodeError(test, []UntrackedEntry{{Path: "file", Kind: "file", Data: make([]byte, 8193)}}, 8192, ErrUntrackedLimit)
	untrackedCodecEncodeError(test, []UntrackedEntry{
		{Path: "first", Kind: "file", Data: make([]byte, 5000)},
		{Path: "second", Kind: "file", Data: make([]byte, 5000)},
	}, 8192, ErrUntrackedLimit)
}

func TestUntrackedCodecWriterBudget(test *testing.T) {
	var output bytes.Buffer
	writer := untrackedLimitedWriter{writer: &output, remaining: 3}
	if count, err := writer.Write([]byte("ab")); count != 2 || err != nil {
		test.Fatalf("within-budget write: %d, %v", count, err)
	}
	if count, err := writer.Write([]byte("cd")); count != 0 || !errors.Is(err, ErrUntrackedLimit) || output.String() != "ab" || writer.remaining != 1 {
		test.Fatalf("over-budget write reached the underlying writer: %d, %v", count, err)
	}
	if count, err := writer.Write([]byte("c")); count != 1 || err != nil || output.String() != "abc" || writer.remaining != 0 {
		test.Fatalf("exact-budget write: %d, %v", count, err)
	}
	if count, err := writer.Write(nil); count != 0 || err != nil {
		test.Fatalf("zero-byte write: %d, %v", count, err)
	}
	if count, err := writer.Write([]byte("d")); count != 0 || !errors.Is(err, ErrUntrackedLimit) || output.String() != "abc" {
		test.Fatalf("exhausted-budget write: %d, %v", count, err)
	}
}

func untrackedCodecEncodeError(test testing.TB, entries []UntrackedEntry, maximumBytes int64, wanted error) {
	test.Helper()
	encoded, err := EncodeUntracked(entries, maximumBytes)
	if encoded != nil || !errors.Is(err, wanted) {
		test.Fatalf("EncodeUntracked returned %d bytes, %v; want nil, %v", len(encoded), err, wanted)
	}
}

func untrackedCodecDecodeError(test testing.TB, encoded []byte, maximumBytes int64, wanted error) {
	test.Helper()
	decoded, err := DecodeUntracked(encoded, maximumBytes)
	if decoded != nil || !errors.Is(err, wanted) {
		test.Fatalf("DecodeUntracked returned %d entries, %v; want nil, %v", len(decoded), err, wanted)
	}
}
