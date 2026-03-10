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
	Short: "Initialize ~/.foreman/config.toml (global system config)",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("home dir: %w", err)
		}
		foremanDir := filepath.Join(home, ".foreman")
		if err := os.MkdirAll(foremanDir, 0o700); err != nil {
			return fmt.Errorf("create ~/.foreman: %w", err)
		}

		configPath := filepath.Join(foremanDir, "config.toml")
		_, statErr := os.Stat(configPath)
		if statErr == nil {
			return fmt.Errorf("~/.foreman/config.toml already exists")
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("check config: %w", statErr)
		}

		template := `# Foreman global config — see foreman.system.example.toml for all options
# Add projects with: foreman project create

[daemon]
poll_interval_secs      = 60
idle_poll_interval_secs = 300
log_level               = "info"
log_format              = "json"
lock_ttl_seconds        = 3600

[dashboard]
enabled    = true
port       = 8080
host       = "127.0.0.1"
auth_token = "${FOREMAN_DASHBOARD_TOKEN}"

[llm]
default_provider = "anthropic"

[llm.anthropic]
api_key = "${ANTHROPIC_API_KEY}"

[cost]
max_cost_per_day_usd   = 150.0
max_cost_per_month_usd = 3000.0
`
		if err := os.WriteFile(configPath, []byte(template), 0o600); err != nil {
			return fmt.Errorf("write config: %w", err)
		}

		fmt.Println("Created ~/.foreman/config.toml")

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
	initCmd.Flags().BoolVar(&initAnalyze, "analyze", false, "Scan repo and generate AGENTS.md")
	rootCmd.AddCommand(initCmd)
}
