package snapshot

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestManifestCanonicalRoundTripPreservesDiagnosticBytes(test *testing.T) {
	value := manifestFixture()
	value.CreatedAt = value.CreatedAt.In(time.FixedZone("fixture", 9*60*60))
	slices.Reverse(value.AdministrativeEntries)
	before, err := json.Marshal(value)
	if err != nil {
		test.Fatal(err)
	}
	contents, err := EncodeManifest(value)
	if err != nil {
		test.Fatal(err)
	}
	if !bytes.Contains(contents, []byte(`"createdAt":"2026-09-12T12:00:00Z"`)) || !bytes.Contains(contents, []byte(`"data":"AP9JTkRY"`)) {
		test.Fatalf("canonical time or raw index bytes missing: %s", contents)
	}
	loaded, err := DecodeManifest(contents)
	if err != nil {
		test.Fatal(err)
	}
	expected := manifestFixture()
	slices.SortFunc(expected.AdministrativeEntries, func(left, right AdminEntry) int { return strings.Compare(left.Path, right.Path) })
	if !reflect.DeepEqual(loaded, expected) {
		test.Fatalf("decoded manifest = %#v; want %#v", loaded, expected)
	}
	second, err := EncodeManifest(loaded)
	if err != nil || !bytes.Equal(second, contents) {
		test.Fatalf("canonical round trip changed bytes: %v", err)
	}
	after, err := json.Marshal(value)
	if err != nil || !bytes.Equal(before, after) {
		test.Fatalf("encoding mutated its caller: %v", err)
	}
	other := manifestFixture()
	other.Files = make(map[string]string)
	for _, name := range []string{"untracked.tar.gz", "unstaged.patch", "staged.patch", "status.bin", "worktree-list.bin"} {
		other.Files[name] = expected.Files[name]
	}
	encodedOther, err := EncodeManifest(other)
	if err != nil || !bytes.Equal(contents, encodedOther) {
		test.Fatalf("map/entry order or timezone changed canonical bytes: %v", err)
	}
}

func TestManifestAcceptsBranchAndDetachedIdentities(test *testing.T) {
	for _, branch := range []string{"topic", "release/topic", "한글-topic"} {
		value := manifestFixture()
		value.Branch = branch
		if err := ValidateManifest(value); err != nil {
			test.Errorf("branch %q: %v", branch, err)
		}
	}
	value := manifestFixture()
	value.Head = strings.Repeat("b", 64)
	value.Branch = ""
	value.RecoveryRef = "refs/treeclear/recovery/" + value.SnapshotID
	value.UntrackedFiles = 1
	value.UntrackedBytes = 0
	contents, err := EncodeManifest(value)
	if err != nil {
		test.Fatal(err)
	}
	loaded, err := DecodeManifest(contents)
	if err != nil || loaded.Head != value.Head || loaded.RecoveryRef != value.RecoveryRef || loaded.Branch != "" {
		test.Fatalf("detached round trip = %#v, %v", loaded, err)
	}
}

func TestManifestRejectsInvalidIdentities(test *testing.T) {
	cases := []struct {
		name   string
		change func(*Manifest)
	}{
		{"snapshot schema missing", func(value *Manifest) { value.SchemaVersion = 0 }},
		{"snapshot schema future", func(value *Manifest) { value.SchemaVersion++ }},
		{"plan schema missing", func(value *Manifest) { value.PlanSchemaVersion = 0 }},
		{"plan schema future", func(value *Manifest) { value.PlanSchemaVersion++ }},
		{"tool missing", func(value *Manifest) { value.ToolVersion = "" }},
		{"tool invalid UTF8", func(value *Manifest) { value.ToolVersion = "dev\xff" }},
		{"tool control", func(value *Manifest) { value.ToolVersion = "dev\n" }},
		{"tool oversized", func(value *Manifest) { value.ToolVersion = strings.Repeat("v", 129) }},
		{"snapshot ID", func(value *Manifest) { value.SnapshotID = "snapshot_../escape" }},
		{"snapshot ID empty suffix", func(value *Manifest) { value.SnapshotID = "snapshot_" }},
		{"snapshot ID oversized", func(value *Manifest) { value.SnapshotID = "snapshot_" + strings.Repeat("a", 128) }},
		{"plan ID", func(value *Manifest) { value.PlanID = "../plan_fixture" }},
		{"candidate ID", func(value *Manifest) { value.CandidateID = "other_fixture" }},
		{"creation missing", func(value *Manifest) { value.CreatedAt = time.Time{} }},
		{"creation unencodable", func(value *Manifest) { value.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }},
		{"repository relative", func(value *Manifest) { value.RepositoryRoot = "repo" }},
		{"common Git directory missing", func(value *Manifest) { value.CommonGitDir = "" }},
		{"worktree traversal", func(value *Manifest) { value.WorktreePath += string(filepath.Separator) + ".." }},
		{"admin invalid UTF8", func(value *Manifest) { value.AdminDir += "\xff" }},
		{"admin NUL", func(value *Manifest) { value.AdminDir += "\x00" }},
		{"HEAD short", func(value *Manifest) { value.Head = "abc123" }},
		{"HEAD missing", func(value *Manifest) { value.Head = "" }},
		{"HEAD zero SHA1", func(value *Manifest) { value.Head = strings.Repeat("0", 40) }},
		{"HEAD zero SHA256", func(value *Manifest) { value.Head = strings.Repeat("0", 64) }},
		{"HEAD uppercase", func(value *Manifest) { value.Head = strings.Repeat("A", 40) }},
		{"HEAD invalid hex", func(value *Manifest) { value.Head = strings.Repeat("x", 40) }},
		{"detached without anchor", func(value *Manifest) { value.Branch = "" }},
		{"branch with recovery ref", func(value *Manifest) { value.RecoveryRef = "refs/treeclear/recovery/" + value.SnapshotID }},
		{"wrong detached anchor", func(value *Manifest) { value.Branch = ""; value.RecoveryRef = "refs/treeclear/recovery/snapshot_other" }},
		{"negative files", func(value *Manifest) { value.UntrackedFiles = -1 }},
		{"negative bytes", func(value *Manifest) { value.UntrackedBytes = -1 }},
		{"bytes without files", func(value *Manifest) { value.UntrackedFiles = 0; value.UntrackedBytes = 1 }},
		{"missing payload set", func(value *Manifest) { value.Files = nil }},
		{"missing payload", func(value *Manifest) { delete(value.Files, "status.bin") }},
		{"extra payload", func(value *Manifest) { value.Files["manifest.json"] = value.PolicyDigest }},
		{"replaced payload", func(value *Manifest) {
			delete(value.Files, "status.bin")
			value.Files["../status.bin"] = value.PolicyDigest
		}},
	}
	for _, field := range []string{"candidate", "policy", "adapter lock", "evidence", "payload"} {
		for _, invalid := range []string{"", "sha256:abc", "SHA256:" + strings.Repeat("a", 64), "sha256:" + strings.Repeat("A", 64), "sha256:" + strings.Repeat("z", 64)} {
			cases = append(cases, struct {
				name   string
				change func(*Manifest)
			}{field + " digest " + invalid, func(value *Manifest) {
				switch field {
				case "candidate":
					value.CandidateFingerprint = invalid
				case "policy":
					value.PolicyDigest = invalid
				case "adapter lock":
					value.AdapterLockDigest = invalid
				case "evidence":
					value.EvidenceDigest = invalid
				case "payload":
					value.Files["status.bin"] = invalid
				}
			}})
		}
	}
	for _, branch := range []string{"HEAD", "-topic", "@", ".topic", "topic..other", "topic.lock", "topic//other", "topic/", "topic.", "topic@{1}", "topic~1", "topic^", "topic:other", "topic?", "topic*", "topic[", `topic\other`, "topic name", "topic\x7f", "topic/.hidden", "topic/other.lock", "topic\xff"} {
		cases = append(cases, struct {
			name   string
			change func(*Manifest)
		}{"branch " + branch, func(value *Manifest) { value.Branch = branch }})
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			value := manifestFixture()
			scenario.change(&value)
			if err := ValidateManifest(value); !errors.Is(err, ErrManifestInvalid) {
				test.Errorf("ValidateManifest error = %v", err)
			}
			if contents, err := EncodeManifest(value); !errors.Is(err, ErrManifestInvalid) || contents != nil {
				test.Errorf("EncodeManifest returned %d bytes, %v", len(contents), err)
			}
		})
	}
}

func TestDecodeManifestRejectsAmbiguousAndNoncanonicalJSON(test *testing.T) {
	contents, err := EncodeManifest(manifestFixture())
	if err != nil {
		test.Fatal(err)
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, contents, "", "  "); err != nil {
		test.Fatal(err)
	}
	cases := []struct {
		name     string
		contents []byte
	}{
		{"empty", nil},
		{"null", []byte("null")},
		{"array", []byte("[]")},
		{"truncated", contents[:len(contents)/2]},
		{"whitespace", append([]byte(" "), contents...)},
		{"newline", append(slices.Clone(contents), '\n')},
		{"formatted", formatted.Bytes()},
		{"duplicate field", append([]byte(`{"schemaVersion":1,`), contents[1:]...)},
		{"unknown field", append([]byte(`{"unknown":true,`), contents[1:]...)},
		{"case alias", bytes.Replace(contents, []byte(`"snapshotId"`), []byte(`"SnapshotId"`), 1)},
		{"escaped field", bytes.Replace(contents, []byte(`"snapshotId"`), []byte(`"snapshot\u0049d"`), 1)},
		{"nested duplicate field", bytes.Replace(contents, []byte(`"kind":"file"`), []byte(`"kind":"file","kind":"file"`), 1)},
		{"nested unknown field", bytes.Replace(contents, []byte(`"kind":"file"`), []byte(`"kind":"file","extra":{}`), 1)},
		{"nested case alias", bytes.Replace(contents, []byte(`"kind":"file"`), []byte(`"Kind":"file"`), 1)},
		{"duplicate payload key", bytes.Replace(contents, []byte(`"files":{`), []byte(`"files":{"status.bin":"`+manifestFixture().Files["status.bin"]+`",`), 1)},
		{"trailing document", append(slices.Clone(contents), []byte(`{}`)...)},
		{"alternate UTC spelling", bytes.Replace(contents, []byte(`12:00:00Z`), []byte(`12:00:00+00:00`), 1)},
		{"unpaired surrogate", bytes.Replace(contents, []byte(`"dev"`), []byte(`"\ud800"`), 1)},
		{"invalid UTF8", bytes.Replace(contents, []byte(`"dev"`), []byte{'"', 0xff, '"'}, 1)},
		{"alternate base64 spelling", bytes.Replace(contents, []byte(`AP9JTkRY`), []byte(`AP9J\nTkRY`), 1)},
		{"numeric byte array", bytes.Replace(contents, []byte(`"data":"AP9JTkRY"`), []byte(`"data":[0,255,73,78,68,88]`), 1)},
		{"noncanonical entries", func() []byte {
			value := manifestFixture()
			slices.Reverse(value.AdministrativeEntries)
			encoded, encodeErr := json.Marshal(value)
			if encodeErr != nil {
				test.Fatal(encodeErr)
			}
			return encoded
		}()},
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			value, err := DecodeManifest(scenario.contents)
			if !errors.Is(err, ErrManifestInvalid) || !reflect.DeepEqual(value, Manifest{}) {
				test.Fatalf("malformed manifest returned %#v, %v", value, err)
			}
		})
	}
}

func TestDecodeManifestBoundsCollectionsBeforeTypedExpansion(test *testing.T) {
	for _, contents := range [][]byte{
		[]byte(`{"administrativeEntries":[` + strings.Repeat(`{},`, maximumAdministrativeEntries) + `{}]}`),
		[]byte(`{` + strings.Repeat(`"unknown":{},`, 32) + `"schemaVersion":1}`),
		[]byte(`{"unknown":` + strings.Repeat(`[`, 9) + `0` + strings.Repeat(`]`, 9) + `}`),
		bytes.Repeat([]byte(" "), maximumManifestBytes+1),
	} {
		value, err := DecodeManifest(contents)
		if !errors.Is(err, ErrManifestLimit) || !errors.Is(err, ErrManifestInvalid) || !reflect.DeepEqual(value, Manifest{}) {
			test.Fatalf("over-limit document returned usable value or wrong error: %v", err)
		}
	}
}

func FuzzDecodeManifest(fuzz *testing.F) {
	contents, err := EncodeManifest(manifestFixture())
	if err != nil {
		fuzz.Fatal(err)
	}
	fuzz.Add(contents)
	fuzz.Add([]byte(`{"schemaVersion":1}`))
	fuzz.Fuzz(func(test *testing.T, input []byte) {
		value, err := DecodeManifest(input)
		if err != nil {
			if !reflect.DeepEqual(value, Manifest{}) {
				test.Fatal("invalid input returned a usable manifest")
			}
			return
		}
		canonical, err := EncodeManifest(value)
		if err != nil || !bytes.Equal(canonical, input) {
			test.Fatalf("accepted input is not canonical: %v", err)
		}
	})
}

func manifestFixture() Manifest {
	files := make(map[string]string)
	for name, contents := range payloadFixture() {
		files[name] = fmt.Sprintf("sha256:%x", sha256.Sum256(contents))
	}
	return Manifest{
		SchemaVersion: 1, PlanSchemaVersion: 1, ToolVersion: "dev",
		SnapshotID: "snapshot_fixture", PlanID: "plan_fixture", CandidateID: "candidate_fixture",
		CandidateFingerprint: "sha256:" + strings.Repeat("a", 64), PolicyDigest: "sha256:" + strings.Repeat("b", 64),
		AdapterLockDigest: "sha256:" + strings.Repeat("c", 64), EvidenceDigest: "sha256:" + strings.Repeat("d", 64),
		CreatedAt:      time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC),
		RepositoryRoot: manifestPath("repo"), CommonGitDir: manifestPath("repo/.git"),
		WorktreePath: manifestPath("topic"), AdminDir: manifestPath("repo/.git/worktrees/topic"),
		Head: strings.Repeat("a", 40), Branch: "topic", Files: files,
		AdministrativeEntries: []AdminEntry{
			{Path: ".", Kind: "directory", Mode: fs.ModeDir | 0o700},
			{Path: "HEAD", Kind: "file", Mode: 0o600, Data: []byte("ref: refs/heads/topic\n")},
			{Path: "commondir", Kind: "file", Mode: 0o644, Data: []byte("../..\n")},
			{Path: "gitdir", Kind: "file", Mode: 0o644, Data: []byte(manifestPath("topic/.git") + "\n")},
			{Path: "index", Kind: "file", Mode: 0o644, Data: []byte{0, 0xff, 'I', 'N', 'D', 'X'}},
			{Path: "logs", Kind: "directory", Mode: fs.ModeDir | 0o700},
			{Path: "logs/HEAD", Kind: "file", Mode: 0o600, Data: []byte("synthetic reflog\n")},
		},
	}
}

func manifestPath(suffix string) string {
	root := "/treeclear-synthetic"
	if runtime.GOOS == "windows" {
		root = `C:\treeclear-synthetic`
	}
	return filepath.Join(root, filepath.FromSlash(suffix))
}

func payloadFixture() map[string][]byte {
	return map[string][]byte{
		"worktree-list.bin": []byte("synthetic worktree\x00\x00"),
		"status.bin":        {},
		"staged.patch":      []byte("staged\x00\xff"),
		"unstaged.patch":    {},
		"untracked.tar.gz":  {0x1f, 0x8b, 0x08, 0},
	}
}
