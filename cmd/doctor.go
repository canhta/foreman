package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/canhta/foreman/internal/config"
	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/sshkey"
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

			check("Git config", func() error {
				if cfg.Git.CloneURL == "" {
					return fmt.Errorf("git.clone_url not configured")
				}
				return nil
			})

			check("SSH key", func() error {
				dir, err := sshkey.DefaultDir()
				if err != nil {
					return err
				}
				kp, err := sshkey.Ensure(dir)
				if err != nil {
					return fmt.Errorf("key not ready: %w", err)
				}
				// Probe SSH connectivity to the git host derived from clone_url.
				host := sshHostFromURL(cfg.Git.CloneURL)
				if host == "" {
					return fmt.Errorf("cannot determine SSH host from clone_url %q", cfg.Git.CloneURL)
				}
				sshArgs := []string{
					"-i", kp.PrivateKeyPath,
					"-o", "StrictHostKeyChecking=accept-new",
					"-o", "BatchMode=yes",
					"-o", "IdentitiesOnly=yes",
					"-o", "ConnectTimeout=10",
					"-T", host,
				}
				c := exec.CommandContext(ctx, "ssh", sshArgs...)
				out, _ := c.CombinedOutput()
				// GitHub/GitLab respond with a greeting on stderr even for auth
				// failures at the shell level; exit code 1 + "successfully authenticated"
				// means key is accepted but no interactive shell — that's success.
				outStr := string(out)
				if strings.Contains(outStr, "successfully authenticated") ||
					strings.Contains(outStr, "Welcome to GitLab") {
					return nil
				}
				if strings.Contains(outStr, "Permission denied") {
					return fmt.Errorf("key not authorized — add the public key as a Deploy Key on the repo\n  run: foreman setup-ssh")
				}
				// Any other non-zero exit is a connectivity issue, not an auth issue.
				if c.ProcessState != nil && c.ProcessState.ExitCode() != 0 && c.ProcessState.ExitCode() != 1 {
					return fmt.Errorf("SSH connectivity failed: %s", strings.TrimSpace(outStr))
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
	t := cfg.Tracker
	switch t.Provider {
	case "github":
		gh := t.GitHub
		token := gh.Token
		if token == "" {
			token = os.Getenv("GITHUB_TOKEN") // fallback for backwards compat
		}
		if token == "" {
			return nil, fmt.Errorf("tracker.github.token (or GITHUB_TOKEN env) required for github tracker")
		}
		owner, repo := gh.Owner, gh.Repo
		if owner == "" || repo == "" {
			owner, repo = parseOwnerRepo(cfg.Git.CloneURL)
		}
		if owner == "" || repo == "" {
			return nil, fmt.Errorf("cannot parse owner/repo from clone_url: %s", cfg.Git.CloneURL)
		}
		return tracker.NewGitHubIssuesTracker(gh.BaseURL, token, owner, repo, t.PickupLabel), nil
	case "jira":
		j := t.Jira
		if j.BaseURL == "" || j.APIToken == "" || j.ProjectKey == "" {
			return nil, fmt.Errorf("tracker.jira.base_url, api_token, and project_key are required")
		}
		return tracker.NewJiraTracker(j.BaseURL, j.Email, j.APIToken, j.ProjectKey, t.PickupLabel), nil
	case "linear":
		l := t.Linear
		if l.APIKey == "" {
			return nil, fmt.Errorf("tracker.linear.api_key is required")
		}
		return tracker.NewLinearTracker(l.APIKey, t.PickupLabel, l.BaseURL), nil
	case "local_file":
		path := t.LocalFile.Path
		if path == "" {
			path = "./tickets"
		}
		return tracker.NewLocalFileTracker(path, t.PickupLabel), nil
	default:
		return nil, fmt.Errorf("unknown tracker provider: %s", t.Provider)
	}
}

func init() {
	rootCmd.AddCommand(newDoctorCmd())
}
