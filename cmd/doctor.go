package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/canhta/foreman/internal/config"
	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/tracker"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var quick bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Health check all configured providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			w := cmd.OutOrStdout()
			fmt.Fprintln(w, "Running health checks...")

			hasFailure := false
			check := func(name string, fn func() error) {
				fmt.Fprintf(w, "  %s... ", name)
				if err := fn(); err != nil {
					fmt.Fprintf(w, "FAIL: %s\n", err)
					hasFailure = true
				} else {
					fmt.Fprintln(w, "OK")
				}
			}

			// Load config
			cfg, err := config.LoadFromFile("foreman.toml")
			if err != nil {
				cfg, err = config.LoadDefaults()
				if err != nil {
					fmt.Fprintf(w, "  Config... FAIL: %s\n", err)
					os.Exit(1)
				}
			}
			fmt.Fprintln(w, "  Config... OK")

			// Database check
			check("Database", func() error {
				database, err := openDB(cfg)
				if err != nil {
					return err
				}
				return database.Close()
			})

			if quick {
				if hasFailure {
					os.Exit(1)
				}
				fmt.Fprintln(w, "\nQuick checks passed.")
				return nil
			}

			// Full checks
			check("LLM provider", func() error {
				provider, err := llm.NewProviderFromConfig(cfg.LLM.DefaultProvider, cfg.LLM)
				if err != nil {
					return err
				}
				return provider.HealthCheck(ctx)
			})

			check("Issue tracker", func() error {
				tr, err := buildTrackerForDoctor(cfg)
				if err != nil {
					return err
				}
				_, err = tr.FetchReadyTickets(ctx)
				return err
			})

			check("Git", func() error {
				if cfg.Git.CloneURL == "" {
					return fmt.Errorf("git.clone_url not configured")
				}
				return nil
			})

			check("Config validation", func() error {
				errs := config.Validate(cfg)
				if len(errs) > 0 {
					return errs[0]
				}
				return nil
			})

			// Docker network isolation advisory
			if cfg.Runner.Mode == "docker" {
				if !cfg.Runner.Docker.AllowNetwork {
					fmt.Fprintln(w, "  [WARN] Docker runner: network isolation is active (--network none). Set docker.allow_network = true to enable network access.")
				} else {
					fmt.Fprintln(w, "  [WARN] Docker runner: network isolation is DISABLED. Containers have network access.")
				}
			}

			// Skills
			fmt.Fprint(w, "  Skills... ")
			skillDir := filepath.Join(".", "skills")
			if _, err := os.Stat(skillDir); os.IsNotExist(err) {
				fmt.Fprintln(w, "no skills/ directory (OK)")
			} else {
				entries, _ := os.ReadDir(skillDir)
				validCount := 0
				for _, e := range entries {
					ext := filepath.Ext(e.Name())
					if ext == ".yml" || ext == ".yaml" {
						validCount++
					}
				}
				fmt.Fprintf(w, "%d skill files found (OK)\n", validCount)
			}

			if hasFailure {
				os.Exit(1)
			}
			fmt.Fprintln(w, "\nAll checks passed.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&quick, "quick", false, "Quick check (database only, for health checks)")
	return cmd
}

func buildTrackerForDoctor(cfg *models.Config) (tracker.IssueTracker, error) {
	token := os.Getenv("GITHUB_TOKEN")
	owner, repo := parseOwnerRepo(cfg.Git.CloneURL)

	switch cfg.Tracker.Provider {
	case "github":
		if token == "" {
			return nil, fmt.Errorf("GITHUB_TOKEN environment variable not set")
		}
		if owner == "" || repo == "" {
			return nil, fmt.Errorf("cannot parse owner/repo from clone_url: %s", cfg.Git.CloneURL)
		}
		return tracker.NewGitHubIssuesTracker("", token, owner, repo, cfg.Tracker.PickupLabel), nil
	case "jira":
		return nil, fmt.Errorf("jira doctor check not yet implemented")
	case "linear":
		return nil, fmt.Errorf("linear doctor check not yet implemented")
	case "local_file":
		return tracker.NewLocalFileTracker(".", cfg.Tracker.PickupLabel), nil
	default:
		return nil, fmt.Errorf("unknown tracker provider: %s", cfg.Tracker.Provider)
	}
}

func init() {
	rootCmd.AddCommand(newDoctorCmd())
}
