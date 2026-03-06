package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/canhta/foreman/internal/channel"
	"github.com/canhta/foreman/internal/channel/whatsapp"
	"github.com/canhta/foreman/internal/daemon"
	"github.com/canhta/foreman/internal/dashboard"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/pipeline"
	"github.com/canhta/foreman/internal/runner"
	"github.com/canhta/foreman/internal/telemetry"
	"github.com/canhta/foreman/internal/tracker"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

// plannerAdapter wraps pipeline.Planner to satisfy daemon.TicketPlanner.
type plannerAdapter struct {
	planner *pipeline.Planner
}

func (a *plannerAdapter) Plan(ctx context.Context, workDir string, ticket *models.Ticket) (*daemon.PlanResult, error) {
	result, err := a.planner.Plan(ctx, workDir, ticket)
	if err != nil {
		return nil, err
	}
	// Convert pipeline types to daemon types.
	tasks := make([]daemon.PlannedTask, len(result.Tasks))
	for i, t := range result.Tasks {
		tasks[i] = daemon.PlannedTask{
			Title:               t.Title,
			Description:         t.Description,
			AcceptanceCriteria:  t.AcceptanceCriteria,
			TestAssertions:      t.TestAssertions,
			FilesToRead:         t.FilesToRead,
			FilesToModify:       t.FilesToModify,
			EstimatedComplexity: t.EstimatedComplexity,
			DependsOn:           t.DependsOn,
		}
	}
	return &daemon.PlanResult{
		Status:  result.Status,
		Message: result.Message,
		CodebasePatterns: daemon.CodebasePatterns{
			Language:   result.CodebasePatterns.Language,
			Framework:  result.CodebasePatterns.Framework,
			TestRunner: result.CodebasePatterns.TestRunner,
			StyleNotes: result.CodebasePatterns.StyleNotes,
		},
		Tasks: tasks,
	}, nil
}

// clarityAdapter wraps pipeline.Pipeline to satisfy daemon.ClarityChecker.
type clarityAdapter struct {
	pipeline *pipeline.Pipeline
}

func (a *clarityAdapter) CheckTicketClarity(ticket *models.Ticket) (bool, error) {
	return a.pipeline.CheckTicketClarity(ticket)
}

// taskRunnerFactory satisfies daemon.DAGTaskRunnerFactory.
type taskRunnerFactory struct {
	llm       pipeline.LLMProvider
	db        pipeline.TaskRunnerDB
	gitProv   git.GitProvider
	cmdRunner runner.CommandRunner
}

func (f *taskRunnerFactory) Create(input daemon.TaskRunnerFactoryInput) daemon.TaskRunner {
	tr := pipeline.NewPipelineTaskRunner(
		f.llm, f.db, f.gitProv, f.cmdRunner,
		pipeline.TaskRunnerConfig{
			Models:                   input.Models,
			WorkDir:                  input.WorkDir,
			CodebasePatterns:         input.CodebasePatterns,
			TestCommand:              input.TestCommand,
			MaxImplementationRetries: input.MaxImplementationRetries,
			MaxSpecReviewCycles:      input.MaxSpecReviewCycles,
			MaxQualityReviewCycles:   input.MaxQualityReviewCycles,
			MaxLlmCallsPerTask:       input.MaxLlmCallsPerTask,
			EnableTDDVerification:    input.EnableTDDVerification,
		},
	)
	return pipeline.NewDAGTaskAdapter(tr, f.db)
}

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Foreman daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. Load config and database.
			cfg, database, err := loadConfigAndDB()
			if err != nil {
				return err
			}
			defer database.Close()

			// 1b. Seed dashboard auth token from config (idempotent).
			if cfg.Dashboard.AuthToken != "" {
				h := sha256.Sum256([]byte(cfg.Dashboard.AuthToken))
				hashStr := hex.EncodeToString(h[:])
				valid, _ := database.ValidateAuthToken(cmd.Context(), hashStr)
				if !valid {
					if seedErr := database.CreateAuthToken(cmd.Context(), hashStr, "config"); seedErr != nil {
						log.Warn().Err(seedErr).Msg("failed to seed dashboard auth token")
					} else {
						log.Info().Msg("dashboard auth token seeded from config")
					}
				}
			}

			// 2. Initialize LLM provider.
			llmProv, err := llm.NewProviderFromConfig(cfg.LLM.DefaultProvider, cfg.LLM)
			if err != nil {
				return fmt.Errorf("LLM provider: %w", err)
			}

			// 3. Initialize tracker.
			tr, err := buildTracker(cfg)
			if err != nil {
				return fmt.Errorf("tracker: %w", err)
			}

			// 4. Initialize git provider.
			gitProv := buildGitProvider(cfg)

			// 5. Initialize PR creator and checker.
			prCreator := buildPRCreator(cfg)
			prChecker := buildPRChecker(cfg)

			// 6. Initialize command runner.
			cmdRunner := buildCommandRunner(cfg)

			// 7. Initialize cost controller and scheduler.
			costCtrl := telemetry.NewCostController(cfg.Cost)
			scheduler := daemon.NewScheduler(database)

			// 8. Build orchestrator adapters.
			planner := pipeline.NewPlanner(llmProv, &cfg.Limits)
			pipelineObj := pipeline.NewPipeline(pipeline.PipelineConfig{
				EnableClarification: cfg.Limits.EnableClarification,
			})

			orch := daemon.NewOrchestrator(
				database,
				tr,
				gitProv,
				prCreator,
				costCtrl,
				scheduler,
				&plannerAdapter{planner: planner},
				&clarityAdapter{pipeline: pipelineObj},
				&taskRunnerFactory{
					llm:       llmProv,
					db:        database,
					gitProv:   gitProv,
					cmdRunner: cmdRunner,
				},
				log.Logger,
				daemon.OrchestratorConfig{
					Models:                 cfg.Models,
					WorkDir:                cfg.Daemon.WorkDir,
					DefaultBranch:          cfg.Git.DefaultBranch,
					BranchPrefix:           cfg.Git.BranchPrefix,
					TestCommand:            "",
					ClarificationLabel:     cfg.Tracker.ClarificationLabel,
					PRReviewers:            cfg.Git.PRReviewers,
					MaxParallelTasks:       cfg.Daemon.MaxParallelTasks,
					TaskTimeoutMinutes:     cfg.Daemon.TaskTimeoutMinutes,
					MaxLlmCallsPerTask:     cfg.Cost.MaxLlmCallsPerTask,
					MaxImplementRetries:    cfg.Limits.MaxImplementationRetries,
					MaxSpecReviewCycles:    cfg.Limits.MaxSpecReviewCycles,
					MaxQualityReviewCycles: cfg.Limits.MaxQualityReviewCycles,
					ContextTokenBudget:     cfg.Limits.ContextTokenBudget,
					PRDraft:                cfg.Git.PRDraft,
					RebaseBeforePR:         cfg.Git.RebaseBeforePR,
					AutoPush:               cfg.Git.AutoPush,
					EnablePartialPR:        cfg.Limits.EnablePartialPR,
					EnableTDDVerification:  cfg.Limits.EnableTDDVerification,
					EnableClarification:    cfg.Limits.EnableClarification,
				},
			)

			// 9. Build daemon.
			d := daemon.NewDaemon(daemon.DaemonConfig{
				RunnerMode:                cfg.Runner.Mode,
				PollIntervalSecs:          cfg.Daemon.PollIntervalSecs,
				IdlePollIntervalSecs:      cfg.Daemon.IdlePollIntervalSecs,
				MaxParallelTickets:        cfg.Daemon.MaxParallelTickets,
				MaxParallelTasks:          cfg.Daemon.MaxParallelTasks,
				TaskTimeoutMinutes:        cfg.Daemon.TaskTimeoutMinutes,
				MergeCheckIntervalSecs:    cfg.Daemon.MergeCheckIntervalSecs,
				ClarificationTimeoutHours: cfg.Tracker.ClarificationTimeoutHours,
				ClarificationLabel:        cfg.Tracker.ClarificationLabel,
			})
			d.SetDB(database)
			d.SetTracker(tr)
			d.SetOrchestrator(orch)
			if prChecker != nil {
				d.SetPRChecker(prChecker)
			}

			// 9b. Initialize channel (optional).
			var ch channel.Channel
			if cfg.Channel.Provider == "whatsapp" {
				sessionDB := cfg.Channel.WhatsApp.SessionDB
				if sessionDB == "" {
					sessionDB = "~/.foreman/whatsapp.db"
				}
				sessionDB = expandHomePath(sessionDB)
				ch = whatsapp.New(sessionDB, log.Logger)
				orch.SetChannel(ch)
				d.SetChannel(ch)

				classifier := channel.NewClassifier(llmProv)
				allowlist := channel.NewAllowlist(cfg.Channel.WhatsApp.AllowedNumbers)
				var pairingMgr *channel.PairingManager
				if cfg.Channel.WhatsApp.DMPolicy == "pairing" {
					pairingMgr = channel.NewPairingManager(database, "whatsapp")
				}
				cmdHandler := daemon.NewDaemonCommandHandler(d)
				router := channel.NewRouter(ch, database, classifier, allowlist, pairingMgr, cmdHandler, log.Logger)
				d.SetChannelRouter(router)
			}

			// 9c. Wire event emitter to orchestrator (always, even without dashboard).
			emitter := telemetry.NewEventEmitter(database)
			orch.SetEventEmitter(emitter)

			// 10. Signal context.
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// 11. Dashboard in background.
			if cfg.Dashboard.Enabled {
				reg := prometheus.NewRegistry()
				_ = telemetry.NewMetrics(reg)
				port := cfg.Dashboard.Port
				if port == 0 {
					port = 3333
				}
				host := cfg.Dashboard.Host
				if host == "" {
					host = "127.0.0.1"
				}
				srv := dashboard.NewServer(database, emitter, d, reg, cfg.Cost, "0.1.0", host, port)
				srv.SetDaemonController(d)
				go func() {
					if err := srv.Start(); err != nil {
						log.Error().Err(err).Msg("dashboard server error")
					}
				}()
				go func() {
					<-ctx.Done()
					shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					_ = srv.Shutdown(shutCtx)
				}()
			}

			// 12. Start daemon (blocks until ctx cancelled).
			log.Info().Msg("Starting Foreman daemon")
			d.Start(ctx)

			// 13. Drain active pipelines.
			drainCtx, drainCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer drainCancel()
			d.WaitForDrain(drainCtx)
			log.Info().Msg("Foreman daemon stopped")

			return nil
		},
	}
	return cmd
}

// buildTracker creates an IssueTracker from config.
func buildTracker(cfg *models.Config) (tracker.IssueTracker, error) {
	token := os.Getenv("GITHUB_TOKEN")
	owner, repo := parseOwnerRepo(cfg.Git.CloneURL)

	switch cfg.Tracker.Provider {
	case "github":
		if token == "" {
			return nil, fmt.Errorf("GITHUB_TOKEN environment variable required for github tracker")
		}
		return tracker.NewGitHubIssuesTracker("", token, owner, repo, cfg.Tracker.PickupLabel), nil
	case "jira":
		return tracker.NewJiraTracker("", "", "", "", cfg.Tracker.PickupLabel), nil
	case "linear":
		return tracker.NewLinearTracker("", cfg.Tracker.PickupLabel, ""), nil
	case "local_file":
		return tracker.NewLocalFileTracker(".", cfg.Tracker.PickupLabel), nil
	default:
		return nil, fmt.Errorf("unknown tracker provider: %s", cfg.Tracker.Provider)
	}
}

func buildGitProvider(cfg *models.Config) git.GitProvider {
	if cfg.Git.Backend == "gogit" {
		return git.NewGoGitProvider()
	}
	return git.NewNativeGitProvider()
}

func buildPRCreator(cfg *models.Config) git.PRCreator {
	token := os.Getenv("GITHUB_TOKEN")
	owner, repo := parseOwnerRepo(cfg.Git.CloneURL)
	if owner == "" || repo == "" || token == "" {
		return nil
	}
	return git.NewGitHubPRCreator("", token, owner, repo)
}

func buildPRChecker(cfg *models.Config) git.PRChecker {
	token := os.Getenv("GITHUB_TOKEN")
	owner, repo := parseOwnerRepo(cfg.Git.CloneURL)
	if owner == "" || repo == "" || token == "" {
		return nil
	}
	return git.NewGitHubPRChecker("", token, owner, repo)
}

func buildCommandRunner(cfg *models.Config) runner.CommandRunner {
	switch cfg.Runner.Mode {
	case "docker":
		d := cfg.Runner.Docker
		return runner.NewDockerRunner(d.Image, d.PersistPerTicket, d.Network, d.CPULimit, d.MemoryLimit, d.AutoReinstallDeps)
	default:
		return runner.NewLocalRunner(&cfg.Runner.Local)
	}
}

func init() {
	rootCmd.AddCommand(newStartCmd())
}
