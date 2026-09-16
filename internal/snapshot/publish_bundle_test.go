package snapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/hellices/treeclear/internal/fssecure"
)

func TestPublishBundleOrdersOwnedBytesAndBindsReceipt(test *testing.T) {
	value, payloads := bundleFixture(test, nil)
	contents := bundleManifest(test, value)
	originalManifest := bytes.Clone(contents)
	originalPayloads := maps.Clone(payloads)
	for name, data := range originalPayloads {
		originalPayloads[name] = bytes.Clone(data)
	}
	directory := test.TempDir()
	destination := filepath.Join(directory, value.SnapshotID)
	called := 0
	publish := func(ctx context.Context, path string, files []fssecure.PrivateFile) (string, error) {
		called++
		if path != destination || ctx != test.Context() {
			test.Fatalf("publication destination/context = %q / %v", path, ctx)
		}
		contents[0] ^= 1
		for _, data := range payloads {
			if len(data) > 0 {
				data[0] ^= 1
			}
		}
		var names []string
		for _, file := range files {
			names = append(names, file.Name)
			want := originalPayloads[file.Name]
			if file.Name == "manifest.json" {
				want = originalManifest
			}
			if !bytes.Equal(file.Contents, want) {
				test.Errorf("publication retained caller-owned bytes for %s", file.Name)
			}
		}
		wantNames := append(slices.Clone(requiredPayloads[:]), "manifest.json")
		if !slices.Equal(names, wantNames) {
			test.Errorf("publication order = %v; want %v", names, wantNames)
		}
		return destination, nil
	}
	receipt, err := publishBundle(test.Context(), directory, contents, payloads, 1<<20, publish)
	if err != nil {
		test.Fatal(err)
	}
	want := BundleReceipt{
		SnapshotID: value.SnapshotID, Path: destination,
		ManifestDigest: fmt.Sprintf("sha256:%x", sha256.Sum256(originalManifest)),
	}
	if receipt != want || called != 1 {
		test.Fatalf("receipt = %#v, calls = %d; want %#v and one call", receipt, called, want)
	}
}

func TestPublishBundleRejectsInvalidBeforePublication(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*Manifest, map[string][]byte, *[]byte, *int64)
		cause  error
	}{
		{"manifest", func(_ *Manifest, _ map[string][]byte, contents *[]byte, _ *int64) {
			*contents = []byte(`{}`)
		}, ErrManifestInvalid},
		{"payload hash", func(_ *Manifest, payloads map[string][]byte, _ *[]byte, _ *int64) {
			payloads["status.bin"] = []byte("corruption")
		}, ErrPayloadIntegrity},
		{"missing payload", func(_ *Manifest, payloads map[string][]byte, _ *[]byte, _ *int64) {
			delete(payloads, "status.bin")
		}, ErrPayloadIntegrity},
		{"extra payload", func(_ *Manifest, payloads map[string][]byte, _ *[]byte, _ *int64) {
			payloads["extra"] = nil
		}, ErrPayloadIntegrity},
		{"invalid archive", func(value *Manifest, payloads map[string][]byte, contents *[]byte, _ *int64) {
			payloads["untracked.tar.gz"] = []byte("not an archive")
			value.Files["untracked.tar.gz"] = fmt.Sprintf("sha256:%x", sha256.Sum256(payloads["untracked.tar.gz"]))
			*contents = bundleManifest(test, *value)
		}, ErrUntrackedInvalid},
		{"accounting", func(value *Manifest, _ map[string][]byte, contents *[]byte, _ *int64) {
			value.UntrackedFiles = 1
			*contents = bundleManifest(test, *value)
		}, ErrBundleInvalid},
		{"budget", func(_ *Manifest, _ map[string][]byte, _ *[]byte, maximum *int64) {
			*maximum = 1
		}, ErrBundleLimit},
		{"invalid budget", func(_ *Manifest, _ map[string][]byte, _ *[]byte, maximum *int64) {
			*maximum = 0
		}, ErrBundleLimit},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			value, payloads := bundleFixture(test, nil)
			contents := bundleManifest(test, value)
			maximum := int64(1 << 20)
			scenario.change(&value, payloads, &contents, &maximum)
			called := false
			publish := func(context.Context, string, []fssecure.PrivateFile) (string, error) {
				called = true
				return "", errors.New("unexpected publication")
			}
			receipt, err := publishBundle(test.Context(), test.TempDir(), contents, payloads, maximum, publish)
			if called || receipt != (BundleReceipt{}) || !errors.Is(err, ErrBundlePublish) || !errors.Is(err, scenario.cause) {
				test.Fatalf("receipt = %#v, publication = %v, error = %v; want no writes and %v", receipt, called, err, scenario.cause)
			}
		})
	}
}

func TestPublishBundleWholeByteBoundary(test *testing.T) {
	value, payloads := bundleFixture(test, nil)
	contents := bundleManifest(test, value)
	maximum := bundleBytes(contents, payloads)
	for _, extra := range []int64{-1, 0} {
		test.Run(fmt.Sprint(extra), func(test *testing.T) {
			calls := 0
			publish := func(_ context.Context, path string, _ []fssecure.PrivateFile) (string, error) {
				calls++
				return path, nil
			}
			receipt, err := publishBundle(test.Context(), test.TempDir(), contents, payloads, maximum+extra, publish)
			if extra < 0 {
				if !errors.Is(err, ErrBundleLimit) || calls != 0 || receipt != (BundleReceipt{}) {
					test.Fatalf("over-budget publication = %#v / %v / calls %d", receipt, err, calls)
				}
			} else if err != nil || calls != 1 || receipt.SnapshotID != value.SnapshotID {
				test.Fatalf("exact-fit publication = %#v / %v / calls %d", receipt, err, calls)
			}
		})
	}
}

func TestPublishBundleFailureNeverReturnsReceipt(test *testing.T) {
	value, payloads := bundleFixture(test, nil)
	contents := bundleManifest(test, value)
	for _, scenario := range []string{"nil context", "canceled", "publication error", "canceled after publication", "empty directory", "relative directory", "empty result", "nil publisher"} {
		test.Run(scenario, func(test *testing.T) {
			ctx, cancel := context.WithCancel(test.Context())
			defer cancel()
			directory := test.TempDir()
			calls := 0
			wantCause := error(fs.ErrInvalid)
			publish := func(_ context.Context, path string, _ []fssecure.PrivateFile) (string, error) {
				calls++
				switch scenario {
				case "publication error":
					return path, fs.ErrPermission
				case "canceled after publication":
					cancel()
					return path, nil
				}
				return "", nil
			}
			wantCalls := 0
			switch scenario {
			case "nil context":
				ctx = nil
			case "canceled":
				cancel()
				wantCause = context.Canceled
			case "publication error":
				wantCalls, wantCause = 1, fs.ErrPermission
			case "canceled after publication":
				wantCalls, wantCause = 1, context.Canceled
			case "empty directory":
				directory = ""
			case "relative directory":
				directory = "trash"
			case "empty result":
				wantCalls = 1
			case "nil publisher":
				publish = nil
			}
			receipt, err := publishBundle(ctx, directory, contents, payloads, 1<<20, publish)
			if receipt != (BundleReceipt{}) || !errors.Is(err, ErrBundlePublish) || !errors.Is(err, wantCause) || calls != wantCalls {
				test.Fatalf("receipt = %#v, calls = %d, error = %v; want zero receipt, %d calls and %v", receipt, calls, err, wantCalls, wantCause)
			}
		})
	}
}

func TestPublishBundlePreservesInputs(test *testing.T) {
	value, payloads := bundleFixture(test, nil)
	contents := bundleManifest(test, value)
	beforeContents := bytes.Clone(contents)
	beforePayloads := maps.Clone(payloads)
	for name, data := range beforePayloads {
		beforePayloads[name] = bytes.Clone(data)
	}
	publish := func(_ context.Context, path string, files []fssecure.PrivateFile) (string, error) {
		for _, file := range files {
			if len(file.Contents) > 0 {
				file.Contents[0] ^= 1
			}
		}
		return path, nil
	}
	if _, err := publishBundle(test.Context(), test.TempDir(), contents, payloads, 1<<20, publish); err != nil {
		test.Fatal(err)
	}
	if !bytes.Equal(contents, beforeContents) || !reflect.DeepEqual(payloads, beforePayloads) {
		test.Fatal("publication mutated caller-owned bytes")
	}
}
