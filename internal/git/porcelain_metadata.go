package git

func validPorcelainObjectID(objectID string) bool {
	if len(objectID) != 40 && len(objectID) != 64 {
		return false
	}
	for _, digit := range objectID {
		if !(digit >= '0' && digit <= '9' || digit >= 'a' && digit <= 'f' || digit >= 'A' && digit <= 'F') {
			return false
		}
	}
	return true
}

func validPorcelainMode(mode string, isWorktree bool) bool {
	switch mode {
	case "000000", "100644", "100755", "120000", "160000":
		return true
	case "040000":
		return isWorktree
	default:
		return false
	}
}
