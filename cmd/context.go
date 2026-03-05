package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	fcontext "github.com/canhta/foreman/internal/context"
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

			// Try to create LLM provider; fall back to offline if it fails
			var gen *fcontext.Generator
			if offline {
				gen = fcontext.NewGenerator(nil, "")
			} else {
				// Attempt to create provider from config
				gen = fcontext.NewGenerator(nil, "")
				offline = true // fallback to offline for now
			}

			content, err := gen.Generate(cmd.Context(), ".", fcontext.GenerateOptions{
				MaxTokens: 120000,
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

			// For now, append a comment about observations (LLM integration to be wired)
			// In full implementation, this would make an LLM call to update the content
			footer := fmt.Sprintf("\n<!--foreman:last-update:%d:observations-cursor:%d-->\n",
				0, newCursor)

			// Strip old footer if present
			cleaned := stripFooter(string(content))
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

var cursorRe = regexp.MustCompile(`<!--foreman:last-update:\d+:observations-cursor:(\d+)-->`)

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
