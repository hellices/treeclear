package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHarnessHelpAndArgumentErrors(test *testing.T) {
	for _, arguments := range [][]string{nil, {"help"}, {"--help"}} {
		var stdout, stderr bytes.Buffer
		if code := Execute(context.Background(), arguments, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "Development harness") {
			test.Fatalf("arguments %v: code/output = %d/%q/%q", arguments, code, stdout.String(), stderr.String())
		}
	}
	for _, arguments := range [][]string{{"unknown"}, {"verify", "001"}, {"gate", "000", "extra"}} {
		var stdout, stderr bytes.Buffer
		if code := Execute(context.Background(), arguments, &stdout, &stderr); code != 2 {
			test.Fatalf("arguments %v: code = %d, stderr = %q", arguments, code, stderr.String())
		}
	}
}

func TestHarnessFindsRootFromDescendant(test *testing.T) {
	root := test.TempDir()
	writeFixtureFile(test, root, "go.mod", "module "+modulePath+"\n\ngo 1.26.0\n")
	child := filepath.Dir(writeFixtureFile(test, root, "nested/deep/file", ""))
	actual, err := findRoot(child)
	if err != nil || actual != root {
		test.Fatalf("root = %s, err = %v", actual, err)
	}
	if _, err := findRoot(test.TempDir()); err == nil {
		test.Fatal("nonrepository directory accepted")
	}
}

func TestHarnessVerifyStopsOnFailure(test *testing.T) {
	root := test.TempDir()
	writeFixtureFile(test, root, "internal/harness/fixture.go", "package harness\n")
	var calls []string
	verifier := verifier{root: root, stdout: io.Discard, stderr: io.Discard}
	verifier.runCommand = func(_ context.Context, _ string, command commandSpec, stdout, _ io.Writer) error {
		calls = append(calls, command.name+" "+strings.Join(command.args, " "))
		switch command.name {
		case "go":
			fmt.Fprintln(stdout, "go version go1.26.5 darwin/arm64")
		case "git":
			fmt.Fprintln(stdout, "git version 2.50.1")
		case "gofmt":
			fmt.Fprintln(stdout, "internal/harness/fixture.go")
		}
		return nil
	}
	if err := verifier.execute(context.Background(), "verify", ""); err == nil {
		test.Fatal("format failure did not stop verification")
	}
	if len(calls) != 3 || strings.HasPrefix(calls[len(calls)-1], "go test") {
		test.Fatalf("commands continued after failure: %v", calls)
	}
}

func TestHarnessToolchainAndCommandFailures(test *testing.T) {
	for _, version := range []string{"go version go1.26.4 darwin/arm64", "go version unknown"} {
		verifier := verifier{root: test.TempDir(), stdout: io.Discard, stderr: io.Discard}
		verifier.runCommand = func(_ context.Context, _ string, _ commandSpec, stdout, _ io.Writer) error {
			fmt.Fprintln(stdout, version)
			return nil
		}
		if err := verifier.execute(context.Background(), "doctor", ""); err == nil {
			test.Fatalf("unapproved toolchain accepted: %s", version)
		}
	}
	sentinel := errors.New("command failed")
	verifier := verifier{root: test.TempDir(), stdout: io.Discard, stderr: io.Discard}
	verifier.runCommand = func(context.Context, string, commandSpec, io.Writer, io.Writer) error { return sentinel }
	if err := verifier.execute(context.Background(), "build", ""); !errors.Is(err, sentinel) {
		test.Fatalf("command failure lost: %v", err)
	}
}

func TestCommandEnvironmentIsDeterministic(test *testing.T) {
	actual := commandEnvironment([]string{"PATH=kept", "GOENV=/hostile/goenv", "GOOS=wrong", "GOARCH=wrong", "GOFLAGS=-toolexec=hostile", "GOTOOLCHAIN=auto", "GOWORK=/hostile/work", "goos=duplicate"}, []string{"GOOS=windows", "GOARCH=arm64", "CGO_ENABLED=0"})
	expected := []string{"PATH=kept", "GOENV=off", "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=-mod=readonly", "GOOS=windows", "GOARCH=arm64", "CGO_ENABLED=0"}
	if !reflect.DeepEqual(actual, expected) {
		test.Fatalf("environment = %v", actual)
	}
}

func TestCommandCancellationPropagates(test *testing.T) {
	if os.Getenv("TREECLEAR_HARNESS_CANCEL_CHILD") == "1" {
		time.Sleep(30 * time.Second)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runCommand(ctx, test.TempDir(), commandSpec{name: "go", args: []string{"version"}}, io.Discard, io.Discard)
	if !errors.Is(err, context.Canceled) {
		test.Fatalf("cancelled command = %v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		test.Fatal(err)
	}
	ctx, stop := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer stop()
	started := time.Now()
	err = runCommand(ctx, test.TempDir(), commandSpec{name: executable, args: []string{"-test.run=^TestCommandCancellationPropagates$"}, env: []string{"TREECLEAR_HARNESS_CANCEL_CHILD=1"}}, io.Discard, io.Discard)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 5*time.Second {
		test.Fatalf("running child was not bounded: error=%v elapsed=%s", err, time.Since(started))
	}
}

func TestCapturedOutputLimitCannotBeBypassedByCopy(test *testing.T) {
	buffer := boundedBuffer{limit: 8}
	if _, err := io.Copy(&buffer, strings.NewReader("too much captured output")); err == nil {
		test.Fatal("io.Copy bypassed the output limit")
	}
	if _, err := io.Copy(&buffer, strings.NewReader("small")); err != nil {
		test.Fatal(err)
	}
	if string(buffer.Bytes()) != "small" {
		test.Fatalf("captured output = %q", buffer.Bytes())
	}
}

func TestGateRejectsSkippedAndMissingRealTests(test *testing.T) {
	for name, testBody := range map[string]string{
		"pass":    `func TestEvidence(test *testing.T) { if test.Name() != "TestEvidence" { test.Fatalf("executed test name = %s", test.Name()) } }`,
		"skip":    `func TestEvidence(test *testing.T) { test.Skip("not implemented") }`,
		"missing": `func TestDifferent(test *testing.T) { test.Log("different test") }`,
	} {
		test.Run(name, func(test *testing.T) {
			root := test.TempDir()
			writeFixtureFile(test, root, "go.mod", "module "+modulePath+"\n\ngo 1.26.0\n")
			writeFixtureFile(test, root, manifestPath, manifestFixture())
			writeFixtureFile(test, root, "internal/harness/evidence_test.go", "package harness\nimport \"testing\"\n"+testBody+"\n")
			var stdout, stderr bytes.Buffer
			verifier := verifier{root: root, stdout: &stdout, stderr: &stderr, runCommand: runCommand}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			err := verifier.execute(ctx, "gate", "000")
			if (err == nil) != (name == "pass") {
				test.Fatalf("gate = %v, stdout = %s, stderr = %s", err, &stdout, &stderr)
			}
		})
	}
}
