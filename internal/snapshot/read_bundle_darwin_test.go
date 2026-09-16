package snapshot

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/fssecure"
)

func TestReadBundleNativePublishedRoundTrip(test *testing.T) {
	receipt, manifest, payloads := publishedReadBundleFixture(test)
	before := bundleDiskState(test, receipt.Path)
	contents, err := ReadBundle(test.Context(), receipt, 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	if !bytes.Equal(contents.ManifestContents, manifest) || len(contents.Payloads) != len(payloads) {
		test.Fatal("stored round trip changed the manifest or payload set")
	}
	for name, want := range payloads {
		if !bytes.Equal(contents.Payloads[name], want) {
			test.Fatalf("stored round trip changed %q", name)
		}
	}
	if err := VerifyBundle(contents.ManifestContents, contents.Payloads, 1<<20); err != nil {
		test.Fatalf("stored bundle verification failed: %v", err)
	}
	if !reflect.DeepEqual(before, bundleDiskState(test, receipt.Path)) {
		test.Fatal("bundle reader changed stored recovery data")
	}
	contents.ManifestContents[0] ^= 1
	contents.Payloads["status.bin"][0] ^= 1
	again, err := ReadBundle(test.Context(), receipt, 1<<20)
	if err != nil || !bytes.Equal(again.ManifestContents, manifest) || !bytes.Equal(again.Payloads["status.bin"], payloads["status.bin"]) {
		test.Fatalf("caller mutation affected stored recovery data: %v", err)
	}
}

func TestReadBundleNativeRejectsChangedStorageWithoutRepair(test *testing.T) {
	for _, scenario := range []string{"manifest", "payload", "missing", "extra", "public file", "public directory", "wrong receipt", "budget"} {
		test.Run(scenario, func(test *testing.T) {
			receipt, manifest, payloads := publishedReadBundleFixture(test)
			maximum := int64(1 << 20)
			var setupErr error
			switch scenario {
			case "manifest":
				manifest[0] ^= 1
				setupErr = os.WriteFile(filepath.Join(receipt.Path, "manifest.json"), manifest, 0o600)
			case "payload":
				payloads["status.bin"][0] ^= 1
				setupErr = os.WriteFile(filepath.Join(receipt.Path, "status.bin"), payloads["status.bin"], 0o600)
			case "missing":
				setupErr = os.Remove(filepath.Join(receipt.Path, "manifest.json"))
			case "extra":
				setupErr = os.WriteFile(filepath.Join(receipt.Path, "extra"), []byte("preserve"), 0o600)
			case "public file":
				setupErr = os.Chmod(filepath.Join(receipt.Path, "status.bin"), 0o644)
			case "public directory":
				setupErr = os.Chmod(receipt.Path, 0o755)
			case "wrong receipt":
				receipt.ManifestDigest = "sha256:" + strings.Repeat("0", 64)
			case "budget":
				maximum = bundleBytes(manifest, payloads) - 1
			}
			if setupErr != nil {
				test.Fatal(setupErr)
			}
			before := bundleDiskState(test, receipt.Path)
			contents, err := ReadBundle(test.Context(), receipt, maximum)
			assertBundleReadFailure(test, contents, err)
			if !reflect.DeepEqual(before, bundleDiskState(test, receipt.Path)) {
				test.Fatal("failed read repaired, removed or changed stored bytes")
			}
			if scenario == "budget" {
				assertBundleReadFailure(test, contents, err, ErrBundleLimit)
			}
		})
	}
}

func publishedReadBundleFixture(test testing.TB) (BundleReceipt, []byte, map[string][]byte) {
	test.Helper()
	directory := filepath.Join(test.TempDir(), "trash")
	if err := fssecure.EnsurePrivateDirectory(directory); err != nil {
		test.Fatal(err)
	}
	value, payloads := bundleFixture(test, []UntrackedEntry{
		{Path: "data", Kind: "directory", Mode: fs.ModeDir | 0o700},
		{Path: "data/binary", Kind: "file", Mode: 0o640, Data: []byte{0, 0xff, 1}},
		{Path: "data/link", Kind: "symlink", Mode: fs.ModeSymlink | 0o777, LinkTarget: "binary"},
	})
	payloads["staged.patch"] = nil
	value.Files["staged.patch"] = fmt.Sprintf("sha256:%x", sha256.Sum256(nil))
	manifest := bundleManifest(test, value)
	receipt, err := PublishBundle(test.Context(), directory, manifest, payloads, 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	return receipt, manifest, payloads
}

func bundleDiskState(test testing.TB, path string) map[string]string {
	test.Helper()
	metadata, err := os.Stat(path)
	if err != nil {
		test.Fatal(err)
	}
	state := map[string]string{".": fmt.Sprintf("%v/%d", metadata.Mode(), metadata.ModTime().UnixNano())}
	entries, err := os.ReadDir(path)
	if err != nil {
		test.Fatal(err)
	}
	for _, entry := range entries {
		file := filepath.Join(path, entry.Name())
		metadata, err := os.Lstat(file)
		if err != nil {
			test.Fatal(err)
		}
		contents, err := os.ReadFile(file)
		if err != nil {
			test.Fatal(err)
		}
		state[entry.Name()] = fmt.Sprintf("%v/%d/%x", metadata.Mode(), metadata.ModTime().UnixNano(), sha256.Sum256(contents))
	}
	return state
}
