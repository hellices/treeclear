package plan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/hellices/treeclear/internal/domain"
)

var ErrPlanNotFound = errors.New("no unexpired plan found")

func (store Store) Latest(ctx context.Context) (domain.Plan, error) {
	if err := store.validateOperation(ctx); err != nil {
		return domain.Plan{}, err
	}
	directory := filepath.Join(store.root, "plans")
	info, err := os.Lstat(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return domain.Plan{}, ErrPlanNotFound
	}
	if err != nil {
		return domain.Plan{}, err
	}
	if !info.IsDir() {
		return domain.Plan{}, fmt.Errorf("%w: plan directory is not a regular directory", ErrPlanInvalid)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return domain.Plan{}, err
	}
	var latest domain.Plan
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return domain.Plan{}, err
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		identifier := strings.TrimSuffix(entry.Name(), ".json")
		if !validPlanID(identifier) {
			return domain.Plan{}, fmt.Errorf("%w: unexpected plan filename %q", ErrPlanInvalid, entry.Name())
		}
		value, err := store.Load(ctx, identifier)
		if errors.Is(err, ErrPlanExpired) {
			continue
		}
		if err != nil {
			return domain.Plan{}, fmt.Errorf("read plan %q: %w", identifier, err)
		}
		if latest.ID == "" || value.GeneratedAt.After(latest.GeneratedAt) || value.GeneratedAt.Equal(latest.GeneratedAt) && value.ID > latest.ID {
			latest = value
		}
	}
	if err := ctx.Err(); err != nil {
		return domain.Plan{}, err
	}
	if latest.ID == "" {
		return domain.Plan{}, ErrPlanNotFound
	}
	if err := validateStoredPlan(latest, store.now()); err != nil {
		return domain.Plan{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.Plan{}, err
	}
	return latest, nil
}
