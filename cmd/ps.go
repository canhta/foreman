package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newPsCmd() *cobra.Command {
	var showAll bool

	cmd := &cobra.Command{
		Use:   "ps",
		Short: "List active pipelines",
		RunE: func(cmd *cobra.Command, args []string) error {
			if showAll {
				fmt.Fprintln(cmd.OutOrStdout(), "All pipelines (including completed):")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Active pipelines:")
			}
			fmt.Fprintln(cmd.OutOrStdout(), "(none)")
			return nil
		},
	}

	cmd.Flags().BoolVar(&showAll, "all", false, "Show all pipelines including completed")
	return cmd
}

func init() {
	rootCmd.AddCommand(newPsCmd())
}
