package plan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/fssecure"
	"github.com/hellices/treeclear/internal/testutil"
)

func saveFixture(test *testing.T, store Store, value domain.Plan) string {
	test.Helper()
	path, err := store.Save(context.Background(), value)
	if err != nil || path == "" {
		test.Fatalf("Save() = %q, %v", path, err)
	}
	return path
}

func TestStoreRoundTripByIDAndPath(test *testing.T) {
	value := storedPlanFixture()
	clock := testutil.NewClock(value.GeneratedAt)
	root := filepath.Join(test.TempDir(), "private", "state")
	key := bytes.Repeat([]byte{0x42}, 32)
	store := NewStore(root, clock.Now, key)
	before, err := json.Marshal(value)
	if err != nil {
		test.Fatal(err)
	}
	path := saveFixture(test, store, value)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		test.Fatal(err)
	}
	if path != filepath.Join(resolvedRoot, "plans", value.ID+".json") {
		test.Fatalf("plan path = %q", path)
	}
	for _, input := range []string{value.ID, path} {
		loaded, err := store.Load(context.Background(), input)
		if err != nil || loaded.ID != value.ID || len(loaded.Candidates) != 1 {
			test.Fatalf("Load(%q) = %q, %v", input, loaded.ID, err)
		}
		loaded.Integrity = domain.PlanIntegrity{}
		after, err := json.Marshal(loaded)
		if err != nil || !bytes.Equal(before, after) {
			test.Fatalf("stored plan changed: %s, %v", after, err)
		}
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		test.Fatal(err)
	}
	if _, err := decodeAuthenticatedPlan(contents, key); err != nil {
		test.Fatalf("on-disk plan does not authenticate: %v", err)
	}
	after, err := json.Marshal(value)
	if err != nil || !bytes.Equal(before, after) {
		test.Fatal("Save mutated caller input")
	}
	if _, err := os.Stat(filepath.Join(root, "integrity.key")); !errors.Is(err, fs.ErrNotExist) {
		test.Fatalf("injected key unexpectedly persisted: %v", err)
	}
}

func TestStoreRejectsExpiredPlan(test *testing.T) {
	value := storedPlanFixture()
	clock := testutil.NewClock(value.GeneratedAt)
	store := NewStore(filepath.Join(test.TempDir(), "state"), clock.Now, bytes.Repeat([]byte{0x42}, 32))
	path := saveFixture(test, store, value)
	clock.Advance(15*time.Minute - time.Nanosecond)
	for _, input := range []string{value.ID, path} {
		if _, err := store.Load(context.Background(), input); err != nil {
			test.Fatalf("plan before expiry rejected for %q: %v", input, err)
		}
	}
	clock.Advance(time.Nanosecond)
	for _, input := range []string{value.ID, path} {
		loaded, err := store.Load(context.Background(), input)
		if !errors.Is(err, ErrPlanExpired) || loaded.ID != "" || len(loaded.Candidates) != 0 {
			test.Fatalf("expired Load(%q) = %q, %v", input, loaded.ID, err)
		}
	}
}

func TestStoreLoadRejectsExpiredIDMismatch(test *testing.T) {
	value := storedPlanFixture()
	clock := testutil.NewClock(value.GeneratedAt)
	value.ExpiresAt = clock.Now().Add(time.Minute)
	root := filepath.Join(test.TempDir(), "state")
	store := NewStore(root, clock.Now, bytes.Repeat([]byte{0x42}, 32))
	path := saveFixture(test, store, value)
	contents, err := os.ReadFile(path)
	if err != nil {
		test.Fatal(err)
	}
	aliasID := "plan_alias"
	aliasPath := filepath.Join(root, "plans", aliasID+".json")
	if err := fssecure.WritePrivateFile(aliasPath, contents); err != nil {
		test.Fatal(err)
	}
	clock.Advance(time.Minute)
	loaded, err := store.Load(context.Background(), aliasID)
	if !errors.Is(err, ErrPlanIntegrity) || errors.Is(err, ErrPlanExpired) || loaded.ID != "" || len(loaded.Candidates) != 0 {
		test.Fatalf("expired ID mismatch = %q, %v; want integrity failure", loaded.ID, err)
	}
	loaded, err = store.Load(context.Background(), aliasPath)
	if !errors.Is(err, ErrPlanExpired) || loaded.ID != "" || len(loaded.Candidates) != 0 {
		test.Fatalf("expired explicit-path load = %q, %v; want expiry failure", loaded.ID, err)
	}
}

func TestStoreAuthenticatesBeforeMetadataValidation(test *testing.T) {
	cases := []struct {
		name         string
		change       func(*domain.Plan)
		trustedError error
	}{
		{"schema", func(value *domain.Plan) { value.SchemaVersion = 99 }, ErrPlanSchema},
		{"expiry", func(value *domain.Plan) {
			value.ExpiresAt = value.GeneratedAt
			value.GeneratedAt = value.GeneratedAt.Add(-time.Minute)
		}, ErrPlanExpired},
		{"identifier", func(value *domain.Plan) { value.ID = "../outside" }, ErrPlanInvalid},
		{"future generation", func(value *domain.Plan) { value.GeneratedAt = value.GeneratedAt.Add(time.Minute) }, ErrPlanInvalid},
		{"expired generation at expiry", func(value *domain.Plan) { value.ExpiresAt = value.GeneratedAt }, ErrPlanInvalid},
		{"expired generation after expiry", func(value *domain.Plan) { value.ExpiresAt = value.GeneratedAt.Add(-time.Minute) }, ErrPlanInvalid},
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			value := storedPlanFixture()
			clock := testutil.NewClock(value.GeneratedAt)
			key := bytes.Repeat([]byte{0x42}, 32)
			store := NewStore(filepath.Join(test.TempDir(), "state"), clock.Now, key)
			path := saveFixture(test, store, value)
			contents, err := os.ReadFile(path)
			if err != nil {
				test.Fatal(err)
			}
			var changed domain.Plan
			if err := json.Unmarshal(contents, &changed); err != nil {
				test.Fatal(err)
			}
			scenario.change(&changed)
			contents, err = json.Marshal(changed)
			if err != nil {
				test.Fatal(err)
			}
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				test.Fatal(err)
			}
			if loaded, err := store.Load(context.Background(), path); !errors.Is(err, ErrPlanIntegrity) || loaded.ID != "" {
				test.Fatalf("unauthenticated metadata used: %q, %v", loaded.ID, err)
			}
			contents, err = encodeSignedPlan(changed, key)
			if err != nil {
				test.Fatal(err)
			}
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				test.Fatal(err)
			}
			if loaded, err := store.Load(context.Background(), path); !errors.Is(err, scenario.trustedError) || loaded.ID != "" || len(loaded.Candidates) != 0 {
				test.Fatalf("authenticated metadata load = %q, %v, want %v", loaded.ID, err, scenario.trustedError)
			}
		})
	}
}

func TestStoreRejectsTamperedAction(test *testing.T) {
	value := storedPlanFixture()
	value.Candidates[0].Action = "none"
	value.Candidates[0].Decision.Classification = domain.Protected
	store := NewStore(filepath.Join(test.TempDir(), "state"), func() time.Time { return value.GeneratedAt }, bytes.Repeat([]byte{0x42}, 32))
	path := saveFixture(test, store, value)
	contents, err := os.ReadFile(path)
	if err != nil {
		test.Fatal(err)
	}
	contents = bytes.Replace(contents, []byte(`"action":"none"`), []byte(`"action":"remove"`), 1)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		test.Fatal(err)
	}
	if loaded, err := store.Load(context.Background(), value.ID); !errors.Is(err, ErrPlanIntegrity) || loaded.ID != "" {
		test.Fatalf("tampered action accepted: %q, %v", loaded.ID, err)
	}
}

func TestStorePlansAreImmutable(test *testing.T) {
	value := storedPlanFixture()
	store := NewStore(filepath.Join(test.TempDir(), "state"), func() time.Time { return value.GeneratedAt }, bytes.Repeat([]byte{0x42}, 32))
	path := saveFixture(test, store, value)
	before, err := os.ReadFile(path)
	if err != nil {
		test.Fatal(err)
	}
	value.Candidates[0].Action = "none"
	if _, err := store.Save(context.Background(), value); !errors.Is(err, fs.ErrExist) {
		test.Fatalf("existing plan overwritten: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		test.Fatal("existing plan bytes changed")
	}
}

func TestStorePersistsAndReusesLocalKey(test *testing.T) {
	value := storedPlanFixture()
	root := filepath.Join(test.TempDir(), "state")
	clock := testutil.NewClock(value.GeneratedAt)
	path := saveFixture(test, NewStore(root, clock.Now, nil), value)
	keyPath := filepath.Join(root, "integrity.key")
	key, err := os.ReadFile(keyPath)
	if err != nil || len(key) != 32 || bytes.Equal(key, make([]byte, 32)) {
		test.Fatalf("persistent key length = %d, error = %v", len(key), err)
	}
	loaded, err := NewStore(root, clock.Now, nil).Load(context.Background(), path)
	if err != nil || loaded.ID != value.ID || loaded.Integrity.KeyID != integrityKeyID(key) {
		test.Fatalf("new store did not reuse local key: %q, %v", loaded.ID, err)
	}
	value.ID = "plan_second"
	saveFixture(test, NewStore(root, clock.Now, nil), value)
	again, err := os.ReadFile(keyPath)
	if err != nil || !bytes.Equal(key, again) {
		test.Fatal("persistent integrity key was replaced")
	}
}

func TestStoreInjectedKeyIsCopied(test *testing.T) {
	value := storedPlanFixture()
	key := bytes.Repeat([]byte{0x42}, 32)
	store := NewStore(filepath.Join(test.TempDir(), "state"), func() time.Time { return value.GeneratedAt }, key)
	clear(key)
	path := saveFixture(test, store, value)
	contents, err := os.ReadFile(path)
	if err != nil {
		test.Fatal(err)
	}
	if _, err := decodeAuthenticatedPlan(contents, bytes.Repeat([]byte{0x42}, 32)); err != nil {
		test.Fatalf("caller key mutation reached store: %v", err)
	}
}

func TestStoreExternalPlanUsesLocalKey(test *testing.T) {
	value := storedPlanFixture()
	clock := testutil.NewClock(value.GeneratedAt)
	key := bytes.Repeat([]byte{0x42}, 32)
	path := saveFixture(test, NewStore(filepath.Join(test.TempDir(), "state"), clock.Now, key), value)
	matching := NewStore(filepath.Join(test.TempDir(), "state"), clock.Now, key)
	if _, err := matching.Load(context.Background(), path); err != nil {
		test.Fatalf("external plan from same installation rejected: %v", err)
	}
	foreign := NewStore(filepath.Join(test.TempDir(), "state"), clock.Now, bytes.Repeat([]byte{0x24}, 32))
	if _, err := foreign.Load(context.Background(), path); !errors.Is(err, ErrPlanIntegrity) {
		test.Fatalf("external plan from foreign installation accepted: %v", err)
	}
}

func TestStoreRejectsInvalidSaveWithoutFilesystemChanges(test *testing.T) {
	cases := []struct {
		name   string
		change func(*domain.Plan)
	}{
		{"missing ID", func(value *domain.Plan) { value.ID = "" }},
		{"traversal ID", func(value *domain.Plan) { value.ID = "../outside" }},
		{"empty ID suffix", func(value *domain.Plan) { value.ID = "plan_" }},
		{"filename ID", func(value *domain.Plan) { value.ID = "plan_name.json" }},
		{"backslash ID", func(value *domain.Plan) { value.ID = `plan_..\outside` }},
		{"ADS ID", func(value *domain.Plan) { value.ID = "plan_name:stream" }},
		{"long ID", func(value *domain.Plan) { value.ID = "plan_" + strings.Repeat("a", 129) }},
		{"schema", func(value *domain.Plan) { value.SchemaVersion = 99 }},
		{"zero expiry", func(value *domain.Plan) { value.ExpiresAt = time.Time{} }},
		{"expired", func(value *domain.Plan) { value.ExpiresAt = value.GeneratedAt }},
		{"future generation", func(value *domain.Plan) { value.GeneratedAt = value.GeneratedAt.Add(time.Minute) }},
		{"malformed text", func(value *domain.Plan) { value.Warnings[0] = "\xff" }},
		{"oversized", func(value *domain.Plan) { value.Warnings[0] = strings.Repeat("a", 16<<20) }},
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			value := storedPlanFixture()
			clock := testutil.NewClock(value.GeneratedAt)
			scenario.change(&value)
			root := filepath.Join(test.TempDir(), "must-not-exist")
			if path, err := NewStore(root, clock.Now, nil).Save(context.Background(), value); err == nil || path != "" {
				test.Fatalf("invalid plan saved: %q, %v", path, err)
			}
			if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
				test.Fatalf("invalid save created state: %v", err)
			}
		})
	}
}

func TestStoreReadAndPreCanceledOperationsDoNotCreateState(test *testing.T) {
	value := storedPlanFixture()
	root := filepath.Join(test.TempDir(), "absent")
	store := NewStore(root, func() time.Time { return value.GeneratedAt }, nil)
	if _, err := store.Load(context.Background(), value.ID); err == nil {
		test.Fatal("load succeeded without state or key")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Save(ctx, value); !errors.Is(err, context.Canceled) {
		test.Fatalf("canceled save error = %v", err)
	}
	if _, err := store.Load(ctx, value.ID); !errors.Is(err, context.Canceled) {
		test.Fatalf("canceled load error = %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
		test.Fatalf("read/cancellation created state: %v", err)
	}
}

func TestStorePreservesExplicitPlanPathText(test *testing.T) {
	store := NewStore(filepath.Join(test.TempDir(), "state"), nil, nil)
	for _, input := range []string{"report", "plan", "report.txt", "./plan_fixture", "alias/../report"} {
		test.Run(input, func(test *testing.T) {
			path, requestedID, err := store.planPath(input)
			if err != nil || path != input || requestedID != "" {
				test.Fatalf("planPath(%q) = %q, %q, %v; want unchanged path", input, path, requestedID, err)
			}
		})
	}
	for _, input := range []string{"", "report\x00"} {
		if path, requestedID, err := store.planPath(input); !errors.Is(err, ErrPlanInvalid) || path != "" || requestedID != "" {
			test.Fatalf("invalid planPath(%q) = %q, %q, %v", input, path, requestedID, err)
		}
	}
}

func TestStoreRejectsInvalidConfigurationWithoutState(test *testing.T) {
	value := storedPlanFixture()
	root := filepath.Join(test.TempDir(), "absent")
	clock := testutil.NewClock(value.GeneratedAt)
	cases := []struct {
		name  string
		store Store
		want  error
	}{
		{"zero store", Store{}, ErrPlanInvalid},
		{"empty root", NewStore("", clock.Now, nil), ErrPlanInvalid},
		{"NUL root", NewStore(root+"\x00", clock.Now, nil), ErrPlanInvalid},
		{"empty injected key", NewStore(root, clock.Now, []byte{}), ErrIntegrityKey},
		{"short injected key", NewStore(root, clock.Now, make([]byte, 31)), ErrIntegrityKey},
		{"long injected key", NewStore(root, clock.Now, make([]byte, 33)), ErrIntegrityKey},
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			if path, err := scenario.store.Save(context.Background(), value); !errors.Is(err, scenario.want) || path != "" {
				test.Fatalf("invalid store Save = %q, %v", path, err)
			}
			if loaded, err := scenario.store.Load(context.Background(), value.ID); !errors.Is(err, scenario.want) || loaded.ID != "" {
				test.Fatalf("invalid store Load = %q, %v", loaded.ID, err)
			}
		})
	}
	if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
		test.Fatalf("invalid configuration created state: %v", err)
	}
}

func TestStoreCanceledAfterValidationDoesNotCreateState(test *testing.T) {
	value := storedPlanFixture()
	root := filepath.Join(test.TempDir(), "absent")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewStore(root, func() time.Time {
		cancel()
		return value.GeneratedAt
	}, nil)
	if path, err := store.Save(ctx, value); !errors.Is(err, context.Canceled) || path != "" {
		test.Fatalf("canceled Save = %q, %v", path, err)
	}
	if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
		test.Fatalf("canceled validation created state: %v", err)
	}
}

func TestStoreLoadCanceledAfterValidationReturnsNoPlan(test *testing.T) {
	value := storedPlanFixture()
	root := filepath.Join(test.TempDir(), "state")
	key := bytes.Repeat([]byte{0x42}, 32)
	path := saveFixture(test, NewStore(root, func() time.Time { return value.GeneratedAt }, key), value)
	for _, input := range []string{value.ID, path} {
		test.Run(input, func(test *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := NewStore(root, func() time.Time {
				cancel()
				return value.GeneratedAt
			}, key)
			loaded, err := store.Load(ctx, input)
			if !errors.Is(err, context.Canceled) || loaded.ID != "" || len(loaded.Candidates) != 0 {
				test.Fatalf("canceled Load(%q) = %q, %v", input, loaded.ID, err)
			}
		})
	}
}

func TestStoreRejectsCorruptKeyWithoutReplacingIt(test *testing.T) {
	for _, size := range []int{0, 1, 31, 33} {
		test.Run(fmt.Sprint(size), func(test *testing.T) {
			value := storedPlanFixture()
			root := filepath.Join(test.TempDir(), "state")
			store := NewStore(root, func() time.Time { return value.GeneratedAt }, nil)
			path := saveFixture(test, store, value)
			keyPath := filepath.Join(root, "integrity.key")
			corrupt := bytes.Repeat([]byte{0x42}, size)
			if err := os.WriteFile(keyPath, corrupt, 0o600); err != nil {
				test.Fatal(err)
			}
			if _, err := store.Load(context.Background(), path); !errors.Is(err, ErrIntegrityKey) {
				test.Fatalf("corrupt key load error = %v", err)
			}
			value.ID = "plan_next"
			if _, err := store.Save(context.Background(), value); !errors.Is(err, ErrIntegrityKey) {
				test.Fatalf("corrupt key save error = %v", err)
			}
			after, err := os.ReadFile(keyPath)
			if err != nil || !bytes.Equal(after, corrupt) {
				test.Fatal("corrupt key silently replaced")
			}
		})
	}
}

func TestStoreLoadNeverRegeneratesMissingKey(test *testing.T) {
	value := storedPlanFixture()
	root := filepath.Join(test.TempDir(), "state")
	store := NewStore(root, func() time.Time { return value.GeneratedAt }, nil)
	path := saveFixture(test, store, value)
	keyPath := filepath.Join(root, "integrity.key")
	if err := os.Remove(keyPath); err != nil {
		test.Fatal(err)
	}
	if _, err := store.Load(context.Background(), path); !errors.Is(err, ErrIntegrityKey) {
		test.Fatalf("missing key error = %v", err)
	}
	if _, err := os.Stat(keyPath); !errors.Is(err, fs.ErrNotExist) {
		test.Fatalf("Load recreated key: %v", err)
	}
}

func TestStoreConcurrentInitializationAndPublication(test *testing.T) {
	for _, sameID := range []bool{false, true} {
		test.Run(fmt.Sprint(sameID), func(test *testing.T) {
			root := filepath.Join(test.TempDir(), "state")
			stamp := storedPlanFixture().GeneratedAt
			const workers = 12
			results := make(chan error, workers)
			var group sync.WaitGroup
			for index := range workers {
				group.Go(func() {
					value := storedPlanFixture()
					if !sameID {
						value.ID = fmt.Sprintf("plan_concurrent_%d", index)
					}
					store := NewStore(root, func() time.Time { return stamp }, nil)
					path, err := store.Save(context.Background(), value)
					if err == nil {
						_, err = NewStore(root, func() time.Time { return stamp }, nil).Load(context.Background(), path)
					}
					results <- err
				})
			}
			group.Wait()
			close(results)
			successes := 0
			for err := range results {
				if err == nil {
					successes++
				} else if !sameID || !errors.Is(err, fs.ErrExist) {
					test.Errorf("concurrent publication: %v", err)
				}
			}
			expected := workers
			if sameID {
				expected = 1
			}
			if successes != expected {
				test.Fatalf("successful publications = %d, want %d", successes, expected)
			}
			entries, err := os.ReadDir(filepath.Join(root, "plans"))
			if err != nil || len(entries) != expected {
				test.Fatalf("published entries = %d, error = %v", len(entries), err)
			}
		})
	}
}

func TestStoreRejectsOversizedLoadAndIDMismatch(test *testing.T) {
	value := storedPlanFixture()
	key := bytes.Repeat([]byte{0x42}, 32)
	store := NewStore(filepath.Join(test.TempDir(), "state"), func() time.Time { return value.GeneratedAt }, key)
	path := saveFixture(test, store, value)
	changed := value
	changed.ID = "plan_different"
	contents, err := encodeSignedPlan(changed, key)
	if err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		test.Fatal(err)
	}
	if _, err := store.Load(context.Background(), value.ID); !errors.Is(err, ErrPlanIntegrity) {
		test.Fatalf("plan ID substitution error = %v", err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte{' '}, (16<<20)+1), 0o600); err != nil {
		test.Fatal(err)
	}
	if loaded, err := store.Load(context.Background(), path); err == nil || loaded.ID != "" {
		test.Fatalf("oversized document accepted: %q, %v", loaded.ID, err)
	}
}
