package domain

const (
	RemovalIntentInventory = "inventory-preview"
	RemovalIntentExplicit  = "explicit-worktree-removal"
	ContentDiscardAll      = "discard-all"
	BackupNone             = "none"
	ExecutionPreviewOnly   = "preview-only"
)

type RemovalPlan struct {
	Intent             string   `json:"intent"`
	ContentDisposition string   `json:"contentDisposition"`
	BackupMode         string   `json:"backupMode"`
	SkipDirty          bool     `json:"skipDirty"`
	SelectedPaths      []string `json:"selectedPaths"`
	Execution          string   `json:"execution"`
}

type CandidateSelection struct {
	Selected   bool   `json:"selected"`
	SkipReason string `json:"skipReason"`
}
