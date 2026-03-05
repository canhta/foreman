package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a single ticket through the pipeline",
		Long:  "Run a specific ticket by external ID (e.g., PROJ-123 or GitHub issue number).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ticketID := args[0]
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Dry run for ticket: %s (plan only)\n", ticketID)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Running pipeline for ticket: %s\n", ticketID)
			// Pipeline execution will be wired here
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Plan only — show tasks, estimated cost, files")
	return cmd
}

func init() {
	rootCmd.AddCommand(newRunCmd())
}
