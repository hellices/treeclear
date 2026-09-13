package snapshot

import (
	"errors"
	"reflect"
	"testing"
)

func TestVerifyPayloadsRequiresExactBytesAndNames(test *testing.T) {
	value := manifestFixture()
	payloads := payloadFixture()
	if err := VerifyPayloads(value, payloads); err != nil {
		test.Fatal(err)
	}
	if !reflect.DeepEqual(payloads, payloadFixture()) || !reflect.DeepEqual(value, manifestFixture()) {
		test.Fatal("verification mutated its inputs")
	}
	for name := range payloadFixture() {
		test.Run("missing "+name, func(test *testing.T) {
			payloads := payloadFixture()
			delete(payloads, name)
			if err := VerifyPayloads(value, payloads); !errors.Is(err, ErrPayloadIntegrity) {
				test.Fatalf("missing payload error = %v", err)
			}
		})
		test.Run("corrupt "+name, func(test *testing.T) {
			payloads := payloadFixture()
			payloads[name] = append(payloads[name], 0xff)
			if err := VerifyPayloads(value, payloads); !errors.Is(err, ErrPayloadIntegrity) {
				test.Fatalf("corrupt payload error = %v", err)
			}
		})
	}
	for _, name := range []string{"extra", "manifest.json", "../status.bin", "STATUS.BIN"} {
		payloads := payloadFixture()
		payloads[name] = nil
		if err := VerifyPayloads(value, payloads); !errors.Is(err, ErrPayloadIntegrity) {
			test.Errorf("extra payload %q error = %v", name, err)
		}
	}
	if err := VerifyPayloads(value, nil); !errors.Is(err, ErrPayloadIntegrity) {
		test.Errorf("nil payloads error = %v", err)
	}
	value.AdapterLockDigest = ""
	if err := VerifyPayloads(value, payloadFixture()); !errors.Is(err, ErrManifestInvalid) {
		test.Errorf("missing adapter provenance accepted: %v", err)
	}
}
