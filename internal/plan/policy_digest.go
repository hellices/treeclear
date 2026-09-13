package plan

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/hellices/treeclear/internal/domain"
)

func PolicyDigest(settings domain.PolicySettings) (string, error) {
	if !utf8.ValidString(string(settings.MinimumTrustGrade)) {
		return "", fmt.Errorf("policy minimumTrustGrade contains invalid UTF-8")
	}
	for branchIndex, branch := range settings.BaseBranches {
		if !utf8.ValidString(branch) {
			return "", fmt.Errorf("policy baseBranches[%d] contains invalid UTF-8", branchIndex)
		}
	}
	settings.BaseBranches = append([]string{}, settings.BaseBranches...)
	sort.Strings(settings.BaseBranches)
	encoded, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("encode policy settings: %w", err)
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(encoded)), nil
}
