package snapshot

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestUntrackedCodecFinalPaddingRejectsHiddenBytes(test *testing.T) {
	for _, profile := range []struct {
		name string
		path string
	}{
		{name: "ustar", path: "last"},
		{name: "pax", path: "last/한글-" + strings.Repeat("x", 160)},
	} {
		for _, size := range []int{1, 511, 513, 1023} {
			test.Run(fmt.Sprintf("%s/%d", profile.name, size), func(test *testing.T) {
				data := bytes.Repeat([]byte{0x31}, size)
				expanded := untrackedCodecTar(test,
					untrackedCodecRecord{header: tar.Header{Name: "first", Typeflag: tar.TypeReg, Size: 1}, data: []byte{1}},
					untrackedCodecRecord{header: tar.Header{Name: profile.path, Typeflag: tar.TypeReg, Size: int64(size)}, data: data},
				)
				decoded, err := DecodeUntracked(untrackedCodecGzip(test, expanded), 1<<20)
				if err != nil {
					test.Fatalf("valid padding fixture: %v", err)
				}
				untrackedCodecEqualEntries(test, decoded, []UntrackedEntry{
					{Path: "first", Kind: "file", Data: []byte{1}},
					{Path: profile.path, Kind: "file", Data: data},
				})
				padding := (512 - size%512) % 512
				start := len(expanded) - 1024 - padding
				if padding == 0 || start < 512 || !bytes.Equal(expanded[start:start+padding], make([]byte, padding)) {
					test.Fatal("fixture lacks final zero alignment padding")
				}
				offsets := []int{0}
				if padding > 2 {
					offsets = append(offsets, padding/2)
				}
				if padding > 1 {
					offsets = append(offsets, padding-1)
				}
				for _, offset := range offsets {
					test.Run(fmt.Sprintf("hidden-byte-%d", offset), func(test *testing.T) {
						corrupt := slices.Clone(expanded)
						corrupt[start+offset] = 0xff
						entries, err := DecodeUntracked(untrackedCodecGzip(test, corrupt), 1<<20)
						if !errors.Is(err, ErrUntrackedInvalid) || entries != nil {
							test.Fatalf("nonzero trailing alignment padding returned %d entries, error %v", len(entries), err)
						}
					})
				}
			})
		}
	}
}
