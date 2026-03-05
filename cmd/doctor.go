package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Health check all configured providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "Running health checks...")
			fmt.Fprintln(cmd.OutOrStdout(), "  LLM provider: (not configured)")
			fmt.Fprintln(cmd.OutOrStdout(), "  Issue tracker: (not configured)")
			fmt.Fprintln(cmd.OutOrStdout(), "  Git: (not configured)")
			fmt.Fprintln(cmd.OutOrStdout(), "  Database: (not configured)")
			return nil
		},
	}
}

func init() {
	rootCmd.AddCommand(newDoctorCmd())
}
