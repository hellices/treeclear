package git

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestClientStatusDiagnosticsNativeDeniedDirectory(test *testing.T) {
	if runtime.GOOS == "windows" {
		test.Skip("Windows chmod cannot establish directory access denial; a verified native ACL fixture is required")
	}
	for _, scenario := range []struct {
		name   string
		stdout []byte
	}{
		{name: "empty"},
		{name: "partial", stdout: []byte("? visible.txt\x00")},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			repository := testutil.NewRepository(test)
			denied := filepath.Join(repository.Root, "denied")
			if err := os.Mkdir(denied, 0o700); err != nil {
				test.Fatal(err)
			}
			leaf := filepath.Join(denied, "hidden.bin")
			contents := []byte{0, 1, 0xff, '\n'}
			writeRawFixtureFile(test, leaf, contents)
			if len(scenario.stdout) != 0 {
				writeRawFixtureFile(test, filepath.Join(repository.Root, "visible.txt"), []byte("visible native fixture\n"))
			}
			information, err := os.Stat(denied)
			if err != nil {
				test.Fatal(err)
			}
			test.Cleanup(func() {
				if err := os.Chmod(denied, information.Mode().Perm()); err != nil {
					test.Errorf("restore temporary directory access: %v", err)
					return
				}
				if remaining, err := os.ReadFile(leaf); err != nil || !bytes.Equal(remaining, contents) {
					test.Errorf("denied source bytes changed: %v", err)
				}
			})
			var captured []execx.Result
			client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
				assertRawReadRequest(test, request, repository.Root)
				result, err := (execx.OSRunner{}).Run(ctx, request)
				if request.Args[0] == "status" {
					captured = append(captured, execx.Result{
						Stdout: bytes.Clone(result.Stdout), Stderr: bytes.Clone(result.Stderr), ExitCode: result.ExitCode,
					})
				}
				return result, err
			}))
			arguments := []string{"status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=none"}
			readable, err := client.run(test.Context(), repository.Root, arguments...)
			wantedReadable := append([]byte("? denied/hidden.bin\x00"), scenario.stdout...)
			if err != nil || readable.ExitCode != 0 || len(readable.Stderr) != 0 || !bytes.Equal(readable.Stdout, wantedReadable) {
				test.Fatalf("readable control did not expose the nonignored untracked leaf: result %#v, error %v", readable, err)
			}
			test.Logf("readable native control: exit=%d stdout=%q stderr=%q", readable.ExitCode, readable.Stdout, readable.Stderr)
			if err := os.Chmod(denied, 0); err != nil {
				test.Skipf("native directory denial prerequisite unavailable: %v", err)
			}
			_, directoryError := os.ReadDir(denied)
			_, leafError := os.ReadFile(leaf)
			if !errors.Is(directoryError, fs.ErrPermission) || !errors.Is(leafError, fs.ErrPermission) {
				test.Skipf("native access denial not established (privileges or ACLs may bypass mode bits): directory=%v, leaf=%v", directoryError, leafError)
			}
			test.Logf("native access denial established: directory=%v, leaf=%v", directoryError, leafError)
			control, err := client.run(test.Context(), repository.Root, arguments...)
			if err != nil || control.ExitCode != 0 || len(control.Stderr) == 0 || !bytes.Equal(control.Stdout, scenario.stdout) {
				test.Skipf("native Git did not reproduce exit-zero diagnostics with an omitted leaf: exit=%d stdout=%q stderr=%q error=%v", control.ExitCode, control.Stdout, control.Stderr, err)
			}
			test.Logf("denied native control: exit=%d stdout=%q stderr=%q", control.ExitCode, control.Stdout, control.Stderr)
			captured = nil
			snapshot, snapshotError := client.StatusSnapshot(test.Context(), repository.Root)
			if snapshotError == nil || snapshot.Status != (domain.GitStatus{}) || snapshot.Raw != nil || snapshot.UntrackedPaths != nil {
				test.Errorf("StatusSnapshot accepted native incomplete enumeration: result=%#v error=%v; want an error and the full zero result", snapshot, snapshotError)
			}
			status, statusError := client.Status(test.Context(), repository.Root)
			if statusError == nil || status != (domain.GitStatus{}) {
				test.Errorf("Status accepted native incomplete enumeration: status=%#v error=%v; want an error and zero status", status, statusError)
			}
			status, raw, rawError := client.StatusRaw(test.Context(), repository.Root)
			if rawError == nil || status != (domain.GitStatus{}) || raw != nil {
				test.Errorf("StatusRaw accepted native incomplete enumeration: status=%#v raw=%q error=%v; want an error, zero status and nil raw", status, raw, rawError)
			}
			if len(captured) != 3 {
				test.Fatalf("native API status observations = %d, want one per API", len(captured))
			}
			for position, result := range captured {
				if result.ExitCode != 0 || len(result.Stderr) == 0 || !bytes.Equal(result.Stdout, scenario.stdout) {
					test.Fatalf("native API status observation %d did not reproduce the control: %#v", position, result)
				}
				test.Logf("native API status observation %d: exit=%d stdout=%q stderr=%q", position, result.ExitCode, result.Stdout, result.Stderr)
			}
		})
	}
}
