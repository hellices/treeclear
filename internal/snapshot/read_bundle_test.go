package snapshot

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/fssecure"
)

func TestReadBundleBindsReceiptAndReturnsOwnedBytes(test *testing.T) {
	receipt, files := readBundleFixture(test, nil)
	maximum := readBundleBytes(files)
	calls := 0
	read := func(ctx context.Context, path string, limits []fssecure.PrivateFileLimit, total int64) ([]fssecure.PrivateFile, error) {
		calls++
		if ctx != test.Context() || path != receipt.Path || total != maximum || len(limits) != len(files) {
			test.Fatalf("unexpected read request: path=%q total=%d limits=%v", path, total, limits)
		}
		for index, limit := range limits {
			if limit.Name != files[index].Name || limit.MaximumBytes != maximum {
				test.Fatalf("limit %d = %#v; want %q/%d", index, limit, files[index].Name, maximum)
			}
		}
		return files, nil
	}
	contents, err := readBundle(test.Context(), receipt, maximum, read)
	if err != nil || calls != 1 {
		test.Fatalf("read bundle: calls=%d error=%v", calls, err)
	}
	if !bytes.Equal(contents.ManifestContents, files[0].Contents) || len(contents.Payloads) != len(requiredPayloads) {
		test.Fatal("bundle reader lost manifest or payloads")
	}
	for _, file := range files[1:] {
		if !bytes.Equal(contents.Payloads[file.Name], file.Contents) {
			test.Fatalf("bundle reader changed %q", file.Name)
		}
	}
	for _, file := range files {
		if len(file.Contents) > 0 {
			file.Contents[0] ^= 1
		}
	}
	if err := VerifyBundle(contents.ManifestContents, contents.Payloads, maximum); err != nil {
		test.Fatalf("returned bytes alias the reader's buffers: %v", err)
	}
}

func TestReadBundleCapsManifestRequest(test *testing.T) {
	receipt, files := readBundleFixture(test, nil)
	maximum := int64(maximumManifestBytes + 1)
	read := func(_ context.Context, _ string, limits []fssecure.PrivateFileLimit, total int64) ([]fssecure.PrivateFile, error) {
		if total != maximum || limits[0].Name != "manifest.json" || limits[0].MaximumBytes != maximumManifestBytes {
			test.Fatalf("manifest request is not bounded: %#v / %d", limits, total)
		}
		for _, limit := range limits[1:] {
			if limit.MaximumBytes != maximum {
				test.Fatalf("unexpected payload bound: %#v", limit)
			}
		}
		return files, nil
	}
	if _, err := readBundle(test.Context(), receipt, maximum, read); err != nil {
		test.Fatal(err)
	}
}

func TestReadBundleRejectsReceiptAndBudgetBeforeIO(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*BundleReceipt, *int64)
		cause  error
	}{
		{"missing ID", func(receipt *BundleReceipt, _ *int64) { receipt.SnapshotID = "" }, fs.ErrInvalid},
		{"invalid ID", func(receipt *BundleReceipt, _ *int64) { receipt.SnapshotID = "snapshot_../outside" }, fs.ErrInvalid},
		{"missing digest", func(receipt *BundleReceipt, _ *int64) { receipt.ManifestDigest = "" }, fs.ErrInvalid},
		{"noncanonical digest", func(receipt *BundleReceipt, _ *int64) {
			receipt.ManifestDigest = strings.ToUpper(receipt.ManifestDigest)
		}, fs.ErrInvalid},
		{"different basename", func(receipt *BundleReceipt, _ *int64) { receipt.SnapshotID = "snapshot_other" }, fs.ErrInvalid},
		{"empty path", func(receipt *BundleReceipt, _ *int64) { receipt.Path = "" }, fs.ErrInvalid},
		{"relative path", func(receipt *BundleReceipt, _ *int64) { receipt.Path = receipt.SnapshotID }, fs.ErrInvalid},
		{"unclean path", func(receipt *BundleReceipt, _ *int64) {
			receipt.Path = filepath.Dir(receipt.Path) + string(os.PathSeparator) + "." + string(os.PathSeparator) + receipt.SnapshotID
		}, fs.ErrInvalid},
		{"NUL path", func(receipt *BundleReceipt, _ *int64) {
			receipt.Path = filepath.Dir(receipt.Path) + "\x00" + string(os.PathSeparator) + receipt.SnapshotID
		}, fs.ErrInvalid},
		{"zero budget", func(_ *BundleReceipt, maximum *int64) { *maximum = 0 }, ErrBundleLimit},
		{"negative budget", func(_ *BundleReceipt, maximum *int64) { *maximum = -1 }, ErrBundleLimit},
		{"overflow budget", func(_ *BundleReceipt, maximum *int64) { *maximum = math.MaxInt64 }, ErrBundleLimit},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			receipt, files := readBundleFixture(test, nil)
			maximum := int64(1 << 20)
			scenario.change(&receipt, &maximum)
			called := false
			read := func(context.Context, string, []fssecure.PrivateFileLimit, int64) ([]fssecure.PrivateFile, error) {
				called = true
				return files, nil
			}
			contents, err := readBundle(test.Context(), receipt, maximum, read)
			assertBundleReadFailure(test, contents, err, scenario.cause)
			if called {
				test.Fatal("invalid input reached the filesystem reader")
			}
		})
	}
}

func TestReadBundleRejectsMissingReaderAndContext(test *testing.T) {
	receipt, files := readBundleFixture(test, nil)
	contents, err := readBundle(test.Context(), receipt, 1<<20, nil)
	assertBundleReadFailure(test, contents, err, fs.ErrInvalid)
	called := false
	read := func(context.Context, string, []fssecure.PrivateFileLimit, int64) ([]fssecure.PrivateFile, error) {
		called = true
		return files, nil
	}
	contents, err = readBundle(nil, receipt, 1<<20, read)
	assertBundleReadFailure(test, contents, err, fs.ErrInvalid)
	ctx, cancel := context.WithCancel(test.Context())
	cancel()
	contents, err = readBundle(ctx, receipt, 1<<20, read)
	assertBundleReadFailure(test, contents, err, context.Canceled)
	if called {
		test.Fatal("invalid context reached the filesystem reader")
	}
}

func TestReadBundleDiscardsPartialReadsAndCancellation(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		cancel bool
		cause  error
	}{
		{"read failure", false, io.ErrUnexpectedEOF},
		{"native limit", false, fssecure.ErrPrivateDirectoryLimit},
		{"cancel after read", true, nil},
		{"cancel with failure", true, io.ErrUnexpectedEOF},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			receipt, files := readBundleFixture(test, nil)
			ctx, cancel := context.WithCancel(test.Context())
			defer cancel()
			read := func(context.Context, string, []fssecure.PrivateFileLimit, int64) ([]fssecure.PrivateFile, error) {
				if scenario.cancel {
					cancel()
				}
				return files, scenario.cause
			}
			contents, err := readBundle(ctx, receipt, 1<<20, read)
			assertBundleReadFailure(test, contents, err, scenario.cause)
			if scenario.cancel && !errors.Is(err, context.Canceled) {
				test.Fatalf("cancellation was lost: %v", err)
			}
			if scenario.cause == fssecure.ErrPrivateDirectoryLimit && !errors.Is(err, ErrBundleLimit) {
				test.Fatalf("bundle limit category was lost: %v", err)
			}
		})
	}
}

func TestReadBundleRejectsUnexpectedFiles(test *testing.T) {
	for _, scenario := range []string{"missing", "extra", "duplicate", "renamed", "reordered"} {
		test.Run(scenario, func(test *testing.T) {
			receipt, files := readBundleFixture(test, nil)
			switch scenario {
			case "missing":
				files = files[:len(files)-1]
			case "extra":
				files = append(files, fssecure.PrivateFile{Name: "extra"})
			case "duplicate":
				files[2] = files[1]
			case "renamed":
				files[2].Name = "STATUS.BIN"
			case "reordered":
				files[1], files[2] = files[2], files[1]
			}
			read := func(context.Context, string, []fssecure.PrivateFileLimit, int64) ([]fssecure.PrivateFile, error) {
				return files, nil
			}
			contents, err := readBundle(test.Context(), receipt, 1<<20, read)
			assertBundleReadFailure(test, contents, err, ErrBundleInvalid, ErrPayloadIntegrity)
		})
	}
}

func TestReadBundleRejectsReturnedByteLimitViolations(test *testing.T) {
	for _, scenario := range []string{"aggregate", "manifest"} {
		test.Run(scenario, func(test *testing.T) {
			receipt, files := readBundleFixture(test, nil)
			maximum := readBundleBytes(files) - 1
			if scenario == "manifest" {
				files[0].Contents = bytes.Repeat([]byte{' '}, maximumManifestBytes+1)
				maximum = 1 << 27
			}
			read := func(context.Context, string, []fssecure.PrivateFileLimit, int64) ([]fssecure.PrivateFile, error) {
				return files, nil
			}
			contents, err := readBundle(test.Context(), receipt, maximum, read)
			assertBundleReadFailure(test, contents, err, ErrBundleLimit)
		})
	}
}

func TestReadBundleKeepsVerificationContractSeparateFromReader(test *testing.T) {
	for _, scenario := range []string{"ordering", "manifest limit"} {
		test.Run(scenario, func(test *testing.T) {
			receipt, files := readBundleFixture(test, nil)
			cause := ErrPayloadIntegrity
			if scenario == "manifest limit" {
				cause = ErrBundleLimit
				files[0].Contents = bytes.Repeat([]byte{' '}, maximumManifestBytes+1)
			}
			read := func(_ context.Context, _ string, limits []fssecure.PrivateFileLimit, maximum int64) ([]fssecure.PrivateFile, error) {
				if scenario == "ordering" {
					limits[1], limits[2] = limits[2], limits[1]
					files[1], files[2] = files[2], files[1]
				} else {
					limits[0].MaximumBytes = maximum
				}
				return files, nil
			}
			contents, err := readBundle(test.Context(), receipt, 1<<27, read)
			assertBundleReadFailure(test, contents, err, ErrBundleInvalid, cause)
		})
	}
}

func TestReadBundleRejectsIntegrityFailures(test *testing.T) {
	for _, scenario := range []struct {
		name  string
		cause error
	}{
		{"wrong receipt digest", ErrPayloadIntegrity},
		{"changed manifest bytes", ErrPayloadIntegrity},
		{"invalid manifest", ErrManifestInvalid},
		{"noncanonical manifest", ErrManifestInvalid},
		{"wrong snapshot ID", ErrManifestInvalid},
		{"changed payload", ErrPayloadIntegrity},
		{"rehashed invalid archive", gzip.ErrHeader},
		{"wrong untracked count", ErrBundleInvalid},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			receipt, files := readBundleFixture(test, nil)
			value, err := DecodeManifest(files[0].Contents)
			if err != nil {
				test.Fatal(err)
			}
			switch scenario.name {
			case "wrong receipt digest":
				receipt.ManifestDigest = "sha256:" + strings.Repeat("0", 64)
			case "changed manifest bytes":
				files[0].Contents[0] ^= 1
			case "invalid manifest":
				files[0].Contents = []byte("{")
				bindReadManifest(&receipt, files)
			case "noncanonical manifest":
				files[0].Contents = append([]byte{' '}, files[0].Contents...)
				bindReadManifest(&receipt, files)
			case "wrong snapshot ID":
				value.SnapshotID = "snapshot_other"
				files[0].Contents = bundleManifest(test, value)
				bindReadManifest(&receipt, files)
			case "changed payload":
				files[1].Contents = append(files[1].Contents, 1)
			case "rehashed invalid archive":
				archive := &files[len(files)-1]
				if archive.Name != "untracked.tar.gz" {
					test.Fatal("fixture payload ordering changed")
				}
				archive.Contents = []byte("not a gzip archive")
				value.Files[archive.Name] = fmt.Sprintf("sha256:%x", sha256.Sum256(archive.Contents))
				files[0].Contents = bundleManifest(test, value)
				bindReadManifest(&receipt, files)
			case "wrong untracked count":
				value.UntrackedFiles++
				files[0].Contents = bundleManifest(test, value)
				bindReadManifest(&receipt, files)
			}
			read := func(context.Context, string, []fssecure.PrivateFileLimit, int64) ([]fssecure.PrivateFile, error) {
				return files, nil
			}
			contents, err := readBundle(test.Context(), receipt, 1<<20, read)
			assertBundleReadFailure(test, contents, err, ErrBundleInvalid, scenario.cause)
		})
	}
}

func TestReadBundleEnforcesExpandedArchiveBudget(test *testing.T) {
	receipt, files := readBundleFixture(test, []UntrackedEntry{
		{Path: "data", Kind: "file", Mode: 0o600, Data: bytes.Repeat([]byte{'a'}, 16<<10)},
	})
	maximum := readBundleBytes(files)
	if maximum >= 16<<10 {
		test.Fatal("compressed fixture does not exercise expansion beyond stored-byte budget")
	}
	read := func(context.Context, string, []fssecure.PrivateFileLimit, int64) ([]fssecure.PrivateFile, error) {
		return files, nil
	}
	contents, err := readBundle(test.Context(), receipt, maximum, read)
	assertBundleReadFailure(test, contents, err, ErrBundleLimit)
}

func readBundleFixture(test testing.TB, entries []UntrackedEntry) (BundleReceipt, []fssecure.PrivateFile) {
	test.Helper()
	value, payloads := bundleFixture(test, entries)
	files := []fssecure.PrivateFile{{Name: "manifest.json", Contents: bundleManifest(test, value)}}
	for _, name := range requiredPayloads {
		files = append(files, fssecure.PrivateFile{Name: name, Contents: payloads[name]})
	}
	receipt := BundleReceipt{SnapshotID: value.SnapshotID, Path: filepath.Join(test.TempDir(), value.SnapshotID)}
	bindReadManifest(&receipt, files)
	return receipt, files
}

func bindReadManifest(receipt *BundleReceipt, files []fssecure.PrivateFile) {
	receipt.ManifestDigest = fmt.Sprintf("sha256:%x", sha256.Sum256(files[0].Contents))
}

func readBundleBytes(files []fssecure.PrivateFile) int64 {
	var total int64
	for _, file := range files {
		total += int64(len(file.Contents))
	}
	return total
}

func assertBundleReadFailure(test testing.TB, contents BundleContents, err error, causes ...error) {
	test.Helper()
	if contents.ManifestContents != nil || contents.Payloads != nil {
		test.Error("failed bundle read returned partial contents")
	}
	for _, cause := range append([]error{ErrBundleRead}, causes...) {
		if cause != nil && !errors.Is(err, cause) {
			test.Errorf("read error = %v; want %v", err, cause)
		}
	}
}
