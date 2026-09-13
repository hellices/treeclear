package snapshot

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeManifestBoundsAggregateJSONWork(test *testing.T) {
	row := "[" + strings.Repeat("0,", maximumAdministrativeEntries-1) + "0]"
	contents := []byte(`{"unknown":[` + strings.Repeat(row+",", 15) + row + `]}`)
	if len(contents) > maximumManifestBytes {
		test.Fatal("fixture exceeds the document limit rather than aggregate JSON work")
	}
	value, err := DecodeManifest(contents)
	if !errors.Is(err, ErrManifestLimit) || !reflect.DeepEqual(value, Manifest{}) {
		test.Fatalf("aggregate JSON work was not bounded before typed decoding: %v", err)
	}
}

func TestDecodeManifestBoundsAdministrativeBytesBeforeTypedExpansion(test *testing.T) {
	encoded := strings.Repeat("A", 8<<20)
	for _, field := range []string{"data", "Data", "dAtA"} {
		test.Run(field, func(test *testing.T) {
			entry := `{"` + field + `":"` + encoded + `"}`
			contents := []byte(`{"toolVersion":0,"administrativeEntries":[` + entry + "," + entry + "," + entry + `]}`)
			if len(contents) > maximumManifestBytes {
				test.Fatal("fixture exceeds the encoded document limit")
			}
			value, err := DecodeManifest(contents)
			if !errors.Is(err, ErrManifestLimit) || !reflect.DeepEqual(value, Manifest{}) {
				test.Fatalf("typed field error preceded the administrative-byte limit: %v", err)
			}
		})
	}
}

func TestManifestMaximumEntryCountFitsJSONWorkBudget(test *testing.T) {
	value := manifestFixture()
	for index := len(value.AdministrativeEntries); index < maximumAdministrativeEntries; index++ {
		value.AdministrativeEntries = append(value.AdministrativeEntries, AdminEntry{
			Path: fmt.Sprintf("extra-%04d", index), Kind: "file", Mode: 0o600,
		})
	}
	contents, err := EncodeManifest(value)
	if err != nil {
		test.Fatal(err)
	}
	loaded, err := DecodeManifest(contents)
	if err != nil || len(loaded.AdministrativeEntries) != maximumAdministrativeEntries {
		test.Fatalf("valid maximum-entry manifest was rejected: %v", err)
	}
}
