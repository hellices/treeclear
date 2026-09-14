package snapshot

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestUntrackedCodecRejectGlobalHeadersAmongFiles(test *testing.T) {
	for _, metadata := range []struct {
		name    string
		records map[string]string
	}{
		{name: "empty"},
		{name: "owner", records: map[string]string{"uid": "42"}},
		{name: "path", records: map[string]string{"path": "global-name"}},
	} {
		for _, position := range []string{"before", "between", "after", "repeated"} {
			test.Run(metadata.name+"/"+position, func(test *testing.T) {
				global := untrackedCodecRecord{header: tar.Header{
					Name: "metadata", Typeflag: tar.TypeXGlobalHeader, Format: tar.FormatPAX, PAXRecords: metadata.records,
				}}
				first := untrackedCodecRecord{header: tar.Header{Name: "first", Typeflag: tar.TypeReg, Size: 1}, data: []byte{1}}
				last := untrackedCodecRecord{header: tar.Header{Name: "last", Typeflag: tar.TypeReg, Size: 1}, data: []byte{2}}
				var records []untrackedCodecRecord
				wantGlobals := 1
				switch position {
				case "before":
					records = []untrackedCodecRecord{global, first, last}
				case "between":
					records = []untrackedCodecRecord{first, global, last}
				case "after":
					records = []untrackedCodecRecord{first, last, global}
				case "repeated":
					records = []untrackedCodecRecord{global, first, global, last}
					wantGlobals = 2
				}
				expanded := untrackedCodecTar(test, records...)
				oracle := tar.NewReader(bytes.NewReader(expanded))
				globals, files := 0, 0
				for {
					header, err := oracle.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						test.Fatalf("standard tar fixture: %v", err)
					}
					if header.Typeflag == tar.TypeXGlobalHeader {
						globals++
					} else if header.Typeflag == tar.TypeReg {
						files++
					}
				}
				if globals != wantGlobals || files != 2 {
					test.Fatalf("standard reader surfaced %d global headers and %d files; want %d and 2", globals, files, wantGlobals)
				}
				entries, err := DecodeUntracked(untrackedCodecGzip(test, expanded), 1<<20)
				if !errors.Is(err, ErrUntrackedInvalid) || entries != nil {
					test.Fatalf("global metadata with ordinary files returned entries: %#v, %v", entries, err)
				}
			})
		}
	}
}
