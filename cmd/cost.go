package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Show cost breakdown",
		Long:  "Show cost breakdown: today, week, month, or per-ticket.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			period := args[0]
			fmt.Fprintf(cmd.OutOrStdout(), "Cost breakdown for: %s\n", period)
			fmt.Fprintln(cmd.OutOrStdout(), "(no data yet)")
			return nil
		},
	}
	return cmd
}

func init() {
	rootCmd.AddCommand(newCostCmd())
}
