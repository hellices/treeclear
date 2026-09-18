package domain

import "time"

type Plan struct {
	SchemaVersion        int                  `json:"schemaVersion"`
	ID                   string               `json:"planId"`
	GeneratedAt          time.Time            `json:"generatedAt"`
	ExpiresAt            time.Time            `json:"expiresAt"`
	ToolVersion          string               `json:"toolVersion"`
	IntendedApplyMode    ApplyMode            `json:"intendedApplyMode"`
	PolicyDigest         string               `json:"policyDigest"`
	AdapterLockDigest    string               `json:"adapterLockDigest"`
	ExecutableIdentities []ExecutableIdentity `json:"executableIdentities,omitempty"`
	Scope                PlanScope            `json:"scope"`
	Candidates           []Candidate          `json:"candidates"`
	Summary              PlanSummary          `json:"summary"`
	Warnings             []string             `json:"warnings,omitempty"`
	Removal              *RemovalPlan         `json:"removal,omitempty"`
	Integrity            PlanIntegrity        `json:"integrity"`
}

type ApplyMode string

const (
	ApplyInteractive ApplyMode = "interactive"
	ApplyScheduled   ApplyMode = "scheduled"
)

type PlanIntegrity struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	MAC       string `json:"mac"`
}

type PlanScope struct {
	Roots []string `json:"roots"`
}

type Candidate struct {
	ID          string              `json:"candidateId"`
	Worktree    Worktree            `json:"worktree"`
	Evidence    EvidenceSet         `json:"evidence"`
	Decision    Decision            `json:"decision"`
	Action      string              `json:"action"`
	Fingerprint string              `json:"fingerprint"`
	Snapshot    SnapshotPlan        `json:"snapshot"`
	Selection   *CandidateSelection `json:"selection,omitempty"`
}

type SnapshotPlan struct {
	Required         bool  `json:"required"`
	MaximumBytes     int64 `json:"maximumBytes"`
	UntrackedFiles   int   `json:"untrackedFiles"`
	UntrackedBytes   int64 `json:"untrackedBytes"`
	SensitiveBlocked bool  `json:"sensitiveBlocked"`
}

type PlanSummary struct {
	Safe             int   `json:"safe"`
	Review           int   `json:"review"`
	Protected        int   `json:"protected"`
	ReclaimableBytes int64 `json:"reclaimableBytes"`
}

type ApplyJournal struct {
	SchemaVersion int            `json:"schemaVersion"`
	PlanID        string         `json:"planId"`
	StartedAt     time.Time      `json:"startedAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
	State         string         `json:"state"`
	Entries       []JournalEntry `json:"entries"`
}

type JournalEntry struct {
	CandidateID string `json:"candidateId"`
	State       string `json:"state"`
	SnapshotID  string `json:"snapshotId,omitempty"`
	Error       string `json:"error,omitempty"`
}
