package snapshot

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"
)

func TestManifestRejectsConflictingAdministrativeTrees(test *testing.T) {
	cases := []struct {
		name   string
		change func(*Manifest)
	}{
		{"missing tree", func(value *Manifest) { value.AdministrativeEntries = nil }},
		{"missing root", func(value *Manifest) { value.AdministrativeEntries = value.AdministrativeEntries[1:] }},
		{"root is file", func(value *Manifest) {
			value.AdministrativeEntries[0].Kind = "file"
			value.AdministrativeEntries[0].Mode = 0o600
		}},
		{"unknown kind", func(value *Manifest) { value.AdministrativeEntries[1].Kind = "symlink" }},
		{"file directory mode", func(value *Manifest) { value.AdministrativeEntries[1].Mode |= fs.ModeDir }},
		{"file symlink mode", func(value *Manifest) { value.AdministrativeEntries[1].Mode |= fs.ModeSymlink }},
		{"file setuid mode", func(value *Manifest) { value.AdministrativeEntries[1].Mode |= fs.ModeSetuid }},
		{"directory file mode", func(value *Manifest) { value.AdministrativeEntries[0].Mode = 0o700 }},
		{"directory special mode", func(value *Manifest) { value.AdministrativeEntries[0].Mode |= fs.ModeSticky }},
		{"directory data", func(value *Manifest) { value.AdministrativeEntries[0].Data = []byte{0} }},
		{"directory nonnil data", func(value *Manifest) { value.AdministrativeEntries[0].Data = []byte{} }},
		{"duplicate path", func(value *Manifest) {
			value.AdministrativeEntries = append(value.AdministrativeEntries, value.AdministrativeEntries[1])
		}},
		{"case alias", func(value *Manifest) {
			value.AdministrativeEntries = append(value.AdministrativeEntries, AdminEntry{Path: "head", Kind: "file", Mode: 0o600})
		}},
		{"Unicode case alias", func(value *Manifest) {
			value.AdministrativeEntries = append(value.AdministrativeEntries, AdminEntry{Path: "Σ", Kind: "file", Mode: 0o600}, AdminEntry{Path: "ς", Kind: "file", Mode: 0o600})
		}},
		{"file as parent", func(value *Manifest) {
			value.AdministrativeEntries = append(value.AdministrativeEntries, AdminEntry{Path: "HEAD/extra", Kind: "file", Mode: 0o600})
		}},
		{"missing parent", func(value *Manifest) { value.AdministrativeEntries[6].Path = "missing/HEAD" }},
		{"parent case mismatch", func(value *Manifest) { value.AdministrativeEntries[5].Path = "Logs" }},
		{"index directory", func(value *Manifest) {
			value.AdministrativeEntries[4] = AdminEntry{Path: "index", Kind: "directory", Mode: fs.ModeDir | 0o700}
		}},
	}
	for _, required := range []string{"HEAD", "commondir", "gitdir"} {
		cases = append(cases, struct {
			name   string
			change func(*Manifest)
		}{"missing " + required, func(value *Manifest) {
			value.AdministrativeEntries = slices.DeleteFunc(value.AdministrativeEntries, func(entry AdminEntry) bool { return entry.Path == required })
		}})
		cases = append(cases, struct {
			name   string
			change func(*Manifest)
		}{"empty " + required, func(value *Manifest) {
			for index := range value.AdministrativeEntries {
				if value.AdministrativeEntries[index].Path == required {
					value.AdministrativeEntries[index].Data = nil
				}
			}
		}})
	}
	for _, invalid := range []string{"", "..", "../outside", "/absolute", "./HEAD", "logs//extra", `logs\extra`, "logs/extra/", "logs/../extra", "C:stream", "NUL", "COM1.txt", "LPT¹", "trailing.", "trailing ", "new\nline", "invalid\xff", "extra?", "extra*", "extra<", "extra|", "extra\x00", strings.Repeat("a", 4097)} {
		cases = append(cases, struct {
			name   string
			change func(*Manifest)
		}{"path " + invalid, func(value *Manifest) { value.AdministrativeEntries[6].Path = invalid }})
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			value := manifestFixture()
			scenario.change(&value)
			if err := ValidateManifest(value); !errors.Is(err, ErrManifestInvalid) {
				test.Fatalf("invalid administrative tree error = %v", err)
			}
			if contents, err := EncodeManifest(value); !errors.Is(err, ErrManifestInvalid) || contents != nil {
				test.Fatalf("invalid tree encoded %d bytes, error %v", len(contents), err)
			}
		})
	}
}

func TestManifestBoundsAdministrativeBytesAndEntries(test *testing.T) {
	value := manifestFixture()
	otherBytes := 0
	for index, entry := range value.AdministrativeEntries {
		if index != 4 {
			otherBytes += len(entry.Data)
		}
	}
	value.AdministrativeEntries[4].Data = make([]byte, maximumAdministrativeBytes-otherBytes)
	if err := ValidateManifest(value); err != nil {
		test.Fatalf("exact administrative byte limit rejected: %v", err)
	}
	contents, err := EncodeManifest(value)
	if err != nil {
		test.Fatal(err)
	}
	loaded, err := DecodeManifest(contents)
	if err != nil || !bytes.Equal(loaded.AdministrativeEntries[4].Data, value.AdministrativeEntries[4].Data) {
		test.Fatalf("bounded diagnostic bytes did not round trip: %v", err)
	}
	value.AdministrativeEntries[4].Data = append(value.AdministrativeEntries[4].Data, 0)
	if err := ValidateManifest(value); !errors.Is(err, ErrManifestLimit) {
		test.Fatalf("aggregate byte limit error = %v", err)
	}
	value = manifestFixture()
	value.AdministrativeEntries = append(value.AdministrativeEntries, make([]AdminEntry, maximumAdministrativeEntries)...)
	if err := ValidateManifest(value); !errors.Is(err, ErrManifestLimit) {
		test.Fatalf("entry limit must precede individual entry validation: %v", err)
	}
}

func TestEncodeManifestBoundsCanonicalDocumentBytes(test *testing.T) {
	value := manifestFixture()
	remaining := maximumAdministrativeBytes
	for index, entry := range value.AdministrativeEntries {
		if index != 4 {
			remaining -= len(entry.Data)
		}
	}
	value.AdministrativeEntries[4].Data = make([]byte, remaining)
	prefix := strings.Repeat("a", 4090)
	for index := len(value.AdministrativeEntries); index < maximumAdministrativeEntries; index++ {
		value.AdministrativeEntries = append(value.AdministrativeEntries, AdminEntry{
			Path: fmt.Sprintf("%s%06d", prefix, index), Kind: "file", Mode: 0o600,
		})
	}
	if err := ValidateManifest(value); err != nil {
		test.Fatalf("fixture exceeds a structural limit rather than its encoded byte limit: %v", err)
	}
	if contents, err := EncodeManifest(value); contents != nil || !errors.Is(err, ErrManifestLimit) {
		test.Fatalf("oversized canonical encoding returned %d bytes, %v", len(contents), err)
	}
}

func TestManifestPreservesPortableNamesAndOriginalModes(test *testing.T) {
	value := manifestFixture()
	value.AdministrativeEntries[0].Mode = fs.ModeDir | 0o755
	for _, name := range []string{".diagnostic", "COMLPT1", "COMLPT9.txt", "한글", "empty-file", "empty-buffer"} {
		value.AdministrativeEntries = append(value.AdministrativeEntries, AdminEntry{Path: name, Kind: "file", Mode: 0o644})
	}
	value.AdministrativeEntries[len(value.AdministrativeEntries)-1].Data = []byte{}
	contents, err := EncodeManifest(value)
	if err != nil {
		test.Fatal(err)
	}
	loaded, err := DecodeManifest(contents)
	if err != nil {
		test.Fatal(err)
	}
	if loaded.AdministrativeEntries[0].Mode != fs.ModeDir|0o755 {
		test.Fatal("recorded directory mode was replaced with storage permissions")
	}
	for _, entry := range loaded.AdministrativeEntries {
		if entry.Path == "empty-file" && entry.Data != nil || entry.Path == "empty-buffer" && entry.Data == nil {
			test.Errorf("diagnostic data representation changed for %q", entry.Path)
		}
	}
}
