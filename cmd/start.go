package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	var daemonMode bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Foreman daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if daemonMode {
				fmt.Fprintln(cmd.OutOrStdout(), "Starting Foreman daemon in background...")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Starting Foreman daemon in foreground...")
			}
			// Daemon wiring will happen in integration
			return nil
		},
	}

	cmd.Flags().BoolVar(&daemonMode, "daemon", false, "Run in background")
	return cmd
}

func init() {
	rootCmd.AddCommand(newStartCmd())
}
