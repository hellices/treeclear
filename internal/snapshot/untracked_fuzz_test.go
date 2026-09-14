package snapshot

import (
	"archive/tar"
	"bytes"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
)

func FuzzUntrackedCodecDecode(fuzz *testing.F) {
	empty := untrackedCodecGzip(fuzz, untrackedCodecTar(fuzz))
	ordinary := untrackedCodecGzip(fuzz, untrackedCodecTar(fuzz, untrackedCodecRecord{
		header: tar.Header{Name: "file", Typeflag: tar.TypeReg, Mode: 0o4750, Size: 3}, data: []byte{0, 1, 0xff},
	}))
	corrupt := slices.Clone(ordinary)
	corrupt[len(corrupt)-8] ^= 1
	for _, seed := range [][]byte{nil, {0}, empty, ordinary, corrupt, append(slices.Clone(empty), empty...), ordinary[:len(ordinary)-1]} {
		fuzz.Add(seed, uint16(8192))
	}
	fuzz.Add(empty, uint16(1023))
	fuzz.Fuzz(func(test *testing.T, contents []byte, budget uint16) {
		if len(contents) > 32<<10 {
			return
		}
		maximumBytes := int64(budget%32768) + 1
		untrackedCodecFuzzDecode(test, contents, maximumBytes)
	})
}

func FuzzUntrackedCodecTar(fuzz *testing.F) {
	ordinary := untrackedCodecTar(fuzz, untrackedCodecRecord{header: tar.Header{Name: "file", Typeflag: tar.TypeReg, Size: 3}, data: []byte{0, 1, 0xff}})
	longName := untrackedCodecTar(fuzz, untrackedCodecRecord{header: tar.Header{Name: "한글/" + strings.Repeat("x", 150), Typeflag: tar.TypeReg}})
	unsafe := untrackedCodecTar(fuzz, untrackedCodecRecord{header: tar.Header{Name: "../escape", Typeflag: tar.TypeReg}})
	huge := append(untrackedCodecPAXPrefix(fuzz, map[string]string{"size": "9223372036854775807"}), ordinary...)
	for _, seed := range [][]byte{nil, make([]byte, 512), make([]byte, 1024), ordinary, longName, unsafe, huge, ordinary[:len(ordinary)-512], append(slices.Clone(ordinary), ordinary...)} {
		fuzz.Add(seed)
	}
	fuzz.Fuzz(func(test *testing.T, expanded []byte) {
		if len(expanded) > 32<<10 {
			return
		}
		untrackedCodecFuzzDecode(test, untrackedCodecGzip(test, expanded), 16<<10)
	})
}

func FuzzUntrackedCodecEncode(fuzz *testing.F) {
	fuzz.Add("file", "", []byte{0, 1, 0xff}, uint32(0o600), uint8(0))
	fuzz.Add("한글/file", "", []byte{}, uint32(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky|0o751), uint8(0))
	fuzz.Add("directory", "", []byte{}, uint32(fs.ModeDir|0o750), uint8(1))
	fuzz.Add("folder/link", "../target", []byte{}, uint32(fs.ModeSymlink|0o777), uint8(2))
	fuzz.Add(".git/config", "", []byte{}, uint32(0o600), uint8(0))
	fuzz.Add("link", "../escape", []byte{}, uint32(fs.ModeSymlink|0o777), uint8(2))
	fuzz.Add("file", "", []byte{}, uint32(fs.ModeSocket), uint8(3))
	fuzz.Fuzz(func(test *testing.T, name, target string, data []byte, mode uint32, kind uint8) {
		if len(name) > 4097 || len(target) > 4097 || len(data) > 8192 {
			return
		}
		kinds := [...]string{"file", "directory", "symlink", "unsupported"}
		entry := UntrackedEntry{Path: name, Kind: kinds[kind%4], Mode: fs.FileMode(mode), LinkTarget: target}
		if entry.Kind == "file" || len(data) > 0 {
			entry.Data = data
		}
		wantedData := slices.Clone(data)
		encoded, err := EncodeUntracked([]UntrackedEntry{entry}, 32<<10)
		if !bytes.Equal(data, wantedData) {
			test.Fatal("encoding modified fuzz input")
		}
		if err != nil {
			if encoded != nil || !errors.Is(err, ErrUntrackedInvalid) && !errors.Is(err, ErrUntrackedLimit) {
				test.Fatalf("unclassified encoding failure: %v", err)
			}
			return
		}
		decoded, err := DecodeUntracked(encoded, 32<<10)
		if err != nil {
			test.Fatalf("encoded archive failed to decode: %v", err)
		}
		untrackedCodecEqualEntries(test, decoded, []UntrackedEntry{entry})
	})
}

func untrackedCodecFuzzDecode(test *testing.T, contents []byte, maximumBytes int64) {
	test.Helper()
	wanted := slices.Clone(contents)
	decoded, err := DecodeUntracked(contents, maximumBytes)
	if !bytes.Equal(contents, wanted) {
		test.Fatal("decoding modified fuzz input")
	}
	if err != nil {
		if decoded != nil || !errors.Is(err, ErrUntrackedInvalid) && !errors.Is(err, ErrUntrackedLimit) {
			test.Fatalf("unclassified decoding failure: %v", err)
		}
		return
	}
	encoded, err := EncodeUntracked(decoded, 1<<20)
	if err != nil {
		test.Fatalf("accepted entries cannot be encoded: %v", err)
	}
	again, err := DecodeUntracked(encoded, 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	untrackedCodecEqualEntries(test, again, decoded)
}
