package cmd

import (
	"fmt"
	"os"
	"path/filepath"

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

			// Validate skill files
			fmt.Fprint(cmd.OutOrStdout(), "  Skills... ")
			skillDir := filepath.Join(".", "skills")
			if _, err := os.Stat(skillDir); os.IsNotExist(err) {
				fmt.Fprintln(cmd.OutOrStdout(), "no skills/ directory (OK)")
			} else {
				entries, _ := os.ReadDir(skillDir)
				validCount := 0
				for _, e := range entries {
					if filepath.Ext(e.Name()) == ".yml" || filepath.Ext(e.Name()) == ".yaml" {
						validCount++
					}
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%d skill files found (OK)\n", validCount)
				// Full validation via skills.ValidateAll() when skills engine is wired
			}

			return nil
		},
	}
}

func init() {
	rootCmd.AddCommand(newDoctorCmd())
}
