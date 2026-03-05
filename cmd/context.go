package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/canhta/foreman/internal/config"
	fcontext "github.com/canhta/foreman/internal/context"
	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

func newContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Manage repository context for AI agents",
		Long:  "Generate and update AGENTS.md with repository-specific conventions for AI coding agents.",
	}

	cmd.AddCommand(newContextGenerateCmd())
	cmd.AddCommand(newContextUpdateCmd())

	return cmd
}

func newContextGenerateCmd() *cobra.Command {
	var (
		offline bool
		dryRun  bool
		force   bool
		output  string
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate AGENTS.md from repository analysis",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Non-interactive safety check
			if !force && fileExistsAt(output) {
				if !term.IsTerminal(int(os.Stdin.Fd())) {
					return fmt.Errorf("%s already exists; use --force to overwrite (non-interactive mode)", output)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s already exists. Use --force to overwrite.\n", output)
				return nil
			}

			var gen *fcontext.Generator
			if offline {
				gen = fcontext.NewGenerator(nil, "")
			} else {
				cfg, cfgErr := config.LoadFromFile("foreman.toml")
				if cfgErr != nil {
					cfg, cfgErr = config.LoadDefaults()
				}
				if cfgErr == nil {
					provider, provErr := llm.NewProviderFromConfig(cfg.LLM.DefaultProvider, cfg.LLM)
					if provErr != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not create LLM provider, falling back to offline: %v\n", provErr)
						offline = true
						gen = fcontext.NewGenerator(nil, "")
					} else {
						gen = fcontext.NewGenerator(provider, cfg.Models.Planner)
					}
				} else {
					offline = true
					gen = fcontext.NewGenerator(nil, "")
				}
			}

			content, err := gen.Generate(cmd.Context(), ".", fcontext.GenerateOptions{
				MaxTokens: 32000,
				Offline:   offline,
			})
			if err != nil {
				return fmt.Errorf("generate: %w", err)
			}

			if dryRun {
				fmt.Fprint(cmd.OutOrStdout(), content)
				return nil
			}

			if err := os.WriteFile(output, []byte(content+"\n"), 0o644); err != nil {
				return fmt.Errorf("write %s: %w", output, err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Generated %s\n", output)
			return nil
		},
	}

	cmd.Flags().BoolVar(&offline, "offline", false, "Generate without LLM (basic analysis only)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print output to stdout instead of writing file")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing file without confirmation")
	cmd.Flags().StringVar(&output, "output", "./AGENTS.md", "Output file path")

	return cmd
}

func newContextUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update AGENTS.md with new observations",
		Long:  "Read observations since last update and refresh AGENTS.md via LLM.",
		RunE: func(cmd *cobra.Command, args []string) error {
			agentsPath := "./AGENTS.md"

			content, err := os.ReadFile(agentsPath)
			if err != nil {
				return fmt.Errorf("read %s: %w (run 'foreman context generate' first)", agentsPath, err)
			}

			// Parse cursor from footer
			cursor := parseCursorFromFooter(string(content))

			// Read observations
			obsLog := fcontext.NewObservationLog(".")
			observations, newCursor, err := obsLog.ReadFrom(cursor)
			if err != nil {
				return fmt.Errorf("read observations: %w", err)
			}

			if len(observations) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No new observations since last update.")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Found %d new observations. Updating AGENTS.md...\n", len(observations))

			// Build observation summary for LLM
			var obsSummary strings.Builder
			for _, obs := range observations {
				obsSummary.WriteString(fmt.Sprintf("- [%s] %s", obs.Type, obs.File))
				for k, v := range obs.Details {
					obsSummary.WriteString(fmt.Sprintf(" %s=%s", k, v))
				}
				obsSummary.WriteByte('\n')
			}

			// Try LLM update; fall back to cursor-only update if unavailable
			cleaned := stripFooter(string(content))
			cfg, cfgErr := config.LoadFromFile("foreman.toml")
			if cfgErr != nil {
				cfg, cfgErr = config.LoadDefaults()
			}
			if cfgErr == nil {
				provider, provErr := llm.NewProviderFromConfig(cfg.LLM.DefaultProvider, cfg.LLM)
				if provErr == nil {
					resp, llmErr := provider.Complete(cmd.Context(), models.LlmRequest{
						SystemPrompt: "You are updating an AGENTS.md file for Foreman, an autonomous coding daemon. Incorporate the new observations into the existing file. Keep the same structure. Only add or modify sections where observations provide new information. Output the complete updated AGENTS.md.",
						UserPrompt:   fmt.Sprintf("## Current AGENTS.md\n\n%s\n\n## New Observations\n\n%s", cleaned, obsSummary.String()),
						Model:        cfg.Models.Planner,
						MaxTokens:    4096,
						Temperature:  0.2,
					})
					if llmErr == nil {
						cleaned = resp.Content
					} else {
						fmt.Fprintf(cmd.ErrOrStderr(), "Warning: LLM update failed, updating cursor only: %v\n", llmErr)
					}
				}
			}

			footer := fmt.Sprintf("\n<!--foreman:last-update:%s:observations-cursor:%d-->\n",
				time.Now().UTC().Format(time.RFC3339), newCursor)
			updated := cleaned + footer

			if err := os.WriteFile(agentsPath, []byte(updated), 0o644); err != nil {
				return fmt.Errorf("write %s: %w", agentsPath, err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Updated %s (cursor: %d)\n", agentsPath, newCursor)
			return nil
		},
	}

	return cmd
}

func fileExistsAt(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var cursorRe = regexp.MustCompile(`<!--foreman:last-update:[^:]+:observations-cursor:(\d+)-->`)

func parseCursorFromFooter(content string) int64 {
	matches := cursorRe.FindStringSubmatch(content)
	if len(matches) < 2 {
		return 0
	}
	v, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func stripFooter(content string) string {
	loc := cursorRe.FindStringIndex(content)
	if loc == nil {
		return content
	}
	return strings.TrimRight(content[:loc[0]], "\n") + "\n"
}

func init() {
	rootCmd.AddCommand(newContextCmd())
}
