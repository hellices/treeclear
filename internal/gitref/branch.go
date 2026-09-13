package gitref

import (
	"strings"
	"unicode/utf8"
)

func ValidBranchName(value string) bool {
	if value == "" || len(value) > 1024 || !utf8.ValidString(value) || value == "HEAD" || strings.HasPrefix(value, "-") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") || strings.Contains(value, "@{") || strings.ContainsAny(value, `~^:?*[\`) {
		return false
	}
	for _, character := range value {
		if character <= 32 || character == 127 {
			return false
		}
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}
