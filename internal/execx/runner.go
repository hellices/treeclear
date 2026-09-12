package execx

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"sort"
	"strings"
	"time"
)

var ErrOutputLimit = errors.New("command output limit exceeded")

type Request struct {
	Directory string
	Name      string
	Args      []string
	Env       []string
	Timeout   time.Duration
	MaxBytes  int
}

type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

type Runner interface {
	Run(context.Context, Request) (Result, error)
}

type OSRunner struct{}

func (OSRunner) Run(ctx context.Context, request Request) (Result, error) {
	result := Result{ExitCode: -1}
	if request.Name == "" || request.MaxBytes <= 0 || request.Timeout <= 0 {
		return result, errors.New("name, timeout, and max bytes are required")
	}
	runContext, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	command := exec.CommandContext(runContext, request.Name, request.Args...)
	command.Dir = request.Directory
	command.Env = request.Env
	command.WaitDelay = time.Second
	stdout := limitedBuffer{limit: request.MaxBytes, cancel: cancel}
	stderr := limitedBuffer{limit: request.MaxBytes, cancel: cancel}
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result.Stdout = stdout.Bytes()
	result.Stderr = stderr.Bytes()
	if command.ProcessState != nil {
		result.ExitCode = command.ProcessState.ExitCode()
	}
	if stdout.exceeded || stderr.exceeded {
		return result, ErrOutputLimit
	}
	if runContext.Err() != nil {
		return result, runContext.Err()
	}
	return result, err
}

type limitedBuffer struct {
	contents bytes.Buffer
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func (buffer *limitedBuffer) Write(contents []byte) (int, error) {
	remaining := buffer.limit - buffer.contents.Len()
	if len(contents) > remaining {
		_, _ = buffer.contents.Write(contents[:remaining])
		buffer.exceeded = true
		buffer.cancel()
		return remaining, ErrOutputLimit
	}
	return buffer.contents.Write(contents)
}

func (buffer *limitedBuffer) Bytes() []byte {
	return buffer.contents.Bytes()
}

func SanitizedEnvironment(environment []string, overrides map[string]string) []string {
	values := make(map[string]string)
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		name = strings.ToUpper(name)
		switch name {
		case "PATH", "HOME", "USERPROFILE", "SYSTEMROOT", "TMPDIR", "TEMP", "TMP":
			values[name] = value
		}
	}
	for name, value := range overrides {
		values[name] = value
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, name+"="+values[name])
	}
	return result
}
