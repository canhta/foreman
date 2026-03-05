package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the Foreman daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "Stopping Foreman daemon...")
			return nil
		},
	}
}

func init() {
	rootCmd.AddCommand(newStopCmd())
}
