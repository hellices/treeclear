package snapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/hellices/treeclear/internal/fssecure"
)

var ErrBundlePublish = errors.New("snapshot bundle publication failed")

type BundleReceipt struct {
	SnapshotID     string `json:"snapshotId"`
	Path           string `json:"path"`
	ManifestDigest string `json:"manifestDigest"`
}

type bundlePublisher func(context.Context, string, []fssecure.PrivateFile) (string, error)

func PublishBundle(ctx context.Context, directory string, manifestContents []byte, payloads map[string][]byte, maximumBytes int64) (BundleReceipt, error) {
	return publishBundle(ctx, directory, manifestContents, payloads, maximumBytes, fssecure.PublishPrivateDirectory)
}

func publishBundle(ctx context.Context, directory string, manifestContents []byte, payloads map[string][]byte, maximumBytes int64, publish bundlePublisher) (receipt BundleReceipt, resultErr error) {
	if ctx == nil {
		return BundleReceipt{}, fmt.Errorf("%w: context is required: %w", ErrBundlePublish, fs.ErrInvalid)
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			receipt = BundleReceipt{}
			resultErr = fmt.Errorf("%w: %w", ErrBundlePublish, resultErr)
		}
	}()
	if err := ctx.Err(); err != nil {
		return BundleReceipt{}, err
	}
	if publish == nil || !filepath.IsAbs(directory) {
		return BundleReceipt{}, fmt.Errorf("absolute private parent and publisher are required: %w", fs.ErrInvalid)
	}
	if err := VerifyBundle(manifestContents, payloads, maximumBytes); err != nil {
		return BundleReceipt{}, err
	}
	if err := ctx.Err(); err != nil {
		return BundleReceipt{}, err
	}
	manifestContents = bytes.Clone(manifestContents)
	value, err := DecodeManifest(manifestContents)
	if err != nil {
		return BundleReceipt{}, err
	}
	files := make([]fssecure.PrivateFile, 0, len(requiredPayloads)+1)
	for _, name := range requiredPayloads {
		if err := ctx.Err(); err != nil {
			return BundleReceipt{}, err
		}
		files = append(files, fssecure.PrivateFile{Name: name, Contents: bytes.Clone(payloads[name])})
	}
	files = append(files, fssecure.PrivateFile{Name: "manifest.json", Contents: manifestContents})
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(manifestContents))
	if err := ctx.Err(); err != nil {
		return BundleReceipt{}, err
	}
	destination, err := publish(ctx, directory+string(os.PathSeparator)+value.SnapshotID, files)
	if err != nil {
		return BundleReceipt{}, err
	}
	if !filepath.IsAbs(destination) || filepath.Base(destination) != value.SnapshotID {
		return BundleReceipt{}, fmt.Errorf("invalid publication destination: %w", fs.ErrInvalid)
	}
	return BundleReceipt{SnapshotID: value.SnapshotID, Path: destination, ManifestDigest: digest}, nil
}
