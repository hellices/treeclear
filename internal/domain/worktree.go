package domain

import "time"

type GitStatus struct {
	Staged    int `json:"staged"`
	Unstaged  int `json:"unstaged"`
	Unmerged  int `json:"unmerged"`
	Untracked int `json:"untracked"`
}

func (status GitStatus) Clean() bool {
	return status.Staged == 0 && status.Unstaged == 0 && status.Unmerged == 0 && status.Untracked == 0
}

type Worktree struct {
	Path               string    `json:"path"`
	RepositoryRoot     string    `json:"repositoryRoot"`
	CommonGitDir       string    `json:"commonGitDir"`
	AdminDir           string    `json:"adminDir"`
	Head               string    `json:"head"`
	Branch             string    `json:"branch,omitempty"`
	Upstream           string    `json:"upstream,omitempty"`
	Primary            bool      `json:"primary"`
	Current            bool      `json:"current"`
	PathSafe           bool      `json:"pathSafe"`
	Detached           bool      `json:"detached"`
	Locked             bool      `json:"locked"`
	LockReason         string    `json:"lockReason,omitempty"`
	Prunable           bool      `json:"prunable"`
	GitStateKnown      bool      `json:"gitStateKnown"`
	CollectionErrors   []string  `json:"collectionErrors,omitempty"`
	Status             GitStatus `json:"status"`
	Recoverable        bool      `json:"recoverable"`
	LastCommitAt       time.Time `json:"lastCommitAt"`
	MetadataModifiedAt time.Time `json:"metadataModifiedAt"`
	EstimatedBytes     int64     `json:"estimatedBytes"`
	IndexHash          string    `json:"indexHash"`
	AdminHash          string    `json:"adminHash"`
}
