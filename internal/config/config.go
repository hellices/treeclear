package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/hellices/treeclear/internal/domain"
)

type Config struct {
	Roots               []string
	InactivityThreshold time.Duration
	PlanExpiry          time.Duration
	BaseBranches        []string
	MinimumTrustGrade   string
	SnapshotMaxBytes    int64
}

type Overrides struct {
	Roots               []string
	InactivityThreshold *time.Duration
	PlanExpiry          *time.Duration
	BaseBranches        []string
	MinimumTrustGrade   *string
	SnapshotMaxBytes    *int64
}

type fileConfig struct {
	Roots               []string  `toml:"roots"`
	InactivityThreshold *Duration `toml:"inactivity_threshold"`
	PlanExpiry          *Duration `toml:"plan_expiry"`
	BaseBranches        []string  `toml:"base_branches"`
	MinimumTrustGrade   *string   `toml:"minimum_trust_grade"`
	SnapshotMaxBytes    *int64    `toml:"snapshot_max_bytes"`
}

func Default() Config {
	return Config{
		InactivityThreshold: 7 * 24 * time.Hour,
		PlanExpiry:          15 * time.Minute,
		BaseBranches:        []string{"main", "master"},
		MinimumTrustGrade:   "versioned-private",
		SnapshotMaxBytes:    64 << 20,
	}
}

func Load(userPath, repositoryPath string, overrides Overrides) (Config, error) {
	cfg := Default()
	for _, path := range []string{userPath, repositoryPath} {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Config{}, err
		}
		var file fileConfig
		if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&file); err != nil {
			return Config{}, err
		}
		applyFile(&cfg, file)
	}
	if len(overrides.Roots) > 0 {
		cfg.Roots = append([]string(nil), overrides.Roots...)
	}
	if overrides.InactivityThreshold != nil {
		cfg.InactivityThreshold = *overrides.InactivityThreshold
	}
	if overrides.PlanExpiry != nil {
		cfg.PlanExpiry = *overrides.PlanExpiry
	}
	if overrides.BaseBranches != nil {
		cfg.BaseBranches = append([]string(nil), overrides.BaseBranches...)
	}
	if overrides.MinimumTrustGrade != nil {
		cfg.MinimumTrustGrade = *overrides.MinimumTrustGrade
	}
	if overrides.SnapshotMaxBytes != nil {
		cfg.SnapshotMaxBytes = *overrides.SnapshotMaxBytes
	}
	if cfg.InactivityThreshold <= 0 || cfg.PlanExpiry <= 0 || cfg.SnapshotMaxBytes <= 0 {
		return Config{}, errors.New("durations and snapshot_max_bytes must be positive")
	}
	switch domain.TrustGrade(cfg.MinimumTrustGrade) {
	case domain.TrustSupportedAPI, domain.TrustSupportedAppServer, domain.TrustSupportedCLI,
		domain.TrustExperimentalAPI, domain.TrustVersionedPrivate, domain.TrustUnversionedPrivate, domain.TrustProcessOnly:
	default:
		return Config{}, fmt.Errorf("unknown minimum_trust_grade %q", cfg.MinimumTrustGrade)
	}
	return cfg, nil
}

func applyFile(cfg *Config, file fileConfig) {
	if file.Roots != nil {
		cfg.Roots = append([]string(nil), file.Roots...)
	}
	if file.InactivityThreshold != nil {
		cfg.InactivityThreshold = file.InactivityThreshold.Duration
	}
	if file.PlanExpiry != nil {
		cfg.PlanExpiry = file.PlanExpiry.Duration
	}
	if file.BaseBranches != nil {
		cfg.BaseBranches = append([]string(nil), file.BaseBranches...)
	}
	if file.MinimumTrustGrade != nil {
		cfg.MinimumTrustGrade = *file.MinimumTrustGrade
	}
	if file.SnapshotMaxBytes != nil {
		cfg.SnapshotMaxBytes = *file.SnapshotMaxBytes
	}
}
