package plan

import (
	"crypto/sha256"
	"fmt"
	"slices"

	"github.com/hellices/treeclear/internal/domain"
)

func EvidenceDigest(value domain.EvidenceSet) (string, error) {
	contents, err := encodePreconditions(canonicalEvidence(value))
	if err != nil {
		return "", fmt.Errorf("encode evidence: %w", err)
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(contents)), nil
}

func canonicalEvidence(value domain.EvidenceSet) domain.EvidenceSet {
	value.Processes = slices.Clone(value.Processes)
	for processIndex := range value.Processes {
		process := &value.Processes[processIndex]
		process.CreatedAt = process.CreatedAt.UTC()
	}
	value.Agents = slices.Clone(value.Agents)
	for agentIndex := range value.Agents {
		agent := &value.Agents[agentIndex]
		agent.CreatedAt = agent.CreatedAt.UTC()
		agent.UpdatedAt = agent.UpdatedAt.UTC()
		agent.ObservedAt = agent.ObservedAt.UTC()
		agent.ProcessRefs = slices.Clone(agent.ProcessRefs)
		for referenceIndex := range agent.ProcessRefs {
			reference := &agent.ProcessRefs[referenceIndex]
			reference.CreatedAt = reference.CreatedAt.UTC()
		}
	}
	return value
}
