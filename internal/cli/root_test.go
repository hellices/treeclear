package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestExecuteWithoutArgumentsIsReadOnly(test *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), nil, &stdout, &stderr, "test")
	if code != 0 || stderr.Len() != 0 {
		test.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Safely clear stale agent worktrees") {
		test.Fatalf("missing help: %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "removed") {
		test.Fatalf("default invocation reported a mutation: %q", stdout.String())
	}
}

func TestExecuteVersion(test *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"version"}, &stdout, &stderr, "v0.0.0-test")
	if code != 0 || stdout.String() != "v0.0.0-test\n" || stderr.Len() != 0 {
		test.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestExecuteHelp(test *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"--help"}, &stdout, &stderr, "test")
	if code != 0 || !strings.Contains(stdout.String(), "version") || stderr.Len() != 0 {
		test.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestExecuteRejectsInvalidArguments(test *testing.T) {
	for _, arguments := range [][]string{{"missing"}, {"version", "extra"}, {"--unknown-flag"}} {
		test.Run(strings.Join(arguments, "_"), func(test *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Execute(context.Background(), arguments, &stdout, &stderr, "test")
			if code != 1 || stdout.Len() != 0 || stderr.Len() == 0 {
				test.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestExecuteReportsOutputFailure(test *testing.T) {
	var stderr bytes.Buffer
	code := Execute(context.Background(), []string{"version"}, failingWriter{}, &stderr, "test")
	if code != 1 || !strings.Contains(stderr.String(), io.ErrClosedPipe.Error()) {
		test.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
}

func TestExecuteEscapesFinalErrorDiagnostics(test *testing.T) {
	var stderr bytes.Buffer
	failure := &os.PathError{Op: "write", Path: "synthetic-\x1b[31m\rpath", Err: errors.New("\nforged\tmessage\u202e")}
	code := Execute(context.Background(), []string{"version"}, diagnosticErrorWriter{failure}, &stderr, "test")
	if code != 1 || stderr.Len() == 0 {
		test.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if strings.ContainsAny(stderr.String(), "\x1b\r\t\u202e") || strings.Count(stderr.String(), "\n") != 1 {
		test.Fatalf("final diagnostic contains unescaped controls: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), `\x1b`) || !strings.Contains(stderr.String(), `\r`) {
		test.Fatalf("diagnostic lost the escaped error detail: %q", stderr.String())
	}
}

type diagnosticErrorWriter struct{ failure error }

func (writer diagnosticErrorWriter) Write([]byte) (int, error) {
	return 0, writer.failure
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}
