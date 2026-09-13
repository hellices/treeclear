package snapshot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"testing"
)

func TestUntrackedCodecStandardEffectiveEntries(test *testing.T) {
	archive := untrackedCodecTar(test,
		untrackedCodecRecord{header: tar.Header{Name: "z-directory", Typeflag: tar.TypeDir, Mode: 0o2750}},
		untrackedCodecRecord{header: tar.Header{Name: "a-file", Typeflag: tar.TypeReg, Mode: 0o4640, Size: 3}, data: []byte{0, 0xff, 1}},
		untrackedCodecRecord{header: tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Mode: 0o1777, Linkname: "./a-file"}},
	)
	decoded, err := DecodeUntracked(untrackedCodecGzip(test, archive), 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	untrackedCodecEqualEntries(test, decoded, []UntrackedEntry{
		{Path: "a-file", Kind: "file", Mode: fs.ModeSetuid | 0o640, Data: []byte{0, 0xff, 1}},
		{Path: "link", Kind: "symlink", Mode: fs.ModeSymlink | fs.ModeSticky | 0o777, LinkTarget: "./a-file"},
		{Path: "z-directory", Kind: "directory", Mode: fs.ModeDir | fs.ModeSetgid | 0o750},
	})
	legacy := untrackedCodecTar(test, untrackedCodecRecord{header: tar.Header{Name: "legacy", Typeflag: tar.TypeReg, Mode: 0o600}})
	legacy[156] = tar.TypeRegA
	untrackedCodecChecksum(legacy[:512])
	decoded, err = DecodeUntracked(untrackedCodecGzip(test, legacy), 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	untrackedCodecEqualEntries(test, decoded, []UntrackedEntry{{Path: "legacy", Kind: "file", Mode: 0o600}})
}

func TestUntrackedCodecPAXEffectiveEntries(test *testing.T) {
	prefix := untrackedCodecPAXPrefix(test, map[string]string{
		"path": "visible/한글", "size": "3", "uid": "42", "gid": "43", "uname": "user", "gname": "group",
		"mtime": "123.000000001", "atime": "124", "ctime": "125",
	})
	base := untrackedCodecTar(test, untrackedCodecRecord{header: tar.Header{Name: ".git/ignored-raw-name", Typeflag: tar.TypeReg, Mode: 0o640, Size: 1}, data: []byte{42}})
	decoded, err := DecodeUntracked(untrackedCodecGzip(test, append(prefix, base...)), 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	untrackedCodecEqualEntries(test, decoded, []UntrackedEntry{{Path: "visible/한글", Kind: "file", Mode: 0o640, Data: []byte{42, 0, 0}}})
	prefix = untrackedCodecPAXPrefix(test, map[string]string{"linkpath": "./한글/../target"})
	base = untrackedCodecTar(test, untrackedCodecRecord{header: tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Mode: 0o777, Linkname: "/ignored-raw-target"}})
	decoded, err = DecodeUntracked(untrackedCodecGzip(test, append(prefix, base...)), 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	untrackedCodecEqualEntries(test, decoded, []UntrackedEntry{{Path: "link", Kind: "symlink", Mode: fs.ModeSymlink | 0o777, LinkTarget: "./한글/../target"}})
}

func TestUntrackedCodecRejectEffectiveEntries(test *testing.T) {
	for name, header := range map[string]tar.Header{
		"traversal":              {Name: "../escape", Typeflag: tar.TypeReg},
		"absolute":               {Name: "/absolute", Typeflag: tar.TypeReg},
		"backslash":              {Name: `a\b`, Typeflag: tar.TypeReg},
		"git-component":          {Name: "folder/.GIT/config", Typeflag: tar.TypeReg},
		"reserved":               {Name: "CON.txt", Typeflag: tar.TypeReg},
		"control":                {Name: "bad\nname", Typeflag: tar.TypeReg},
		"negative-mode":          {Name: "entry", Typeflag: tar.TypeReg, Mode: -1, Format: tar.FormatGNU},
		"numeric-type-mode":      {Name: "entry", Typeflag: tar.TypeReg, Mode: 0o100644},
		"unsupported-mode":       {Name: "entry", Typeflag: tar.TypeReg, Mode: 0o10000},
		"file-link":              {Name: "entry", Typeflag: tar.TypeReg, Linkname: "target"},
		"directory-link":         {Name: "entry", Typeflag: tar.TypeDir, Linkname: "target"},
		"directory-size":         {Name: "entry", Typeflag: tar.TypeDir, Size: 1},
		"directory-root":         {Name: ".", Typeflag: tar.TypeDir},
		"directory-trailing":     {Name: "directory/", Typeflag: tar.TypeDir},
		"link-size":              {Name: "entry", Typeflag: tar.TypeSymlink, Size: 1, Linkname: "target"},
		"link-empty":             {Name: "entry", Typeflag: tar.TypeSymlink},
		"link-traversal":         {Name: "entry", Typeflag: tar.TypeSymlink, Linkname: "../escape"},
		"link-absolute":          {Name: "entry", Typeflag: tar.TypeSymlink, Linkname: "/absolute"},
		"link-git":               {Name: "entry", Typeflag: tar.TypeSymlink, Linkname: ".git/../target"},
		"link-reserved":          {Name: "entry", Typeflag: tar.TypeSymlink, Linkname: "aux"},
		"device-major":           {Name: "entry", Typeflag: tar.TypeReg, Devmajor: 1},
		"device-minor":           {Name: "entry", Typeflag: tar.TypeReg, Devminor: 1},
		"gnu-header":             {Name: "entry", Typeflag: tar.TypeReg, Format: tar.FormatGNU},
		"gnu-long-name":          {Name: strings.Repeat("x", 150), Typeflag: tar.TypeReg, Format: tar.FormatGNU},
		"gnu-long-link":          {Name: "entry", Typeflag: tar.TypeSymlink, Linkname: strings.Repeat("x", 150), Format: tar.FormatGNU},
		"global":                 {Name: "metadata", Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"path": "entry"}},
		"global-without-records": {Name: "metadata", Typeflag: tar.TypeXGlobalHeader},
	} {
		test.Run(name, func(test *testing.T) {
			untrackedCodecDecodeError(test, untrackedCodecGzip(test, untrackedCodecTar(test, untrackedCodecRecord{header: header})), 1<<20, ErrUntrackedInvalid)
		})
	}
	for _, typeflag := range []byte{tar.TypeLink, tar.TypeChar, tar.TypeBlock, tar.TypeFifo, tar.TypeCont, tar.TypeGNUSparse, 'D', 'Z'} {
		test.Run(fmt.Sprintf("type-%c", typeflag), func(test *testing.T) {
			archive := untrackedCodecTar(test, untrackedCodecRecord{header: tar.Header{Name: "entry", Typeflag: tar.TypeReg}})
			archive[156] = typeflag
			untrackedCodecChecksum(archive[:512])
			untrackedCodecDecodeError(test, untrackedCodecGzip(test, archive), 1<<20, ErrUntrackedInvalid)
		})
	}
	for name, records := range map[string]map[string]string{
		"path-traversal": {"path": "../escape"},
		"path-git":       {"path": "dir/.GiT/config"},
		"path-limit":     {"path": strings.Repeat("x", 4097)},
		"size-limit":     {"size": "9223372036854775807"},
		"size-negative":  {"size": "-1"},
		"sparse":         {"GNU.sparse.map": "", "GNU.sparse.numblocks": "0", "GNU.sparse.size": "1099511627776"},
		"sparse-unknown": {"GNU.sparse.major": "9", "GNU.sparse.minor": "9"},
		"xattr":          {"SCHILY.xattr.user.test": "value"},
		"acl":            {"SCHILY.acl.access": "user::rwx"},
		"vendor":         {"TREECLEAR.unknown": "value"},
		"comment":        {"comment": "unsupported metadata"},
	} {
		test.Run("pax-"+name, func(test *testing.T) {
			wanted := ErrUntrackedInvalid
			if name == "path-limit" || name == "size-limit" {
				wanted = ErrUntrackedLimit
			}
			prefix := untrackedCodecPAXPrefix(test, records)
			base := untrackedCodecTar(test, untrackedCodecRecord{header: tar.Header{Name: "entry", Typeflag: tar.TypeReg}})
			untrackedCodecDecodeError(test, untrackedCodecGzip(test, append(prefix, base...)), 1<<20, wanted)
		})
	}
	prefix := untrackedCodecPAXPrefix(test, map[string]string{"linkpath": "../escape"})
	base := untrackedCodecTar(test, untrackedCodecRecord{header: tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "target"}})
	untrackedCodecDecodeError(test, untrackedCodecGzip(test, append(prefix, base...)), 1<<20, ErrUntrackedInvalid)
}

func TestUntrackedCodecGzipFraming(test *testing.T) {
	archive := untrackedCodecTar(test, untrackedCodecRecord{header: tar.Header{Name: "file", Typeflag: tar.TypeReg, Size: 3}, data: []byte{0, 1, 0xff}})
	encoded := untrackedCodecGzip(test, archive)
	for length := 0; length < len(encoded); length++ {
		test.Run(fmt.Sprintf("truncated-%d", length), func(test *testing.T) {
			untrackedCodecDecodeError(test, encoded[:length], 1<<20, ErrUntrackedInvalid)
		})
	}
	for name, suffix := range map[string][]byte{
		"garbage":       []byte("trailing garbage"),
		"zero":          {0},
		"second-member": encoded,
		"empty-member":  untrackedCodecGzip(test, nil),
	} {
		test.Run(name, func(test *testing.T) {
			untrackedCodecDecodeError(test, append(slices.Clone(encoded), suffix...), 1<<20, ErrUntrackedInvalid)
		})
	}
	for name, offset := range map[string]int{"magic": 0, "method": 2, "checksum": len(encoded) - 8, "size": len(encoded) - 4} {
		test.Run(name, func(test *testing.T) {
			corrupt := slices.Clone(encoded)
			corrupt[offset] ^= 0xff
			untrackedCodecDecodeError(test, corrupt, 1<<20, ErrUntrackedInvalid)
		})
	}
	untrackedCodecDecodeError(test, untrackedCodecGzip(test, nil), 1<<20, ErrUntrackedInvalid)
}

func TestUntrackedCodecTarFraming(test *testing.T) {
	archive := untrackedCodecTar(test, untrackedCodecRecord{header: tar.Header{Name: "file", Typeflag: tar.TypeReg, Size: 1024}, data: make([]byte, 1024)})
	second := untrackedCodecTar(test, untrackedCodecRecord{header: tar.Header{Name: "hidden", Typeflag: tar.TypeReg}})
	orphan := append(untrackedCodecPAXPrefix(test, map[string]string{"path": "unused"}), make([]byte, 1024)...)
	badChecksum := slices.Clone(archive)
	badChecksum[0] ^= 1
	badEnd := slices.Clone(archive)
	badEnd[len(badEnd)-512] = 1
	for name, expanded := range map[string][]byte{
		"empty":                    nil,
		"one-end-block":            make([]byte, 512),
		"missing-both-end-blocks":  archive[:len(archive)-1024],
		"missing-second-end-block": archive[:len(archive)-512],
		"truncated-header":         archive[:511],
		"truncated-data":           archive[:1024],
		"unaligned-zero-suffix":    append(slices.Clone(archive), 0),
		"nonzero-suffix":           append(slices.Clone(archive), bytes.Repeat([]byte{1}, 512)...),
		"hidden-second-archive":    append(slices.Clone(archive), second...),
		"hidden-after-padding":     append(append(slices.Clone(archive), make([]byte, 512)...), second...),
		"orphan-pax-header":        orphan,
		"bad-checksum":             badChecksum,
		"bad-end-block":            badEnd,
	} {
		test.Run(name, func(test *testing.T) {
			untrackedCodecDecodeError(test, untrackedCodecGzip(test, expanded), 1<<20, ErrUntrackedInvalid)
		})
	}
	for _, extra := range []int{0, 512, 2048} {
		expanded := append(slices.Clone(archive), make([]byte, extra)...)
		decoded, err := DecodeUntracked(untrackedCodecGzip(test, expanded), int64(len(expanded)))
		if err != nil {
			test.Fatalf("valid tar padding %d: %v", extra, err)
		}
		untrackedCodecEqualEntries(test, decoded, []UntrackedEntry{{Path: "file", Kind: "file", Data: make([]byte, 1024)}})
		untrackedCodecDecodeError(test, untrackedCodecGzip(test, expanded), int64(len(expanded)-1), ErrUntrackedLimit)
	}
}

func TestUntrackedCodecMetadataBudget(test *testing.T) {
	entries := []UntrackedEntry{{Path: strings.Repeat("x", 2048), Kind: "file"}}
	encoded, err := EncodeUntracked(entries, 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	expanded := untrackedCodecExpand(test, encoded)
	if len(expanded) <= 2048 || expanded[156] != tar.TypeXHeader {
		test.Fatal("fixture lacks PAX metadata overhead")
	}
	if _, err := EncodeUntracked(entries, int64(len(expanded))); err != nil {
		test.Fatal(err)
	}
	if _, err := DecodeUntracked(encoded, int64(len(expanded))); err != nil {
		test.Fatal(err)
	}
	untrackedCodecEncodeError(test, entries, int64(len(expanded)-1), ErrUntrackedLimit)
	untrackedCodecDecodeError(test, encoded, int64(len(expanded)-1), ErrUntrackedLimit)
	prefix := untrackedCodecPAXPrefix(test, map[string]string{"uname": strings.Repeat("u", 8192)})
	base := untrackedCodecTar(test, untrackedCodecRecord{header: tar.Header{Name: "file", Typeflag: tar.TypeReg}})
	untrackedCodecDecodeError(test, untrackedCodecGzip(test, append(prefix, base...)), 2048, ErrUntrackedLimit)
}

func TestUntrackedCodecWrappedCausesAndNoPartialOutput(test *testing.T) {
	archive := untrackedCodecTar(test, untrackedCodecRecord{header: tar.Header{Name: "file", Typeflag: tar.TypeReg}})
	encoded := untrackedCodecGzip(test, archive)
	corrupt := slices.Clone(encoded)
	corrupt[len(corrupt)-8] ^= 1
	untrackedCodecDecodeError(test, corrupt, 1<<20, gzip.ErrChecksum)
	untrackedCodecDecodeError(test, encoded[:len(encoded)-1], 1<<20, io.ErrUnexpectedEOF)
	archive[0] ^= 1
	untrackedCodecDecodeError(test, untrackedCodecGzip(test, archive), 1<<20, tar.ErrHeader)
	partial := untrackedCodecTar(test,
		untrackedCodecRecord{header: tar.Header{Name: "valid", Typeflag: tar.TypeReg, Size: 1}, data: []byte{1}},
		untrackedCodecRecord{header: tar.Header{Name: "../invalid", Typeflag: tar.TypeReg}},
	)
	untrackedCodecDecodeError(test, untrackedCodecGzip(test, partial), 1<<20, ErrUntrackedInvalid)
	untrackedCodecEncodeError(test, []UntrackedEntry{{Path: "valid", Kind: "file"}, {Path: "../invalid", Kind: "file"}}, 1<<20, ErrUntrackedInvalid)
	decoded, err := DecodeUntracked(nil, 1<<20)
	if decoded != nil || !errors.Is(err, ErrUntrackedInvalid) {
		test.Fatalf("empty input: %#v, %v", decoded, err)
	}
}

func untrackedCodecPAXPrefix(test testing.TB, records map[string]string) []byte {
	test.Helper()
	expanded := untrackedCodecTar(test, untrackedCodecRecord{header: tar.Header{Name: "metadata", Typeflag: tar.TypeXGlobalHeader, PAXRecords: records}})
	if len(expanded) < 1536 || expanded[156] != tar.TypeXGlobalHeader {
		test.Fatal("unexpected standard PAX fixture layout")
	}
	expanded[156] = tar.TypeXHeader
	untrackedCodecChecksum(expanded[:512])
	return expanded[:len(expanded)-1024]
}

func untrackedCodecChecksum(header []byte) {
	for index := 148; index < 156; index++ {
		header[index] = ' '
	}
	checksum := 0
	for _, value := range header {
		checksum += int(value)
	}
	copy(header[148:156], fmt.Sprintf("%06o\x00 ", checksum))
}
