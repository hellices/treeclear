package snapshot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"reflect"
	"testing"
)

func TestVerifyBundleValid(test *testing.T) {
	for _, scenario := range []struct {
		name    string
		entries []UntrackedEntry
		files   int
		data    int64
	}{
		{name: "empty"},
		{name: "empty file", entries: []UntrackedEntry{{Path: "empty", Kind: "file", Mode: 0o600}}, files: 1},
		{name: "directory only", entries: []UntrackedEntry{{Path: "empty", Kind: "directory", Mode: fs.ModeDir | 0o700}}},
		{name: "files and symlink", entries: []UntrackedEntry{
			{Path: "assets", Kind: "directory", Mode: fs.ModeDir | 0o700},
			{Path: "assets/data", Kind: "file", Mode: 0o640, Data: []byte{0, 1, 0xff}},
			{Path: "assets/link", Kind: "symlink", Mode: fs.ModeSymlink | 0o777, LinkTarget: "data"},
		}, files: 2, data: 3},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			value, payloads := bundleFixture(test, scenario.entries)
			value.UntrackedFiles, value.UntrackedBytes = scenario.files, scenario.data
			if err := VerifyBundle(bundleManifest(test, value), payloads, 1<<20); err != nil {
				test.Fatalf("valid bundle rejected: %v", err)
			}
		})
	}
}

func TestVerifyBundleWholeByteBoundary(test *testing.T) {
	value, payloads := bundleFixture(test, nil)
	contents := bundleManifest(test, value)
	budget := bundleBytes(contents, payloads)
	if _, err := DecodeUntracked(payloads["untracked.tar.gz"], budget-1); err != nil {
		test.Fatalf("archive limit would mask the whole-byte boundary: %v", err)
	}
	if err := VerifyBundle(contents, payloads, budget); err != nil {
		test.Fatalf("exact whole-byte fit rejected: %v", err)
	}
	assertBundleError(test, VerifyBundle(contents, payloads, budget-1), ErrBundleLimit)
	assertBundleError(test, VerifyBundle(contents, payloads, budget-int64(len(contents))), ErrBundleLimit)
	for _, name := range requiredPayloads {
		test.Run(name, func(test *testing.T) {
			if len(payloads[name]) == 0 {
				test.Fatal("accounting fixture must have nonempty payloads")
			}
			assertBundleError(test, VerifyBundle(contents, payloads, budget-int64(len(payloads[name]))), ErrBundleLimit)
		})
	}
}

func TestVerifyBundleCountsEncodedAdministrativeOverhead(test *testing.T) {
	value, payloads := bundleFixture(test, nil)
	var administrativeBytes int64
	for index := range value.AdministrativeEntries {
		if value.AdministrativeEntries[index].Path == "index" {
			value.AdministrativeEntries[index].Data = bytes.Repeat([]byte{0xff}, 4096)
		}
		administrativeBytes += int64(len(value.AdministrativeEntries[index].Data))
	}
	contents := bundleManifest(test, value)
	budget := bundleBytes(contents, payloads)
	if err := VerifyBundle(contents, payloads, budget); err != nil {
		test.Fatalf("exact encoded manifest and payload budget rejected: %v", err)
	}
	dataOnly := budget - int64(len(contents)) + administrativeBytes
	assertBundleError(test, VerifyBundle(contents, payloads, dataOnly), ErrBundleLimit)
	assertBundleError(test, VerifyBundle(contents, payloads, budget-1), ErrBundleLimit)
}

func TestVerifyBundleBudgetValidation(test *testing.T) {
	value, payloads := bundleFixture(test, nil)
	contents := bundleManifest(test, value)
	for _, budget := range []int64{-1, 0, int64(int(^uint(0) >> 1)), math.MaxInt64} {
		test.Run(fmt.Sprint(budget), func(test *testing.T) {
			err := VerifyBundle(contents, payloads, budget)
			assertBundleError(test, err, ErrBundleLimit, ErrUntrackedInvalid)
		})
	}
	if err := VerifyBundle(contents, payloads, int64(int(^uint(0)>>1))-1); err != nil {
		test.Fatalf("largest safely representable budget rejected: %v", err)
	}
}

func TestVerifyBundlePreflightsBytesBeforeContent(test *testing.T) {
	value, payloads := bundleFixture(test, nil)
	contents := bundleManifest(test, value)
	payloads["worktree-list.bin"][0] ^= 1
	err := VerifyBundle(contents, payloads, bundleBytes(contents, payloads)-1)
	assertBundleError(test, err, ErrBundleLimit)
	if errors.Is(err, ErrPayloadIntegrity) {
		test.Fatal("hashed corrupt payload before rejecting aggregate size")
	}
	err = VerifyBundle([]byte("not canonical JSON"), payloads, 1)
	assertBundleError(test, err, ErrBundleLimit)
	if errors.Is(err, ErrManifestInvalid) {
		test.Fatal("decoded oversized manifest before rejecting aggregate size")
	}
	err = VerifyBundle([]byte("{"), payloads, int64(len(payloads["worktree-list.bin"])))
	assertBundleError(test, err, ErrBundleLimit)
	if errors.Is(err, ErrManifestInvalid) {
		test.Fatal("decoded manifest before checking payload byte lengths")
	}
}

func TestVerifyBundlePayloadIntegrity(test *testing.T) {
	for _, name := range requiredPayloads {
		for _, operation := range []string{"missing", "renamed", "corrupt"} {
			test.Run(name+"/"+operation, func(test *testing.T) {
				value, payloads := bundleFixture(test, nil)
				switch operation {
				case "missing":
					delete(payloads, name)
				case "renamed":
					payloads["unexpected"] = payloads[name]
					delete(payloads, name)
				case "corrupt":
					payloads[name] = append(bytes.Clone(payloads[name]), 1)
				}
				assertBundleError(test, VerifyBundle(bundleManifest(test, value), payloads, 1<<20), ErrPayloadIntegrity)
			})
		}
	}
	value, payloads := bundleFixture(test, nil)
	payloads["extra"] = nil
	assertBundleError(test, VerifyBundle(bundleManifest(test, value), payloads, 1<<20), ErrPayloadIntegrity)
	assertBundleError(test, VerifyBundle(bundleManifest(test, value), nil, 1<<20), ErrPayloadIntegrity)
}

func TestVerifyBundleManifestValidation(test *testing.T) {
	value, payloads := bundleFixture(test, nil)
	contents := bundleManifest(test, value)
	for _, malformed := range [][]byte{nil, []byte("{}"), append([]byte(" "), contents...), append(bytes.Clone(contents), '\n')} {
		assertBundleError(test, VerifyBundle(malformed, payloads, 1<<20), ErrManifestInvalid)
	}
	value.UntrackedFiles = -1
	malformed, err := json.Marshal(value)
	if err != nil {
		test.Fatal(err)
	}
	assertBundleError(test, VerifyBundle(malformed, payloads, 1<<20), ErrManifestInvalid)
}

func TestVerifyBundlePreservesManifestLimits(test *testing.T) {
	test.Run("serialized document", func(test *testing.T) {
		_, payloads := bundleFixture(test, nil)
		assertBundleError(test, VerifyBundle(bytes.Repeat([]byte{' '}, maximumManifestBytes+1), payloads, 1<<27), ErrBundleLimit, ErrManifestLimit)
	})
	for _, scenario := range []string{"administrative data", "administrative count"} {
		test.Run(scenario, func(test *testing.T) {
			value, payloads := bundleFixture(test, nil)
			if scenario == "administrative data" {
				value.AdministrativeEntries = []AdminEntry{{Path: "index", Kind: "file", Mode: 0o600, Data: make([]byte, maximumAdministrativeBytes+1)}}
			} else {
				value.AdministrativeEntries = make([]AdminEntry, maximumAdministrativeEntries+1)
			}
			contents, err := json.Marshal(value)
			if err != nil {
				test.Fatal(err)
			}
			assertBundleError(test, VerifyBundle(contents, payloads, 1<<27), ErrBundleLimit, ErrManifestLimit)
		})
	}
}

func TestVerifyBundleRejectsRehashedInvalidArchive(test *testing.T) {
	for _, scenario := range []string{"invalid gzip", "checksum", "appended data", "invalid tar", "traversal tar"} {
		test.Run(scenario, func(test *testing.T) {
			value, payloads := bundleFixture(test, nil)
			archive := payloads["untracked.tar.gz"]
			var cause error
			switch scenario {
			case "invalid gzip":
				archive, cause = []byte("not a gzip archive"), gzip.ErrHeader
			case "checksum":
				archive[len(archive)-8] ^= 1
				cause = gzip.ErrChecksum
			case "appended data":
				archive = append(archive, 1)
			case "invalid tar":
				archive = untrackedCodecGzip(test, bytes.Repeat([]byte{'x'}, 1024))
				cause = tar.ErrHeader
			case "traversal tar":
				archive = untrackedCodecGzip(test, untrackedCodecTar(test, untrackedCodecRecord{
					header: tar.Header{Name: "../escape", Typeflag: tar.TypeReg, Mode: 0o600, Format: tar.FormatUSTAR},
				}))
			}
			payloads["untracked.tar.gz"] = archive
			value.Files["untracked.tar.gz"] = fmt.Sprintf("sha256:%x", sha256.Sum256(archive))
			if err := VerifyPayloads(value, payloads); err != nil {
				test.Fatalf("hash-only compatibility control rejected matching bytes: %v", err)
			}
			err := VerifyBundle(bundleManifest(test, value), payloads, 1<<20)
			assertBundleError(test, err, ErrUntrackedInvalid)
			if cause != nil && !errors.Is(err, cause) {
				test.Fatalf("archive cause %v lost: %v", cause, err)
			}
		})
	}
}

func TestVerifyBundlePreservesExpandedArchiveLimit(test *testing.T) {
	value, payloads := bundleFixture(test, []UntrackedEntry{{Path: "large", Kind: "file", Mode: 0o600, Data: make([]byte, 64<<10)}})
	contents := bundleManifest(test, value)
	budget := bundleBytes(contents, payloads)
	if budget >= 64<<10 {
		test.Fatal("fixture does not distinguish encoded and expanded budgets")
	}
	assertBundleError(test, VerifyBundle(contents, payloads, budget), ErrBundleLimit, ErrUntrackedLimit)
}

func TestVerifyBundlePreservesUntrackedEntryLimit(test *testing.T) {
	for _, count := range []int{maximumUntrackedEntries, maximumUntrackedEntries + 1} {
		test.Run(fmt.Sprint(count), func(test *testing.T) {
			value, payloads := bundleFixture(test, nil)
			records := make([]untrackedCodecRecord, count)
			for index := range records {
				records[index].header = tar.Header{Name: fmt.Sprintf("file-%04d", index), Typeflag: tar.TypeReg, Mode: 0o600, Format: tar.FormatUSTAR}
			}
			archive := untrackedCodecGzip(test, untrackedCodecTar(test, records...))
			payloads["untracked.tar.gz"] = archive
			value.Files["untracked.tar.gz"] = fmt.Sprintf("sha256:%x", sha256.Sum256(archive))
			value.UntrackedFiles = count
			err := VerifyBundle(bundleManifest(test, value), payloads, 8<<20)
			if count == maximumUntrackedEntries {
				if err != nil {
					test.Fatalf("valid entry-count boundary rejected: %v", err)
				}
			} else {
				assertBundleError(test, err, ErrBundleLimit, ErrUntrackedLimit)
			}
		})
	}
}

func TestVerifyBundleAccountingMismatch(test *testing.T) {
	entries := []UntrackedEntry{
		{Path: "assets", Kind: "directory", Mode: fs.ModeDir | 0o700},
		{Path: "assets/data", Kind: "file", Mode: 0o600, Data: []byte("data")},
		{Path: "link", Kind: "symlink", Mode: fs.ModeSymlink | 0o777, LinkTarget: "assets/data"},
	}
	for _, scenario := range []struct {
		name  string
		files int
		data  int64
	}{
		{"missing symlink", 1, 4}, {"counted directory", 3, 4},
		{"short data", 2, 3}, {"extra data", 2, 5}, {"counted link text", 2, 15},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			value, payloads := bundleFixture(test, entries)
			value.UntrackedFiles, value.UntrackedBytes = scenario.files, scenario.data
			assertBundleError(test, VerifyBundle(bundleManifest(test, value), payloads, 1<<20))
		})
	}
	value, payloads := bundleFixture(test, nil)
	value.UntrackedFiles = 1
	assertBundleError(test, VerifyBundle(bundleManifest(test, value), payloads, 1<<20))
}

func TestVerifyBundleDoesNotMutateInputs(test *testing.T) {
	for _, invalid := range []bool{false, true} {
		test.Run(fmt.Sprint(invalid), func(test *testing.T) {
			value, payloads := bundleFixture(test, nil)
			if invalid {
				value.UntrackedFiles = 1
			}
			contents := bundleManifest(test, value)
			originalContents := bytes.Clone(contents)
			originalPayloads := make(map[string][]byte, len(payloads))
			for name, data := range payloads {
				originalPayloads[name] = bytes.Clone(data)
			}
			if err := VerifyBundle(contents, payloads, 1<<20); (err != nil) != invalid {
				test.Fatalf("error = %v; invalid = %v", err, invalid)
			}
			if !bytes.Equal(contents, originalContents) || !reflect.DeepEqual(payloads, originalPayloads) {
				test.Fatal("verification modified caller-owned bytes or payload map")
			}
		})
	}
}

func bundleFixture(test testing.TB, entries []UntrackedEntry) (Manifest, map[string][]byte) {
	test.Helper()
	value, payloads := manifestFixture(), payloadFixture()
	for _, name := range requiredPayloads {
		payloads[name] = []byte("opaque fixture " + name)
	}
	archive, err := EncodeUntracked(entries, 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	payloads["untracked.tar.gz"] = archive
	for name, data := range payloads {
		value.Files[name] = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	}
	for _, entry := range entries {
		if entry.Kind != "directory" {
			value.UntrackedFiles++
		}
		value.UntrackedBytes += int64(len(entry.Data))
	}
	return value, payloads
}

func bundleManifest(test testing.TB, value Manifest) []byte {
	test.Helper()
	contents, err := EncodeManifest(value)
	if err != nil {
		test.Fatal(err)
	}
	return contents
}

func bundleBytes(contents []byte, payloads map[string][]byte) int64 {
	total := int64(len(contents))
	for _, data := range payloads {
		total += int64(len(data))
	}
	return total
}

func assertBundleError(test testing.TB, err error, causes ...error) {
	test.Helper()
	for _, cause := range append([]error{ErrBundleInvalid}, causes...) {
		if !errors.Is(err, cause) {
			test.Errorf("error = %v; want cause %v", err, cause)
		}
	}
}
