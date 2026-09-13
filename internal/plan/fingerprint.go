package plan

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/hellices/treeclear/internal/domain"
)

type candidatePreconditions struct {
	Path               string                `json:"path"`
	RepositoryRoot     string                `json:"repositoryRoot"`
	CommonGitDir       string                `json:"commonGitDir"`
	AdminDir           string                `json:"adminDir"`
	Head               string                `json:"head"`
	Branch             string                `json:"branch"`
	Upstream           string                `json:"upstream"`
	IndexHash          string                `json:"indexHash"`
	AdminHash          string                `json:"adminHash"`
	Primary            bool                  `json:"primary"`
	Current            bool                  `json:"current"`
	PathSafe           bool                  `json:"pathSafe"`
	Detached           bool                  `json:"detached"`
	Locked             bool                  `json:"locked"`
	Prunable           bool                  `json:"prunable"`
	GitStateKnown      bool                  `json:"gitStateKnown"`
	CollectionErrors   []string              `json:"collectionErrors"`
	Recoverable        bool                  `json:"recoverable"`
	Status             domain.GitStatus      `json:"status"`
	LastCommitAt       time.Time             `json:"lastCommitAt"`
	MetadataModifiedAt time.Time             `json:"metadataModifiedAt"`
	Snapshot           domain.SnapshotPlan   `json:"snapshot"`
	Action             string                `json:"action"`
	Decision           decisionPrecondition  `json:"decision"`
	Processes          []processPrecondition `json:"processes"`
	Agents             []agentPrecondition   `json:"agents"`
	Adapters           []adapterPrecondition `json:"adapters"`
	HasWarnings        bool                  `json:"hasWarnings"`
}

type decisionPrecondition struct {
	Classification domain.Classification `json:"classification"`
	ReasonCodes    []string              `json:"reasonCodes"`
}

type processPrecondition struct {
	PID         int32                `json:"pid"`
	CreatedAt   time.Time            `json:"createdAt"`
	Executable  string               `json:"executable"`
	CWD         string               `json:"cwd"`
	State       domain.EvidenceState `json:"state"`
	Fingerprint string               `json:"fingerprint"`
	HasError    bool                 `json:"hasError"`
}

type agentPrecondition struct {
	AdapterID         string                     `json:"adapterId"`
	AdapterVersion    string                     `json:"adapterVersion"`
	BundleDigest      string                     `json:"bundleDigest"`
	Provider          string                     `json:"provider"`
	SourceID          string                     `json:"sourceId"`
	SessionID         string                     `json:"sessionId"`
	ThreadID          string                     `json:"threadId"`
	ProjectID         string                     `json:"projectId"`
	CWD               string                     `json:"cwd"`
	RepositoryRoot    string                     `json:"repositoryRoot"`
	WorktreePath      string                     `json:"worktreePath"`
	State             domain.EvidenceState       `json:"state"`
	CreatedAt         time.Time                  `json:"createdAt"`
	UpdatedAt         time.Time                  `json:"updatedAt"`
	ProcessRefs       []domain.ProcessReference  `json:"processRefs"`
	Binding           *domain.WorktreeBinding    `json:"binding"`
	SourceKind        string                     `json:"sourceKind"`
	SupportGrade      domain.TrustGrade          `json:"supportGrade"`
	Confidence        string                     `json:"confidence"`
	SchemaVersion     string                     `json:"schemaVersion"`
	RawFingerprint    string                     `json:"rawFingerprint"`
	TrustRecordDigest string                     `json:"trustRecordDigest"`
	Executable        *domain.ExecutableIdentity `json:"executable"`
	RevalidationMode  string                     `json:"revalidationMode"`
	HasWarnings       bool                       `json:"hasWarnings"`
}

type adapterPrecondition struct {
	AdapterID            string            `json:"adapterId"`
	Applicable           bool              `json:"applicable"`
	Healthy              bool              `json:"healthy"`
	Trusted              bool              `json:"trusted"`
	BestGrade            domain.TrustGrade `json:"bestGrade"`
	OfflineRevalidatable bool              `json:"offlineRevalidatable"`
	HasError             bool              `json:"hasError"`
}

func CandidateFingerprint(candidate domain.Candidate) (string, error) {
	worktree := candidate.Worktree
	preconditions := candidatePreconditions{
		Path: worktree.Path, RepositoryRoot: worktree.RepositoryRoot, CommonGitDir: worktree.CommonGitDir,
		AdminDir: worktree.AdminDir, Head: worktree.Head, Branch: worktree.Branch, Upstream: worktree.Upstream,
		IndexHash: worktree.IndexHash, AdminHash: worktree.AdminHash, Primary: worktree.Primary,
		Current: worktree.Current, PathSafe: worktree.PathSafe, Detached: worktree.Detached,
		Locked: worktree.Locked, Prunable: worktree.Prunable, GitStateKnown: worktree.GitStateKnown,
		CollectionErrors: append([]string{}, worktree.CollectionErrors...), Recoverable: worktree.Recoverable,
		Status: worktree.Status, LastCommitAt: worktree.LastCommitAt.UTC(), MetadataModifiedAt: worktree.MetadataModifiedAt.UTC(),
		Snapshot: candidate.Snapshot, Action: candidate.Action,
		Decision:  decisionPrecondition{Classification: candidate.Decision.Classification, ReasonCodes: []string{}},
		Processes: []processPrecondition{}, Agents: []agentPrecondition{}, Adapters: []adapterPrecondition{},
		HasWarnings: len(candidate.Evidence.Warnings) != 0,
	}
	slices.Sort(preconditions.CollectionErrors)
	for _, reason := range candidate.Decision.Reasons {
		preconditions.Decision.ReasonCodes = append(preconditions.Decision.ReasonCodes, reason.Code)
	}
	slices.Sort(preconditions.Decision.ReasonCodes)
	for _, process := range candidate.Evidence.Processes {
		preconditions.Processes = append(preconditions.Processes, processPrecondition{
			PID: process.PID, CreatedAt: process.CreatedAt.UTC(), Executable: process.Executable, CWD: process.CWD,
			State: process.State, Fingerprint: process.Fingerprint, HasError: process.Error != "",
		})
	}
	for _, agent := range candidate.Evidence.Agents {
		switch agent.RevalidationMode {
		case "planning-only":
			continue
		case "local-readonly":
		default:
			return "", errors.New("agent revalidation mode must be local-readonly or planning-only")
		}
		precondition, err := offlineAgentPrecondition(agent)
		if err != nil {
			return "", err
		}
		preconditions.Agents = append(preconditions.Agents, precondition)
	}
	for _, adapter := range candidate.Evidence.Adapters {
		preconditions.Adapters = append(preconditions.Adapters, adapterPrecondition{
			AdapterID: adapter.AdapterID, Applicable: adapter.Applicable, Healthy: adapter.Healthy,
			Trusted: adapter.Trusted, BestGrade: adapter.BestGrade, OfflineRevalidatable: adapter.OfflineRevalidatable,
			HasError: adapter.Error != "",
		})
	}
	if err := sortPreconditions(preconditions.Processes); err != nil {
		return "", err
	}
	if err := sortPreconditions(preconditions.Agents); err != nil {
		return "", err
	}
	if err := sortPreconditions(preconditions.Adapters); err != nil {
		return "", err
	}
	contents, err := encodePreconditions(preconditions)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(contents)), nil
}

func offlineAgentPrecondition(agent domain.AgentEvidence) (agentPrecondition, error) {
	references := append([]domain.ProcessReference{}, agent.ProcessRefs...)
	for index := range references {
		references[index].CreatedAt = references[index].CreatedAt.UTC()
	}
	if err := sortPreconditions(references); err != nil {
		return agentPrecondition{}, err
	}
	var executable *domain.ExecutableIdentity
	if agent.Executable != nil {
		copy := *agent.Executable
		copy.Arguments = append([]string{}, copy.Arguments...)
		executable = &copy
	}
	return agentPrecondition{
		AdapterID: agent.AdapterID, AdapterVersion: agent.AdapterVersion, BundleDigest: agent.BundleDigest,
		Provider: agent.Provider, SourceID: agent.SourceID, SessionID: agent.SessionID, ThreadID: agent.ThreadID,
		ProjectID: agent.ProjectID, CWD: agent.CWD, RepositoryRoot: agent.RepositoryRoot, WorktreePath: agent.WorktreePath,
		State: agent.State, CreatedAt: agent.CreatedAt.UTC(), UpdatedAt: agent.UpdatedAt.UTC(),
		ProcessRefs: references, Binding: agent.Binding, SourceKind: agent.SourceKind, SupportGrade: agent.SupportGrade,
		Confidence: agent.Confidence, SchemaVersion: agent.SchemaVersion, RawFingerprint: agent.RawFingerprint,
		TrustRecordDigest: agent.TrustRecordDigest, Executable: executable, RevalidationMode: agent.RevalidationMode,
		HasWarnings: len(agent.Warnings) != 0,
	}, nil
}

func sortPreconditions[Value any](values []Value) error {
	type encodedValue struct {
		value Value
		key   string
	}
	encoded := make([]encodedValue, len(values))
	for index, value := range values {
		contents, err := encodePreconditions(value)
		if err != nil {
			return err
		}
		encoded[index] = encodedValue{value: value, key: string(contents)}
	}
	sort.Slice(encoded, func(first, second int) bool { return encoded[first].key < encoded[second].key })
	for index, item := range encoded {
		values[index] = item.value
	}
	return nil
}

func encodePreconditions(value any) ([]byte, error) {
	if err := validatePreconditionText(reflect.ValueOf(value)); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func validatePreconditionText(value reflect.Value) error {
	switch value.Kind() {
	case reflect.String:
		if !utf8.ValidString(value.String()) {
			return errors.New("fingerprint precondition contains invalid UTF-8")
		}
	case reflect.Pointer, reflect.Interface:
		if !value.IsNil() {
			return validatePreconditionText(value.Elem())
		}
	case reflect.Struct:
		for index := range value.NumField() {
			if value.Type().Field(index).IsExported() {
				if err := validatePreconditionText(value.Field(index)); err != nil {
					return err
				}
			}
		}
	case reflect.Slice, reflect.Array:
		for index := range value.Len() {
			if err := validatePreconditionText(value.Index(index)); err != nil {
				return err
			}
		}
	}
	return nil
}
