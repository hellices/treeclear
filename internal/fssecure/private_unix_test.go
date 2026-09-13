//go:build darwin || linux

package fssecure

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func makePrivateFixture(test *testing.T, path string, directory bool) {
	test.Helper()
	mode := os.FileMode(0o600)
	if directory {
		mode = 0o700
	}
	if err := os.Chmod(path, mode); err != nil {
		test.Fatal(err)
	}
}

func makeBroadFixture(test *testing.T, path string, directory bool) {
	test.Helper()
	mode := os.FileMode(0o644)
	if directory {
		mode = 0o755
	}
	if err := os.Chmod(path, mode); err != nil {
		test.Fatal(err)
	}
}

func securitySnapshot(test *testing.T, path string) string {
	test.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		test.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		test.Fatal("unavailable Unix ownership information")
	}
	return fmt.Sprintf("%v:%d:%d", info.Mode(), stat.Uid, stat.Gid)
}

func assertPrivateObject(test *testing.T, path string, directory bool) {
	test.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		test.Fatal(err)
	}
	want := os.FileMode(0o600)
	if directory {
		want = os.ModeDir | 0o700
	}
	if info.Mode() != want {
		test.Errorf("mode of %q = %v; want %v", path, info.Mode(), want)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		test.Errorf("%q does not have verified current-user ownership", path)
	}
}

func symlinkCapabilityUnavailable(err error) bool {
	return false
}

func TestReadPrivateFileUnixReadOnlyPermissions(test *testing.T) {
	path := filepath.Join(privateDirectoryFixture(test), "read-only")
	writePrivateFixture(test, path, []byte("read only"))
	if err := os.Chmod(path, 0o400); err != nil {
		test.Fatal(err)
	}
	before := securitySnapshot(test, path)
	contents, err := ReadPrivateFile(path, 32)
	if err != nil || string(contents) != "read only" {
		test.Fatalf("ReadPrivateFile = %q, %v", contents, err)
	}
	if after := securitySnapshot(test, path); after != before {
		test.Fatal("read changed owner-only read permissions")
	}
}

func TestReadPrivateFileUnixRejectsFIFOWithoutBlocking(test *testing.T) {
	path := filepath.Join(privateDirectoryFixture(test), "fifo")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		test.Fatal(err)
	}
	type readResult struct {
		contents []byte
		err      error
	}
	finished := make(chan readResult, 1)
	go func() {
		contents, err := ReadPrivateFile(path, 32)
		finished <- readResult{contents: contents, err: err}
	}()
	select {
	case result := <-finished:
		if result.contents != nil || result.err == nil {
			test.Fatalf("FIFO ReadPrivateFile = %q, %v; want nil and error", result.contents, result.err)
		}
	case <-time.After(time.Second):
		if descriptor, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK, 0); err == nil {
			unix.Close(descriptor)
		}
		test.Fatal("reading a FIFO blocked")
	}
}

func TestReadPrivateFileUnixRejectsDevice(test *testing.T) {
	contents, err := ReadPrivateFile(os.DevNull, 32)
	if err == nil || contents != nil {
		test.Fatalf("device ReadPrivateFile = %q, %v; want nil and error", contents, err)
	}
}

type ownershipFixture struct {
	fs.FileInfo
	system any
}

func (fixture ownershipFixture) Sys() any {
	return fixture.system
}

func TestVerifyUnixOwnerRejectsUnknownAndForeignOwnership(test *testing.T) {
	info, err := os.Stat(privateDirectoryFixture(test))
	if err != nil {
		test.Fatal(err)
	}
	for _, entry := range []struct {
		name   string
		system any
	}{
		{name: "unavailable"},
		{name: "wrong-type", system: "not Unix ownership"},
		{name: "foreign-owner", system: &syscall.Stat_t{Uid: uint32(os.Geteuid()) + 1}},
		{name: "nil-stat", system: (*syscall.Stat_t)(nil)},
	} {
		test.Run(entry.name, func(test *testing.T) {
			if err := verifyUnixOwner(ownershipFixture{FileInfo: info, system: entry.system}); err == nil {
				test.Fatal("accepted unverifiable current-user ownership")
			}
		})
	}
}
