package snapshot

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"unicode/utf8"
)

func validateUntrackedBudget(maximumBytes int64) error {
	if maximumBytes <= 0 || maximumBytes >= int64(int(^uint(0)>>1)) {
		return fmt.Errorf("%w: byte budget must be positive and safely representable", ErrUntrackedInvalid)
	}
	return nil
}

func validateUntrackedEntries(entries []UntrackedEntry, maximumBytes int64) error {
	if len(entries) > maximumUntrackedEntries {
		return fmt.Errorf("%w: entry count", ErrUntrackedLimit)
	}
	type entryPath struct {
		original string
		identity string
	}
	paths := make([]entryPath, len(entries))
	byIdentity := make(map[string]int, len(entries))
	remaining := maximumBytes
	for index, entry := range entries {
		if err := validateUntrackedEntry(entry); err != nil {
			return err
		}
		if int64(len(entry.Data)) > remaining {
			return fmt.Errorf("%w: aggregate file bytes", ErrUntrackedLimit)
		}
		remaining -= int64(len(entry.Data))
		identity := foldAdministrativePath(entry.Path)
		if _, found := byIdentity[identity]; found {
			return fmt.Errorf("%w: duplicate or case-folded entry path", ErrUntrackedInvalid)
		}
		byIdentity[identity] = index
		paths[index] = entryPath{original: entry.Path, identity: identity}
	}
	slices.SortFunc(paths, func(left, right entryPath) int { return strings.Compare(left.identity, right.identity) })
	for _, entry := range paths {
		prefix := entry.identity + "/"
		descendantIndex, _ := slices.BinarySearchFunc(paths, prefix, func(candidate entryPath, target string) int {
			return strings.Compare(candidate.identity, target)
		})
		if descendantIndex < len(paths) && strings.HasPrefix(paths[descendantIndex].identity, prefix) {
			ancestor := entries[byIdentity[entry.identity]]
			if ancestor.Kind != "directory" || !strings.HasPrefix(paths[descendantIndex].original, ancestor.Path+"/") {
				return fmt.Errorf("%w: conflicting entry ancestor", ErrUntrackedInvalid)
			}
		}
	}
	for index := 1; index < len(paths); index++ {
		if untrackedPathSpellingConflict(paths[index-1].original, paths[index].original) {
			return fmt.Errorf("%w: case-folded parent directory aliases", ErrUntrackedInvalid)
		}
	}
	return nil
}

func validateUntrackedEntry(entry UntrackedEntry) error {
	if len(entry.Path) > maximumUntrackedText || len(entry.LinkTarget) > maximumUntrackedText {
		return fmt.Errorf("%w: entry path or link text bytes", ErrUntrackedLimit)
	}
	if entry.Path == "." || !validAdministrativePath(entry.Path) {
		return fmt.Errorf("%w: invalid entry path", ErrUntrackedInvalid)
	}
	for component := range strings.SplitSeq(entry.Path, "/") {
		if strings.EqualFold(component, ".git") {
			return fmt.Errorf("%w: .git entry component", ErrUntrackedInvalid)
		}
	}
	var modeType fs.FileMode
	switch entry.Kind {
	case "file":
		if entry.LinkTarget != "" {
			return fmt.Errorf("%w: regular file has a link target", ErrUntrackedInvalid)
		}
	case "directory":
		modeType = fs.ModeDir
		if entry.Data != nil || entry.LinkTarget != "" {
			return fmt.Errorf("%w: directory has data or a link target", ErrUntrackedInvalid)
		}
	case "symlink":
		modeType = fs.ModeSymlink
		if entry.Data != nil || !validUntrackedLink(entry.Path, entry.LinkTarget) {
			return fmt.Errorf("%w: symlink has data or an invalid target", ErrUntrackedInvalid)
		}
	default:
		return fmt.Errorf("%w: unsupported entry kind", ErrUntrackedInvalid)
	}
	if entry.Mode&^untrackedPermissionBits != modeType {
		return fmt.Errorf("%w: unsupported or mismatched entry mode", ErrUntrackedInvalid)
	}
	return nil
}

func validUntrackedLink(entryPath, target string) bool {
	if target == "" || !utf8.ValidString(target) || path.IsAbs(target) {
		return false
	}
	for component := range strings.SplitSeq(target, "/") {
		if component == "." || component == ".." {
			continue
		}
		if !validAdministrativePath(component) || strings.EqualFold(component, ".git") {
			return false
		}
	}
	resolved := path.Join(path.Dir(entryPath), target)
	return resolved != ".." && !strings.HasPrefix(resolved, "../")
}

func untrackedPathSpellingConflict(left, right string) bool {
	for {
		leftComponent, leftRest, leftMore := strings.Cut(left, "/")
		rightComponent, rightRest, rightMore := strings.Cut(right, "/")
		if !strings.EqualFold(leftComponent, rightComponent) {
			return false
		}
		if leftComponent != rightComponent {
			return true
		}
		if !leftMore || !rightMore {
			return false
		}
		left, right = leftRest, rightRest
	}
}
