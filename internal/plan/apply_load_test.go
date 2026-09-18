package plan

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/fssecure"
)

func TestLoadForApplyRejectsAuthenticatedLegacyAndPreviewPlans(test *testing.T) {
	preview, clock := previewPlanFixture(test)
	legacy := storedPlanFixture()
	legacy.GeneratedAt, legacy.ExpiresAt = preview.GeneratedAt, preview.ExpiresAt
	store := NewStore(filepath.Join(test.TempDir(), "state"), clock.Now, bytes.Repeat([]byte{0x42}, 32))
	for _, value := range []domain.Plan{legacy, preview} {
		path := saveFixture(test, store, value)
		before, err := os.ReadFile(path)
		if err != nil {
			test.Fatal(err)
		}
		for _, reference := range []string{value.ID, path} {
			loaded, err := store.LoadForApply(context.Background(), reference)
			if !errors.Is(err, ErrPlanNotExecutable) || !reflect.DeepEqual(loaded, domain.Plan{}) {
				test.Fatalf("schema %d acquired execution permission: %v", value.SchemaVersion, err)
			}
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			test.Fatal("refusal modified the stored plan")
		}
		if _, err := store.Load(context.Background(), value.ID); err != nil {
			test.Fatalf("read-only inspection stopped working: %v", err)
		}
	}
}

func TestLoadForApplyAuthenticatesAndValidatesBeforeRefusal(test *testing.T) {
	value, clock := previewPlanFixture(test)
	root := filepath.Join(test.TempDir(), "state")
	store := NewStore(root, clock.Now, bytes.Repeat([]byte{0x42}, 32))
	path := saveFixture(test, store, value)
	contents, err := os.ReadFile(path)
	if err != nil {
		test.Fatal(err)
	}
	changed := bytes.Replace(contents, []byte(`"action":"none"`), []byte(`"action":"remove"`), 1)
	if bytes.Equal(contents, changed) {
		test.Fatal("tamper fixture made no change")
	}
	tampered := filepath.Join(root, "tampered.json")
	if err := fssecure.WritePrivateFile(tampered, changed); err != nil {
		test.Fatal(err)
	}
	loaded, err := store.LoadForApply(context.Background(), tampered)
	if !errors.Is(err, ErrPlanIntegrity) || !reflect.DeepEqual(loaded, domain.Plan{}) {
		test.Fatalf("apply loader bypassed authentication: %v", err)
	}
	clock.Advance(15 * time.Minute)
	loaded, err = store.LoadForApply(context.Background(), path)
	if !errors.Is(err, ErrPlanExpired) || !reflect.DeepEqual(loaded, domain.Plan{}) {
		test.Fatalf("apply loader bypassed expiry: %v", err)
	}
}
