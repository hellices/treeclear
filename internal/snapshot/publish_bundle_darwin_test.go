package snapshot

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/hellices/treeclear/internal/fssecure"
)

func TestPublishBundleNativePrivateRoundTrip(test *testing.T) {
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
	contents := bundleManifest(test, value)
	maximum := int64(1 << 20)
	receipt, err := PublishBundle(test.Context(), directory, contents, payloads, maximum)
	if err != nil {
		test.Fatal(err)
	}
	canonicalDirectory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		test.Fatal(err)
	}
	if receipt.Path != filepath.Join(canonicalDirectory, value.SnapshotID) || receipt.SnapshotID != value.SnapshotID || receipt.ManifestDigest != fmt.Sprintf("sha256:%x", sha256.Sum256(contents)) {
		test.Fatalf("incorrect receipt: %#v", receipt)
	}
	info, err := os.Stat(receipt.Path)
	if err != nil || info.Mode().Perm() != 0o700 {
		test.Fatalf("snapshot directory is not private: %v / %v", info, err)
	}
	manifest, err := fssecure.ReadPrivateFile(filepath.Join(receipt.Path, "manifest.json"), maximum)
	if err != nil || !bytes.Equal(manifest, contents) {
		test.Fatalf("stored manifest differs: %v", err)
	}
	stored := make(map[string][]byte, len(requiredPayloads))
	for _, name := range requiredPayloads {
		path := filepath.Join(receipt.Path, name)
		stored[name], err = fssecure.ReadPrivateFile(path, maximum)
		if err != nil || !bytes.Equal(stored[name], payloads[name]) {
			test.Fatalf("stored %s differs: %v", name, err)
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			test.Fatalf("stored %s is not a private regular file: %v / %v", name, info, err)
		}
	}
	if err := VerifyBundle(manifest, stored, maximum); err != nil {
		test.Fatalf("on-disk bundle failed verification: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != value.SnapshotID {
		test.Fatalf("publication left unexpected entries: %v / %v", entries, err)
	}
	second, err := PublishBundle(test.Context(), directory, contents, payloads, maximum)
	if !errors.Is(err, fs.ErrExist) || second != (BundleReceipt{}) {
		test.Fatalf("existing snapshot was not protected: %#v / %v", second, err)
	}
	unchanged, err := os.ReadFile(filepath.Join(receipt.Path, "manifest.json"))
	if err != nil || !bytes.Equal(unchanged, contents) {
		test.Fatalf("existing manifest changed: %v", err)
	}
}

func TestPublishBundleInvalidHasNoFilesystemEffects(test *testing.T) {
	for _, scenario := range []string{"corrupt", "over budget", "missing parent"} {
		test.Run(scenario, func(test *testing.T) {
			root := test.TempDir()
			directory := filepath.Join(root, "missing")
			value, payloads := bundleFixture(test, nil)
			contents := bundleManifest(test, value)
			maximum := bundleBytes(contents, payloads)
			switch scenario {
			case "corrupt":
				payloads["status.bin"][0] ^= 1
			case "over budget":
				maximum--
			}
			receipt, err := PublishBundle(test.Context(), directory, contents, payloads, maximum)
			if err == nil || receipt != (BundleReceipt{}) {
				test.Fatalf("invalid publication succeeded: %#v / %v", receipt, err)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				test.Fatalf("rejected publication wrote entries: %v / %v", entries, err)
			}
		})
	}
}

func TestPublishBundleResolvesParentBeforeDotDot(test *testing.T) {
	root := test.TempDir()
	for _, suffix := range []string{"lexical/trash", "physical/child", "physical/trash"} {
		if err := fssecure.EnsurePrivateDirectory(filepath.Join(root, suffix)); err != nil {
			test.Fatal(err)
		}
	}
	alias := filepath.Join(root, "lexical", "alias")
	if err := os.Symlink(filepath.Join(root, "physical", "child"), alias); err != nil {
		test.Fatal(err)
	}
	directory := alias + "/../trash"
	value, payloads := bundleFixture(test, nil)
	contents := bundleManifest(test, value)
	receipt, err := PublishBundle(test.Context(), directory, contents, payloads, 1<<20)
	if err != nil {
		test.Fatal(err)
	}
	physical, err := filepath.EvalSymlinks(filepath.Join(root, "physical", "trash"))
	if err != nil || receipt.Path != filepath.Join(physical, value.SnapshotID) {
		test.Fatalf("publication used a lexically cleaned parent: %#v / %v", receipt, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "lexical", "trash"))
	if err != nil || len(entries) != 0 {
		test.Fatalf("publication wrote the lexical sibling: %v / %v", entries, err)
	}
}
