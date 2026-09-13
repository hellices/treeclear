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
			if len(fields) != fieldCount || fields[fieldCount-1] == "" || !validStatusCode(fields[1]) || !validSubmoduleCode(fields[2]) {
				return domain.GitStatus{}, fmt.Errorf("malformed Git status record %q", record)
			}
			if record[0] == '2' {
				score := fields[8]
				if len(score) < 2 || !strings.ContainsRune("RC", rune(score[0])) {
					return domain.GitStatus{}, errors.New("invalid Git rename score")
				}
				percentage, err := strconv.Atoi(score[1:])
				if err != nil || percentage < 0 || percentage > 100 || index+1 >= len(records) || records[index+1] == "" {
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

func validStatusCode(code string) bool {
	return len(code) == 2 && strings.ContainsRune(".MADRCUT", rune(code[0])) && strings.ContainsRune(".MADRCUT", rune(code[1]))
}

func validSubmoduleCode(code string) bool {
	return code == "N..." || (len(code) == 4 && code[0] == 'S' && strings.ContainsRune(".C", rune(code[1])) && strings.ContainsRune(".M", rune(code[2])) && strings.ContainsRune(".U", rune(code[3])))
}
