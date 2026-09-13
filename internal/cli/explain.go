package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/plan"
)

func newExplainCommand(dependencies Dependencies) *cobra.Command {
	var reference, format string
	command := &cobra.Command{
		Use:   "explain <candidate-id>",
		Short: "Inspect recorded reasons and evidence in an authenticated plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			if err := command.Context().Err(); err != nil {
				return err
			}
			if arguments[0] == "" {
				return errors.New("candidate ID must not be empty")
			}
			if format != "human" && format != "json" {
				return errors.New("--format must be human or json")
			}
			if command.Flags().Changed("plan") && reference == "" {
				return errors.New("--plan must be a nonempty ID or file path")
			}
			runtime, err := resolveDependencies(dependencies)
			if err != nil {
				return err
			}
			store := plan.NewStore(runtime.DataDirectory, runtime.Now, nil)
			var value domain.Plan
			if reference == "" {
				value, err = store.Latest(command.Context())
			} else {
				resolved := reference
				if !plan.IsID(resolved) && !filepath.IsAbs(resolved) {
					resolved = runtime.WorkingDirectory + string(filepath.Separator) + resolved
				}
				value, err = store.Load(command.Context(), resolved)
			}
			if err != nil {
				return err
			}
			var selected *domain.Candidate
			for index := range value.Candidates {
				if value.Candidates[index].ID == arguments[0] {
					if selected != nil {
						return fmt.Errorf("candidate %q is ambiguous in plan %q", arguments[0], value.ID)
					}
					selected = &value.Candidates[index]
				}
			}
			if selected == nil {
				return fmt.Errorf("candidate %q not found in plan %q", arguments[0], value.ID)
			}
			if err := writePlanWarnings(command.ErrOrStderr(), value.Warnings); err != nil {
				return err
			}
			return renderExplanation(command.OutOrStdout(), format, value.ID, *selected)
		},
	}
	command.Flags().StringVar(&reference, "plan", "", "Plan ID or private file path (default: newest authenticated unexpired plan)")
	command.Flags().StringVar(&format, "format", "human", "Output format: human or json")
	return command
}

func renderExplanation(output io.Writer, format, planID string, candidate domain.Candidate) error {
	if format == "json" {
		return json.NewEncoder(output).Encode(candidate)
	}
	if _, err := fmt.Fprintf(output, "Plan: %s\nCandidate: %s\nClassification: %s\nAction: %s\nFingerprint: %s\nInactive for: %s\nReasons:\n", strconv.Quote(planID), strconv.Quote(candidate.ID), strconv.Quote(string(candidate.Decision.Classification)), strconv.Quote(candidate.Action), strconv.Quote(candidate.Fingerprint), candidate.Decision.InactiveFor); err != nil {
		return err
	}
	for _, reason := range candidate.Decision.Reasons {
		if _, err := fmt.Fprintf(output, "  %s: %s\n", strconv.Quote(reason.Code), strconv.Quote(reason.Message)); err != nil {
			return err
		}
	}
	for _, section := range []struct {
		title string
		value any
	}{
		{"Git", candidate.Worktree}, {"Evidence", candidate.Evidence}, {"Snapshot", candidate.Snapshot},
	} {
		contents, err := json.MarshalIndent(section.value, "", "  ")
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "%s:\n%s\n", section.title, contents); err != nil {
			return err
		}
	}
	return nil
}
