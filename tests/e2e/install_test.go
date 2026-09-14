package e2e

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/execx"
)

func TestMakeInstallUsesGoDestinations(test *testing.T) {
	for _, destination := range []string{"gopath", "gobin"} {
		test.Run(destination, func(test *testing.T) {
			fixture := newInstallFixture(test)
			binaryDirectory := filepath.Join(fixture.environment["GOPATH"], "bin")
			arguments := []string{"install"}
			version := "dev"
			if destination == "gobin" {
				binaryDirectory = filepath.Join(test.TempDir(), "설치 with spaces", "bin")
				fixture.environment["GOBIN"] = binaryDirectory
				version = "v0.0.0-preview-install-test"
				arguments = append(arguments, "VERSION="+version)
			}
			if output, err := fixture.make(test, arguments...); err != nil {
				test.Fatalf("install: %v\n%s", err, output)
			}
			binary := filepath.Join(binaryDirectory, "treeclear")
			assertInstalledPreview(test, binary, version)
			if destination == "gobin" {
				version = "v0.0.0-preview-upgrade-test"
				if output, err := fixture.make(test, "install", "VERSION="+version); err != nil {
					test.Fatalf("upgrade: %v\n%s", err, output)
				}
				assertInstalledPreview(test, binary, version)
			}
		})
	}
}

func TestMakeInstallRejectsCrossTargets(test *testing.T) {
	otherArchitecture := "amd64"
	if runtime.GOARCH == otherArchitecture {
		otherArchitecture = "arm64"
	}
	for _, target := range []struct {
		name  string
		value string
	}{{"GOOS", "windows"}, {"GOOS", "linux"}, {"GOARCH", otherArchitecture}} {
		test.Run(target.name+"="+target.value, func(test *testing.T) {
			fixture := newInstallFixture(test)
			binary := fixture.previousBinary(test)
			fixture.environment[target.name] = target.value
			output, err := fixture.make(test, "install")
			if err == nil || !strings.Contains(string(output), "native macOS") {
				test.Fatalf("cross-target installation must be refused: %v\n%s", err, output)
			}
			assertPreviousInstallation(test, binary)
		})
	}
}

func TestMakeInstallRejectsInvalidDestination(test *testing.T) {
	for _, destination := range []string{"relative", "file"} {
		test.Run(destination, func(test *testing.T) {
			fixture := newInstallFixture(test)
			binary := fixture.previousBinary(test)
			fixture.environment["GOBIN"] = "relative-install-destination"
			diagnostic := "GOBIN must be an absolute path"
			if destination == "file" {
				fixture.environment["GOBIN"] = binary
				diagnostic = "not a directory"
			}
			output, err := fixture.make(test, "install")
			if err == nil || !strings.Contains(string(output), diagnostic) {
				test.Fatalf("invalid Go install destination must fail: %v\n%s", err, output)
			}
			assertPreviousInstallation(test, binary)
		})
	}
}

func TestMakeInstallBuildFailurePreservesBinary(test *testing.T) {
	fixture := newInstallFixture(test)
	binary := fixture.previousBinary(test)
	fixture.environment["GOFLAGS"] += " -gcflags=github.com/hellices/treeclear/cmd/treeclear=-invalid-install-test-flag"
	output, err := fixture.make(test, "install")
	if err == nil || !strings.Contains(string(output), "flag provided but not defined") {
		test.Fatalf("compiler failure must propagate: %v\n%s", err, output)
	}
	assertPreviousInstallation(test, binary)
}

type installFixture struct {
	environment map[string]string
}

func newInstallFixture(test *testing.T) installFixture {
	test.Helper()
	if runtime.GOOS != "darwin" {
		test.Skip("make install is a native macOS preview entry point")
	}
	ctx, cancel := context.WithTimeout(test.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "env", "-json", "GOCACHE", "GOMODCACHE")
	output, err := command.Output()
	if err != nil {
		test.Fatalf("locate existing Go caches: %v", err)
	}
	var caches map[string]string
	if err := json.Unmarshal(output, &caches); err != nil {
		test.Fatal(err)
	}
	home := test.TempDir()
	return installFixture{environment: map[string]string{
		"HOME": home, "USERPROFILE": home, "XDG_CONFIG_HOME": filepath.Join(home, "config"),
		"GOPATH": filepath.Join(test.TempDir(), "go path"), "GOBIN": "",
		"GOCACHE": caches["GOCACHE"], "GOMODCACHE": caches["GOMODCACHE"],
		"GOENV": "off", "GOWORK": "off", "GOTOOLCHAIN": "local", "GOFLAGS": "-mod=readonly",
		"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": filepath.Join(home, "gitconfig"),
	}}
}

func (fixture installFixture) make(test *testing.T, arguments ...string) ([]byte, error) {
	test.Helper()
	ctx, cancel := context.WithTimeout(test.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "make", arguments...)
	command.Dir = filepath.Join("..", "..")
	command.Env = execx.SanitizedEnvironment(os.Environ(), fixture.environment)
	command.WaitDelay = time.Second
	return command.CombinedOutput()
}

func (fixture installFixture) previousBinary(test *testing.T) string {
	test.Helper()
	fixture.environment["GOBIN"] = test.TempDir()
	binary := filepath.Join(fixture.environment["GOBIN"], "treeclear")
	if err := os.WriteFile(binary, []byte("previous installation\n"), 0o700); err != nil {
		test.Fatal(err)
	}
	return binary
}

func assertPreviousInstallation(test *testing.T, binary string) {
	test.Helper()
	content, err := os.ReadFile(binary)
	if err != nil || string(content) != "previous installation\n" {
		test.Fatalf("failed installation changed existing binary: %v, %q", err, content)
	}
}

func assertInstalledPreview(test *testing.T, binary, version string) {
	test.Helper()
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		test.Fatalf("read installed build info: %v", err)
	}
	settings := make(map[string]string)
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if settings["GOOS"] != runtime.GOOS || settings["GOARCH"] != runtime.GOARCH {
		test.Fatalf("installed target is not native: %v", settings)
	}
	home := test.TempDir()
	environment := execx.SanitizedEnvironment(os.Environ(), map[string]string{
		"HOME": home, "USERPROFILE": home, "APPDATA": home, "LOCALAPPDATA": home,
		"XDG_CONFIG_HOME": home, "PATH": filepath.Join(home, "no-tools"),
	})
	for _, arguments := range [][]string{{"version"}, {}, {"--help"}, {"apply"}, {"restore"}, {"trash"}, {"schedule"}} {
		ctx, cancel := context.WithTimeout(test.Context(), 15*time.Second)
		command := exec.CommandContext(ctx, binary, arguments...)
		command.Dir, command.Env = home, environment
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		cancel()
		switch {
		case len(arguments) == 0 || arguments[0] == "--help":
			if err != nil || stderr.Len() != 0 {
				test.Fatalf("installed help: %v, %q", err, stderr.String())
			}
			for _, name := range []string{"scan", "plan", "explain", "version"} {
				if !strings.Contains(stdout.String(), name) {
					test.Fatalf("installed help omits %q: %s", name, stdout.String())
				}
			}
		case arguments[0] == "version":
			if err != nil || stdout.String() != version+"\n" || stderr.Len() != 0 {
				test.Fatalf("installed version: %v, %q, %q", err, stdout.String(), stderr.String())
			}
		default:
			if err == nil || stdout.Len() != 0 || !strings.Contains(stderr.String(), "unknown command") {
				test.Fatalf("unsupported command %v must fail: %v, %q, %q", arguments, err, stdout.String(), stderr.String())
			}
		}
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		test.Fatalf("preview smoke commands created home/configuration state: %v, %v", err, entries)
	}
}
