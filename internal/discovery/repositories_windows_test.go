package discovery

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

func TestFinderRejectsJunction(test *testing.T) {
	repository := testutil.NewRepository(test)
	root := canonicalDirectory(test, test.TempDir())
	junction := filepath.Join(root, "junction")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "cmd.exe", "/d", "/c", "mklink", "/J", junction, repository.Root)
	command.Dir = root
	command.Env = execx.SanitizedEnvironment(os.Environ(), nil)
	if output, err := command.CombinedOutput(); err != nil {
		test.Fatalf("create temporary junction: %v: %s", err, output)
	}
	found, failures := Find(ctx, []string{root})
	if len(found) != 0 || len(failures) != 0 {
		test.Fatalf("discovery followed a junction: %#v, %v", found, failures)
	}
	found, failures = Find(ctx, []string{junction})
	if len(found) != 0 || len(failures) != 1 {
		test.Fatalf("junction root was not rejected: %#v, %v", found, failures)
	}
}
