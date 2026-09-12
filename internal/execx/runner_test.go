package execx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func helperRequest(test *testing.T, mode string, arguments ...string) Request {
	test.Helper()
	executable, err := os.Executable()
	if err != nil {
		test.Fatal(err)
	}
	return Request{
		Name: executable, Directory: test.TempDir(),
		Args:    append([]string{"-test.run=^TestRunnerHelper$", "--"}, arguments...),
		Env:     append(os.Environ(), "TREECLEAR_EXEC_HELPER="+mode),
		Timeout: 10 * time.Second, MaxBytes: 4096,
	}
}

func TestRunnerPassesLiteralArguments(test *testing.T) {
	arguments := []string{"space name", "line\nbreak", "$(echo unexpected)", "& exit 99", "--flag"}
	result, err := (OSRunner{}).Run(context.Background(), helperRequest(test, "echo", arguments...))
	if err != nil || result.ExitCode != 0 {
		test.Fatalf("Run = %#v, %v", result, err)
	}
	var actual []string
	if err := json.Unmarshal(result.Stdout, &actual); err != nil || !reflect.DeepEqual(actual, arguments) {
		test.Fatalf("arguments = %q, error = %v", actual, err)
	}
}

func TestRunnerReportsNonzeroExit(test *testing.T) {
	result, err := (OSRunner{}).Run(context.Background(), helperRequest(test, "exit"))
	if err == nil || result.ExitCode != 7 || string(result.Stderr) != "failure\n" {
		test.Fatalf("Run = %#v, %v", result, err)
	}
}

func TestRunnerBoundsOutputAndCancels(test *testing.T) {
	for _, mode := range []string{"stdout", "stderr"} {
		test.Run(mode, func(test *testing.T) {
			request := helperRequest(test, mode)
			request.MaxBytes = 64
			started := time.Now()
			result, err := (OSRunner{}).Run(context.Background(), request)
			if !errors.Is(err, ErrOutputLimit) || len(result.Stdout) > 64 || len(result.Stderr) > 64 {
				test.Fatalf("stdout bytes = %d, stderr bytes = %d, error = %v", len(result.Stdout), len(result.Stderr), err)
			}
			if time.Since(started) >= 5*time.Second {
				test.Fatal("output overflow waited for the command timeout")
			}
		})
	}
}

func TestLimitedBufferBoundsIOCopy(test *testing.T) {
	canceled := false
	buffer := limitedBuffer{limit: 64, cancel: func() { canceled = true }}
	reader := struct{ io.Reader }{strings.NewReader(strings.Repeat("x", 256))}
	_, err := io.Copy(&buffer, reader)
	if !errors.Is(err, ErrOutputLimit) || len(buffer.Bytes()) != 64 || !canceled {
		test.Fatalf("copied %d bytes, canceled = %t, error = %v", len(buffer.Bytes()), canceled, err)
	}
}

func TestRunnerHonorsCancellationAndTimeout(test *testing.T) {
	request := helperRequest(test, "wait")
	request.Timeout = 100 * time.Millisecond
	if _, err := (OSRunner{}).Run(context.Background(), request); !errors.Is(err, context.DeadlineExceeded) {
		test.Fatalf("timeout error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (OSRunner{}).Run(ctx, request); !errors.Is(err, context.Canceled) {
		test.Fatalf("cancellation error = %v", err)
	}
}

func TestRunnerRejectsUnboundedRequests(test *testing.T) {
	for _, request := range []Request{{}, {Name: "git", Timeout: time.Second}, {Name: "git", MaxBytes: 10}} {
		if _, err := (OSRunner{}).Run(context.Background(), request); err == nil {
			test.Fatalf("accepted unbounded request: %#v", request)
		}
	}
}

func TestSanitizedEnvironment(test *testing.T) {
	actual := SanitizedEnvironment([]string{"GIT_DIR=/outside", "PATH=/bin", "HOME=/private", "LC_ALL=bad", "TOKEN=secret"}, map[string]string{"LC_ALL": "C"})
	want := []string{"HOME=/private", "LC_ALL=C", "PATH=/bin"}
	if !reflect.DeepEqual(actual, want) {
		test.Fatalf("environment = %q, want %q", actual, want)
	}
}

func TestRunnerHelper(test *testing.T) {
	mode := os.Getenv("TREECLEAR_EXEC_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "echo":
		for index, argument := range os.Args {
			if argument == "--" {
				_ = json.NewEncoder(os.Stdout).Encode(os.Args[index+1:])
				os.Exit(0)
			}
		}
	case "exit":
		fmt.Fprintln(os.Stderr, "failure")
		os.Exit(7)
	case "stdout", "stderr":
		output := os.Stdout
		if mode == "stderr" {
			output = os.Stderr
		}
		for range 256 {
			_, _ = output.WriteString(strings.Repeat("x", 4096))
		}
		time.Sleep(time.Hour)
	case "wait":
		time.Sleep(time.Hour)
	}
	os.Exit(8)
}
