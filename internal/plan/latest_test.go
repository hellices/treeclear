package plan

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/fssecure"
	"github.com/hellices/treeclear/internal/testutil"
)

func TestStoreLatestUsesAuthenticatedGenerationTime(test *testing.T) {
	value := storedPlanFixture()
	clock := testutil.NewClock(value.GeneratedAt)
	root := filepath.Join(test.TempDir(), "state")
	store := NewStore(root, clock.Now, bytes.Repeat([]byte{0x42}, 32))
	var olderPath string
	for _, entry := range []struct {
		id        string
		generated time.Duration
		expiry    time.Duration
	}{
		{"plan_older", -3 * time.Minute, 10 * time.Minute},
		{"plan_newer_a", -time.Minute, 10 * time.Minute},
		{"plan_newer_z", -time.Minute, 10 * time.Minute},
		{"plan_expired", 0, time.Minute},
	} {
		value.ID = entry.id
		value.GeneratedAt = clock.Now().Add(entry.generated)
		value.ExpiresAt = clock.Now().Add(entry.expiry)
		path := saveFixture(test, store, value)
		if entry.id == "plan_older" {
			olderPath = path
		}
	}
	if err := os.Chtimes(olderPath, clock.Now(), clock.Now().Add(24*time.Hour)); err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plans", ".treeclear-in-flight.tmp"), []byte("partial staging"), 0o600); err != nil {
		test.Fatal(err)
	}
	clock.Advance(2 * time.Minute)
	latest, err := store.Latest(context.Background())
	if err != nil || latest.ID != "plan_newer_z" || latest.Integrity.MAC == "" {
		test.Fatalf("latest plan = %q, %v", latest.ID, err)
	}
}

func TestStoreLatestRejectsInvalidDocumentsInsteadOfFallingBack(test *testing.T) {
	for _, scenario := range []string{"tampered_expired", "mismatched_id", "invalid_filename", "directory"} {
		test.Run(scenario, func(test *testing.T) {
			value := storedPlanFixture()
			clock := testutil.NewClock(value.GeneratedAt)
			root := filepath.Join(test.TempDir(), "state")
			store := NewStore(root, clock.Now, bytes.Repeat([]byte{0x42}, 32))
			path := saveFixture(test, store, value)
			contents, err := os.ReadFile(path)
			if err != nil {
				test.Fatal(err)
			}
			badPath := filepath.Join(root, "plans", "plan_bad.json")
			switch scenario {
			case "tampered_expired":
				value.ID = "plan_bad"
				value.ExpiresAt = clock.Now().Add(time.Minute)
				badPath = saveFixture(test, store, value)
				if err := os.WriteFile(badPath, []byte(`{"expiresAt":"2000-01-01T00:00:00Z"}`), 0o600); err != nil {
					test.Fatal(err)
				}
				clock.Advance(2 * time.Minute)
			case "mismatched_id":
				if err := fssecure.WritePrivateFile(badPath, contents); err != nil {
					test.Fatal(err)
				}
			case "invalid_filename":
				if err := fssecure.WritePrivateFile(filepath.Join(root, "plans", "invalid.json"), contents); err != nil {
					test.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(badPath, 0o700); err != nil {
					test.Fatal(err)
				}
			}
			latest, err := store.Latest(context.Background())
			if err == nil || latest.ID != "" || len(latest.Candidates) != 0 {
				test.Fatalf("invalid document silently skipped: %q, %v", latest.ID, err)
			}
		})
	}
}

func TestStoreLatestRejectsExpiredInvalidDocuments(test *testing.T) {
	cases := []struct {
		name   string
		change func(*domain.Plan)
		want   error
	}{
		{"mismatched_id", func(value *domain.Plan) { value.ID = "plan_different" }, ErrPlanIntegrity},
		{"generation_at_expiry", func(value *domain.Plan) { value.GeneratedAt = value.ExpiresAt }, ErrPlanInvalid},
		{"generation_after_expiry", func(value *domain.Plan) { value.GeneratedAt = value.ExpiresAt.Add(time.Nanosecond) }, ErrPlanInvalid},
	}
	for _, scenario := range cases {
		test.Run(scenario.name, func(test *testing.T) {
			value := storedPlanFixture()
			value.ID = "plan_available"
			clock := testutil.NewClock(value.GeneratedAt)
			root := filepath.Join(test.TempDir(), "state")
			key := bytes.Repeat([]byte{0x42}, 32)
			store := NewStore(root, clock.Now, key)
			saveFixture(test, store, value)
			value.ID = "plan_invalid"
			value.ExpiresAt = clock.Now().Add(time.Minute)
			path := filepath.Join(root, "plans", value.ID+".json")
			scenario.change(&value)
			contents, err := encodeSignedPlan(value, key)
			if err != nil {
				test.Fatal(err)
			}
			if err := fssecure.WritePrivateFile(path, contents); err != nil {
				test.Fatal(err)
			}
			clock.Advance(2 * time.Minute)
			latest, err := store.Latest(context.Background())
			if !errors.Is(err, scenario.want) || errors.Is(err, ErrPlanExpired) || latest.ID != "" || len(latest.Candidates) != 0 {
				test.Fatalf("Latest skipped invalid expired document: %q, %v; want %v", latest.ID, err, scenario.want)
			}
		})
	}
}

func TestStoreLatestMissingExpiredAndCanceledDoNotInitializeState(test *testing.T) {
	value := storedPlanFixture()
	clock := testutil.NewClock(value.GeneratedAt)
	root := filepath.Join(test.TempDir(), "state")
	store := NewStore(root, clock.Now, nil)
	latest, err := store.Latest(context.Background())
	if !errors.Is(err, ErrPlanNotFound) || latest.ID != "" {
		test.Fatalf("missing latest = %q, %v", latest.ID, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Latest(ctx); !errors.Is(err, context.Canceled) {
		test.Fatalf("canceled latest = %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
		test.Fatalf("latest initialized missing state: %v", err)
	}
	saveFixture(test, store, value)
	clock.Advance(15 * time.Minute)
	latest, err = store.Latest(context.Background())
	if !errors.Is(err, ErrPlanNotFound) || latest.ID != "" {
		test.Fatalf("all expired latest = %q, %v", latest.ID, err)
	}
}

func TestStoreLatestDoesNotReturnPlanThatExpiresDuringSelection(test *testing.T) {
	value := storedPlanFixture()
	root := filepath.Join(test.TempDir(), "state")
	key := bytes.Repeat([]byte{0x42}, 32)
	saveFixture(test, NewStore(root, func() time.Time { return value.GeneratedAt }, key), value)
	calls := 0
	store := NewStore(root, func() time.Time {
		calls++
		if calls == 1 {
			return value.GeneratedAt
		}
		return value.ExpiresAt
	}, key)
	latest, err := store.Latest(context.Background())
	if err == nil || latest.ID != "" {
		test.Fatalf("returned expired selection: %q, %v", latest.ID, err)
	}
}

func TestIsIDMatchesStoreIdentifierContract(test *testing.T) {
	for input, want := range map[string]bool{
		"plan_a": true, "plan_A-0_b": true, "plan_": false, "plan_a.json": false,
		"./plan_a": false, "report": false, "plan_../other": false,
		"plan_" + strings.Repeat("a", 123): true, "plan_" + strings.Repeat("a", 124): false,
	} {
		if actual := IsID(input); actual != want {
			test.Errorf("IsID(%q) = %t, want %t", input, actual, want)
		}
	}
}

func TestStoreLatestCanceledAtFinalValidationReturnsNoPlan(test *testing.T) {
	value := storedPlanFixture()
	root := filepath.Join(test.TempDir(), "state")
	key := bytes.Repeat([]byte{0x42}, 32)
	saveFixture(test, NewStore(root, func() time.Time { return value.GeneratedAt }, key), value)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	store := NewStore(root, func() time.Time {
		calls++
		if calls == 2 {
			cancel()
		}
		return value.GeneratedAt
	}, key)
	latest, err := store.Latest(ctx)
	if !errors.Is(err, context.Canceled) || latest.ID != "" {
		test.Fatalf("canceled latest selection = %q, %v", latest.ID, err)
	}
}
