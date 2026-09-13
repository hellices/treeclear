package domain

import "time"

type EvidenceState string
type TrustGrade string

const (
	EvidenceActive        EvidenceState = "active"
	EvidenceIdle          EvidenceState = "idle"
	EvidenceCompleted     EvidenceState = "completed"
	EvidenceArchived      EvidenceState = "archived"
	EvidenceInactive      EvidenceState = "inactive"
	EvidenceUnknown       EvidenceState = "unknown"
	EvidenceNotApplicable EvidenceState = "not-applicable"
)

const (
	TrustSupportedAPI       TrustGrade = "supported-api"
	TrustSupportedAppServer TrustGrade = "supported-app-server"
	TrustSupportedCLI       TrustGrade = "supported-cli"
	TrustExperimentalAPI    TrustGrade = "experimental-api"
	TrustVersionedPrivate   TrustGrade = "versioned-private"
	TrustUnversionedPrivate TrustGrade = "unversioned-private"
	TrustProcessOnly        TrustGrade = "process-only"
)

type ProcessEvidence struct {
	PID         int32         `json:"pid"`
	CreatedAt   time.Time     `json:"createdAt"`
	Executable  string        `json:"executable"`
	CWD         string        `json:"cwd,omitempty"`
	State       EvidenceState `json:"state"`
	Error       string        `json:"error,omitempty"`
	Fingerprint string        `json:"fingerprint"`
}

type ProcessReference struct {
	PID         int32     `json:"pid"`
	CreatedAt   time.Time `json:"createdAt"`
	Executable  string    `json:"executable"`
	Fingerprint string    `json:"fingerprint"`
}

type ExecutableIdentity struct {
	Path              string   `json:"path"`
	Version           string   `json:"version"`
	SHA256            string   `json:"sha256"`
	Arguments         []string `json:"arguments"`
	WorkingDirectory  string   `json:"workingDirectory"`
	EnvironmentDigest string   `json:"environmentDigest"`
	InvocationDigest  string   `json:"invocationDigest"`
}

type WorktreeBinding struct {
	Kind       string `json:"kind"`
	Identifier string `json:"identifier"`
	Version    string `json:"version,omitempty"`
}

type AgentEvidence struct {
	AdapterID         string              `json:"adapterId"`
	AdapterVersion    string              `json:"adapterVersion"`
	BundleDigest      string              `json:"bundleDigest"`
	Provider          string              `json:"provider"`
	SourceID          string              `json:"sourceId"`
	SessionID         string              `json:"sessionId,omitempty"`
	ThreadID          string              `json:"threadId,omitempty"`
	ProjectID         string              `json:"projectId,omitempty"`
	CWD               string              `json:"cwd,omitempty"`
	RepositoryRoot    string              `json:"repositoryRoot,omitempty"`
	WorktreePath      string              `json:"worktreePath,omitempty"`
	State             EvidenceState       `json:"state"`
	CreatedAt         time.Time           `json:"createdAt,omitempty"`
	UpdatedAt         time.Time           `json:"updatedAt,omitempty"`
	ObservedAt        time.Time           `json:"observedAt"`
	ProcessRefs       []ProcessReference  `json:"processRefs,omitempty"`
	Binding           *WorktreeBinding    `json:"binding,omitempty"`
	SourceKind        string              `json:"sourceKind"`
	SupportGrade      TrustGrade          `json:"supportGrade"`
	Confidence        string              `json:"confidence"`
	SchemaVersion     string              `json:"schemaVersion,omitempty"`
	RawFingerprint    string              `json:"rawFingerprint"`
	TrustRecordDigest string              `json:"trustRecordDigest,omitempty"`
	Executable        *ExecutableIdentity `json:"executable,omitempty"`
	RevalidationMode  string              `json:"revalidationMode"`
	Warnings          []string            `json:"warnings,omitempty"`
}

type EvidenceSet struct {
	Processes []ProcessEvidence `json:"processes"`
	Agents    []AgentEvidence   `json:"agents"`
	Adapters  []AdapterStatus   `json:"adapters,omitempty"`
	Warnings  []string          `json:"warnings,omitempty"`
}

type AdapterStatus struct {
	AdapterID            string     `json:"adapterId"`
	Applicable           bool       `json:"applicable"`
	Healthy              bool       `json:"healthy"`
	Trusted              bool       `json:"trusted"`
	BestGrade            TrustGrade `json:"bestGrade,omitempty"`
	OfflineRevalidatable bool       `json:"offlineRevalidatable"`
	Error                string     `json:"error,omitempty"`
}
