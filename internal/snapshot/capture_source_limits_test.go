//go:build darwin || linux || windows

package snapshot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hellices/treeclear/internal/git"
)

func TestCaptureSourceNativeInspectionLimits(test *testing.T) {
	for _, scenario := range []string{"index-bytes", "administrative-bytes", "administrative-entries", "nested-administrative-entries"} {
		test.Run(scenario, func(test *testing.T) {
			fixture := newCaptureNativeFixture(test, false, false)
			if _, err := CaptureSource(test.Context(), git.NewClient(nil), fixture.expected, 1<<20); err != nil {
				test.Fatalf("unchanged native control: %v", err)
			}
			if scenario == "administrative-entries" || scenario == "nested-administrative-entries" {
				directory := fixture.expected.AdminDir
				if scenario == "nested-administrative-entries" {
					directory = filepath.Join(directory, "diagnostics")
					if err := os.Mkdir(directory, 0o700); err != nil {
						test.Fatal(err)
					}
				}
				for position := range 4096 {
					if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("diagnostic-%04d", position)), nil, 0o600); err != nil {
						test.Fatal(err)
					}
				}
			} else {
				name := "diagnostic-grown"
				if scenario == "index-bytes" {
					name = "index"
				}
				file, err := os.OpenFile(filepath.Join(fixture.expected.AdminDir, name), os.O_CREATE|os.O_WRONLY, 0o600)
				if err != nil {
					test.Fatal(err)
				}
				if err := errors.Join(file.Truncate((16<<20)+1), file.Close()); err != nil {
					test.Fatal(err)
				}
			}
			actual, err := CaptureSource(test.Context(), git.NewClient(nil), fixture.expected, 64<<20)
			if !errors.Is(err, ErrSourceInvalid) || !errors.Is(err, ErrSourceLimit) || !reflect.DeepEqual(actual, SourceCapture{}) {
				test.Fatalf("known native inspection capacity lost its source category: %v", err)
			}
		})
	}
}

func TestCaptureSourceNativeSelectionLimit(test *testing.T) {
	fixture := newCaptureNativeFixture(test, false, false)
	for position := range 4097 {
		if err := os.WriteFile(filepath.Join(fixture.worktree, fmt.Sprintf("untracked-%04d", position)), nil, 0o600); err != nil {
			test.Fatal(err)
		}
	}
	fixture.expected = captureNativeInspect(test, fixture.repository, fixture.worktree)
	if fixture.expected.Status.Untracked != 4097 {
		test.Fatalf("legacy inspection must retain complete counts: %#v", fixture.expected.Status)
	}
	actual, err := CaptureSource(test.Context(), git.NewClient(nil), fixture.expected, 64<<20)
	if !errors.Is(err, ErrSourceInvalid) || !errors.Is(err, ErrSourceLimit) || !reflect.DeepEqual(actual, SourceCapture{}) {
		test.Fatalf("known native selection capacity lost its source category: %v", err)
	}
}
