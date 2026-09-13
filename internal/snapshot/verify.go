package snapshot

import (
	"crypto/sha256"
	"errors"
	"fmt"
)

var ErrPayloadIntegrity = errors.New("snapshot payload integrity verification failed")

var requiredPayloads = [...]string{
	"worktree-list.bin",
	"status.bin",
	"staged.patch",
	"unstaged.patch",
	"untracked.tar.gz",
}

func VerifyPayloads(value Manifest, payloads map[string][]byte) error {
	if err := ValidateManifest(value); err != nil {
		return err
	}
	if len(payloads) != len(requiredPayloads) {
		return fmt.Errorf("%w: unexpected payload set", ErrPayloadIntegrity)
	}
	for _, name := range requiredPayloads {
		contents, found := payloads[name]
		if !found || fmt.Sprintf("sha256:%x", sha256.Sum256(contents)) != value.Files[name] {
			return fmt.Errorf("%w: missing or corrupt %s", ErrPayloadIntegrity, name)
		}
	}
	return nil
}
