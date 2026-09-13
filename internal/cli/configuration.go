package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hellices/treeclear/internal/pathutil"
)

func resolveDependencies(dependencies Dependencies) (Dependencies, error) {
	var err error
	if dependencies.WorkingDirectory == "" {
		dependencies.WorkingDirectory, err = os.Getwd()
		if err != nil {
			return dependencies, err
		}
	}
	dependencies.WorkingDirectory, err = pathutil.Canonical(dependencies.WorkingDirectory)
	if err != nil {
		return dependencies, err
	}
	if dependencies.UserConfigPath == "" || dependencies.DataDirectory == "" {
		directory, err := os.UserConfigDir()
		if err != nil {
			return dependencies, err
		}
		directory += string(filepath.Separator) + "treeclear"
		if dependencies.UserConfigPath == "" {
			dependencies.UserConfigPath = directory + string(filepath.Separator) + "config.toml"
		}
		if dependencies.DataDirectory == "" {
			dependencies.DataDirectory = directory
		}
	}
	if dependencies.RepositoryConfigPath == "" {
		dependencies.RepositoryConfigPath = "treeclear.toml"
	}
	for _, path := range []*string{&dependencies.UserConfigPath, &dependencies.RepositoryConfigPath, &dependencies.DataDirectory} {
		*path, err = resolveInputPath(dependencies.WorkingDirectory, *path)
		if err != nil {
			return dependencies, err
		}
	}
	return dependencies, nil
}

func resolveInputPath(workingDirectory, input string) (string, error) {
	if input == "" || strings.ContainsRune(input, 0) {
		return "", fmt.Errorf("invalid path %q", input)
	}
	if filepath.IsAbs(input) {
		return input, nil
	}
	if filepath.Separator == '\\' {
		volume := filepath.VolumeName(input)
		workingVolume := filepath.VolumeName(workingDirectory)
		if volume != "" {
			if !strings.EqualFold(volume, workingVolume) {
				return "", fmt.Errorf("drive-relative path %q does not match the working directory volume; use an absolute path", input)
			}
			input = input[len(volume):]
		} else if os.IsPathSeparator(input[0]) {
			return workingVolume + input, nil
		}
	}
	return workingDirectory + string(filepath.Separator) + input, nil
}
