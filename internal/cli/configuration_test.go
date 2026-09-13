package cli

import (
	"path/filepath"
	"testing"

	"github.com/hellices/treeclear/internal/pathutil"
)

func TestResolveDependenciesAnchorsRelativeConfigurationPaths(test *testing.T) {
	dependencies, _ := planFixture(test)
	dependencies.UserConfigPath = "user.toml"
	dependencies.RepositoryConfigPath = "repository.toml"
	dependencies.DataDirectory = "state"
	resolved, err := resolveDependencies(dependencies)
	if err != nil {
		test.Fatal(err)
	}
	root, err := pathutil.Canonical(dependencies.WorkingDirectory)
	if err != nil {
		test.Fatal(err)
	}
	for _, setting := range []struct {
		name, path, filename string
	}{
		{"user config", resolved.UserConfigPath, "user.toml"},
		{"repository config", resolved.RepositoryConfigPath, "repository.toml"},
		{"data directory", resolved.DataDirectory, "state"},
	} {
		test.Run(setting.name, func(test *testing.T) {
			if setting.path != filepath.Join(root, setting.filename) {
				test.Fatalf("configuration path = %q; want %q", setting.path, filepath.Join(root, setting.filename))
			}
		})
	}
}

func TestResolveInputPathPreservesRawSuffixes(test *testing.T) {
	root := test.TempDir()
	for _, input := range []string{"report.json", "./plan_name", "alias/../report.json", "../config.toml"} {
		input = filepath.FromSlash(input)
		resolved, err := resolveInputPath(root, input)
		if err != nil || resolved != root+string(filepath.Separator)+input {
			test.Errorf("relative %q = %q, %v", input, resolved, err)
		}
		absolute := root + string(filepath.Separator) + input
		resolved, err = resolveInputPath(root, absolute)
		if err != nil || resolved != absolute {
			test.Errorf("absolute %q = %q, %v", absolute, resolved, err)
		}
	}
	for _, input := range []string{"", "bad\x00path"} {
		if _, err := resolveInputPath(root, input); err == nil {
			test.Errorf("accepted invalid path %q", input)
		}
	}
}
