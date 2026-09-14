package snapshot

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestUntrackedCodecStandardParserErrorClasses(test *testing.T) {
	for _, malformed := range []struct {
		name          string
		start         int
		field         []byte
		unknownFormat bool
	}{
		{name: "mode-octal", start: 100, field: []byte("0000008\x00")},
		{name: "size-octal", start: 124, field: []byte("00000000008\x00")},
		{name: "size-overflow", start: 124, field: []byte{0x80, 0, 0, 0, 0x80, 0, 0, 0, 0, 0, 0, 0}},
		{name: "unterminated-uid", start: 108, field: []byte("77777777"), unknownFormat: true},
	} {
		test.Run(malformed.name, func(test *testing.T) {
			expanded := untrackedCodecTar(test, untrackedCodecRecord{header: tar.Header{Name: "entry", Typeflag: tar.TypeReg}})
			copy(expanded[malformed.start:], malformed.field)
			untrackedCodecChecksum(expanded[:512])
			header, parseErr := tar.NewReader(bytes.NewReader(expanded)).Next()
			if malformed.unknownFormat {
				if parseErr != nil || header.Format != tar.FormatUnknown {
					test.Fatalf("standard unterminated-field fixture: %#v, %v", header, parseErr)
				}
			} else if !errors.Is(parseErr, tar.ErrHeader) {
				test.Fatalf("standard numeric fixture error = %v", parseErr)
			}
			entries, err := DecodeUntracked(untrackedCodecGzip(test, expanded), 8192)
			if entries != nil || !errors.Is(err, ErrUntrackedInvalid) || errors.Is(err, ErrUntrackedLimit) || errors.Is(err, tar.ErrFieldTooLong) {
				test.Fatalf("malformed numeric field classification: %#v, %v", entries, err)
			}
		})
	}
	for _, metadataBytes := range []int{1 << 20, 1<<20 + 1} {
		test.Run(fmt.Sprintf("metadata-%d", metadataBytes), func(test *testing.T) {
			metadata := []byte(fmt.Sprintf("%d uname=%s\n", metadataBytes, strings.Repeat("u", metadataBytes-15)))
			if len(metadata) != metadataBytes {
				test.Fatal("PAX fixture record length mismatch")
			}
			expanded := untrackedCodecTar(test,
				untrackedCodecRecord{header: tar.Header{Name: "metadata", Typeflag: tar.TypeReg, Size: int64(len(metadata))}, data: metadata},
				untrackedCodecRecord{header: tar.Header{Name: "entry", Typeflag: tar.TypeReg}},
			)
			expanded[156] = tar.TypeXHeader
			untrackedCodecChecksum(expanded[:512])
			encoded := untrackedCodecGzip(test, expanded)
			if len(expanded) >= 2<<20 || len(encoded) >= 2<<20 {
				test.Fatal("fixture reaches the caller's byte budget")
			}
			header, parseErr := tar.NewReader(bytes.NewReader(expanded)).Next()
			entries, err := DecodeUntracked(encoded, 2<<20)
			if metadataBytes == 1<<20 {
				if parseErr != nil || header.Name != "entry" || len(header.Uname) != metadataBytes-15 || err != nil {
					test.Fatalf("valid exact-cap metadata fixture: %v, %v", parseErr, err)
				}
				untrackedCodecEqualEntries(test, entries, []UntrackedEntry{{Path: "entry", Kind: "file"}})
				return
			}
			if !errors.Is(parseErr, tar.ErrFieldTooLong) {
				test.Fatalf("standard metadata-cap error = %v", parseErr)
			}
			if entries != nil || !errors.Is(err, ErrUntrackedLimit) || !errors.Is(err, tar.ErrFieldTooLong) || errors.Is(err, ErrUntrackedInvalid) {
				test.Fatalf("standard metadata capacity classification: %#v, %v", entries, err)
			}
		})
	}
}
