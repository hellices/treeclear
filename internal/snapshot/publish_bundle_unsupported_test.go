//go:build !darwin

package snapshot

import (
	"errors"
	"os"
	"testing"
)

func TestPublishBundleUnsupportedPlatformDoesNotWrite(test *testing.T) {
	directory := test.TempDir()
	value, payloads := bundleFixture(test, nil)
	contents := bundleManifest(test, value)
	receipt, err := PublishBundle(test.Context(), directory, contents, payloads, 1<<20)
	if !errors.Is(err, errors.ErrUnsupported) || receipt != (BundleReceipt{}) {
		test.Fatalf("unsupported publication = %#v / %v", receipt, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		test.Fatalf("unsupported publication wrote entries: %v / %v", entries, err)
	}
}
