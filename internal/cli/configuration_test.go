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
	for path, name := range map[string]string{
		resolved.UserConfigPath: "user.toml", resolved.RepositoryConfigPath: "repository.toml", resolved.DataDirectory: "state",
	} {
		if path != filepath.Join(root, name) {
			test.Fatalf("configuration path = %q; want %q", path, filepath.Join(root, name))
		}
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
