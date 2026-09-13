package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
)

type Dependencies struct {
	Stdout               io.Writer
	Stderr               io.Writer
	BuildVersion         string
	Inventory            InventoryLoader
	Processes            ProcessCollector
	Now                  func() time.Time
	WorkingDirectory     string
	UserConfigPath       string
	RepositoryConfigPath string
	DataDirectory        string
}

func NewRootCommand(dependencies Dependencies) *cobra.Command {
	root := &cobra.Command{
		Use:           "treeclear",
		Short:         "Safely clear stale agent worktrees",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			return command.Help()
		},
	}
	root.SetOut(dependencies.Stdout)
	root.SetErr(dependencies.Stderr)
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(newScanCommand(dependencies))
	root.AddCommand(newPlanCommand(dependencies))
	root.AddCommand(newExplainCommand(dependencies))
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the Treeclear version",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			_, err := fmt.Fprintln(command.OutOrStdout(), dependencies.BuildVersion)
			return err
		},
	})
	return root
}

func Execute(ctx context.Context, arguments []string, stdout, stderr io.Writer, buildVersion string) int {
	command := NewRootCommand(Dependencies{
		Stdout:       stdout,
		Stderr:       stderr,
		BuildVersion: buildVersion,
	})
	command.SetArgs(arguments)
	if err := command.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintf(command.ErrOrStderr(), "%q\n", err.Error())
		return 1
	}
	return 0
}
