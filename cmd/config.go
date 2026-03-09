package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/canhta/foreman/internal/config"
	"github.com/canhta/foreman/internal/models"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show active configuration (redacted)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			if jsonOutput {
				summary := buildConfigSummaryMap(cfg)
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(summary)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 3, ' ', 0)

			// LLM
			fmt.Fprintln(w, "[LLM]")
			fmt.Fprintf(w, "  provider\t%s\n", cfg.LLM.DefaultProvider)
			fmt.Fprintf(w, "  planner\t%s\n", cfg.Models.Planner)
			fmt.Fprintf(w, "  implementer\t%s\n", cfg.Models.Implementer)
			fmt.Fprintf(w, "  spec_reviewer\t%s\n", cfg.Models.SpecReviewer)
			fmt.Fprintf(w, "  quality_reviewer\t%s\n", cfg.Models.QualityReviewer)
			fmt.Fprintf(w, "  final_reviewer\t%s\n", cfg.Models.FinalReviewer)
			apiKey := redactConfigKey(getActiveAPIKey(cfg))
			fmt.Fprintf(w, "  api_key\t%s\n", apiKey)
			fmt.Fprintln(w)

			// TRACKER
			fmt.Fprintln(w, "[TRACKER]")
			fmt.Fprintf(w, "  provider\t%s\n", cfg.Tracker.Provider)
			fmt.Fprintf(w, "  poll_interval\t%s\n", fmt.Sprintf("%ds", cfg.Daemon.PollIntervalSecs))
			fmt.Fprintln(w)

			// GIT
			fmt.Fprintln(w, "[GIT]")
			fmt.Fprintf(w, "  provider\t%s\n", cfg.Git.Provider)
			fmt.Fprintf(w, "  clone_url\t%s\n", cfg.Git.CloneURL)
			fmt.Fprintf(w, "  branch_prefix\t%s\n", cfg.Git.BranchPrefix)
			fmt.Fprintln(w)

			// AGENT RUNNER
			fmt.Fprintln(w, "[AGENT RUNNER]")
			fmt.Fprintf(w, "  provider\t%s\n", cfg.AgentRunner.Provider)
			fmt.Fprintf(w, "  max_turns\t%d\n", cfg.AgentRunner.MaxTurnsDefault)
			fmt.Fprintln(w)

			// COST BUDGETS
			fmt.Fprintln(w, "[COST BUDGETS]")
			fmt.Fprintf(w, "  daily\t$%.2f\n", cfg.Cost.MaxCostPerDayUSD)
			fmt.Fprintf(w, "  monthly\t$%.2f\n", cfg.Cost.MaxCostPerMonthUSD)
			fmt.Fprintf(w, "  per_ticket\t$%.2f\n", cfg.Cost.MaxCostPerTicketUSD)
			fmt.Fprintf(w, "  alert_threshold\t%s\n", fmt.Sprintf("%d%%", cfg.Cost.AlertThresholdPct))
			fmt.Fprintln(w)

			// DAEMON
			fmt.Fprintln(w, "[DAEMON]")
			fmt.Fprintf(w, "  max_parallel_tickets\t%d\n", cfg.Daemon.MaxParallelTickets)
			fmt.Fprintf(w, "  max_parallel_tasks\t%d\n", cfg.Daemon.MaxParallelTasks)
			fmt.Fprintf(w, "  work_dir\t%s\n", cfg.Daemon.WorkDir)
			fmt.Fprintf(w, "  log_level\t%s\n", cfg.Daemon.LogLevel)
			fmt.Fprintln(w)

			// DATABASE
			fmt.Fprintln(w, "[DATABASE]")
			fmt.Fprintf(w, "  driver\t%s\n", cfg.Database.Driver)
			switch cfg.Database.Driver {
			case "postgres":
				fmt.Fprintf(w, "  url\t%s\n", redactConfigKey(cfg.Database.Postgres.URL))
			default:
				fmt.Fprintf(w, "  path\t%s\n", cfg.Database.SQLite.Path)
			}
			fmt.Fprintln(w)

			// MCP SERVERS
			fmt.Fprintln(w, "[MCP SERVERS]")
			if len(cfg.MCP.Servers) == 0 {
				fmt.Fprintln(w, "  (none)")
			} else {
				names := make([]string, len(cfg.MCP.Servers))
				for i, s := range cfg.MCP.Servers {
					names[i] = s.Name
				}
				fmt.Fprintf(w, "  servers\t%s\n", strings.Join(names, ", "))
			}
			fmt.Fprintln(w)

			// RATE LIMIT
			fmt.Fprintln(w, "[RATE LIMIT]")
			fmt.Fprintf(w, "  requests_per_minute\t%d\n", cfg.RateLimit.RequestsPerMinute)

			return w.Flush()
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

// loadConfig loads configuration without requiring a running database.
func loadConfig() (*models.Config, error) {
	cfg, err := config.LoadFromFile("foreman.toml")
	if err != nil {
		cfg, err = config.LoadDefaults()
		if err != nil {
			return nil, fmt.Errorf("config: %w — run 'foreman doctor' to validate setup", err)
		}
	}
	return cfg, nil
}

// redactConfigKey redacts an API key or secret for display purposes.
// Defined locally in cmd package — separate from dashboard.redactKey.
func redactConfigKey(key string) string {
	if key == "" {
		return "(not set)"
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:7] + "..." + key[len(key)-4:]
}

// getActiveAPIKey returns the API key for the configured default LLM provider.
func getActiveAPIKey(cfg *models.Config) string {
	switch cfg.LLM.DefaultProvider {
	case "anthropic":
		return cfg.LLM.Anthropic.APIKey
	case "openai":
		return cfg.LLM.OpenAI.APIKey
	case "openrouter":
		return cfg.LLM.OpenRouter.APIKey
	default:
		return cfg.LLM.Local.APIKey
	}
}

// buildConfigSummaryMap builds a map[string]interface{} for JSON output.
func buildConfigSummaryMap(cfg *models.Config) map[string]interface{} {
	mcpNames := make([]string, len(cfg.MCP.Servers))
	for i, s := range cfg.MCP.Servers {
		mcpNames[i] = s.Name
	}

	dbInfo := map[string]string{
		"driver": cfg.Database.Driver,
	}
	switch cfg.Database.Driver {
	case "postgres":
		dbInfo["url"] = redactConfigKey(cfg.Database.Postgres.URL)
	default:
		dbInfo["path"] = cfg.Database.SQLite.Path
	}

	return map[string]interface{}{
		"llm": map[string]interface{}{
			"provider":         cfg.LLM.DefaultProvider,
			"planner":          cfg.Models.Planner,
			"implementer":      cfg.Models.Implementer,
			"spec_reviewer":    cfg.Models.SpecReviewer,
			"quality_reviewer": cfg.Models.QualityReviewer,
			"final_reviewer":   cfg.Models.FinalReviewer,
			"api_key":          redactConfigKey(getActiveAPIKey(cfg)),
		},
		"tracker": map[string]interface{}{
			"provider":      cfg.Tracker.Provider,
			"poll_interval": fmt.Sprintf("%ds", cfg.Daemon.PollIntervalSecs),
		},
		"git": map[string]interface{}{
			"provider":      cfg.Git.Provider,
			"clone_url":     cfg.Git.CloneURL,
			"branch_prefix": cfg.Git.BranchPrefix,
		},
		"agent_runner": map[string]interface{}{
			"provider":  cfg.AgentRunner.Provider,
			"max_turns": cfg.AgentRunner.MaxTurnsDefault,
		},
		"cost_budgets": map[string]interface{}{
			"daily":           fmt.Sprintf("$%.2f", cfg.Cost.MaxCostPerDayUSD),
			"monthly":         fmt.Sprintf("$%.2f", cfg.Cost.MaxCostPerMonthUSD),
			"per_ticket":      fmt.Sprintf("$%.2f", cfg.Cost.MaxCostPerTicketUSD),
			"alert_threshold": fmt.Sprintf("%d%%", cfg.Cost.AlertThresholdPct),
		},
		"daemon": map[string]interface{}{
			"max_parallel_tickets": cfg.Daemon.MaxParallelTickets,
			"max_parallel_tasks":   cfg.Daemon.MaxParallelTasks,
			"work_dir":             cfg.Daemon.WorkDir,
			"log_level":            cfg.Daemon.LogLevel,
		},
		"database": dbInfo,
		"mcp_servers": map[string]interface{}{
			"servers": mcpNames,
		},
		"rate_limit": map[string]interface{}{
			"requests_per_minute": cfg.RateLimit.RequestsPerMinute,
		},
	}
}

func init() {
	rootCmd.AddCommand(newConfigCmd())
}
