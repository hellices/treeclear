package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const warningPreviewCount = 5
const warningPreviewBytes = 512

func writeWarningPreview(output io.Writer, warnings []string, label, details string) error {
	truncated := false
	for _, warning := range warnings[:min(len(warnings), warningPreviewCount)] {
		quoted, shortened := quotedWarningPreview(warning)
		truncated = truncated || shortened
		if _, err := fmt.Fprintf(output, "%s: %s\n", label, quoted); err != nil {
			return err
		}
	}
	if len(warnings) > warningPreviewCount {
		if _, err := fmt.Fprintf(output, "... %d additional warnings; %s.\n", len(warnings)-warningPreviewCount, details); err != nil {
			return err
		}
	}
	if truncated {
		if _, err := fmt.Fprintf(output, "... warning text truncated; %s.\n", details); err != nil {
			return err
		}
	}
	return nil
}

func quotedWarningPreview(warning string) (string, bool) {
	if len(warning) <= warningPreviewBytes {
		quoted := strconv.Quote(warning)
		if len(quoted) <= warningPreviewBytes {
			return quoted, false
		}
	}
	var preview strings.Builder
	preview.Grow(warningPreviewBytes)
	preview.WriteByte('"')
	for len(warning) > 0 {
		_, size := utf8.DecodeRuneInString(warning)
		quoted := strconv.Quote(warning[:size])
		fragment := quoted[1 : len(quoted)-1]
		if preview.Len()+len(fragment)+len(`..."`) > warningPreviewBytes {
			break
		}
		preview.WriteString(fragment)
		warning = warning[size:]
	}
	preview.WriteString(`..."`)
	return preview.String(), true
}
