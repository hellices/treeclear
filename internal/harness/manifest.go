package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"strings"
)

const modulePath = "github.com/hellices/treeclear"
const manifestPath = "tests/acceptance/requirements.json"

type Manifest struct {
	SchemaVersion int     `json:"schemaVersion"`
	ActiveStage   string  `json:"activeStage"`
	Stages        []Stage `json:"stages"`
}

type Stage struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Requirements []Requirement `json:"requirements"`
}

type Requirement struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Package     string `json:"package"`
	Test        string `json:"test"`
}

func DecodeManifest(reader io.Reader) (Manifest, error) {
	data, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
	if err != nil {
		return Manifest{}, err
	}
	if len(data) > 1<<20 {
		return Manifest{}, errors.New("acceptance manifest exceeds 1 MiB")
	}
	if err := rejectDuplicateKeys(json.NewDecoder(strings.NewReader(string(data)))); err != nil {
		return Manifest{}, fmt.Errorf("ambiguous acceptance manifest: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode acceptance manifest: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Manifest{}, errors.New("acceptance manifest contains trailing data")
	}
	if manifest.SchemaVersion != 1 || len(manifest.Stages) != 5 {
		return Manifest{}, errors.New("acceptance manifest must use schema 1 and stages 000 through 004")
	}
	packagePattern := regexp.MustCompile(`^\./[A-Za-z0-9_-]+(/[A-Za-z0-9_-]+)*$`)
	testPattern := regexp.MustCompile(`^Test[A-Z0-9_][A-Za-z0-9_]*$`)
	idPattern := regexp.MustCompile(`^[A-Z][A-Z0-9_-]+$`)
	seen := make(map[string]bool)
	for index, stage := range manifest.Stages {
		if stage.ID != fmt.Sprintf("%03d", index) || strings.TrimSpace(stage.Name) == "" || len(stage.Requirements) == 0 {
			return Manifest{}, fmt.Errorf("invalid or empty stage at position %d", index)
		}
		for _, requirement := range stage.Requirements {
			if !idPattern.MatchString(requirement.ID) || seen[requirement.ID] {
				return Manifest{}, fmt.Errorf("invalid or duplicate requirement ID %q", requirement.ID)
			}
			seen[requirement.ID] = true
			if strings.TrimSpace(requirement.Description) == "" || !packagePattern.MatchString(requirement.Package) || !testPattern.MatchString(requirement.Test) {
				return Manifest{}, fmt.Errorf("invalid evidence selector for %s", requirement.ID)
			}
		}
	}
	if _, err := manifest.Select(manifest.ActiveStage); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func rejectDuplicateKeys(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	seen := make(map[string]bool)
	for decoder.More() {
		if delimiter == '{' {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, valid := keyToken.(string)
			if !valid || seen[key] {
				return fmt.Errorf("duplicate or invalid JSON key %q", keyToken)
			}
			seen[key] = true
		}
		if err := rejectDuplicateKeys(decoder); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func (manifest Manifest) Select(stageID string) ([]Requirement, error) {
	var selected []Requirement
	for _, stage := range manifest.Stages {
		selected = append(selected, stage.Requirements...)
		if stage.ID == stageID {
			return selected, nil
		}
	}
	return nil, fmt.Errorf("unknown acceptance stage %q", stageID)
}

func (manifest Manifest) CheckScope(root string) error {
	return walkRepository(root, func(relative string, entry fs.DirEntry) error {
		if entry.IsDir() {
			return nil
		}
		required := owningStage(relative)
		if required > manifest.ActiveStage {
			return fmt.Errorf("%s belongs to stage %s, but activeStage is %s; activate its cumulative acceptance gate", relative, required, manifest.ActiveStage)
		}
		return nil
	})
}

func owningStage(relative string) string {
	for _, prefix := range []string{"internal/schedule/", "internal/integration/", "integrations/", ".agents/skills/treeclear/", ".github/workflows/release.yml"} {
		if strings.HasPrefix(relative, prefix) {
			return "004"
		}
	}
	for _, prefix := range []string{"internal/signing/", "internal/adapterstore/", "internal/adapterupdate/", "internal/adaptertrust/", "internal/adapterdev/", "trust/", "schemas/adapter-index/"} {
		if strings.HasPrefix(relative, prefix) {
			return "003"
		}
	}
	for _, prefix := range []string{"internal/adapter/", "adapters/", "schemas/adapter/", "schemas/evidence/"} {
		if strings.HasPrefix(relative, prefix) {
			return "002"
		}
	}
	if strings.HasSuffix(relative, ".go") {
		for _, prefix := range []string{"internal/harness/", "internal/testutil/", "tools/harness/"} {
			if strings.HasPrefix(relative, prefix) {
				return "000"
			}
		}
		return "001"
	}
	return "000"
}

func (requirement Requirement) importPath() string {
	return modulePath + "/" + strings.TrimPrefix(requirement.Package, "./")
}
