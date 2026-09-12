package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type verifier struct {
	root       string
	stdout     io.Writer
	stderr     io.Writer
	runCommand commandRunner
}

func Execute(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 || (len(arguments) == 1 && (arguments[0] == "help" || arguments[0] == "--help" || arguments[0] == "-h")) {
		fmt.Fprintln(stdout, "Development harness (not the Treeclear product CLI)")
		fmt.Fprintln(stdout, "Usage: go run ./tools/harness <doctor|fmt|docs|vet|test|race|build|cross|verify|status|gate [000..004]>")
		return 0
	}
	command := arguments[0]
	valid := false
	for _, name := range []string{"doctor", "fmt", "docs", "vet", "test", "race", "build", "cross", "verify", "status", "gate"} {
		valid = valid || command == name
	}
	if !valid || len(arguments) > 2 || (len(arguments) == 2 && command != "gate") {
		fmt.Fprintln(stderr, "invalid harness command or arguments; run with --help")
		return 2
	}
	stage := ""
	if len(arguments) == 2 {
		stage = arguments[1]
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	root, err := findRoot(cwd)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	verifier := verifier{root: root, stdout: stdout, stderr: stderr, runCommand: runCommand}
	if err := verifier.execute(ctx, command, stage); err != nil {
		fmt.Fprintln(stderr, "FAIL:", err)
		return 1
	}
	return 0
}

func findRoot(start string) (string, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		contents, err := os.ReadFile(filepath.Join(current, "go.mod"))
		if err == nil {
			for _, line := range strings.Split(string(contents), "\n") {
				if strings.TrimSpace(line) == "module "+modulePath {
					return current, nil
				}
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("run the harness inside the Treeclear repository")
		}
		current = parent
	}
}

func (verifier verifier) execute(ctx context.Context, command, stage string) error {
	fmt.Fprintln(verifier.stdout, "==>", command)
	switch command {
	case "verify":
		for _, step := range []string{"doctor", "fmt", "docs", "vet", "test", "race", "build", "cross"} {
			if err := verifier.execute(ctx, step, ""); err != nil {
				return err
			}
		}
		fmt.Fprintln(verifier.stdout, "PASS: active-stage verification; future stages remain unverified")
		return nil
	case "doctor":
		return verifier.doctor(ctx)
	case "fmt":
		return verifier.format(ctx)
	case "docs":
		return CheckDocs(verifier.root)
	case "vet":
		return verifier.runCommand(ctx, verifier.root, commandSpec{name: "go", args: []string{"vet", "./..."}}, verifier.stdout, verifier.stderr)
	case "test", "gate":
		return verifier.test(ctx, stage)
	case "race":
		return verifier.runCommand(ctx, verifier.root, commandSpec{name: "go", args: []string{"test", "-race", "-count=1", "-timeout=5m", "./..."}, env: []string{"CGO_ENABLED=1"}}, verifier.stdout, verifier.stderr)
	case "build":
		return verifier.runCommand(ctx, verifier.root, commandSpec{name: "go", args: []string{"build", "-trimpath", "./..."}}, verifier.stdout, verifier.stderr)
	case "cross":
		for _, operatingSystem := range []string{"darwin", "windows"} {
			for _, architecture := range []string{"amd64", "arm64"} {
				fmt.Fprintf(verifier.stdout, "Cross-build only: %s/%s (not native test evidence)\n", operatingSystem, architecture)
				command := commandSpec{name: "go", args: []string{"build", "-trimpath", "./..."}, env: []string{"GOOS=" + operatingSystem, "GOARCH=" + architecture, "CGO_ENABLED=0"}}
				if err := verifier.runCommand(ctx, verifier.root, command, verifier.stdout, verifier.stderr); err != nil {
					return err
				}
			}
		}
		return nil
	case "status":
		manifest, err := verifier.manifest()
		if err != nil {
			return err
		}
		fmt.Fprintf(verifier.stdout, "activeStage: %s (configured scope, not a test result)\n", manifest.ActiveStage)
		for _, stage := range manifest.Stages {
			state := "planned / unverified"
			if stage.ID <= manifest.ActiveStage {
				state = "required by active gate"
			}
			fmt.Fprintf(verifier.stdout, "%s %s: %s (%d requirements)\n", stage.ID, stage.Name, state, len(stage.Requirements))
		}
		return nil
	}
	return fmt.Errorf("unknown command %q", command)
}

func (verifier verifier) doctor(ctx context.Context) error {
	goVersion, err := verifier.capture(ctx, commandSpec{name: "go", args: []string{"version"}})
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(goVersion), "go version go1.26.5 ") {
		return fmt.Errorf("Go 1.26.5 is required, got %q", strings.TrimSpace(string(goVersion)))
	}
	gitVersion, err := verifier.capture(ctx, commandSpec{name: "git", args: []string{"--version"}})
	if err != nil {
		return err
	}
	fields := strings.Fields(string(gitVersion))
	if len(fields) < 3 || fields[0] != "git" || fields[1] != "version" {
		return fmt.Errorf("unrecognized Git version: %q", gitVersion)
	}
	parts := strings.Split(fields[2], ".")
	if len(parts) < 2 {
		return fmt.Errorf("unrecognized Git version: %q", gitVersion)
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	if majorErr != nil || minorErr != nil || major < 2 || (major == 2 && minor < 36) {
		return fmt.Errorf("Git 2.36 or newer is required, got %q", strings.TrimSpace(string(gitVersion)))
	}
	fmt.Fprint(verifier.stdout, string(goVersion), string(gitVersion))
	return nil
}

func (verifier verifier) format(ctx context.Context) error {
	var paths []string
	if err := walkRepository(verifier.root, func(relative string, entry fs.DirEntry) error {
		if entry.IsDir() || !strings.HasSuffix(relative, ".go") {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("Go source is a symlink: %s", relative)
		}
		paths = append(paths, relative)
		return nil
	}); err != nil {
		return err
	}
	if len(paths) == 0 {
		return errors.New("no Go sources found; formatting is not verified")
	}
	output, err := verifier.capture(ctx, commandSpec{name: "gofmt", args: append([]string{"-l"}, paths...)})
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(output)) != 0 {
		return fmt.Errorf("unformatted Go files (run gofmt -w on these files):\n%s", output)
	}
	return nil
}

func (verifier verifier) manifest() (Manifest, error) {
	file, err := os.Open(filepath.Join(verifier.root, filepath.FromSlash(manifestPath)))
	if err != nil {
		return Manifest{}, err
	}
	defer file.Close()
	return DecodeManifest(file)
}

func (verifier verifier) test(ctx context.Context, stage string) error {
	manifest, err := verifier.manifest()
	if err != nil {
		return err
	}
	if err := manifest.CheckScope(verifier.root); err != nil {
		return err
	}
	if stage == "" {
		stage = manifest.ActiveStage
	}
	requirements, err := manifest.Select(stage)
	if err != nil {
		return err
	}
	for _, requirement := range requirements {
		path := filepath.Join(verifier.root, filepath.FromSlash(strings.TrimPrefix(requirement.Package, "./")))
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("unverified %s: required package %s is unavailable", requirement.ID, requirement.Package)
		}
	}
	output, commandErr := verifier.capture(ctx, commandSpec{name: "go", args: []string{"test", "-json", "-count=1", "-timeout=5m", "./..."}})
	if commandErr != nil {
		fmt.Fprint(verifier.stderr, string(output))
		return commandErr
	}
	if err := CheckEvidence(bytes.NewReader(output), requirements); err != nil {
		fmt.Fprint(verifier.stderr, string(output))
		return err
	}
	fmt.Fprintf(verifier.stdout, "PASS: %d executed acceptance requirements through stage %s\n", len(requirements), stage)
	return nil
}

type boundedBuffer struct {
	contents bytes.Buffer
	limit    int
}

func (buffer *boundedBuffer) Write(contents []byte) (int, error) {
	if buffer.contents.Len()+len(contents) > buffer.limit {
		return 0, fmt.Errorf("command output exceeds %d bytes", buffer.limit)
	}
	return buffer.contents.Write(contents)
}

func (buffer *boundedBuffer) Bytes() []byte {
	return buffer.contents.Bytes()
}

func (verifier verifier) capture(ctx context.Context, command commandSpec) ([]byte, error) {
	output := boundedBuffer{limit: 32 << 20}
	err := verifier.runCommand(ctx, verifier.root, command, &output, verifier.stderr)
	return output.Bytes(), err
}
