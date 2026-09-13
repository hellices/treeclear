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
	"time"
)

const (
	maximumUntrackedEntries = 4096
	maximumUntrackedText    = 4096
	untrackedBlockBytes     = 512
	untrackedPermissionBits = fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky
)

var (
	ErrUntrackedInvalid = errors.New("invalid untracked archive")
	ErrUntrackedLimit   = errors.New("untracked archive limit exceeded")
)

type UntrackedEntry struct {
	Path       string
	Kind       string
	Mode       fs.FileMode
	Data       []byte
	LinkTarget string
}

func EncodeUntracked(entries []UntrackedEntry, maximumBytes int64) ([]byte, error) {
	if err := validateUntrackedBudget(maximumBytes); err != nil {
		return nil, err
	}
	if err := validateUntrackedEntries(entries, maximumBytes); err != nil {
		return nil, err
	}
	entries = slices.Clone(entries)
	sortUntrackedEntries(entries)
	var encoded bytes.Buffer
	compressed := gzip.NewWriter(&untrackedLimitedWriter{writer: &encoded, remaining: maximumBytes})
	archive := tar.NewWriter(&untrackedLimitedWriter{writer: compressed, remaining: maximumBytes})
	for _, entry := range entries {
		header := tar.Header{
			Name: entry.Path, Linkname: entry.LinkTarget, Mode: untrackedTarMode(entry.Mode),
			ModTime: time.Unix(0, 0).UTC(), Format: tar.FormatUSTAR | tar.FormatPAX,
		}
		switch entry.Kind {
		case "file":
			header.Typeflag, header.Size = tar.TypeReg, int64(len(entry.Data))
		case "directory":
			header.Typeflag = tar.TypeDir
		case "symlink":
			header.Typeflag = tar.TypeSymlink
		}
		if err := archive.WriteHeader(&header); err != nil {
			return nil, untrackedFormatError("encode tar header", err)
		}
		if _, err := archive.Write(entry.Data); err != nil {
			return nil, untrackedFormatError("encode tar data", err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, untrackedFormatError("finish tar stream", err)
	}
	if err := compressed.Close(); err != nil {
		return nil, untrackedFormatError("finish gzip member", err)
	}
	return encoded.Bytes(), nil
}

func DecodeUntracked(contents []byte, maximumBytes int64) ([]UntrackedEntry, error) {
	if err := validateUntrackedBudget(maximumBytes); err != nil {
		return nil, err
	}
	if int64(len(contents)) > maximumBytes {
		return nil, fmt.Errorf("%w: compressed bytes", ErrUntrackedLimit)
	}
	input := bytes.NewReader(contents)
	compressed, err := gzip.NewReader(input)
	if err != nil {
		return nil, untrackedFormatError("read gzip header", err)
	}
	compressed.Multistream(false)
	expanded, readErr := io.ReadAll(io.LimitReader(compressed, maximumBytes+1))
	closeErr := compressed.Close()
	if int64(len(expanded)) > maximumBytes {
		return nil, fmt.Errorf("%w: expanded tar bytes", ErrUntrackedLimit)
	}
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, untrackedFormatError("read complete gzip member", err)
	}
	if input.Len() != 0 {
		return nil, fmt.Errorf("%w: appended gzip member or data", ErrUntrackedInvalid)
	}
	if len(expanded) < 2*untrackedBlockBytes || len(expanded)%untrackedBlockBytes != 0 {
		return nil, fmt.Errorf("%w: incomplete or unaligned tar stream", ErrUntrackedInvalid)
	}
	return decodeUntrackedTar(expanded, maximumBytes)
}

func decodeUntrackedTar(expanded []byte, maximumBytes int64) ([]UntrackedEntry, error) {
	input := bytes.NewReader(expanded)
	archive := tar.NewReader(input)
	entries := make([]UntrackedEntry, 0)
	padding := 0
	for {
		before := len(expanded) - input.Len()
		header, err := archive.Next()
		if err == io.EOF {
			consumed := len(expanded) - input.Len() - before
			if consumed != padding+2*untrackedBlockBytes || !untrackedZeroBytes(expanded[before+padding:]) {
				return nil, fmt.Errorf("%w: missing tar end blocks or trailing content", ErrUntrackedInvalid)
			}
			break
		}
		if err != nil {
			return nil, untrackedFormatError("read tar header", err)
		}
		if len(entries) >= maximumUntrackedEntries {
			return nil, fmt.Errorf("%w: entry count", ErrUntrackedLimit)
		}
		entry, err := untrackedHeaderEntry(header, maximumBytes)
		if err != nil {
			return nil, err
		}
		if header.Size > int64(input.Len()) {
			return nil, untrackedFormatError("truncated tar data", io.ErrUnexpectedEOF)
		}
		if header.Size > 0 {
			entry.Data = make([]byte, int(header.Size))
			if _, err := io.ReadFull(archive, entry.Data); err != nil {
				return nil, untrackedFormatError("read tar data", err)
			}
		}
		entries = append(entries, entry)
		padding = int((untrackedBlockBytes - header.Size%untrackedBlockBytes) % untrackedBlockBytes)
	}
	if err := validateUntrackedEntries(entries, maximumBytes); err != nil {
		return nil, err
	}
	sortUntrackedEntries(entries)
	return entries, nil
}

func untrackedHeaderEntry(header *tar.Header, maximumBytes int64) (UntrackedEntry, error) {
	if header.Format != tar.FormatUSTAR && header.Format != tar.FormatPAX {
		return UntrackedEntry{}, fmt.Errorf("%w: unsupported tar format", ErrUntrackedInvalid)
	}
	if header.Devmajor != 0 || header.Devminor != 0 || len(header.Xattrs) != 0 {
		return UntrackedEntry{}, fmt.Errorf("%w: unsupported tar metadata", ErrUntrackedInvalid)
	}
	for key := range header.PAXRecords {
		switch key {
		case "path", "linkpath", "size", "uid", "gid", "uname", "gname", "mtime", "atime", "ctime":
		default:
			return UntrackedEntry{}, fmt.Errorf("%w: unsupported PAX metadata", ErrUntrackedInvalid)
		}
	}
	if header.Mode < 0 || header.Mode&^int64(0o7777) != 0 {
		return UntrackedEntry{}, fmt.Errorf("%w: unsupported tar mode", ErrUntrackedInvalid)
	}
	entry := UntrackedEntry{Path: header.Name, LinkTarget: header.Linkname, Mode: untrackedFileMode(header.Mode)}
	switch header.Typeflag {
	case tar.TypeReg:
		entry.Kind = "file"
	case tar.TypeDir:
		entry.Kind, entry.Mode = "directory", entry.Mode|fs.ModeDir
	case tar.TypeSymlink:
		entry.Kind, entry.Mode = "symlink", entry.Mode|fs.ModeSymlink
	default:
		return UntrackedEntry{}, fmt.Errorf("%w: unsupported tar entry type", ErrUntrackedInvalid)
	}
	if header.Size < 0 || entry.Kind != "file" && header.Size != 0 {
		return UntrackedEntry{}, fmt.Errorf("%w: invalid tar entry size", ErrUntrackedInvalid)
	}
	if header.Size > maximumBytes {
		return UntrackedEntry{}, fmt.Errorf("%w: declared tar entry bytes", ErrUntrackedLimit)
	}
	if err := validateUntrackedEntry(entry); err != nil {
		return UntrackedEntry{}, err
	}
	return entry, nil
}

func untrackedTarMode(mode fs.FileMode) int64 {
	permissions := int64(mode.Perm())
	if mode&fs.ModeSetuid != 0 {
		permissions |= 0o4000
	}
	if mode&fs.ModeSetgid != 0 {
		permissions |= 0o2000
	}
	if mode&fs.ModeSticky != 0 {
		permissions |= 0o1000
	}
	return permissions
}

func untrackedFileMode(mode int64) fs.FileMode {
	permissions := fs.FileMode(mode & 0o777)
	if mode&0o4000 != 0 {
		permissions |= fs.ModeSetuid
	}
	if mode&0o2000 != 0 {
		permissions |= fs.ModeSetgid
	}
	if mode&0o1000 != 0 {
		permissions |= fs.ModeSticky
	}
	return permissions
}

func sortUntrackedEntries(entries []UntrackedEntry) {
	slices.SortFunc(entries, func(left, right UntrackedEntry) int { return strings.Compare(left.Path, right.Path) })
}

func untrackedZeroBytes(contents []byte) bool {
	for _, value := range contents {
		if value != 0 {
			return false
		}
	}
	return true
}

func untrackedFormatError(subject string, err error) error {
	if errors.Is(err, ErrUntrackedLimit) {
		return fmt.Errorf("%s: %w", subject, err)
	}
	if errors.Is(err, tar.ErrFieldTooLong) {
		return fmt.Errorf("%w: %s: %w", ErrUntrackedLimit, subject, err)
	}
	return fmt.Errorf("%w: %s: %w", ErrUntrackedInvalid, subject, err)
}

type untrackedLimitedWriter struct {
	writer    io.Writer
	remaining int64
}

func (writer *untrackedLimitedWriter) Write(contents []byte) (int, error) {
	if int64(len(contents)) > writer.remaining {
		return 0, fmt.Errorf("%w: archive byte budget", ErrUntrackedLimit)
	}
	count, err := writer.writer.Write(contents)
	writer.remaining -= int64(count)
	return count, err
}
