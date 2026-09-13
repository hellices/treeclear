package domain

import "time"

type Classification string

const (
	Protected Classification = "protected"
	Review    Classification = "review"
	Safe      Classification = "safe"
)

type Reason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Decision struct {
	Classification Classification `json:"classification"`
	Reasons        []Reason       `json:"reasons"`
	InactiveFor    time.Duration  `json:"inactiveFor"`
}

type Policy struct {
	Now      time.Time      `json:"now"`
	Settings PolicySettings `json:"settings"`
}

type PolicySettings struct {
	InactivityThreshold time.Duration `json:"inactivityThreshold"`
	PlanExpiry          time.Duration `json:"planExpiry"`
	BaseBranches        []string      `json:"baseBranches"`
	MinimumTrustGrade   TrustGrade    `json:"minimumTrustGrade"`
	SnapshotMaxBytes    int64         `json:"snapshotMaxBytes"`
}
