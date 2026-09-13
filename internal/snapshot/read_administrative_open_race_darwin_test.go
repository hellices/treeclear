//go:build darwin

package snapshot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadAdministrativeFocusedDarwinLateDirectoryOpenRace(test *testing.T) {
	exerciseAdministrativeReadDarwinLateOpenRace(test, openAdministrativeReadDirectory)
}

func TestReadAdministrativeFocusedDarwinLateRootOpenRace(test *testing.T) {
	exerciseAdministrativeReadDarwinLateOpenRace(test, func(parent *os.Root, name string) (*os.Root, error) {
		return openAdministrativeReadRoot(filepath.Join(parent.Name(), name))
	})
}

func exerciseAdministrativeReadDarwinLateOpenRace(test *testing.T, openDirectory func(*os.Root, string) (*os.Root, error)) {
	test.Helper()
	directory := test.TempDir()
	target := filepath.Join(directory, "racing")
	pipe := filepath.Join(directory, "pipe")
	if err := os.Mkdir(target, 0o700); err != nil {
		test.Fatal(err)
	}
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		test.Fatal(err)
	}
	parent, err := os.OpenRoot(directory)
	if err != nil {
		test.Fatal(err)
	}
	defer parent.Close()
	baseline, err := openDirectory(parent, "racing")
	if err != nil {
		test.Fatal(err)
	}
	if err := baseline.Close(); err != nil {
		test.Fatal(err)
	}
	ctx, cancel := context.WithCancel(test.Context())
	defer cancel()
	mutationDone := make(chan error, 1)
	var replacements atomic.Uint64
	var openedDirectories atomic.Uint64
	go func() {
		for {
			if ctx.Err() != nil {
				mutationDone <- nil
				return
			}
			if err := unix.RenameatxNp(unix.AT_FDCWD, target, unix.AT_FDCWD, pipe, unix.RENAME_SWAP); err != nil {
				mutationDone <- fmt.Errorf("fixture replacement: %w", err)
				return
			}
			replacements.Add(1)
		}
	}()
	progress := make(chan struct{}, 1)
	openingDone := make(chan error, 1)
	go func() {
		for attempt := 0; attempt < 5000; attempt++ {
			if ctx.Err() != nil {
				break
			}
			root, _ := openDirectory(parent, "racing")
			if root != nil {
				openedDirectories.Add(1)
				if err := root.Close(); err != nil {
					openingDone <- err
					return
				}
			}
			select {
			case progress <- struct{}{}:
			default:
			}
			runtime.Gosched()
		}
		openingDone <- nil
	}()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	timedOut := false
	var openingErr error
	finished := false
	for !finished {
		select {
		case openingErr = <-openingDone:
			finished = true
		case <-progress:
			timer.Reset(2 * time.Second)
		case <-timer.C:
			timedOut, finished = true, true
		}
	}
	cancel()
	mutationErr := <-mutationDone
	if timedOut {
		information, err := os.Lstat(pipe)
		if err != nil {
			test.Fatal(err)
		}
		if information.IsDir() {
			pipe = target
		}
		keeper, err := os.OpenFile(pipe, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err != nil {
			test.Fatalf("release fixture FIFO reader: %v; mutation=%v", err, mutationErr)
		}
		defer keeper.Close()
		select {
		case openingErr = <-openingDone:
		case <-time.After(2 * time.Second):
			test.Fatal("fixture reader did not finish after opening its owned FIFO")
		}
	}
	if mutationErr != nil || openingErr != nil {
		test.Fatalf("fixture operation failed: mutation=%v, opening=%v", mutationErr, openingErr)
	}
	test.Logf("replacement attempts=%d, opened directories=%d, GOMAXPROCS=%d", replacements.Load(), openedDirectories.Load(), runtime.GOMAXPROCS(0))
	if timedOut {
		test.Fatal("administrative directory open blocked while a FIFO replaced the directory")
	}
	if replacements.Load() == 0 || openedDirectories.Load() == 0 {
		test.Fatal("race coverage requires successful replacements and directory opens")
	}
}
