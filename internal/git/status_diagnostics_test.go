package git

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
)

func TestClientStatusRejectsSuccessfulDiagnostics(test *testing.T) {
	for _, payload := range []struct {
		name     string
		contents []byte
	}{
		{"empty", nil},
		{"partial", []byte("? visible.txt\x00")},
	} {
		for _, diagnostic := range []struct {
			name string
			text string
		}{
			{"enumeration warning", "warning: could not open directory 'denied/': Permission denied\n"},
			{"unrecognized diagnostic", "unknown diagnostic\n"},
			{"whitespace", " \n"},
		} {
			test.Run(payload.name+"/"+diagnostic.name, func(test *testing.T) {
				directory := test.TempDir()
				client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
					result := statusSnapshotFixtureResult(test, request, directory, payload.contents)
					if request.Args[0] == "status" {
						result.Stderr = []byte(diagnostic.text)
					}
					return result, nil
				}))
				test.Run("snapshot", func(test *testing.T) {
					actual, err := client.StatusSnapshot(test.Context(), directory)
					assertStatusSnapshotError(test, actual, err)
					if !strings.Contains(err.Error(), diagnostic.text) {
						test.Fatalf("status error lost diagnostic: %v", err)
					}
				})
				test.Run("raw", func(test *testing.T) {
					status, raw, err := client.StatusRaw(test.Context(), directory)
					assertStatusSnapshotError(test, StatusSnapshot{Status: status, Raw: raw}, err)
				})
				test.Run("summary", func(test *testing.T) {
					status, err := client.Status(test.Context(), directory)
					assertStatusSnapshotError(test, StatusSnapshot{Status: status}, err)
				})
			})
		}
	}
}

func TestClientStatusBoundsSuccessfulDiagnostics(test *testing.T) {
	directory := test.TempDir()
	diagnostic := strings.Repeat("d", 4096) + "excluded diagnostic suffix"
	client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
		result := statusSnapshotFixtureResult(test, request, directory, []byte("? visible.txt\x00"))
		if request.Args[0] == "status" {
			result.Stderr = []byte(diagnostic)
		}
		return result, nil
	}))
	actual, err := client.StatusSnapshot(test.Context(), directory)
	assertStatusSnapshotError(test, actual, err)
	if !strings.Contains(err.Error(), diagnostic[:4096]) || strings.Contains(err.Error(), "excluded diagnostic suffix") || len(err.Error()) > 4352 {
		test.Fatalf("status diagnostic was not retained within its bound: error length %d", len(err.Error()))
	}
}

func TestClientStatusDiagnosticsPreserveCommandFailure(test *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, execx.ErrOutputLimit} {
		test.Run(cause.Error(), func(test *testing.T) {
			directory := test.TempDir()
			client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
				result := statusSnapshotFixtureResult(test, request, directory, []byte("? visible.txt\x00"))
				if request.Args[0] == "status" {
					result.Stderr = []byte("partial observation diagnostic\n")
					return result, cause
				}
				return result, nil
			}))
			actual, err := client.StatusSnapshot(test.Context(), directory)
			assertStatusSnapshotError(test, actual, err)
			if !errors.Is(err, cause) {
				test.Fatalf("status diagnostic lost command cause %v: %v", cause, err)
			}
			status, raw, err := client.StatusRaw(test.Context(), directory)
			if !errors.Is(err, cause) || status != (domain.GitStatus{}) || raw != nil {
				test.Fatalf("legacy status diagnostic lost command cause: status %#v, raw %q, error %v", status, raw, err)
			}
		})
	}
}
