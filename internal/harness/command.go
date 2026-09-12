package harness

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

type commandSpec struct {
	name string
	args []string
	env  []string
}

type commandRunner func(context.Context, string, commandSpec, io.Writer, io.Writer) error

func runCommand(ctx context.Context, root string, command commandSpec, stdout, stderr io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	process := exec.CommandContext(ctx, command.name, command.args...)
	process.Dir = root
	process.Env = commandEnvironment(os.Environ(), command.env)
	process.Stdout = stdout
	process.Stderr = stderr
	process.WaitDelay = 2 * time.Second
	if err := process.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s: %w", command.name, ctx.Err())
		}
		return fmt.Errorf("%s %s: %w", command.name, strings.Join(command.args, " "), err)
	}
	return nil
}

func commandEnvironment(inherited, overrides []string) []string {
	controlled := map[string]bool{"GOENV": true, "GOTOOLCHAIN": true, "GOWORK": true, "GOFLAGS": true, "GOOS": true, "GOARCH": true, "CGO_ENABLED": true, "GOEXPERIMENT": true}
	for _, entry := range overrides {
		name, _, _ := strings.Cut(entry, "=")
		controlled[strings.ToUpper(name)] = true
	}
	var environment []string
	for _, entry := range inherited {
		name, _, _ := strings.Cut(entry, "=")
		if !controlled[strings.ToUpper(name)] {
			environment = append(environment, entry)
		}
	}
	environment = append(environment, "GOENV=off", "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=-mod=readonly")
	return append(environment, overrides...)
}
