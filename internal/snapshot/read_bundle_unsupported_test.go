//go:build !darwin

package snapshot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadBundleUnsupportedDoesNotOpenStorage(test *testing.T) {
	receipt, _ := readBundleFixture(test, nil)
	contents, err := ReadBundle(test.Context(), receipt, 1<<20)
	assertBundleReadFailure(test, contents, err, errors.ErrUnsupported)
	entries, err := os.ReadDir(filepath.Dir(receipt.Path))
	if err != nil || len(entries) != 0 {
		test.Fatalf("unsupported reader changed its nonexistent destination: %v / %v", entries, err)
	}
}
