//go:build darwin || linux

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hellices/treeclear/internal/config"
	"github.com/hellices/treeclear/internal/pathutil"
)

func TestPlanResolvesRelativeRootSymlinkBeforeParentTraversal(test *testing.T) {
	dependencies, _ := planFixture(test)
	outer := filepath.Join(dependencies.WorkingDirectory, "outer")
	target := filepath.Join(outer, "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		test.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dependencies.WorkingDirectory, "alias")); err != nil {
		test.Fatal(err)
	}
	configuration, runtime, err := scanConfiguration(dependencies, config.Overrides{Roots: []string{"alias/.."}})
	if err != nil {
		test.Fatal(err)
	}
	_, request, err := configuredPlanBuilder(context.Background(), runtime, configuration)
	want, canonicalErr := pathutil.Canonical(outer)
	if err != nil || canonicalErr != nil || len(request.Roots) != 1 || request.Roots[0] != want {
		test.Fatalf("plan roots = %q, want %q, errors = %v, %v", request.Roots, want, err, canonicalErr)
	}
}
