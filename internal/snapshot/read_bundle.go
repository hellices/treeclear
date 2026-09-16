package snapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hellices/treeclear/internal/fssecure"
)

var ErrBundleRead = errors.New("snapshot bundle read failed")

type BundleContents struct {
	ManifestContents []byte
	Payloads         map[string][]byte
}

type bundleReader func(context.Context, string, []fssecure.PrivateFileLimit, int64) ([]fssecure.PrivateFile, error)

func ReadBundle(ctx context.Context, receipt BundleReceipt, maximumBytes int64) (BundleContents, error) {
	return readBundle(ctx, receipt, maximumBytes, fssecure.ReadPrivateDirectory)
}

func readBundle(ctx context.Context, receipt BundleReceipt, maximumBytes int64, read bundleReader) (contents BundleContents, resultErr error) {
	if ctx == nil {
		return BundleContents{}, fmt.Errorf("%w: context is required: %w", ErrBundleRead, fs.ErrInvalid)
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if errors.Is(resultErr, fssecure.ErrPrivateDirectoryLimit) {
			resultErr = fmt.Errorf("%w: %w", ErrBundleLimit, resultErr)
		}
		if resultErr != nil {
			contents = BundleContents{}
			resultErr = fmt.Errorf("%w: %w", ErrBundleRead, resultErr)
		}
	}()
	if err := ctx.Err(); err != nil {
		return BundleContents{}, err
	}
	if read == nil || !validIdentifier(receipt.SnapshotID, "snapshot_") || !validDigest(receipt.ManifestDigest) ||
		!filepath.IsAbs(receipt.Path) || filepath.Clean(receipt.Path) != receipt.Path ||
		filepath.Base(receipt.Path) != receipt.SnapshotID || strings.ContainsRune(receipt.Path, 0) {
		return BundleContents{}, fmt.Errorf("invalid snapshot receipt or reader: %w", fs.ErrInvalid)
	}
	if err := validateUntrackedBudget(maximumBytes); err != nil {
		return BundleContents{}, fmt.Errorf("%w: %w: %w", ErrBundleInvalid, ErrBundleLimit, err)
	}
	limits := make([]fssecure.PrivateFileLimit, 0, len(requiredPayloads)+1)
	limits = append(limits, fssecure.PrivateFileLimit{Name: "manifest.json", MaximumBytes: min(maximumBytes, int64(maximumManifestBytes))})
	for _, name := range requiredPayloads {
		limits = append(limits, fssecure.PrivateFileLimit{Name: name, MaximumBytes: maximumBytes})
	}
	files, err := read(ctx, receipt.Path, slices.Clone(limits), maximumBytes)
	if err != nil {
		return BundleContents{}, err
	}
	if err := ctx.Err(); err != nil {
		return BundleContents{}, err
	}
	if len(files) != len(limits) {
		return BundleContents{}, bundleError(fmt.Errorf("%w: unexpected stored file set", ErrPayloadIntegrity))
	}
	remaining := maximumBytes
	for index, file := range files {
		if file.Name != limits[index].Name {
			return BundleContents{}, bundleError(fmt.Errorf("%w: unexpected stored file ordering", ErrPayloadIntegrity))
		}
		size := int64(len(file.Contents))
		if size > remaining || size > limits[index].MaximumBytes {
			return BundleContents{}, fmt.Errorf("%w: %w: stored bytes at %s", ErrBundleInvalid, ErrBundleLimit, file.Name)
		}
		remaining -= size
	}
	contents = BundleContents{
		ManifestContents: bytes.Clone(files[0].Contents),
		Payloads:         make(map[string][]byte, len(requiredPayloads)),
	}
	for _, file := range files[1:] {
		if err := ctx.Err(); err != nil {
			return BundleContents{}, err
		}
		contents.Payloads[file.Name] = bytes.Clone(file.Contents)
	}
	if fmt.Sprintf("sha256:%x", sha256.Sum256(contents.ManifestContents)) != receipt.ManifestDigest {
		return BundleContents{}, bundleError(fmt.Errorf("%w: receipt manifest digest differs", ErrPayloadIntegrity))
	}
	value, err := DecodeManifest(contents.ManifestContents)
	if err != nil {
		return BundleContents{}, bundleError(err)
	}
	if value.SnapshotID != receipt.SnapshotID {
		return BundleContents{}, bundleError(fmt.Errorf("%w: receipt snapshot ID differs", ErrManifestInvalid))
	}
	if err := ctx.Err(); err != nil {
		return BundleContents{}, err
	}
	if err := VerifyBundle(contents.ManifestContents, contents.Payloads, maximumBytes); err != nil {
		return BundleContents{}, err
	}
	return contents, nil
}
