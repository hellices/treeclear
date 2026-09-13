package plan

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"unicode/utf8"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/fssecure"
)

const integrityKeySize = 32

const integrityAlgorithm = "hmac-sha256"

var (
	ErrPlanIntegrity = errors.New("plan integrity verification failed")
	ErrIntegrityKey  = errors.New("invalid plan integrity key")
)

func encodeSignedPlan(value domain.Plan, key []byte) ([]byte, error) {
	if err := validateIntegrityKey(key); err != nil {
		return nil, err
	}
	value.Integrity = domain.PlanIntegrity{Algorithm: integrityAlgorithm, KeyID: integrityKeyID(key)}
	payload, err := canonicalPlanJSON(value)
	if err != nil {
		return nil, err
	}
	authenticator := hmac.New(sha256.New, key)
	_, _ = authenticator.Write(payload)
	value.Integrity.MAC = hex.EncodeToString(authenticator.Sum(nil))
	return canonicalPlanJSON(value)
}

func decodeAuthenticatedPlan(contents, key []byte) (domain.Plan, error) {
	if err := validateIntegrityKey(key); err != nil {
		return domain.Plan{}, err
	}
	contents = bytes.TrimSpace(contents)
	if !utf8.Valid(contents) {
		return domain.Plan{}, ErrPlanIntegrity
	}
	var value domain.Plan
	if err := json.Unmarshal(contents, &value); err != nil {
		return domain.Plan{}, fmt.Errorf("%w: invalid document", ErrPlanIntegrity)
	}
	canonical, err := canonicalPlanJSON(value)
	if err != nil || !bytes.Equal(canonical, contents) {
		return domain.Plan{}, fmt.Errorf("%w: noncanonical document", ErrPlanIntegrity)
	}
	if value.Integrity.Algorithm != integrityAlgorithm || value.Integrity.KeyID != integrityKeyID(key) {
		return domain.Plan{}, ErrPlanIntegrity
	}
	claimed, err := hex.DecodeString(value.Integrity.MAC)
	if err != nil || len(claimed) != sha256.Size || hex.EncodeToString(claimed) != value.Integrity.MAC {
		return domain.Plan{}, ErrPlanIntegrity
	}
	unsigned := value
	unsigned.Integrity.MAC = ""
	payload, err := canonicalPlanJSON(unsigned)
	if err != nil {
		return domain.Plan{}, ErrPlanIntegrity
	}
	authenticator := hmac.New(sha256.New, key)
	_, _ = authenticator.Write(payload)
	if !hmac.Equal(claimed, authenticator.Sum(nil)) {
		return domain.Plan{}, ErrPlanIntegrity
	}
	return value, nil
}

func integrityKeyID(key []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(key))
}

func validateIntegrityKey(key []byte) error {
	if len(key) != integrityKeySize {
		return fmt.Errorf("%w: expected %d bytes", ErrIntegrityKey, integrityKeySize)
	}
	return nil
}

func canonicalPlanJSON(value domain.Plan) ([]byte, error) {
	value.GeneratedAt = value.GeneratedAt.UTC()
	value.ExpiresAt = value.ExpiresAt.UTC()
	value.Candidates = slices.Clone(value.Candidates)
	for candidateIndex := range value.Candidates {
		candidate := &value.Candidates[candidateIndex]
		candidate.Worktree.LastCommitAt = candidate.Worktree.LastCommitAt.UTC()
		candidate.Worktree.MetadataModifiedAt = candidate.Worktree.MetadataModifiedAt.UTC()
		candidate.Evidence.Processes = slices.Clone(candidate.Evidence.Processes)
		for processIndex := range candidate.Evidence.Processes {
			process := &candidate.Evidence.Processes[processIndex]
			process.CreatedAt = process.CreatedAt.UTC()
		}
		candidate.Evidence.Agents = slices.Clone(candidate.Evidence.Agents)
		for agentIndex := range candidate.Evidence.Agents {
			agent := &candidate.Evidence.Agents[agentIndex]
			agent.CreatedAt = agent.CreatedAt.UTC()
			agent.UpdatedAt = agent.UpdatedAt.UTC()
			agent.ObservedAt = agent.ObservedAt.UTC()
			agent.ProcessRefs = slices.Clone(agent.ProcessRefs)
			for referenceIndex := range agent.ProcessRefs {
				reference := &agent.ProcessRefs[referenceIndex]
				reference.CreatedAt = reference.CreatedAt.UTC()
			}
		}
	}
	return encodePreconditions(value)
}

func (store Store) localIntegrityKey(ctx context.Context, create bool) ([]byte, error) {
	if store.injectedKey != nil {
		return store.injectedKey, nil
	}
	path := filepath.Join(store.root, "integrity.key")
	key, err := fssecure.ReadPrivateFile(path, integrityKeySize)
	if err == nil {
		if err := validateIntegrityKey(key); err != nil {
			return nil, err
		}
		return key, nil
	}
	if !create || !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %w", ErrIntegrityKey, err)
	}
	key = make([]byte, integrityKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrIntegrityKey, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := fssecure.WritePrivateFile(path, key); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("%w: %w", ErrIntegrityKey, err)
		}
		key, err = fssecure.ReadPrivateFile(path, integrityKeySize)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrIntegrityKey, err)
		}
	}
	if err := validateIntegrityKey(key); err != nil {
		return nil, err
	}
	return key, nil
}
