package pathutil

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func ValidateAbsoluteForm(value string) error {
	if value == "" || !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("invalid absolute path form %q", value)
	}
	prepared, err := preparePath(value)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(prepared) || prepared != value || filepath.Clean(prepared) != prepared {
		return fmt.Errorf("noncanonical absolute path form %q", value)
	}
	return nil
}
