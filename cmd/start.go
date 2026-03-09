package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/canhta/foreman/internal/agent"
	"github.com/canhta/foreman/internal/agent/mcp"
	"github.com/canhta/foreman/internal/channel"
	"github.com/canhta/foreman/internal/channel/whatsapp"
	"github.com/canhta/foreman/internal/daemon"
	"github.com/canhta/foreman/internal/dashboard"
	"github.com/canhta/foreman/internal/envloader"
	"github.com/canhta/foreman/internal/git"
	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/pipeline"
	"github.com/canhta/foreman/internal/prompts"
	"github.com/canhta/foreman/internal/runner"
	"github.com/canhta/foreman/internal/skills"
	"github.com/canhta/foreman/internal/sshkey"
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

// agentPlannerAdapter wraps pipeline.AgentPlanner to satisfy daemon.TicketPlanner.
type agentPlannerAdapter struct {
	planner *pipeline.AgentPlanner
}

func (a *agentPlannerAdapter) Plan(ctx context.Context, workDir string, ticket *models.Ticket) (*daemon.PlanResult, error) {
	result, err := a.planner.Plan(ctx, workDir, ticket)
	if err != nil {
		return nil, err
	}
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
	llm             pipeline.LLMProvider
	db              fullTaskRunnerDB
	gitProv         git.GitProvider
	cmdRunner       runner.CommandRunner
	metrics         *telemetry.Metrics
	agentRunner     agent.AgentRunner
	agentRunnerName string
	registry        *prompts.Registry
}

// fullTaskRunnerDB is the combined interface required by taskRunnerFactory.
// It satisfies both pipeline.TaskRunnerDB and pipeline.ConsistencyReviewDB.
type fullTaskRunnerDB interface {
	pipeline.TaskRunnerDB
	pipeline.ConsistencyReviewDB
}

func (f *taskRunnerFactory) Create(input daemon.TaskRunnerFactoryInput) daemon.TaskRunner {
	cfg := pipeline.TaskRunnerConfig{
		Models:                     input.Models,
		WorkDir:                    input.WorkDir,
		CodebasePatterns:           input.CodebasePatterns,
		TestCommand:                input.TestCommand,
		MaxImplementationRetries:   input.MaxImplementationRetries,
		MaxSpecReviewCycles:        input.MaxSpecReviewCycles,
		MaxQualityReviewCycles:     input.MaxQualityReviewCycles,
		MaxLlmCallsPerTask:         input.MaxLlmCallsPerTask,
		ContextTokenBudget:         input.ContextTokenBudget,
		ContextFeedbackBoost:       input.ContextFeedbackBoost,
		EnableTDDVerification:      input.EnableTDDVerification,
		IntermediateReviewInterval: input.IntermediateReviewInterval,
		Cache:                      input.ContextCache,
		PromptVersions:             input.PromptVersions,
		HookRunner:                 input.HookRunner,
		DiscoveryBoard:             input.DiscoveryBoard,
		AgentRunner:                f.agentRunner,
		AgentRunnerName:            f.agentRunnerName,
	}
	tr := pipeline.NewPipelineTaskRunner(f.llm, f.db, f.gitProv, f.cmdRunner, cfg)
	if f.registry != nil {
		tr.WithRegistry(f.registry)
	}
	if f.metrics != nil {
		tr.SetMetrics(f.metrics)
	}
	return pipeline.NewDAGTaskAdapterWithConsistency(tr, f.db, input.TicketID, f.llm, f.db, f.gitProv, cfg, input.BranchName, input.EnvFiles)
}

func newStartCmd() *cobra.Command {
	var dashboardPort int

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

			// 1c. Hash prompt templates and record snapshots (REQ-OBS-001).
			promptsDir := cfg.PromptsDir
			if promptsDir == "" {
				promptsDir = "prompts"
			}
			hashes, hashErr := telemetry.HashPromptTemplates(promptsDir)
			if hashErr != nil {
				log.Warn().Err(hashErr).Str("prompts_dir", promptsDir).Msg("could not hash prompt templates; skipping")
				hashes = nil
			} else {
				for name, sha := range hashes {
					if upsertErr := database.UpsertPromptSnapshot(cmd.Context(), name, sha); upsertErr != nil {
						log.Warn().Err(upsertErr).Str("template", name).Msg("failed to record prompt snapshot")
					}
				}
				log.Info().Int("count", len(hashes)).Str("prompts_dir", promptsDir).Msg("prompt templates hashed")
			}

			// 1d. Load prompt registry (graceful — nil registry if dir missing or empty).
			var promptRegistry *prompts.Registry
			if reg, regErr := prompts.Load(promptsDir); regErr != nil {
				log.Warn().Err(regErr).Str("prompts_dir", promptsDir).Msg("could not load prompt registry; pipeline components will use legacy prompts")
			} else {
				promptRegistry = reg
				log.Info().Str("prompts_dir", promptsDir).Msg("prompt registry loaded")
			}

			// 2. Initialize LLM provider.
			baseProv, err := llm.NewProviderFromConfig(cfg.LLM.DefaultProvider, cfg.LLM)
			if err != nil {
				return fmt.Errorf("LLM provider: %w", err)
			}
			var llmProv llm.LlmProvider = llm.NewCircuitBreakerProvider(baseProv, llm.DefaultCircuitBreakerConfig())
			// Build cost controller before the recording provider so it is available
			// from the first LLM call (no post-construction mutation window) (ARCH-O04).
			costCtrl := telemetry.NewCostController(cfg.Cost)
			recordingProv := llm.NewRecordingProvider(llmProv, database, costCtrl)
			llmProv = recordingProv

			// 3. Initialize tracker.
			tr, err := buildTracker(cfg)
			if err != nil {
				return fmt.Errorf("tracker: %w", err)
			}

			// 4. Initialize git provider and ensure the work repo is ready.
			gitProv := buildGitProvider(cfg)
			repoReady := gitProv.EnsureRepo(context.Background(), cfg.Daemon.WorkDir) == nil

			// 4b. Load user env files into process environment.
			if len(cfg.Daemon.EnvFiles) > 0 {
				if err := envloader.Load(cfg.Daemon.EnvFiles); err != nil {
					log.Warn().Err(err).Msg("failed to load env files at startup")
				} else {
					log.Info().Int("count", len(cfg.Daemon.EnvFiles)).Msg("env files loaded into process environment")
				}
			}

			// 5. Initialize PR creator and checker.
			prCreator := buildPRCreator(cfg)
			prChecker := buildPRChecker(cfg)

			// 6. Initialize command runner.
			cmdRunner := buildCommandRunner(cfg)

			// 6b. Initialize MCP manager (optional — only when [mcp] servers are configured).
			mcpMgr := buildMCPManager(cmd.Context(), cfg)
			if mcpMgr != nil {
				defer mcpMgr.Close()
			}

			// 7. Initialize scheduler.
			scheduler := daemon.NewScheduler(database)

			// 8. Build Prometheus registry and metrics (needed by planner and task runner).
			// Created here so metrics are available for all pipeline components.
			promReg := prometheus.NewRegistry()
			appMetrics := telemetry.NewMetrics(promReg)

			// 8b. Build pipeline agent runner (optional — only when provider != "builtin" and != "").
			var pipelineAgentRunner agent.AgentRunner
			agentRunnerName := cfg.AgentRunner.Provider
			if agentRunnerName != "" && agentRunnerName != "builtin" {
				var arErr error
				pipelineAgentRunner, arErr = agent.NewAgentRunner(
					cfg.AgentRunner, cmdRunner, llmProv, cfg.Models.Implementer,
					database, cfg.LLM, mcpMgr, appMetrics,
				)
				if arErr != nil {
					return fmt.Errorf("pipeline agent runner: %w", arErr)
				}
				// Wire prompt registry into ClaudeCodeRunner when applicable.
				if promptRegistry != nil {
					if ccr, ok := pipelineAgentRunner.(*agent.ClaudeCodeRunner); ok {
						ccr.WithRegistry(promptRegistry)
					}
				}
				if closer, ok := pipelineAgentRunner.(interface{ Close() error }); ok {
					defer closer.Close()
				}
				log.Info().Str("provider", agentRunnerName).Msg("pipeline agent runner initialized")
			}

			// 8c. Build orchestrator adapters — select planner based on agent runner.
			var ticketPlanner daemon.TicketPlanner
			if pipelineAgentRunner != nil {
				ap := pipeline.NewAgentPlanner(pipelineAgentRunner, &cfg.Limits)
				ticketPlanner = &agentPlannerAdapter{planner: ap}
				log.Info().Msg("using agent-based planner")
			} else {
				planner := pipeline.NewPlannerWithModel(llmProv, &cfg.Limits, cfg.Models.Planner).
					WithConfidenceScoring(cfg.Limits.PlanConfidenceThreshold).
					WithHandoffStore(database).
					WithMetrics(appMetrics)
				ticketPlanner = &plannerAdapter{planner: planner}
			}
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
				ticketPlanner,
				&clarityAdapter{pipeline: pipelineObj},
				&taskRunnerFactory{
					llm:             llmProv,
					db:              database,
					gitProv:         gitProv,
					cmdRunner:       cmdRunner,
					metrics:         appMetrics,
					agentRunner:     pipelineAgentRunner,
					agentRunnerName: agentRunnerName,
					registry:        promptRegistry,
				},
				log.Logger,
				daemon.OrchestratorConfig{
					Models:                     cfg.Models,
					WorkDir:                    cfg.Daemon.WorkDir,
					DefaultBranch:              cfg.Git.DefaultBranch,
					BranchPrefix:               cfg.Git.BranchPrefix,
					TestCommand:                "",
					ClarificationLabel:         cfg.Tracker.ClarificationLabel,
					PRReviewers:                cfg.Git.PRReviewers,
					MaxParallelTasks:           cfg.Daemon.MaxParallelTasks,
					TaskTimeoutMinutes:         cfg.Daemon.TaskTimeoutMinutes,
					MaxLlmCallsPerTask:         cfg.Cost.MaxLlmCallsPerTask,
					MaxImplementRetries:        cfg.Limits.MaxImplementationRetries,
					MaxSpecReviewCycles:        cfg.Limits.MaxSpecReviewCycles,
					MaxQualityReviewCycles:     cfg.Limits.MaxQualityReviewCycles,
					ContextTokenBudget:         cfg.Limits.ContextTokenBudget,
					ContextFeedbackBoost:       cfg.Context.ContextFeedbackBoost,
					PRDraft:                    cfg.Git.PRDraft,
					RebaseBeforePR:             cfg.Git.RebaseBeforePR,
					AutoPush:                   cfg.Git.AutoPush,
					EnablePartialPR:            cfg.Limits.EnablePartialPR,
					EnableTDDVerification:      cfg.Limits.EnableTDDVerification,
					EnableClarification:        cfg.Limits.EnableClarification,
					IntermediateReviewInterval: cfg.Limits.IntermediateReviewInterval,
					PromptVersions:             hashes,
					EnvFiles:                   cfg.Daemon.EnvFiles,
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
				LockTTLSeconds:            cfg.Daemon.LockTTLSeconds,
			})
			d.SetDB(database)
			d.SetTracker(tr)
			d.SetOrchestrator(orch)
			d.SetScheduler(scheduler)
			d.SetRepoReady(repoReady)
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
			// appMetrics and promReg were created in step 8 above.
			emitter := telemetry.NewEventEmitter(database)
			emitter.SetDroppedCounter(appMetrics.EventsDroppedTotal)
			orch.SetEventEmitter(emitter)

			// 9d. Build skill hook runner (best-effort — non-fatal if skills dir missing).
			// Skills are loaded from "./skills" in the working directory, and also
			// from the prompt registry (skills/ subdirectory) when available.
			// If neither source exists, hooks are silently disabled.
			{
				skillsDir := "./skills"
				loadedSkills, loadErr := skills.LoadSkillsDir(skillsDir)
				if loadErr != nil {
					log.Warn().Err(loadErr).Str("skills_dir", skillsDir).Msg("failed to load skills directory; skill hooks disabled")
					loadedSkills = nil
				}

				// Supplement YAML skills with registry-based skills (SKILL.md files).
				if promptRegistry != nil {
					regSkills, regErr := skills.LoadFromRegistry(promptRegistry)
					if regErr != nil {
						log.Warn().Err(regErr).Msg("failed to load skills from prompt registry; skipping registry skills")
					} else if len(regSkills) > 0 {
						loadedSkills = append(loadedSkills, regSkills...)
						log.Info().Int("count", len(regSkills)).Msg("registry skills loaded")
					}
				}

				if len(loadedSkills) > 0 {
					engine := skills.NewEngine(llmProv, cmdRunner, cfg.Daemon.WorkDir, cfg.Git.DefaultBranch)
					hr := skills.NewHookRunner(engine, loadedSkills)
					orch.SetHookRunner(hr)
					d.SetHookRunner(hr)
					d.SetSkillEventEmitter(emitter)
					log.Info().Int("count", len(loadedSkills)).Str("skills_dir", skillsDir).Msg("skill hooks registered")
				}
			}

			// 10. Signal context.
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Write PID file so `foreman status` can detect the running daemon.
			pidFile := pidFilePath()
			if err := os.MkdirAll(filepath.Dir(pidFile), 0o755); err == nil {
				_ = os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644)
				defer os.Remove(pidFile)
			}

			// 11. Dashboard in background.
			if cfg.Dashboard.Enabled {
				port := cfg.Dashboard.Port
				if dashboardPort > 0 {
					port = dashboardPort
				}
				host := cfg.Dashboard.Host
				srv := dashboard.NewServer(database, emitter, d, promReg, cfg.Cost, "0.1.0", host, port)
				srv.SetConfigProvider(&staticConfigProvider{cfg: cfg})
				srv.SetDaemonController(d)
				srv.SetTrackerSyncer(d)
				srv.SetPromptSnapshotQuerier(database)
				go func() {
					if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	cmd.Flags().IntVar(&dashboardPort, "dashboard-port", 0, "Override dashboard port")
	return cmd
}

// buildTracker creates an IssueTracker from config.
func buildTracker(cfg *models.Config) (tracker.IssueTracker, error) {
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
			owner, repo = parseOwnerRepo(cfg.Git.CloneURL) // fallback
		}
		return tracker.NewGitHubIssuesTracker(gh.BaseURL, token, owner, repo, t.PickupLabel), nil
	case "jira":
		j := t.Jira
		if j.BaseURL == "" {
			return nil, fmt.Errorf("tracker.jira.base_url is required")
		}
		if j.APIToken == "" {
			return nil, fmt.Errorf("tracker.jira.api_token is required")
		}
		if j.ProjectKey == "" {
			return nil, fmt.Errorf("tracker.jira.project_key is required")
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

func buildGitProvider(cfg *models.Config) git.GitProvider {
	if cfg.Git.Backend == "gogit" {
		return git.NewGoGitProvider()
	}
	p := git.NewNativeGitProviderWithClone(cfg.Git.CloneURL)

	// HTTPS clone URL → inject GitHub PAT via credential helper (works even
	// when org deploy keys are disabled).
	if strings.HasPrefix(cfg.Git.CloneURL, "https://") {
		token := cfg.Git.GitHub.Token
		if token == "" {
			token = os.Getenv("GITHUB_TOKEN")
		}
		if token != "" {
			return p.WithHTTPToken(token)
		}
		return p
	}

	// SSH clone URL → inject the Foreman dedicated key if present.
	if dir, err := sshkey.DefaultDir(); err == nil {
		if kp, err := sshkey.Ensure(dir); err == nil {
			return p.WithSSHKey(kp.PrivateKeyPath)
		}
	}
	return p
}

func buildPRCreator(cfg *models.Config) git.PRCreator {
	token := cfg.Git.GitHub.Token
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN") // backwards compat fallback
	}
	if token == "" {
		token = cfg.Tracker.GitHub.Token // reuse tracker token if git token not set
	}
	owner, repo := parseOwnerRepo(cfg.Git.CloneURL)
	if owner == "" || repo == "" || token == "" {
		return nil
	}
	return git.NewGitHubPRCreator(cfg.Git.GitHub.BaseURL, token, owner, repo)
}

func buildPRChecker(cfg *models.Config) git.PRChecker {
	token := cfg.Git.GitHub.Token
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN") // backwards compat fallback
	}
	if token == "" {
		token = cfg.Tracker.GitHub.Token // reuse tracker token if git token not set
	}
	owner, repo := parseOwnerRepo(cfg.Git.CloneURL)
	if owner == "" || repo == "" || token == "" {
		return nil
	}
	return git.NewGitHubPRChecker(cfg.Git.GitHub.BaseURL, token, owner, repo)
}

func buildCommandRunner(cfg *models.Config) runner.CommandRunner {
	switch cfg.Runner.Mode {
	case "docker":
		d := cfg.Runner.Docker
		return runner.NewDockerRunner(d.Image, d.PersistPerTicket, d.Network, d.CPULimit, d.MemoryLimit, d.AutoReinstallDeps, d.AllowNetwork)
	default:
		return runner.NewLocalRunner(&cfg.Runner.Local)
	}
}

// buildMCPManager initialises an MCP Manager from the [mcp] config section.
// Each entry with a non-empty Command is registered as a stdio client.
// The manager's tool cache is populated via CacheToolSummaries before returning.
// Returns nil (not an error) when no servers are configured.
func buildMCPManager(ctx context.Context, cfg *models.Config) *mcp.Manager {
	if len(cfg.MCP.Servers) == 0 {
		return nil
	}
	mgr := mcp.NewManager()
	for _, entry := range cfg.MCP.Servers {
		if entry.Command == "" {
			log.Warn().Str("server", entry.Name).Msg("mcp: skipping server with no command")
			continue
		}
		serverCfg := mcp.MCPServerConfig{
			Name:          entry.Name,
			Command:       entry.Command,
			Args:          entry.Args,
			Env:           entry.Env,
			RestartPolicy: entry.RestartPolicy,
			Transport:     "stdio",
			AllowedTools:  entry.AllowedTools,
		}
		if entry.MaxRestarts != 0 {
			v := entry.MaxRestarts
			serverCfg.MaxRestarts = &v
		}
		if entry.RestartDelaySecs != 0 {
			v := entry.RestartDelaySecs
			serverCfg.RestartDelaySecs = &v
		}
		transport, err := mcp.NewProcessTransport(serverCfg)
		if err != nil {
			log.Warn().Err(err).Str("server", entry.Name).Msg("mcp: failed to start stdio transport")
			continue
		}
		client := mcp.NewStdioClientWithTransport(transport, entry.Name)
		if err := client.Initialize(ctx); err != nil {
			log.Warn().Err(err).Str("server", entry.Name).Msg("mcp: failed to initialize stdio client")
			_ = client.Close()
			continue
		}
		mgr.RegisterClient(entry.Name, client)
		log.Info().Str("server", entry.Name).Msg("mcp: registered stdio server")
	}
	mgr.CacheToolSummaries(ctx)
	if cfg.MCP.ResourceMaxBytes > 0 {
		mgr.SetResourceMaxBytes(cfg.MCP.ResourceMaxBytes)
	}
	return mgr
}

func init() {
	rootCmd.AddCommand(newStartCmd())
}
