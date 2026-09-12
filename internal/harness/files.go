package harness

import (
	"io/fs"
	"path/filepath"
)

func walkRepository(root string, visit func(string, fs.DirEntry) error) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".superpowers", ".harness", "vendor", "node_modules", "bin", "dist":
				return filepath.SkipDir
			}
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return visit(filepath.ToSlash(relative), entry)
	})
}
