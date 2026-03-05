package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var initAnalyze bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize foreman.toml in the current directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := filepath.Join(".", "foreman.toml")
		if _, err := os.Stat(configPath); err == nil {
			return fmt.Errorf("foreman.toml already exists")
		}

		template := `# Foreman configuration — see foreman.example.toml for all options

[daemon]
poll_interval_secs = 60
max_parallel_tickets = 3
work_dir = "~/.foreman/work"
log_level = "info"

[llm]
default_provider = "anthropic"

[llm.anthropic]
api_key = "${ANTHROPIC_API_KEY}"

[tracker]
provider = "github"
pickup_label = "foreman"

[git]
default_branch = "main"
branch_prefix = "foreman/"
pr_draft = true

[database]
driver = "sqlite"

[database.sqlite]
path = "~/.foreman/foreman.db"
`
		if err := os.WriteFile(configPath, []byte(template), 0o644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}

		fmt.Println("Created foreman.toml")

		if initAnalyze {
			fmt.Println("Analyzing repository...")
			// TODO: Wire to context.AnalyzeRepo() when available
			fmt.Println("Note: --analyze will generate .foreman-context.md in a future release")
		}

		return nil
	},
}

func init() {
	initCmd.Flags().BoolVar(&initAnalyze, "analyze", false, "Scan repo and generate .foreman-context.md")
	rootCmd.AddCommand(initCmd)
}
