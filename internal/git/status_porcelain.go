package git

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hellices/treeclear/internal/domain"
)

func parseStatusPorcelainZ(contents []byte) (domain.GitStatus, error) {
	status := domain.GitStatus{}
	if len(contents) == 0 {
		return status, nil
	}
	if contents[len(contents)-1] != 0 {
		return status, errors.New("Git status is missing a NUL terminator")
	}
	records := strings.Split(string(contents[:len(contents)-1]), "\x00")
	for index := 0; index < len(records); index++ {
		record := records[index]
		if len(record) < 3 || record[1] != ' ' {
			return domain.GitStatus{}, fmt.Errorf("invalid Git status record %q", record)
		}
		switch record[0] {
		case '?':
			status.Untracked++
		case '!':
		case '1', '2', 'u':
			fieldCount := 9
			if record[0] == '2' {
				fieldCount = 10
			} else if record[0] == 'u' {
				fieldCount = 11
			}
			fields := strings.SplitN(record, " ", fieldCount)
			if len(fields) != fieldCount || fields[fieldCount-1] == "" || !validStatusCode(record[0], fields[1]) || !validSubmoduleCode(fields[2]) {
				return domain.GitStatus{}, fmt.Errorf("malformed Git status record %q", record)
			}
			if fields[1] == ".." && (fields[2] == "N..." || fields[2] == "S...") {
				return domain.GitStatus{}, errors.New("Git status record has no changes")
			}
			modeEnd, objectIDEnd := 6, 8
			if record[0] == 'u' {
				modeEnd, objectIDEnd = 7, 10
			}
			for fieldIndex := 3; fieldIndex < modeEnd; fieldIndex++ {
				mode := fields[fieldIndex]
				if !validPorcelainMode(mode, fieldIndex == modeEnd-1) {
					return domain.GitStatus{}, fmt.Errorf("invalid Git status mode %q", mode)
				}
			}
			objectIDWidth := len(fields[modeEnd])
			for _, objectID := range fields[modeEnd:objectIDEnd] {
				if !validPorcelainObjectID(objectID) || len(objectID) != objectIDWidth {
					return domain.GitStatus{}, fmt.Errorf("invalid Git status object ID %q", objectID)
				}
			}
			if record[0] == '2' {
				if !validRenameScore(fields[1], fields[8]) {
					return domain.GitStatus{}, errors.New("invalid Git rename score")
				}
				if index+1 >= len(records) || records[index+1] == "" {
					return domain.GitStatus{}, errors.New("incomplete Git rename record")
				}
				index++
			}
			if record[0] == 'u' {
				status.Unmerged++
				continue
			}
			if fields[1][0] != '.' {
				status.Staged++
			}
			if fields[1][1] != '.' || (fields[2][0] == 'S' && fields[2][1:] != "...") {
				status.Unstaged++
			}
		default:
			return domain.GitStatus{}, fmt.Errorf("unknown Git status prefix %q", record[:1])
		}
	}
	return status, nil
}

func validStatusCode(kind byte, code string) bool {
	if len(code) != 2 {
		return false
	}
	if kind == 'u' {
		switch code {
		case "DD", "AU", "UD", "UA", "DU", "AA", "UU":
			return true
		default:
			return false
		}
	}
	allowed := ".MADT"
	if kind == '2' {
		allowed += "RC"
	}
	return strings.ContainsRune(allowed, rune(code[0])) && strings.ContainsRune(allowed, rune(code[1]))
}

func validRenameScore(code, score string) bool {
	if len(code) != 2 || len(score) < 2 || !strings.ContainsRune("RC", rune(score[0])) {
		return false
	}
	if !(code[0] == score[0] && strings.ContainsRune(".MADT", rune(code[1])) || code[1] == score[0] && strings.ContainsRune(".MADT", rune(code[0]))) {
		return false
	}
	percentage, err := strconv.Atoi(score[1:])
	return err == nil && percentage >= 0 && percentage <= 100 && score[1:] == strconv.Itoa(percentage)
}

func validSubmoduleCode(code string) bool {
	return code == "N..." || (len(code) == 4 && code[0] == 'S' && strings.ContainsRune(".C", rune(code[1])) && strings.ContainsRune(".M", rune(code[2])) && strings.ContainsRune(".U", rune(code[3])))
}
