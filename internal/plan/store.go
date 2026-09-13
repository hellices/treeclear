package plan

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/fssecure"
)

const maximumPlanBytes = 16 << 20

var (
	ErrPlanExpired = errors.New("plan expired")
	ErrPlanSchema  = errors.New("unsupported plan schema")
	ErrPlanInvalid = errors.New("invalid plan")
)

type Store struct {
	root        string
	now         func() time.Time
	injectedKey []byte
	initErr     error
}

func NewStore(root string, now func() time.Time, integrityKey []byte) Store {
	if now == nil {
		now = time.Now
	}
	store := Store{now: now, injectedKey: slices.Clone(integrityKey)}
	if root == "" || strings.ContainsRune(root, 0) {
		store.initErr = fmt.Errorf("%w: state directory is required", ErrPlanInvalid)
		return store
	}
	store.root, store.initErr = filepath.Abs(root)
	return store
}

func (store Store) Save(ctx context.Context, value domain.Plan) (string, error) {
	if err := store.validateOperation(ctx); err != nil {
		return "", err
	}
	if err := validateStoredPlan(value, store.now()); err != nil {
		return "", err
	}
	preview := value
	preview.Integrity = domain.PlanIntegrity{
		Algorithm: integrityAlgorithm, KeyID: "sha256:" + strings.Repeat("0", 64), MAC: strings.Repeat("0", 64),
	}
	encoded, err := canonicalPlanJSON(preview)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrPlanInvalid, err)
	}
	if len(encoded) > maximumPlanBytes {
		return "", fmt.Errorf("%w: document exceeds %d bytes", ErrPlanInvalid, maximumPlanBytes)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := fssecure.EnsurePrivateDirectory(store.root); err != nil {
		return "", err
	}
	key, err := store.localIntegrityKey(ctx, true)
	if err != nil {
		return "", err
	}
	contents, err := encodeSignedPlan(value, key)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path := filepath.Join(store.root, "plans", value.ID+".json")
	if err := fssecure.WritePrivateFile(path, contents); err != nil {
		return "", err
	}
	return path, nil
}

func (store Store) Load(ctx context.Context, idOrPath string) (domain.Plan, error) {
	if err := store.validateOperation(ctx); err != nil {
		return domain.Plan{}, err
	}
	path, requestedID, err := store.planPath(idOrPath)
	if err != nil {
		return domain.Plan{}, err
	}
	key, err := store.localIntegrityKey(ctx, false)
	if err != nil {
		return domain.Plan{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.Plan{}, err
	}
	contents, err := fssecure.ReadPrivateFile(path, maximumPlanBytes)
	if err != nil {
		return domain.Plan{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.Plan{}, err
	}
	value, err := decodeAuthenticatedPlan(contents, key)
	if err != nil {
		return domain.Plan{}, err
	}
	if err := validateStoredPlan(value, store.now()); err != nil {
		return domain.Plan{}, err
	}
	if requestedID != "" && value.ID != requestedID {
		return domain.Plan{}, fmt.Errorf("%w: requested identifier does not match", ErrPlanIntegrity)
	}
	if err := ctx.Err(); err != nil {
		return domain.Plan{}, err
	}
	return value, nil
}

func (store Store) validateOperation(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if store.initErr != nil {
		return store.initErr
	}
	if store.root == "" || store.now == nil {
		return ErrPlanInvalid
	}
	if store.injectedKey != nil {
		return validateIntegrityKey(store.injectedKey)
	}
	return nil
}

func (store Store) planPath(idOrPath string) (string, string, error) {
	if validPlanID(idOrPath) {
		return filepath.Join(store.root, "plans", idOrPath+".json"), idOrPath, nil
	}
	if idOrPath == "" || strings.ContainsRune(idOrPath, 0) {
		return "", "", ErrPlanInvalid
	}
	path, err := filepath.Abs(idOrPath)
	return path, "", err
}

func validateStoredPlan(value domain.Plan, now time.Time) error {
	if now.IsZero() {
		return fmt.Errorf("%w: current time is unknown", ErrPlanInvalid)
	}
	if value.SchemaVersion != 1 {
		return ErrPlanSchema
	}
	if !validPlanID(value.ID) {
		return fmt.Errorf("%w: invalid identifier", ErrPlanInvalid)
	}
	if !value.ExpiresAt.After(now) {
		return ErrPlanExpired
	}
	if !value.GeneratedAt.IsZero() && (value.GeneratedAt.After(now) || !value.GeneratedAt.Before(value.ExpiresAt)) {
		return fmt.Errorf("%w: invalid generation time", ErrPlanInvalid)
	}
	return nil
}

func IsID(identifier string) bool {
	return validPlanID(identifier)
}

func validPlanID(identifier string) bool {
	if !strings.HasPrefix(identifier, "plan_") || len(identifier) <= 5 || len(identifier) > 128 {
		return false
	}
	for _, character := range identifier[5:] {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}
