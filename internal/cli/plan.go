package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/hellices/treeclear/internal/config"
	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/execx"
	"github.com/hellices/treeclear/internal/fssecure"
	"github.com/hellices/treeclear/internal/git"
	"github.com/hellices/treeclear/internal/inventory"
	"github.com/hellices/treeclear/internal/pathutil"
	"github.com/hellices/treeclear/internal/plan"
	"github.com/hellices/treeclear/internal/process"
)

func newPlanCommand(dependencies Dependencies) *cobra.Command {
	var roots []string
	var threshold, format, output string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Save an expiring cleanup plan without removing anything",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			if err := command.Context().Err(); err != nil {
				return err
			}
			if format != "human" && format != "json" {
				return errors.New("--format must be human or json")
			}
			if command.Flags().Changed("output") && (output == "" || strings.ContainsRune(output, 0)) {
				return errors.New("--output must be a nonempty file path")
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
			configuration, runtime, err := scanConfiguration(dependencies, overrides)
			if err != nil {
				return err
			}
			builder, request, err := configuredPlanBuilder(command.Context(), runtime, configuration)
			if err != nil {
				return err
			}
			value, buildErr := builder.Build(command.Context(), request)
			if value.ID == "" {
				return buildErr
			}
			store := plan.NewStore(runtime.DataDirectory, runtime.Now, nil)
			path, err := store.Save(command.Context(), value)
			if err != nil {
				return err
			}
			value, err = store.Load(command.Context(), value.ID)
			if err != nil {
				return fmt.Errorf("plan saved at %q; reload failed: %w", path, err)
			}
			contents, err := json.Marshal(value)
			if err != nil {
				return fmt.Errorf("plan saved at %q; encoding failed: %w", path, err)
			}
			if output != "" {
				if err := command.Context().Err(); err != nil {
					return fmt.Errorf("plan saved at %q; export canceled: %w", path, err)
				}
				destination, err := resolveInputPath(runtime.WorkingDirectory, output)
				if err == nil {
					err = fssecure.WritePrivateExport(runtime.DataDirectory, destination, contents)
				}
				if err != nil {
					return fmt.Errorf("plan %s saved at %q; export failed: %w", value.ID, path, err)
				}
			}
			if err := writePlanWarnings(command.ErrOrStderr(), value.Warnings); err != nil {
				return fmt.Errorf("plan saved at %q; diagnostics failed: %w", path, err)
			}
			if err := renderPlan(command.OutOrStdout(), format, path, value, contents); err != nil {
				return fmt.Errorf("plan saved at %q; output failed: %w", path, err)
			}
			if buildErr != nil {
				return &incompletePlanError{planID: value.ID, cause: buildErr}
			}
			return nil
		},
	}
	command.Flags().StringArrayVar(&roots, "root", nil, "Repository discovery root (repeatable; defaults to configured roots or the containing repository)")
	command.Flags().StringVar(&threshold, "inactivity-threshold", "", "Required inactivity, such as 24h or 7d (default configuration: 7d)")
	command.Flags().StringVar(&format, "format", "human", "Output format: human or json")
	command.Flags().StringVar(&output, "output", "", "Export an independent private copy on the state filesystem; never overwrite")
	return command
}

func configuredPlanBuilder(ctx context.Context, dependencies Dependencies, configuration config.Config) (plan.Builder, plan.Request, error) {
	client := git.NewClient(execx.OSRunner{})
	client.BaseBranches = append([]string(nil), configuration.BaseBranches...)
	if dependencies.Inventory == nil || len(configuration.Roots) == 0 {
		if err := client.CheckVersion(ctx); err != nil {
			return plan.Builder{}, plan.Request{}, err
		}
	}
	if len(configuration.Roots) == 0 {
		root, err := client.WorktreeRoot(ctx, dependencies.WorkingDirectory)
		if err != nil {
			return plan.Builder{}, plan.Request{}, fmt.Errorf("no default repository scope; use --root <path> or configure roots to create a plan: %w", err)
		}
		configuration.Roots = []string{root}
	}
	roots := make([]string, len(configuration.Roots))
	for index, root := range configuration.Roots {
		canonical, err := pathutil.Canonical(root)
		if err != nil {
			return plan.Builder{}, plan.Request{}, err
		}
		roots[index] = canonical
	}
	loader := dependencies.Inventory
	if loader == nil {
		loader = inventory.Loader{
			Git: client, DataDir: dependencies.DataDirectory,
			CWD: func() (string, error) { return dependencies.WorkingDirectory, nil },
		}
	}
	collector := dependencies.Processes
	if collector == nil {
		collector = process.Collector{Source: process.NativeSource()}
	}
	return plan.Builder{Inventory: loader, Processes: collector, Now: dependencies.Now, Version: dependencies.BuildVersion}, plan.Request{
		Roots: roots, IntendedApplyMode: domain.ApplyInteractive,
		Settings: domain.PolicySettings{
			InactivityThreshold: configuration.InactivityThreshold, PlanExpiry: configuration.PlanExpiry,
			BaseBranches: append([]string(nil), configuration.BaseBranches...), MinimumTrustGrade: domain.TrustGrade(configuration.MinimumTrustGrade),
			SnapshotMaxBytes: configuration.SnapshotMaxBytes,
		},
	}, nil
}

func renderPlan(output io.Writer, format, path string, value domain.Plan, contents []byte) error {
	if format == "json" {
		_, err := fmt.Fprintln(output, string(contents))
		return err
	}
	if _, err := fmt.Fprintf(output, "Plan: %s\nSaved: %s\nExpires: %s\n", strconv.Quote(value.ID), strconv.Quote(path), value.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")); err != nil {
		return err
	}
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "CANDIDATE\tCLASSIFICATION\tACTION\tBRANCH\tPATH\tREASONS"); err != nil {
		return err
	}
	for _, candidate := range value.Candidates {
		var reasons []string
		for _, reason := range candidate.Decision.Reasons {
			reasons = append(reasons, reason.Code)
		}
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", candidate.ID, candidate.Decision.Classification, candidate.Action, strconv.Quote(candidate.Worktree.Branch), strconv.Quote(candidate.Worktree.Path), strings.Join(reasons, ",")); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(output, "Safe: %d; review: %d; protected: %d; reclaimable bytes: %d\nNo worktrees were removed.\n", value.Summary.Safe, value.Summary.Review, value.Summary.Protected, value.Summary.ReclaimableBytes)
	return err
}

func writePlanWarnings(output io.Writer, warnings []string) error {
	return writeWarningPreview(output, warnings, "warning", "see saved plan JSON for full diagnostics")
}

type incompletePlanError struct {
	planID string
	cause  error
}

func (failure *incompletePlanError) Error() string {
	return fmt.Sprintf("plan %s saved; collection incomplete; see saved plan JSON for full diagnostics", failure.planID)
}

func (failure *incompletePlanError) Unwrap() error {
	return failure.cause
}
