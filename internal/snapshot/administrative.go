package snapshot

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
	"unicode"
)

func validateAdministrativeEntries(entries []AdminEntry) error {
	byPath := make(map[string]AdminEntry, len(entries))
	identities := make(map[string]bool, len(entries))
	totalBytes := 0
	for _, entry := range entries {
		if !validAdministrativePath(entry.Path) {
			return fmt.Errorf("%w: invalid administrative entry path", ErrManifestInvalid)
		}
		identity := foldAdministrativePath(entry.Path)
		if identities[identity] {
			return fmt.Errorf("%w: conflicting administrative entry paths", ErrManifestInvalid)
		}
		identities[identity] = true
		byPath[entry.Path] = entry
		if len(entry.Data) > maximumAdministrativeBytes-totalBytes {
			return manifestLimit("aggregate administrative bytes")
		}
		totalBytes += len(entry.Data)
		switch entry.Kind {
		case "directory":
			if entry.Mode & ^fs.ModePerm != fs.ModeDir || entry.Data != nil {
				return fmt.Errorf("%w: invalid administrative directory mode or data", ErrManifestInvalid)
			}
		case "file":
			if entry.Mode & ^fs.ModePerm != 0 || entry.Path == "." {
				return fmt.Errorf("%w: invalid administrative file mode or path", ErrManifestInvalid)
			}
		default:
			return fmt.Errorf("%w: unsupported administrative entry kind", ErrManifestInvalid)
		}
	}
	if root, found := byPath["."]; !found || root.Kind != "directory" {
		return fmt.Errorf("%w: administrative root directory is required", ErrManifestInvalid)
	}
	for _, entry := range entries {
		if entry.Path == "." {
			continue
		}
		if parent, found := byPath[path.Dir(entry.Path)]; !found || parent.Kind != "directory" {
			return fmt.Errorf("%w: administrative parent directory is missing", ErrManifestInvalid)
		}
	}
	for _, name := range []string{"HEAD", "commondir", "gitdir"} {
		if entry, found := byPath[name]; !found || entry.Kind != "file" || len(entry.Data) == 0 {
			return fmt.Errorf("%w: required administrative file %s is missing or empty", ErrManifestInvalid, name)
		}
	}
	if entry, found := byPath["index"]; found && entry.Kind != "file" {
		return fmt.Errorf("%w: administrative index is not a file", ErrManifestInvalid)
	}
	return nil
}

func validAdministrativePath(value string) bool {
	if len(value) > 4096 || !fs.ValidPath(value) {
		return false
	}
	if value == "." {
		return true
	}
	if strings.ContainsAny(value, `\<>:"|?*`) {
		return false
	}
	for _, character := range value {
		if character < 32 || character == 127 {
			return false
		}
	}
	for _, component := range strings.Split(value, "/") {
		if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return false
		}
		base, _, _ := strings.Cut(component, ".")
		base = strings.ToUpper(strings.TrimRight(base, " "))
		switch base {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			return false
		}
		if strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT") {
			suffix := base[3:]
			if len(suffix) == 1 && suffix[0] >= '1' && suffix[0] <= '9' || suffix == "¹" || suffix == "²" || suffix == "³" {
				return false
			}
		}
	}
	return true
}

func foldAdministrativePath(value string) string {
	return strings.Map(func(character rune) rune {
		minimum := character
		for folded := unicode.SimpleFold(character); folded != character; folded = unicode.SimpleFold(folded) {
			if folded < minimum {
				minimum = folded
			}
		}
		return minimum
	}, value)
}
