package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDocumentationLinksStayInsideRepository(test *testing.T) {
	root := test.TempDir()
	writeFixtureFile(test, root, "docs/valid file.md", "# Target\n")
	writeFixtureFile(test, root, "README.md", "[Target](docs/valid%20file.md#target)\n[Online](https://example.invalid/not-fetched)\n```md\n[Example](not-created.md)\n```\n")
	if err := CheckDocs(root); err != nil {
		test.Fatal(err)
	}
	writeFixtureFile(test, root, "docs/broken.md", "[Missing](missing.md)\n")
	if err := CheckDocs(root); err == nil {
		test.Fatal("missing local document accepted")
	}
	if err := os.Remove(filepath.Join(root, "docs", "broken.md")); err != nil {
		test.Fatal(err)
	}
	outside := writeFixtureFile(test, test.TempDir(), "outside.md", "private\n")
	writeFixtureFile(test, root, "README.md", "[Escape]("+filepath.ToSlash(outside)+")\n")
	if err := CheckDocs(root); err == nil {
		test.Fatal("absolute outside path accepted")
	}
}

func TestDocumentationRejectsTraversal(test *testing.T) {
	parent := test.TempDir()
	root := filepath.Join(parent, "repository")
	writeFixtureFile(test, parent, "outside.md", "private\n")
	writeFixtureFile(test, root, "README.md", "[Escape](../outside.md)\n")
	if err := CheckDocs(root); err == nil {
		test.Fatal("link escaped repository")
	}
}
