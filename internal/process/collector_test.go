package process

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/pathutil"
)

type fakeSource struct {
	processes []Info
	err       error
}

func (source fakeSource) List(context.Context) ([]Info, error) {
	return source.processes, source.err
}

type sourceFunc func(context.Context) ([]Info, error)

func (source sourceFunc) List(ctx context.Context) ([]Info, error) {
	return source(ctx)
}

func TestCollectorAssociatesOnlyContainedCWD(test *testing.T) {
	fixture := newProcessFixture(test)
	sibling := fixture.info
	sibling.PID = 11
	sibling.CWD = fixture.sibling
	sibling.Executable = writeFixtureFile(test, filepath.Join(fixture.worktree, "program"))
	sibling.CommandLine = []string{fixture.child}
	collector := Collector{Source: fakeSource{processes: []Info{fixture.info, sibling}}}
	actual, errs := collector.Collect(context.Background(), fixture.worktrees())
	if len(errs) != 0 || !actual.Complete || len(actual.Errors) != 0 {
		test.Fatalf("Collect() completeness = %v, errors = %v / %v", actual.Complete, errs, actual.Errors)
	}
	evidence := actual.ByWorktree[fixture.worktree]
	if len(evidence) != 1 || evidence[0].PID != 10 || evidence[0].State != domain.EvidenceActive {
		test.Fatalf("contained evidence = %#v", actual)
	}
	canonicalCWD, err := pathutil.Canonical(fixture.child)
	if err != nil {
		test.Fatal(err)
	}
	if evidence[0].CWD != canonicalCWD || len(actual.Uninspectable) != 0 || len(actual.GlobalUnknown) != 0 {
		test.Fatalf("unexpected path or unknown evidence = %#v", actual)
	}
}

func TestCollectorRetainsUnknownWhenContainmentReadFails(test *testing.T) {
	for _, failure := range []string{"cwd disappears", "cwd inaccessible", "worktree disappears after match"} {
		test.Run(failure, func(test *testing.T) {
			if failure == "cwd inaccessible" && runtime.GOOS == "windows" {
				test.Skip("POSIX permission fixture; disappearance cases still run on Windows")
			}
			fixture := newProcessFixture(test)
			canonicalCWD, err := pathutil.Canonical(fixture.info.CWD)
			if err != nil {
				test.Fatal(err)
			}
			canonicalOther, err := pathutil.Canonical(fixture.other)
			if err != nil {
				test.Fatal(err)
			}
			changed := false
			matchedBeforeFailure := false
			collector := Collector{
				Source: fakeSource{processes: []Info{fixture.info}},
				contains: func(parent, child string) (bool, error) {
					if child != canonicalCWD {
						test.Fatalf("containment input %q was not the canonical cwd %q", child, canonicalCWD)
					}
					if !changed && (failure != "worktree disappears after match" || parent == canonicalOther) {
						changed = true
						switch failure {
						case "cwd disappears":
							if err := os.Remove(fixture.child); err != nil {
								test.Fatal(err)
							}
						case "cwd inaccessible":
							if err := os.Chmod(fixture.worktree, 0); err != nil {
								test.Fatal(err)
							}
							test.Cleanup(func() {
								if err := os.Chmod(fixture.worktree, 0o700); err != nil {
									test.Error(err)
								}
							})
							if _, err := os.Stat(fixture.child); err == nil {
								test.Skip("current user bypasses directory permissions")
							}
						case "worktree disappears after match":
							if !matchedBeforeFailure {
								test.Fatal("fixture must first establish a positive containment match")
							}
							if err := os.Remove(fixture.other); err != nil {
								test.Fatal(err)
							}
						}
					}
					contained, err := pathutil.ContainsChecked(parent, child)
					matchedBeforeFailure = matchedBeforeFailure || contained
					return contained, err
				},
			}
			actual, errs := collector.Collect(context.Background(), fixture.worktrees())
			if !changed || !actual.Complete || len(errs) == 0 || len(actual.Errors) == 0 {
				test.Fatalf("containment failure was not reported separately from enumeration: %#v, %v", actual, errs)
			}
			wantError := os.ErrNotExist
			if failure == "cwd inaccessible" {
				wantError = os.ErrPermission
			}
			if !errors.Is(errors.Join(errs...), wantError) {
				test.Fatalf("containment read error was lost: %v", errs)
			}
			evidence := actual.Uninspectable[fixture.info.PID]
			if evidence.State != domain.EvidenceUnknown || evidence.Error == "" || evidence.CWD != canonicalCWD || evidence.Executable != fixture.info.Executable || !evidence.CreatedAt.Equal(fixture.info.CreatedAt) {
				test.Fatalf("containment failure discarded or trusted the process identity: %#v", evidence)
			}
			if len(actual.GlobalUnknown) != 1 || !reflect.DeepEqual(actual.GlobalUnknown[0], evidence) {
				test.Fatalf("unresolved relevance must block every candidate: %#v", actual.GlobalUnknown)
			}
			for _, worktree := range fixture.worktrees() {
				if len(actual.ByWorktree[worktree.Path]) != 0 {
					test.Fatalf("partial containment match was published before all reads succeeded: %#v", actual.ByWorktree)
				}
			}
			assertFingerprint(test, evidence.Fingerprint)
		})
	}
}

func TestCollectorRetainsInaccessibleProcessesRegardlessOfOwner(test *testing.T) {
	for _, relation := range []OwnerRelation{OwnerSame, OwnerUnknown, OwnerOther, ""} {
		test.Run(string(relation), func(test *testing.T) {
			fixture := newProcessFixture(test)
			info := fixture.info
			info.CWD = ""
			info.OwnerRelation = relation
			info.Inspectable = false
			info.Error = "cwd: access denied"
			info.CommandLine = []string{fixture.outside, "--unrelated=" + fixture.outside}
			actual, errs := (Collector{Source: fakeSource{processes: []Info{info}}}).Collect(context.Background(), fixture.worktrees())
			if len(errs) == 0 || len(actual.Errors) == 0 || !actual.Complete {
				test.Fatalf("inspection failure must be reported without losing enumeration completeness: %#v, %v", actual, errs)
			}
			evidence, exists := actual.Uninspectable[info.PID]
			if !exists || evidence.State != domain.EvidenceUnknown || !strings.Contains(evidence.Error, "access denied") {
				test.Fatalf("Uninspectable = %#v", actual.Uninspectable)
			}
			if evidence.PID != info.PID || !evidence.CreatedAt.Equal(info.CreatedAt) || evidence.Executable != info.Executable {
				test.Fatalf("partial process identity was discarded: %#v", evidence)
			}
			if len(actual.GlobalUnknown) != 1 || !reflect.DeepEqual(actual.GlobalUnknown[0], evidence) {
				test.Fatalf("GlobalUnknown = %#v; want retained process for every candidate", actual.GlobalUnknown)
			}
			for _, worktree := range fixture.worktrees() {
				if len(actual.ByWorktree[worktree.Path]) != 0 {
					test.Fatal("global evidence must not also be appended to ByWorktree before correlation")
				}
			}
			assertFingerprint(test, evidence.Fingerprint)
		})
	}
}

func TestCollectorAssociatesUnknownThroughAvailablePathHints(test *testing.T) {
	for _, hint := range []string{"executable", "argument", "option value", "available cwd"} {
		test.Run(hint, func(test *testing.T) {
			fixture := newProcessFixture(test)
			info := fixture.info
			info.Inspectable = false
			info.Error = "inspection denied"
			info.CWD = ""
			info.OwnerRelation = OwnerOther
			script := writeFixtureFile(test, filepath.Join(fixture.worktree, "script with spaces"))
			switch hint {
			case "executable":
				info.Executable = script
			case "argument":
				info.CommandLine = []string{"runner", script, script}
			case "option value":
				info.CommandLine = []string{"runner", "--workspace=" + fixture.worktree}
			case "available cwd":
				info.CWD = fixture.child
			}
			actual, errs := (Collector{Source: fakeSource{processes: []Info{info}}}).Collect(context.Background(), fixture.worktrees())
			if len(errs) == 0 || len(actual.Uninspectable) != 1 || len(actual.GlobalUnknown) != 0 {
				test.Fatalf("Collect() = %#v, %v", actual, errs)
			}
			evidence := actual.ByWorktree[fixture.worktree]
			if len(evidence) != 1 || evidence[0].State != domain.EvidenceUnknown || evidence[0].PID != info.PID {
				test.Fatalf("hint association = %#v", actual.ByWorktree)
			}
			if len(actual.ByWorktree[fixture.other]) != 0 {
				test.Fatal("localized hint was associated with an unrelated worktree")
			}
		})
	}
}

func TestCollectorRejectsMissingOrMalformedCWD(test *testing.T) {
	for _, invalid := range []string{"empty", "relative", "nul", "missing", "file"} {
		test.Run(invalid, func(test *testing.T) {
			fixture := newProcessFixture(test)
			info := fixture.info
			switch invalid {
			case "empty":
				info.CWD = ""
			case "relative":
				info.CWD = "."
			case "nul":
				info.CWD += "\x00"
			case "missing":
				info.CWD = filepath.Join(fixture.worktree, "missing")
			case "file":
				info.CWD = fixture.info.Executable
			}
			actual, errs := (Collector{Source: fakeSource{processes: []Info{info}}}).Collect(context.Background(), fixture.worktrees())
			if len(errs) == 0 || len(actual.GlobalUnknown) != 1 || actual.Uninspectable[info.PID].State != domain.EvidenceUnknown {
				test.Fatalf("malformed cwd must be unknown, not inactive: %#v, %v", actual, errs)
			}
			if len(actual.ByWorktree[fixture.worktree]) != 0 {
				test.Fatal("unresolved cwd was accepted as a path hint")
			}
		})
	}
}

func TestCollectorRejectsIncompleteIdentityEvenWithReadableCWD(test *testing.T) {
	for _, invalid := range []string{"creation time", "executable", "relative executable", "malformed executable", "inspection error", "uninspectable"} {
		test.Run(invalid, func(test *testing.T) {
			fixture := newProcessFixture(test)
			info := fixture.info
			switch invalid {
			case "creation time":
				info.CreatedAt = time.Time{}
			case "executable":
				info.Executable = ""
			case "relative executable":
				info.Executable = "runner"
			case "malformed executable":
				info.Executable += "\x00"
			case "inspection error":
				info.Error = "owner lookup failed"
			case "uninspectable":
				info.Inspectable = false
			}
			actual, errs := (Collector{Source: fakeSource{processes: []Info{info}}}).Collect(context.Background(), fixture.worktrees())
			evidence := actual.ByWorktree[fixture.worktree]
			if len(errs) == 0 || len(evidence) != 1 || evidence[0].State != domain.EvidenceUnknown || evidence[0].Error == "" {
				test.Fatalf("incomplete identity must remain unknown: %#v, %v", actual, errs)
			}
			if actual.Uninspectable[info.PID].State != domain.EvidenceUnknown {
				test.Fatal("incomplete process was not retained by PID")
			}
		})
	}
}

func TestCollectorRejectsMalformedAncillaryEvidence(test *testing.T) {
	for _, field := range []string{"command line", "owner", "executable directory"} {
		test.Run(field, func(test *testing.T) {
			fixture := newProcessFixture(test)
			info := fixture.info
			switch field {
			case "command line":
				info.CommandLine = []string{"program", "bad\x00argument"}
			case "owner":
				info.Owner += "\x00"
			case "executable directory":
				info.Executable = fixture.outside
			}
			actual, errs := (Collector{Source: fakeSource{processes: []Info{info}}}).Collect(context.Background(), fixture.worktrees())
			evidence := actual.ByWorktree[fixture.worktree]
			if len(errs) == 0 || len(evidence) != 1 || evidence[0].State != domain.EvidenceUnknown || evidence[0].Error == "" {
				test.Fatalf("malformed %s became successful evidence: %#v, %v", field, actual, errs)
			}
		})
	}
}

func TestCollectorPreservesPartialEnumerationAndGlobalFailure(test *testing.T) {
	fixture := newProcessFixture(test)
	failure := errors.New("process enumeration incomplete")
	collector := Collector{Source: fakeSource{processes: []Info{fixture.info}, err: failure}}
	actual, errs := collector.Collect(context.Background(), fixture.worktrees())
	assertIncomplete(test, actual, errs)
	if !errors.Is(errors.Join(errs...), failure) {
		test.Fatalf("enumeration cause was lost: %v", errs)
	}
	if evidence := actual.ByWorktree[fixture.worktree]; len(evidence) != 1 || evidence[0].State != domain.EvidenceActive {
		test.Fatalf("partial enumeration was discarded: %#v", actual.ByWorktree)
	}
}

func TestCollectorMissingSourceAndCancellationAreIncomplete(test *testing.T) {
	fixture := newProcessFixture(test)
	actual, errs := (Collector{}).Collect(context.Background(), fixture.worktrees())
	assertIncomplete(test, actual, errs)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	collector := Collector{Source: sourceFunc(func(context.Context) ([]Info, error) {
		test.Error("canceled collection must not invoke source")
		return nil, nil
	})}
	actual, errs = collector.Collect(ctx, fixture.worktrees())
	assertIncomplete(test, actual, errs)
	if !errors.Is(errors.Join(errs...), context.Canceled) {
		test.Fatalf("cancellation cause was lost: %v", errs)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	collector.Source = sourceFunc(func(context.Context) ([]Info, error) {
		cancel()
		return []Info{fixture.info}, nil
	})
	actual, errs = collector.Collect(ctx, fixture.worktrees())
	assertIncomplete(test, actual, errs)
}

func TestCollectorMalformedEnumerationIsGloballyUnknown(test *testing.T) {
	for _, invalid := range []string{"negative PID", "reserved PID", "duplicate PID"} {
		test.Run(invalid, func(test *testing.T) {
			fixture := newProcessFixture(test)
			processes := []Info{fixture.info}
			switch invalid {
			case "negative PID":
				processes[0].PID = -1
			case "reserved PID":
				processes[0].PID = 0
			case "duplicate PID":
				duplicate := fixture.info
				duplicate.CreatedAt = duplicate.CreatedAt.Add(time.Second)
				duplicate.CWD = fixture.other
				processes = append(processes, duplicate)
			}
			actual, errs := (Collector{Source: fakeSource{processes: processes}}).Collect(context.Background(), fixture.worktrees())
			assertIncomplete(test, actual, errs)
			if actual.Uninspectable[processes[0].PID].State != domain.EvidenceUnknown {
				test.Fatalf("malformed PID identity was not retained: %#v", actual.Uninspectable)
			}
		})
	}
}

func TestCollectorInvalidWorktreePathGetsUnknownEvidence(test *testing.T) {
	missing := filepath.Join(test.TempDir(), "missing")
	worktrees := []domain.Worktree{{Path: missing}, {Path: ""}}
	actual, errs := (Collector{Source: fakeSource{}}).Collect(context.Background(), worktrees)
	if len(errs) != len(worktrees) || !actual.Complete {
		test.Fatalf("Collect() = %#v, %v", actual, errs)
	}
	for _, worktree := range worktrees {
		evidence := actual.ByWorktree[worktree.Path]
		if len(evidence) != 1 || evidence[0].State != domain.EvidenceUnknown || evidence[0].Error == "" {
			test.Fatalf("invalid worktree path lost unknown evidence: %#v", actual.ByWorktree)
		}
	}
}

func TestCollectorFingerprintTracksIdentityNotTimeZone(test *testing.T) {
	fixture := newProcessFixture(test)
	collectFingerprint := func(info Info) string {
		test.Helper()
		actual, errs := (Collector{Source: fakeSource{processes: []Info{info}}}).Collect(context.Background(), fixture.worktrees())
		if len(errs) != 0 || len(actual.ByWorktree[fixture.worktree]) != 1 {
			test.Fatalf("Collect() = %#v, %v", actual, errs)
		}
		fingerprint := actual.ByWorktree[fixture.worktree][0].Fingerprint
		assertFingerprint(test, fingerprint)
		return fingerprint
	}
	baseline := collectFingerprint(fixture.info)
	if actual := collectFingerprint(fixture.info); actual != baseline {
		test.Fatal("identical process identity has an unstable fingerprint")
	}
	localized := fixture.info
	localized.CreatedAt = localized.CreatedAt.In(time.FixedZone("fixture", 9*60*60))
	if actual := collectFingerprint(localized); actual != baseline {
		test.Fatal("time zone altered the creation-time identity")
	}
	for _, field := range []string{"PID", "creation time", "executable", "cwd"} {
		changed := fixture.info
		switch field {
		case "PID":
			changed.PID++
		case "creation time":
			changed.CreatedAt = changed.CreatedAt.Add(time.Nanosecond)
		case "executable":
			changed.Executable = writeFixtureFile(test, filepath.Join(fixture.outside, "other-program"))
		case "cwd":
			changed.CWD = fixture.worktree
		}
		if actual := collectFingerprint(changed); actual == baseline {
			test.Errorf("changing %s did not change the fingerprint", field)
		}
	}
}

func TestCollectorIsConcurrentAndDoesNotMutateSource(test *testing.T) {
	fixture := newProcessFixture(test)
	fixture.info.CommandLine = []string{"program", fixture.child}
	processes := []Info{fixture.info}
	before := fixture.info
	before.CommandLine = append([]string(nil), fixture.info.CommandLine...)
	collector := Collector{Source: fakeSource{processes: processes}}
	worktrees := fixture.worktrees()
	originalWorktrees := append([]domain.Worktree(nil), worktrees...)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			actual, errs := collector.Collect(context.Background(), worktrees)
			if len(errs) != 0 || len(actual.ByWorktree[fixture.worktree]) != 1 {
				test.Errorf("concurrent Collect() = %#v, %v", actual, errs)
			}
		})
	}
	workers.Wait()
	if !reflect.DeepEqual(processes[0], before) || !reflect.DeepEqual(worktrees, originalWorktrees) {
		test.Fatal("collector mutated its inputs")
	}
}

type processFixture struct {
	worktree string
	other    string
	sibling  string
	outside  string
	child    string
	info     Info
}

func newProcessFixture(test *testing.T) processFixture {
	test.Helper()
	root := test.TempDir()
	fixture := processFixture{
		worktree: filepath.Join(root, "wt"),
		other:    filepath.Join(root, "other"),
		sibling:  filepath.Join(root, "wt-other"),
		outside:  filepath.Join(root, "outside"),
		child:    filepath.Join(root, "wt", "child"),
	}
	for _, directory := range []string{fixture.child, fixture.other, fixture.sibling, fixture.outside} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			test.Fatal(err)
		}
	}
	fixture.info = Info{
		PID: 10, CreatedAt: time.Unix(100, 0).UTC(),
		Executable: writeFixtureFile(test, filepath.Join(fixture.outside, "program")),
		CWD:        fixture.child, Owner: "fixture-user", OwnerRelation: OwnerSame, Inspectable: true,
	}
	return fixture
}

func (fixture processFixture) worktrees() []domain.Worktree {
	return []domain.Worktree{{Path: fixture.worktree}, {Path: fixture.other}}
}

func writeFixtureFile(test *testing.T, name string) string {
	test.Helper()
	if err := os.WriteFile(name, nil, 0o600); err != nil {
		test.Fatal(err)
	}
	return name
}

func assertFingerprint(test *testing.T, fingerprint string) {
	test.Helper()
	decoded, err := hex.DecodeString(fingerprint)
	if err != nil || len(decoded) != 32 {
		test.Fatalf("fingerprint = %q, %v; want SHA-256 hex", fingerprint, err)
	}
}

func assertIncomplete(test *testing.T, collection Collection, errs []error) {
	test.Helper()
	if collection.Complete || len(errs) == 0 || len(collection.Errors) == 0 {
		test.Fatalf("incomplete enumeration was not reported: %#v, %v", collection, errs)
	}
	for _, evidence := range collection.GlobalUnknown {
		if evidence.PID == 0 && evidence.State == domain.EvidenceUnknown && evidence.Error != "" {
			return
		}
	}
	test.Fatalf("missing global enumeration sentinel: %#v", collection.GlobalUnknown)
}
