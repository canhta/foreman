package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	fcontext "github.com/canhta/foreman/internal/context"
)

var initAnalyze bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize foreman.toml in the current directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := filepath.Join(".", "foreman.toml")
		_, statErr := os.Stat(configPath)
		if statErr == nil {
			return fmt.Errorf("foreman.toml already exists")
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("check config: %w", statErr)
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
			gen := fcontext.NewGenerator(nil, "")
			content, err := gen.Generate(cmd.Context(), ".", fcontext.GenerateOptions{Offline: true})
			if err != nil {
				return fmt.Errorf("analyze: %w", err)
			}
			if err := os.WriteFile("AGENTS.md", []byte(content+"\n"), 0o644); err != nil {
				return fmt.Errorf("write AGENTS.md: %w", err)
			}
			fmt.Println("Generated AGENTS.md")
		}

		return nil
	},
}

func init() {
	initCmd.Flags().BoolVar(&initAnalyze, "analyze", false, "Scan repo and generate .foreman-context.md")
	rootCmd.AddCommand(initCmd)
}
