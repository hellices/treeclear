package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func manifestFixture() string {
	return `{"schemaVersion":1,"activeStage":"000","stages":[
		{"id":"000","name":"Harness","requirements":[{"id":"H001","description":"Harness evidence","package":"./internal/harness","test":"TestEvidence"}]},
		{"id":"001","name":"Core","requirements":[{"id":"C001","description":"Core safety","package":"./internal/policy","test":"TestUnknownProtected"}]},
		{"id":"002","name":"Adapters","requirements":[{"id":"A001","description":"Adapter safety","package":"./internal/adapter","test":"TestUnknownAdapter"}]},
		{"id":"003","name":"Lifecycle","requirements":[{"id":"L001","description":"Signature safety","package":"./internal/signing","test":"TestRejectTamper"}]},
		{"id":"004","name":"Operations","requirements":[{"id":"O001","description":"Scheduler safety","package":"./internal/schedule","test":"TestPlanOnly"}]}
	]}`
}

func TestManifestRejectsInvalidContracts(test *testing.T) {
	cases := map[string]string{
		"duplicate JSON key":    strings.Replace(manifestFixture(), `"activeStage":"000"`, `"activeStage":"004","activeStage":"000"`, 1),
		"duplicate nested key":  strings.Replace(manifestFixture(), `"test":"TestEvidence"`, `"test":"TestSkipped","test":"TestEvidence"`, 1),
		"unknown field":         strings.Replace(manifestFixture(), `"schemaVersion":1`, `"unexpected":true,"schemaVersion":1`, 1),
		"future schema":         strings.Replace(manifestFixture(), `"schemaVersion":1`, `"schemaVersion":2`, 1),
		"unknown active stage":  strings.Replace(manifestFixture(), `"activeStage":"000"`, `"activeStage":"999"`, 1),
		"duplicate requirement": strings.Replace(manifestFixture(), `"C001"`, `"H001"`, 1),
		"duplicate stage":       strings.Replace(manifestFixture(), `"id":"001"`, `"id":"000"`, 1),
		"escaping package":      strings.Replace(manifestFixture(), `./internal/harness`, `./../outside`, 1),
		"package pattern":       strings.Replace(manifestFixture(), `./internal/harness`, `./internal/...`, 1),
		"test pattern":          strings.Replace(manifestFixture(), `TestEvidence`, `Test.*`, 1),
		"empty description":     strings.Replace(manifestFixture(), `Harness evidence`, ` `, 1),
		"empty requirements":    strings.Replace(manifestFixture(), `[{"id":"H001","description":"Harness evidence","package":"./internal/harness","test":"TestEvidence"}]`, `[]`, 1),
		"trailing value":        manifestFixture() + `{}`,
		"truncated":             manifestFixture()[:30],
	}
	for name, input := range cases {
		test.Run(name, func(test *testing.T) {
			if _, err := DecodeManifest(strings.NewReader(input)); err == nil {
				test.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestManifestSelectsCumulativeEvidence(test *testing.T) {
	manifest, err := DecodeManifest(strings.NewReader(manifestFixture()))
	if err != nil {
		test.Fatal(err)
	}
	requirements, err := manifest.Select("001")
	if err != nil || len(requirements) != 2 || requirements[0].ID != "H001" || requirements[1].ID != "C001" {
		test.Fatalf("requirements = %+v, error = %v", requirements, err)
	}
	if _, err := manifest.Select("999"); err == nil {
		test.Fatal("unknown stage accepted")
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		test.Fatal(err)
	}
	if _, err := DecodeManifest(strings.NewReader(string(data))); err != nil {
		test.Fatalf("manifest round trip: %v", err)
	}
}

func TestScopeRejectsPrematureProductCode(test *testing.T) {
	for _, relative := range []string{"cmd/treeclear/main.go", "internal/domain/plan.go", "internal/adapter/manifest.go", "internal/adapterupdate/update.go", "internal/schedule/service.go"} {
		test.Run(relative, func(test *testing.T) {
			root := test.TempDir()
			writeFixtureFile(test, root, relative, "package fixture\n")
			manifest, err := DecodeManifest(strings.NewReader(manifestFixture()))
			if err != nil {
				test.Fatal(err)
			}
			if err := manifest.CheckScope(root); err == nil {
				test.Fatal("product code accepted while only stage 000 is active")
			}
			manifest.ActiveStage = "004"
			if err := manifest.CheckScope(root); err != nil {
				test.Fatal(err)
			}
		})
	}
}

func writeFixtureFile(test *testing.T, root, relative, contents string) string {
	test.Helper()
	filename := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		test.Fatal(err)
	}
	return filename
}
