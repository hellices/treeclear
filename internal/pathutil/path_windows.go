package pathutil

import (
	"fmt"
	"strings"
)

func preparePath(path string) (string, error) {
	path = strings.ReplaceAll(path, "/", `\`)
	extended := strings.HasPrefix(path, `\\?\`)
	if extended {
		path = path[4:]
		if len(path) >= 4 && strings.EqualFold(path[:4], `UNC\`) {
			path = `\\` + path[4:]
		} else if len(path) < 3 || path[1] != ':' || path[2] != '\\' {
			return "", fmt.Errorf("unsupported extended path %q", path)
		}
	}
	var volume, remainder string
	switch {
	case strings.HasPrefix(path, `\\`):
		parts := strings.Split(path[2:], `\`)
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" || parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
			return "", fmt.Errorf("invalid UNC path %q", path)
		}
		for _, component := range parts[:2] {
			if err := validateWindowsComponent(component, true); err != nil {
				return "", err
			}
		}
		volume = `\\` + strings.ToLower(parts[0]) + `\` + strings.ToLower(parts[1])
		remainder = path[2+len(parts[0])+1+len(parts[1]):]
	case len(path) >= 2 && path[1] == ':':
		drive := path[0]
		if !((drive >= 'a' && drive <= 'z') || (drive >= 'A' && drive <= 'Z')) || len(path) < 3 || path[2] != '\\' {
			return "", fmt.Errorf("invalid or drive-relative path %q", path)
		}
		volume = strings.ToUpper(path[:2])
		remainder = path[2:]
	case strings.HasPrefix(path, `\`):
		return "", fmt.Errorf("unsupported rooted path %q", path)
	default:
		remainder = path
	}
	for _, component := range strings.Split(remainder, `\`) {
		if err := validateWindowsComponent(component, extended); err != nil {
			return "", err
		}
	}
	return volume + remainder, nil
}

func validateWindowsComponent(component string, extended bool) error {
	if component == "" {
		return nil
	}
	if component == "." || component == ".." {
		if !extended {
			return nil
		}
		return fmt.Errorf("ambiguous extended path component %q", component)
	}
	if strings.ContainsAny(component, `<>:"|?*`) || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
		return fmt.Errorf("invalid Windows path component %q", component)
	}
	for _, character := range component {
		if character < 32 {
			return fmt.Errorf("invalid Windows path component %q", component)
		}
	}
	base, _, _ := strings.Cut(component, ".")
	base = strings.ToUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return fmt.Errorf("reserved Windows path component %q", component)
	}
	if strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT") {
		suffix := strings.TrimPrefix(strings.TrimPrefix(base, "COM"), "LPT")
		if len(suffix) == 1 && suffix[0] >= '1' && suffix[0] <= '9' || suffix == "¹" || suffix == "²" || suffix == "³" {
			return fmt.Errorf("reserved Windows path component %q", component)
		}
	}
	return nil
}
