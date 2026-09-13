package git

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/hellices/treeclear/internal/execx"
)

type commandExitStatus int

func (status commandExitStatus) Error() string {
	return fmt.Sprintf("exit status %d", status)
}

func isQuietCommandExit(result execx.Result, err error, exitCode int) bool {
	if result.ExitCode != exitCode || len(result.Stdout) != 0 || len(result.Stderr) != 0 {
		return false
	}
	for current := err; current != nil; current = errors.Unwrap(current) {
		switch status := current.(type) {
		case commandExitStatus:
			return int(status) == exitCode
		case *exec.ExitError:
			return status != nil && status.ProcessState != nil && status.ExitCode() == exitCode
		}
	}
	return false
}
