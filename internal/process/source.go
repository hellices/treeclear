package process

import (
	"context"
	"time"

	"github.com/hellices/treeclear/internal/domain"
)

type Info struct {
	PID           int32
	CreatedAt     time.Time
	Executable    string
	CommandLine   []string
	CWD           string
	Owner         string
	OwnerRelation OwnerRelation
	Inspectable   bool
	Error         string
}

type OwnerRelation string

const (
	OwnerSame    OwnerRelation = "same-user"
	OwnerOther   OwnerRelation = "other-user"
	OwnerUnknown OwnerRelation = "unknown"
)

type Source interface {
	List(context.Context) ([]Info, error)
}

type Collection struct {
	ByWorktree    map[string][]domain.ProcessEvidence
	Uninspectable map[int32]domain.ProcessEvidence
	GlobalUnknown []domain.ProcessEvidence
	Complete      bool
	Errors        []string
}
