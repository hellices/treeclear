//go:build darwin || linux || windows

package snapshot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/testutil"
)

func TestUntrackedArchiveManifestComposition(test *testing.T) {
	const byteBudget = 1 << 20
	repository := testutil.NewRepository(test)
	worktree := repository.AddWorktree(test, "archive-codec", "topic")
	directory := filepath.Join(worktree, "assets")
	filename := filepath.Join(directory, "원본 payload.bin")
	if err := os.Mkdir(directory, 0o750); err != nil {
		test.Fatal(err)
	}
	wantData := bytes.Repeat([]byte{0, 1, 0xff, '\n'}, 513)
	if err := os.WriteFile(filename, wantData, 0o600); err != nil {
		test.Fatal(err)
	}
	fileInfo, err := os.Stat(filename)
	if err != nil {
		test.Fatal(err)
	}
	directoryInfo, err := os.Stat(directory)
	if err != nil {
		test.Fatal(err)
	}
	entries := []UntrackedEntry{
		{Path: "assets/원본 payload.bin", Kind: "file", Mode: fileInfo.Mode(), Data: bytes.Clone(wantData)},
		{Path: "assets", Kind: "directory", Mode: directoryInfo.Mode()},
	}
	archive, err := EncodeUntracked(entries, byteBudget)
	if err != nil {
		test.Fatalf("encode untracked fixture: %v", err)
	}
	if entries[0].Path != "assets/원본 payload.bin" || !bytes.Equal(entries[0].Data, wantData) {
		test.Fatal("encoding mutated caller-owned entries")
	}
	assertUntrackedStandardArchive(test, archive, entries, byteBudget)
	permuted, err := EncodeUntracked([]UntrackedEntry{entries[1], entries[0]}, byteBudget)
	if err != nil || !bytes.Equal(permuted, archive) {
		test.Fatalf("archive depends on input order: %v", err)
	}
	manifest := gitManifestFixture(test, repository, worktree)
	manifest.UntrackedFiles = 1
	manifest.UntrackedBytes = int64(len(wantData))
	payloads := payloadFixture()
	payloads["untracked.tar.gz"] = archive
	manifest.Files["untracked.tar.gz"] = fmt.Sprintf("sha256:%x", sha256.Sum256(archive))
	encodedManifest, err := EncodeManifest(manifest)
	if err != nil {
		test.Fatal(err)
	}
	loadedManifest, err := DecodeManifest(encodedManifest)
	if err != nil {
		test.Fatal(err)
	}
	if err := VerifyPayloads(loadedManifest, payloads); err != nil {
		test.Fatalf("archive did not compose with existing manifest integrity: %v", err)
	}
	decoded, err := DecodeUntracked(payloads["untracked.tar.gz"], byteBudget)
	wantEntries := []UntrackedEntry{entries[1], entries[0]}
	if err != nil || !reflect.DeepEqual(decoded, wantEntries) {
		test.Fatalf("decoded archive changed original bytes or modes: %#v, %v", decoded, err)
	}
	decoded[1].Data[0] ^= 0xff
	loadedAgain, err := DecodeUntracked(archive, byteBudget)
	if err != nil || !reflect.DeepEqual(loadedAgain, wantEntries) {
		test.Fatalf("decoded bytes alias a later read: %#v, %v", loadedAgain, err)
	}
	corrupt := bytes.Clone(archive)
	corrupt[len(corrupt)-1] ^= 0xff
	payloads["untracked.tar.gz"] = corrupt
	if err := VerifyPayloads(loadedManifest, payloads); !errors.Is(err, ErrPayloadIntegrity) {
		test.Fatalf("corrupt archive passed manifest verification: %v", err)
	}
	loadedManifest.Files["untracked.tar.gz"] = fmt.Sprintf("sha256:%x", sha256.Sum256(corrupt))
	if err := VerifyPayloads(loadedManifest, payloads); err != nil {
		test.Fatalf("matching hashes must remain distinct from archive format checks: %v", err)
	}
	if partial, err := DecodeUntracked(corrupt, byteBudget); err == nil || partial != nil {
		test.Fatalf("corrupt gzip trailer returned entries: %#v, %v", partial, err)
	}
	remaining, err := os.ReadFile(filename)
	if err != nil || !bytes.Equal(remaining, wantData) {
		test.Fatalf("codec changed source bytes: %v", err)
	}
	remainingInfo, err := os.Stat(filename)
	if err != nil || remainingInfo.Mode() != fileInfo.Mode() {
		test.Fatalf("codec changed source mode: %v", err)
	}
	if after := gitManifestFixture(test, repository, worktree); !reflect.DeepEqual(after.AdministrativeEntries, manifest.AdministrativeEntries) {
		test.Fatal("codec changed source Git administrative metadata")
	}
}

func assertUntrackedStandardArchive(test *testing.T, contents []byte, entries []UntrackedEntry, byteBudget int64) {
	test.Helper()
	compressed, err := gzip.NewReader(bytes.NewReader(contents))
	if err != nil {
		test.Fatal(err)
	}
	expanded, readErr := io.ReadAll(io.LimitReader(compressed, byteBudget+1))
	closeErr := compressed.Close()
	if readErr != nil || closeErr != nil || int64(len(expanded)) > byteBudget {
		test.Fatalf("standard gzip reader: %v, %v, %d bytes", readErr, closeErr, len(expanded))
	}
	wanted := make(map[string]UntrackedEntry, len(entries))
	for _, entry := range entries {
		wanted[entry.Path] = entry
	}
	reader := tar.NewReader(bytes.NewReader(expanded))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			test.Fatal(err)
		}
		name := strings.TrimSuffix(header.Name, "/")
		entry, found := wanted[name]
		if !found || header.FileInfo().Mode() != entry.Mode || header.Linkname != entry.LinkTarget {
			test.Fatalf("unexpected standard tar header: %#v", header)
		}
		data, err := io.ReadAll(reader)
		if err != nil || !bytes.Equal(data, entry.Data) {
			test.Fatalf("standard tar bytes for %q: %v", name, err)
		}
		delete(wanted, name)
	}
	if len(wanted) != 0 {
		test.Fatalf("standard tar reader missed %d entries", len(wanted))
	}
}
