//go:build !darwin && !linux && !windows

package snapshot

import (
	"errors"
	"testing"
)

func TestReadAdministrativeFocusedUnsupportedPlatform(test *testing.T) {
	entries, err := ReadAdministrative(test.Context(), test.TempDir())
	if entries != nil || !errors.Is(err, errors.ErrUnsupported) {
		test.Fatalf("unsupported platform did not fail closed: %#v, %v", entries, err)
	}
}
