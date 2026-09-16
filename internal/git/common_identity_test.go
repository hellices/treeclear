package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hellices/treeclear/internal/execx"
)

func assertCommonIdentityRejectsReplacedAlias(test *testing.T, makeAlias func(*testing.T, string, string, string)) {
	test.Helper()
	for _, replace := range []bool{false, true} {
		name := "unchanged"
		if replace {
			name = "replaced-by-alias"
		}
		test.Run(name, func(test *testing.T) {
			fixture := readonlyIndexCanonicalTemporaryDirectory(test)
			common := filepath.Join(fixture, "common")
			moved := filepath.Join(fixture, "moved")
			if err := os.Mkdir(common, 0o700); err != nil {
				test.Fatal(err)
			}
			initial := readonlyIndexInitialPinnedInfo(test, common)
			calls := 0
			client := NewClient(runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
				calls++
				if calls != 1 || !reflect.DeepEqual(request.Args, []string{"rev-parse", "--path-format=absolute", "--git-common-dir"}) {
					test.Fatalf("unexpected Git command before common identity validation: %#v", request)
				}
				if replace {
					if err := os.Rename(common, moved); err != nil {
						test.Fatal(err)
					}
					makeAlias(test, fixture, common, moved)
					if !os.SameFile(initial, readonlyIndexInitialPinnedInfo(test, moved)) || !os.SameFile(initial, readonlyIndexInitialPinnedInfo(test, common)) {
						test.Fatal("replacement alias does not retain the original native directory identity")
					}
				}
				return execx.Result{Stdout: []byte(filepath.ToSlash(common) + "\n")}, nil
			}))
			err := client.verifyCommonGitDir(test.Context(), fixture, common)
			if calls != 1 || replace && !errors.Is(err, ErrWorktreeChanged) && !errors.Is(err, errors.ErrUnsupported) || !replace && err != nil {
				test.Errorf("common comparison accepted a replaced canonical path: replace=%t calls=%d error=%v", replace, calls, err)
			}
			retained := common
			if replace {
				retained = moved
			}
			if !os.SameFile(initial, readonlyIndexInitialPinnedInfo(test, retained)) {
				test.Fatal("fixture changed the native common-store identity instead of only its canonical path")
			}
		})
	}
}
