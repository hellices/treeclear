package plan

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/fssecure"
	"github.com/hellices/treeclear/internal/testutil"
)

const historicalV1SignedPlan = `{"schemaVersion":1,"planId":"plan_fixture","generatedAt":"2026-09-13T12:00:00Z","expiresAt":"2026-09-13T12:15:00Z","toolVersion":"test","intendedApplyMode":"interactive","policyDigest":"sha256:policy","adapterLockDigest":"sha256:adapters","executableIdentities":[{"path":"/bin/provider","version":"","sha256":"sha256:executable","arguments":["--read-only"],"workingDirectory":"","environmentDigest":"","invocationDigest":""}],"scope":{"roots":["/repo"]},"candidates":[{"candidateId":"candidate","worktree":{"path":"/repo/feature","repositoryRoot":"/repo/main","commonGitDir":"/repo/main/.git","adminDir":"/repo/main/.git/worktrees/feature","head":"head","branch":"feature","upstream":"origin/main","primary":false,"current":false,"pathSafe":true,"detached":false,"locked":false,"prunable":false,"gitStateKnown":true,"status":{"staged":0,"unstaged":0,"unmerged":0,"untracked":0},"recoverable":true,"lastCommitAt":"2026-01-01T12:00:00Z","metadataModifiedAt":"2026-01-01T12:00:00Z","estimatedBytes":100,"indexHash":"index","adminHash":"admin"},"evidence":{"processes":[{"pid":20,"createdAt":"2026-01-01T12:00:00Z","executable":"/bin/editor","cwd":"/repo/feature","state":"inactive","fingerprint":"proc-b"},{"pid":10,"createdAt":"2026-01-01T12:00:00Z","executable":"/bin/editor","cwd":"/repo/feature","state":"inactive","fingerprint":"proc-a"}],"agents":[{"adapterId":"adapter-b","adapterVersion":"1","bundleDigest":"bundle","provider":"provider","sourceId":"source","sessionId":"session","threadId":"thread","projectId":"project","cwd":"/repo/feature","repositoryRoot":"/repo/main","worktreePath":"/repo/feature","state":"inactive","createdAt":"2026-01-01T12:00:00Z","updatedAt":"2026-01-01T12:00:00Z","observedAt":"2026-01-01T13:00:00Z","processRefs":[{"pid":20,"createdAt":"2026-01-01T12:00:00Z","executable":"/bin/editor","fingerprint":"ref-b"},{"pid":10,"createdAt":"2026-01-01T12:00:00Z","executable":"/bin/editor","fingerprint":"ref-a"}],"binding":{"kind":"worktree","identifier":"binding","version":"1"},"sourceKind":"file","supportGrade":"versioned-private","confidence":"high","schemaVersion":"1","rawFingerprint":"raw","trustRecordDigest":"trust","executable":{"path":"/bin/provider","version":"1","sha256":"binary","arguments":["--first","--second"],"workingDirectory":"/repo/feature","environmentDigest":"env","invocationDigest":"invocation"},"revalidationMode":"local-readonly"},{"adapterId":"adapter-a","adapterVersion":"1","bundleDigest":"bundle","provider":"provider","sourceId":"source","sessionId":"session","threadId":"thread","projectId":"project","cwd":"/repo/feature","repositoryRoot":"/repo/main","worktreePath":"/repo/feature","state":"inactive","createdAt":"2026-01-01T12:00:00Z","updatedAt":"2026-01-01T12:00:00Z","observedAt":"2026-01-01T13:00:00Z","processRefs":[{"pid":20,"createdAt":"2026-01-01T12:00:00Z","executable":"/bin/editor","fingerprint":"ref-b"},{"pid":10,"createdAt":"2026-01-01T12:00:00Z","executable":"/bin/editor","fingerprint":"ref-a"}],"binding":{"kind":"worktree","identifier":"binding","version":"1"},"sourceKind":"file","supportGrade":"versioned-private","confidence":"high","schemaVersion":"1","rawFingerprint":"raw","trustRecordDigest":"trust","executable":{"path":"/bin/provider","version":"1","sha256":"binary","arguments":["--first","--second"],"workingDirectory":"/repo/feature","environmentDigest":"env","invocationDigest":"invocation"},"revalidationMode":"local-readonly"}],"adapters":[{"adapterId":"adapter-b","applicable":true,"healthy":true,"trusted":true,"bestGrade":"versioned-private","offlineRevalidatable":true},{"adapterId":"adapter-a","applicable":true,"healthy":true,"trusted":true,"bestGrade":"versioned-private","offlineRevalidatable":true}]},"decision":{"classification":"safe","reasons":[{"code":"safe","message":"Safe"},{"code":"offline","message":"Offline"}],"inactiveFor":691200000000000},"action":"remove","fingerprint":"sha256:b4083e834dddc15392c72110182f51a2356e135d2433a62797efb3f4b887dd80","snapshot":{"required":true,"maximumBytes":1024,"untrackedFiles":1,"untrackedBytes":20,"sensitiveBlocked":false}}],"summary":{"safe":1,"review":0,"protected":0,"reclaimableBytes":100},"warnings":["explanation"],"integrity":{"algorithm":"hmac-sha256","keyId":"sha256:425ed4e4a36b30ea21b90e21c712c649e8214c29b7eaf68089d1039c6e55384c","mac":"0e67f732bf1e5869581094a1ef58b4901b5210a69d802910254e1d8b5da075fb"}}`

const historicalV1Fingerprint = "sha256:b4083e834dddc15392c72110182f51a2356e135d2433a62797efb3f4b887dd80"

func TestHistoricalV1PlanRetainsBytesFingerprintAndSnapshotIntent(test *testing.T) {
	contents := []byte(historicalV1SignedPlan)
	key := bytes.Repeat([]byte{0x42}, 32)
	value, err := decodeAuthenticatedPlan(contents, key)
	if err != nil || value.SchemaVersion != 1 || len(value.Candidates) != 1 {
		test.Fatalf("historical v1 document no longer authenticates: %v", err)
	}
	candidate := value.Candidates[0]
	if value.Removal != nil || candidate.Selection != nil || candidate.Action != "remove" || !candidate.Snapshot.Required {
		test.Fatal("historical snapshot intent acquired new disposal semantics")
	}
	fingerprint, err := CandidateFingerprint(candidate)
	if err != nil || fingerprint != historicalV1Fingerprint || candidate.Fingerprint != historicalV1Fingerprint {
		test.Fatalf("historical fingerprint changed: %q, %v", fingerprint, err)
	}
	resigned, err := encodeSignedPlan(value, key)
	if err != nil || !bytes.Equal(contents, resigned) {
		test.Fatalf("historical candidate serialization or MAC changed: %v", err)
	}
	clock := testutil.NewClock(value.GeneratedAt)
	root := filepath.Join(test.TempDir(), "state")
	path := filepath.Join(root, "plans", value.ID+".json")
	if err := fssecure.WritePrivateFile(path, contents); err != nil {
		test.Fatal(err)
	}
	store := NewStore(root, clock.Now, key)
	loaded, err := store.Load(context.Background(), value.ID)
	if err != nil || !reflect.DeepEqual(loaded, value) {
		test.Fatalf("historical inspection changed the recorded plan: %v", err)
	}
	loaded, err = store.LoadForApply(context.Background(), value.ID)
	if !errors.Is(err, ErrPlanNotExecutable) || !reflect.DeepEqual(loaded, domain.Plan{}) {
		test.Fatalf("historical plan acquired execution permission: %v", err)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(unchanged, contents) {
		test.Fatalf("historical plan was rewritten on inspection/refusal: %v", err)
	}
}
