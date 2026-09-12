package inventory

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestLoaderProtectsJunctionWorktree(test *testing.T) {
	repository := testutil.NewRepository(test)
	linked := repository.AddWorktree(test, "feature", "feature/one")
	target := filepath.Join(filepath.Dir(repository.Root), "moved")
	if err := os.Rename(linked, target); err != nil {
		test.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "cmd.exe", "/d", "/c", "mklink", "/J", linked, target)
	command.Dir = filepath.Dir(repository.Root)
	command.Env = execx.SanitizedEnvironment(os.Environ(), nil)
	if output, err := command.CombinedOutput(); err != nil {
		test.Fatalf("create temporary junction: %v: %s", err, output)
	}
	worktrees, failures := newTestLoader(repository.Root).Load(ctx, []string{repository.Root})
	if len(worktrees) != 2 || len(failures) == 0 {
		test.Fatalf("junction worktree was not protected: %#v, %v", worktrees, failures)
	}
	assertUnknownUnsafe(test, findWorktree(test, worktrees, linked))
}
