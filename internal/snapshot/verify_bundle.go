package snapshot

import (
	"errors"
	"fmt"
)

var (
	ErrBundleInvalid = errors.New("invalid snapshot bundle")
	ErrBundleLimit   = errors.New("snapshot bundle limit exceeded")
)

func VerifyBundle(manifestContents []byte, payloads map[string][]byte, maximumBytes int64) error {
	if err := validateUntrackedBudget(maximumBytes); err != nil {
		return fmt.Errorf("%w: %w: %w", ErrBundleInvalid, ErrBundleLimit, err)
	}
	remaining := maximumBytes
	if int64(len(manifestContents)) > remaining {
		return fmt.Errorf("%w: %w: encoded manifest bytes", ErrBundleInvalid, ErrBundleLimit)
	}
	remaining -= int64(len(manifestContents))
	if len(payloads) != len(requiredPayloads) {
		return bundleError(fmt.Errorf("%w: unexpected payload set", ErrPayloadIntegrity))
	}
	for _, name := range requiredPayloads {
		contents, found := payloads[name]
		if !found {
			return bundleError(fmt.Errorf("%w: missing %s", ErrPayloadIntegrity, name))
		}
		if int64(len(contents)) > remaining {
			return fmt.Errorf("%w: %w: aggregate bytes at %s", ErrBundleInvalid, ErrBundleLimit, name)
		}
		remaining -= int64(len(contents))
	}
	value, err := DecodeManifest(manifestContents)
	if err != nil {
		return bundleError(err)
	}
	if err := VerifyPayloads(value, payloads); err != nil {
		return bundleError(err)
	}
	entries, err := DecodeUntracked(payloads["untracked.tar.gz"], maximumBytes)
	if err != nil {
		return bundleError(err)
	}
	var files int
	var dataBytes int64
	for _, entry := range entries {
		switch entry.Kind {
		case "file":
			files++
			dataBytes += int64(len(entry.Data))
		case "symlink":
			files++
		}
	}
	if files != value.UntrackedFiles || dataBytes != value.UntrackedBytes {
		return fmt.Errorf("%w: untracked accounting does not match archive", ErrBundleInvalid)
	}
	return nil
}

func bundleError(err error) error {
	if errors.Is(err, ErrManifestLimit) || errors.Is(err, ErrUntrackedLimit) {
		return fmt.Errorf("%w: %w: %w", ErrBundleInvalid, ErrBundleLimit, err)
	}
	return fmt.Errorf("%w: %w", ErrBundleInvalid, err)
}
