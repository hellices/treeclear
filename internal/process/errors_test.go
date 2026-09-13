package process

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
)

func TestCollectorReportsTypedLocalErrors(test *testing.T) {
	for _, scenario := range []string{"process", "nested_roots", "worktree_path"} {
		test.Run(scenario, func(test *testing.T) {
			fixture := newProcessFixture(test)
			fixture.info.Error = "synthetic name inspection denial"
			worktrees := fixture.worktrees()
			collector := Collector{Source: fakeSource{processes: []Info{fixture.info}}}
			wantPaths := []string{fixture.worktree}
			switch scenario {
			case "nested_roots":
				worktrees = append(worktrees, domain.Worktree{Path: fixture.child})
				wantPaths = append(wantPaths, fixture.child)
			case "worktree_path":
				worktrees[0].Path = filepath.Join(fixture.outside, "missing")
				wantPaths = []string{worktrees[0].Path}
				collector.Source = fakeSource{}
			}
			collection, failures := collector.Collect(context.Background(), worktrees)
			if !collection.Complete || len(collection.GlobalUnknown) != 0 || len(failures) != 1 {
				test.Fatalf("local collection = %#v, %v", collection, failures)
			}
			scoped, ok := failures[0].(*WorktreeError)
			if !ok || scoped.Err == nil || !slices.Equal(scoped.Paths, wantPaths) {
				test.Fatalf("local error lacks exact typed provenance: %#v; want %q", failures[0], wantPaths)
			}
			if len(collection.Errors) != len(failures) || collection.Errors[0] != failures[0].Error() {
				test.Fatalf("diagnostics lost ordered error projection: %q, %v", collection.Errors, failures)
			}
			for _, path := range scoped.Paths {
				if records := collection.ByWorktree[path]; len(records) != 1 || records[0].State != domain.EvidenceUnknown {
					test.Fatalf("scope %q has no associated unknown evidence: %#v", path, records)
				}
			}
			worktrees[0].Path = "changed caller slice"
			if !slices.Equal(scoped.Paths, wantPaths) {
				test.Fatal("typed scope aliases caller input")
			}
		})
	}
}

func TestCollectorKeepsGlobalErrorsUnscoped(test *testing.T) {
	for _, scenario := range []string{"enumeration", "unbound", "containment", "cancellation"} {
		test.Run(scenario, func(test *testing.T) {
			fixture := newProcessFixture(test)
			collector := Collector{Source: fakeSource{processes: []Info{fixture.info}}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "enumeration":
				collector.Source = fakeSource{err: errors.New("synthetic incomplete enumeration")}
			case "unbound":
				fixture.info.CWD = fixture.outside
				fixture.info.Error = "synthetic inspection denial"
				collector.Source = fakeSource{processes: []Info{fixture.info}}
			case "containment":
				collector.contains = func(string, string) (bool, error) {
					return false, errors.New("synthetic uncertain containment")
				}
			case "cancellation":
				cancel()
			}
			collection, failures := collector.Collect(ctx, fixture.worktrees())
			if len(failures) == 0 || len(collection.GlobalUnknown) == 0 || len(collection.Errors) != len(failures) {
				test.Fatalf("global collection = %#v, %v", collection, failures)
			}
			for index, failure := range failures {
				if _, scoped := failure.(*WorktreeError); scoped {
					test.Fatalf("global failure gained local provenance: %#v", failure)
				}
				if collection.Errors[index] != failure.Error() {
					test.Fatal("diagnostics changed error order")
				}
			}
		})
	}
}

func TestWorktreeErrorPreservesUnderlyingError(test *testing.T) {
	underlying := errors.New("synthetic error")
	failure := &WorktreeError{Err: underlying, Paths: []string{"synthetic worktree"}}
	if failure.Error() != underlying.Error() || !errors.Is(failure, underlying) || errors.Unwrap(failure) != underlying {
		test.Fatalf("worktree error lost underlying identity: %v", failure)
	}
	for _, invalid := range []*WorktreeError{nil, {Paths: []string{"synthetic worktree"}}} {
		if invalid.Error() == "" || invalid.Unwrap() != nil {
			test.Fatal("invalid worktree error lacks a safe diagnostic")
		}
	}
}
