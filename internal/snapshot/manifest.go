package snapshot

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hellices/treeclear/internal/pathutil"
)

const (
	maximumManifestBytes         = 32 << 20
	maximumAdministrativeEntries = 4096
	maximumAdministrativeBytes   = 16 << 20
)

var (
	ErrManifestInvalid = errors.New("invalid snapshot manifest")
	ErrManifestLimit   = errors.New("snapshot manifest limit exceeded")
)

type Manifest struct {
	SchemaVersion         int               `json:"schemaVersion"`
	ToolVersion           string            `json:"toolVersion"`
	PlanSchemaVersion     int               `json:"planSchemaVersion"`
	SnapshotID            string            `json:"snapshotId"`
	PlanID                string            `json:"planId"`
	CandidateID           string            `json:"candidateId"`
	CandidateFingerprint  string            `json:"candidateFingerprint"`
	PolicyDigest          string            `json:"policyDigest"`
	AdapterLockDigest     string            `json:"adapterLockDigest"`
	EvidenceDigest        string            `json:"evidenceDigest"`
	CreatedAt             time.Time         `json:"createdAt"`
	RepositoryRoot        string            `json:"repositoryRoot"`
	CommonGitDir          string            `json:"commonGitDir"`
	WorktreePath          string            `json:"worktreePath"`
	AdminDir              string            `json:"adminDir"`
	Head                  string            `json:"head"`
	Branch                string            `json:"branch,omitempty"`
	RecoveryRef           string            `json:"recoveryRef,omitempty"`
	Files                 map[string]string `json:"files"`
	UntrackedFiles        int               `json:"untrackedFiles"`
	UntrackedBytes        int64             `json:"untrackedBytes"`
	AdministrativeEntries []AdminEntry      `json:"administrativeEntries"`
}

type AdminEntry struct {
	Path string      `json:"path"`
	Kind string      `json:"kind"`
	Mode fs.FileMode `json:"mode"`
	Data []byte      `json:"data"`
}

func ValidateManifest(value Manifest) error {
	if len(value.AdministrativeEntries) > maximumAdministrativeEntries {
		return manifestLimit("administrative entry count")
	}
	if value.SchemaVersion != 1 || value.PlanSchemaVersion != 1 {
		return fmt.Errorf("%w: unsupported schema version", ErrManifestInvalid)
	}
	if len(value.ToolVersion) == 0 || len(value.ToolVersion) > 128 {
		return fmt.Errorf("%w: tool version is required and bounded", ErrManifestInvalid)
	}
	for _, character := range value.ToolVersion {
		if character < '!' || character > '~' {
			return fmt.Errorf("%w: invalid tool version", ErrManifestInvalid)
		}
	}
	if !validIdentifier(value.SnapshotID, "snapshot_") || !validIdentifier(value.PlanID, "plan_") || !validIdentifier(value.CandidateID, "candidate_") {
		return fmt.Errorf("%w: invalid snapshot, plan or candidate identifier", ErrManifestInvalid)
	}
	for _, field := range []struct{ name, digest string }{
		{"candidate fingerprint", value.CandidateFingerprint},
		{"policy digest", value.PolicyDigest},
		{"adapter lock digest", value.AdapterLockDigest},
		{"evidence digest", value.EvidenceDigest},
	} {
		if !validDigest(field.digest) {
			return fmt.Errorf("%w: invalid %s", ErrManifestInvalid, field.name)
		}
	}
	created := value.CreatedAt.UTC()
	if created.IsZero() {
		return fmt.Errorf("%w: creation time is required", ErrManifestInvalid)
	}
	if _, err := created.MarshalJSON(); err != nil {
		return fmt.Errorf("%w: invalid creation time", ErrManifestInvalid)
	}
	for _, field := range []struct{ name, path string }{
		{"repository root", value.RepositoryRoot},
		{"common Git directory", value.CommonGitDir},
		{"worktree path", value.WorktreePath},
		{"administrative directory", value.AdminDir},
	} {
		if len(field.path) > 32<<10 {
			return manifestLimit(field.name)
		}
		if err := pathutil.ValidateAbsoluteForm(field.path); err != nil {
			return fmt.Errorf("%w: invalid %s: %w", ErrManifestInvalid, field.name, err)
		}
	}
	if !(validLowerHex(value.Head, 40) || validLowerHex(value.Head, 64)) || strings.Trim(value.Head, "0") == "" {
		return fmt.Errorf("%w: HEAD must be a full nonzero object ID", ErrManifestInvalid)
	}
	if value.Branch == "" {
		if value.RecoveryRef != "refs/treeclear/recovery/"+value.SnapshotID {
			return fmt.Errorf("%w: detached HEAD requires its snapshot recovery ref", ErrManifestInvalid)
		}
	} else if !validBranch(value.Branch) || value.RecoveryRef != "" {
		return fmt.Errorf("%w: invalid or conflicting branch identity", ErrManifestInvalid)
	}
	if value.UntrackedFiles < 0 || value.UntrackedBytes < 0 || value.UntrackedFiles == 0 && value.UntrackedBytes != 0 {
		return fmt.Errorf("%w: inconsistent untracked accounting", ErrManifestInvalid)
	}
	if len(value.Files) != len(requiredPayloads) {
		return fmt.Errorf("%w: unexpected payload set", ErrManifestInvalid)
	}
	for _, name := range requiredPayloads {
		if !validDigest(value.Files[name]) {
			return fmt.Errorf("%w: missing or invalid payload digest for %s", ErrManifestInvalid, name)
		}
	}
	return validateAdministrativeEntries(value.AdministrativeEntries)
}

func validIdentifier(value, prefix string) bool {
	if len(value) <= len(prefix) || len(value) > 128 || !strings.HasPrefix(value, prefix) {
		return false
	}
	for _, character := range value[len(prefix):] {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func validDigest(value string) bool {
	return strings.HasPrefix(value, "sha256:") && validLowerHex(value[len("sha256:"):], 64)
}

func validLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func validBranch(value string) bool {
	if value == "" || len(value) > 1024 || !utf8.ValidString(value) || value == "HEAD" || strings.HasPrefix(value, "-") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") || strings.Contains(value, "@{") || strings.ContainsAny(value, `~^:?*[\`) {
		return false
	}
	for _, character := range value {
		if character <= 32 || character == 127 {
			return false
		}
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}

func manifestLimit(subject string) error {
	return fmt.Errorf("%w: %w: %s", ErrManifestInvalid, ErrManifestLimit, subject)
}
