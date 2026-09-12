package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/hellices/treeclear/internal/config"
	"github.com/hellices/treeclear/internal/correlate"
	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/git"
	"github.com/hellices/treeclear/internal/inventory"
	"github.com/hellices/treeclear/internal/policy"
	"github.com/hellices/treeclear/internal/process"
)

type InventoryLoader interface {
	Load(context.Context, []string) ([]domain.Worktree, []error)
}

type ProcessCollector interface {
	Collect(context.Context, []domain.Worktree) (process.Collection, []error)
}

type ScanWorktree struct {
	Worktree domain.Worktree    `json:"worktree"`
	Evidence domain.EvidenceSet `json:"evidence"`
	Decision domain.Decision    `json:"decision"`
}

type ScanResult struct {
	SchemaVersion int            `json:"schemaVersion"`
	ToolVersion   string         `json:"toolVersion"`
	CollectedAt   time.Time      `json:"collectedAt"`
	Complete      bool           `json:"complete"`
	Worktrees     []ScanWorktree `json:"worktrees"`
	Warnings      []string       `json:"warnings,omitempty"`
}

func newScanCommand(dependencies Dependencies) *cobra.Command {
	var roots []string
	var threshold, format string
	command := &cobra.Command{
		Use:   "scan",
		Short: "Inspect worktrees without writing plans or removing anything",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			if err := command.Context().Err(); err != nil {
				return err
			}
			if format != "human" && format != "json" {
				return errors.New("--format must be human or json")
			}
			overrides := config.Overrides{Roots: roots}
			for _, root := range roots {
				if root == "" {
					return errors.New("--root must not be empty")
				}
			}
			if command.Flags().Changed("inactivity-threshold") {
				var duration config.Duration
				if err := duration.UnmarshalText([]byte(threshold)); err != nil {
					return err
				}
				if duration.Duration <= 0 {
					return errors.New("--inactivity-threshold must be positive")
				}
				overrides.InactivityThreshold = &duration.Duration
			}
			configuration, runtimeDependencies, err := scanConfiguration(dependencies, overrides)
			if err != nil {
				return err
			}
			result, scanErr := scan(command.Context(), runtimeDependencies, configuration)
			if result.SchemaVersion == 0 {
				return scanErr
			}
			if err := renderScan(command.OutOrStdout(), format, result); err != nil {
				return err
			}
			return scanErr
		},
	}
	command.Flags().StringArrayVar(&roots, "root", nil, "Repository discovery root (repeatable; defaults to configured roots or the containing repository)")
	command.Flags().StringVar(&threshold, "inactivity-threshold", "", "Required inactivity, such as 24h or 7d (default configuration: 7d)")
	command.Flags().StringVar(&format, "format", "human", "Output format: human or json")
	return command
}

func scanConfiguration(dependencies Dependencies, overrides config.Overrides) (config.Config, Dependencies, error) {
	var err error
	if dependencies.WorkingDirectory == "" {
		dependencies.WorkingDirectory, err = os.Getwd()
		if err != nil {
			return config.Config{}, dependencies, err
		}
	}
	if dependencies.UserConfigPath == "" || dependencies.DataDirectory == "" {
		directory, err := os.UserConfigDir()
		if err != nil {
			return config.Config{}, dependencies, err
		}
		if dependencies.UserConfigPath == "" {
			dependencies.UserConfigPath = filepath.Join(directory, "treeclear", "config.toml")
		}
		if dependencies.DataDirectory == "" {
			dependencies.DataDirectory = filepath.Join(directory, "treeclear")
		}
	}
	if dependencies.RepositoryConfigPath == "" {
		dependencies.RepositoryConfigPath = filepath.Join(dependencies.WorkingDirectory, "treeclear.toml")
	}
	configuration, err := config.Load(dependencies.UserConfigPath, dependencies.RepositoryConfigPath, overrides)
	if err != nil {
		return config.Config{}, dependencies, err
	}
	for index, root := range configuration.Roots {
		if root == "" {
			return config.Config{}, dependencies, errors.New("configured roots must not be empty")
		}
		if !filepath.IsAbs(root) {
			configuration.Roots[index] = filepath.Join(dependencies.WorkingDirectory, root)
		}
	}
	return configuration, dependencies, nil
}

func scan(ctx context.Context, dependencies Dependencies, configuration config.Config) (ScanResult, error) {
	loader := dependencies.Inventory
	client := git.NewClient(execx.OSRunner{})
	client.BaseBranches = append([]string(nil), configuration.BaseBranches...)
	if loader == nil || len(configuration.Roots) == 0 {
		if err := client.CheckVersion(ctx); err != nil {
			return ScanResult{}, err
		}
	}
	if len(configuration.Roots) == 0 {
		root, err := client.WorktreeRoot(ctx, dependencies.WorkingDirectory)
		if err != nil {
			return ScanResult{}, fmt.Errorf("no default repository scope; use --root <path> or configure roots to begin scanning: %w", err)
		}
		configuration.Roots = []string{root}
	}
	if loader == nil {
		loader = inventory.Loader{
			Git: client, DataDir: dependencies.DataDirectory,
			CWD: func() (string, error) { return dependencies.WorkingDirectory, nil },
		}
	}
	collector := dependencies.Processes
	if collector == nil {
		collector = process.Collector{Source: process.GopsutilSource{}}
	}
	now := dependencies.Now
	if now == nil {
		now = time.Now
	}
	result := ScanResult{
		SchemaVersion: 1, ToolVersion: dependencies.BuildVersion, CollectedAt: now().UTC(),
		Worktrees: []ScanWorktree{}, Warnings: []string{"Core-only scan: agent-provider adapters are not implemented; this is not authorization to remove worktrees."},
	}
	worktrees, collectionErrors := loader.Load(ctx, configuration.Roots)
	worktrees = append([]domain.Worktree(nil), worktrees...)
	collectionErrors = append([]error(nil), collectionErrors...)
	processes := process.Collection{Complete: true}
	if len(worktrees) != 0 {
		processContext, cancel := context.WithTimeout(ctx, 30*time.Second)
		var processErrors []error
		processes, processErrors = collector.Collect(processContext, worktrees)
		cancel()
		collectionErrors = append(collectionErrors, processErrors...)
	}
	if !processes.Complete && len(collectionErrors) == 0 {
		collectionErrors = append(collectionErrors, errors.New("process enumeration is incomplete"))
	}
	if err := ctx.Err(); err != nil {
		collectionErrors = append(collectionErrors, err)
	}
	if len(collectionErrors) != 0 {
		processes.Complete = false
	}
	for _, err := range collectionErrors {
		result.Warnings = append(result.Warnings, err.Error())
	}
	result.Complete = len(collectionErrors) == 0 && processes.Complete
	evidence := correlate.Group(worktrees, processes, nil)
	settings := domain.PolicySettings{
		InactivityThreshold: configuration.InactivityThreshold, PlanExpiry: configuration.PlanExpiry,
		BaseBranches: append([]string(nil), configuration.BaseBranches...), MinimumTrustGrade: domain.TrustGrade(configuration.MinimumTrustGrade),
		SnapshotMaxBytes: configuration.SnapshotMaxBytes,
	}
	sort.Slice(worktrees, func(left, right int) bool { return worktrees[left].Path < worktrees[right].Path })
	for _, worktree := range worktrees {
		collected := evidence[worktree.Path]
		result.Worktrees = append(result.Worktrees, ScanWorktree{
			Worktree: worktree, Evidence: collected,
			Decision: policy.Evaluate(worktree, collected, domain.Policy{Now: result.CollectedAt, Settings: settings}),
		})
	}
	if !result.Complete {
		return result, fmt.Errorf("scan incomplete: %d collection error(s); see warnings", len(collectionErrors))
	}
	return result, nil
}

func renderScan(output io.Writer, format string, result ScanResult) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "CLASSIFICATION\tBRANCH\tPATH\tREASONS"); err != nil {
		return err
	}
	for _, item := range result.Worktrees {
		codes := make([]string, 0, len(item.Decision.Reasons))
		for _, reason := range item.Decision.Reasons {
			codes = append(codes, reason.Code)
		}
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", item.Decision.Classification, strconv.Quote(item.Worktree.Branch), strconv.Quote(item.Worktree.Path), strings.Join(codes, ",")); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	for index, warning := range result.Warnings {
		if index == 5 {
			_, err := fmt.Fprintf(output, "... %d additional warnings; use --format json for details.\n", len(result.Warnings)-index)
			return err
		}
		if _, err := fmt.Fprintf(output, "Warning: %q\n", warning); err != nil {
			return err
		}
	}
	return nil
}
