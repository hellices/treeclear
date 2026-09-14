package git

import (
	"context"
	"errors"
	"strconv"
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
	for _, diagnosticBytes := range []int{4095, 4096, 4097, 4101} {
		test.Run(strconv.Itoa(diagnosticBytes), func(test *testing.T) {
			directory := test.TempDir()
			diagnostic := strings.Repeat("d", diagnosticBytes-1) + "!"
			client := NewClient(runnerFunc(func(ctx context.Context, request execx.Request) (execx.Result, error) {
				result := statusSnapshotFixtureResult(test, request, directory, []byte("? visible.txt\x00"))
				if request.Args[0] == "status" {
					result.Stderr = []byte(diagnostic)
				}
				return result, nil
			}))
			actual, err := client.StatusSnapshot(test.Context(), directory)
			assertStatusSnapshotError(test, actual, err)
			wantError := "git status reported diagnostics: " + diagnostic[:min(len(diagnostic), 4096)]
			if err.Error() != wantError {
				test.Fatalf("status diagnostic differs from its exact bound: error length %d, want %d", len(err.Error()), len(wantError))
			}
		})
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
