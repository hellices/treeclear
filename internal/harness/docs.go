package harness

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var markdownLink = regexp.MustCompile(`!?\[[^\]\n]+\]\(([^)\n]+)\)`)

func CheckDocs(root string) error {
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	var failures []error
	err = walkRepository(root, func(relative string, entry fs.DirEntry) error {
		if entry.IsDir() || !strings.HasSuffix(relative, ".md") {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("documentation source is a symlink: %s", relative)
		}
		filename := filepath.Join(root, filepath.FromSlash(relative))
		contents, err := os.Open(filename)
		if err != nil {
			return err
		}
		defer contents.Close()
		scanner := bufio.NewScanner(contents)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		fence := ""
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
				marker := line[:3]
				if fence == "" {
					fence = marker
				} else if fence == marker {
					fence = ""
				}
				continue
			}
			if fence != "" {
				continue
			}
			for _, match := range markdownLink.FindAllStringSubmatch(line, -1) {
				if err := checkLink(canonicalRoot, filepath.Dir(filename), match[1]); err != nil {
					failures = append(failures, fmt.Errorf("%s:%d: %w", relative, lineNumber, err))
				}
			}
		}
		return scanner.Err()
	})
	return errors.Join(append(failures, err)...)
}

func checkLink(root, directory, target string) error {
	target = strings.TrimSpace(target)
	if strings.HasPrefix(target, "<") {
		closing := strings.Index(target, ">")
		if closing < 0 {
			return fmt.Errorf("invalid Markdown target %q", target)
		}
		target = target[1:closing]
	} else if before, _, found := strings.Cut(target, ` "`); found {
		target = before
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("invalid link %q: %w", target, err)
	}
	if parsed.Scheme == "https" || parsed.Scheme == "http" || parsed.Scheme == "mailto" {
		return nil
	}
	if parsed.Scheme != "" || parsed.Host != "" || filepath.IsAbs(parsed.Path) {
		return fmt.Errorf("local link is not repository-relative: %q", target)
	}
	if parsed.Path == "" {
		return nil
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(directory, filepath.FromSlash(parsed.Path)))
	if err != nil {
		return fmt.Errorf("unresolved local link %q: %w", target, err)
	}
	if !containsPath(root, resolved) {
		return fmt.Errorf("local link escapes repository: %q", target)
	}
	return nil
}

func containsPath(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
