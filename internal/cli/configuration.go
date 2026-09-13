package cli

import (
	"os"
	"path/filepath"
)

func resolveDependencies(dependencies Dependencies) (Dependencies, error) {
	var err error
	if dependencies.WorkingDirectory == "" {
		dependencies.WorkingDirectory, err = os.Getwd()
		if err != nil {
			return dependencies, err
		}
	}
	dependencies.WorkingDirectory, err = filepath.Abs(dependencies.WorkingDirectory)
	if err != nil {
		return dependencies, err
	}
	if dependencies.UserConfigPath == "" || dependencies.DataDirectory == "" {
		directory, err := os.UserConfigDir()
		if err != nil {
			return dependencies, err
		}
		if dependencies.UserConfigPath == "" {
			dependencies.UserConfigPath = filepath.Join(directory, "treeclear", "config.toml")
		}
		if dependencies.DataDirectory == "" {
			dependencies.DataDirectory = filepath.Join(directory, "treeclear")
		}
	}
	if !filepath.IsAbs(dependencies.DataDirectory) {
		dependencies.DataDirectory = dependencies.WorkingDirectory + string(filepath.Separator) + dependencies.DataDirectory
	}
	if dependencies.RepositoryConfigPath == "" {
		dependencies.RepositoryConfigPath = filepath.Join(dependencies.WorkingDirectory, "treeclear.toml")
	}
	return dependencies, nil
}
