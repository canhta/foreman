package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

// CopilotConfig holds configuration for the Copilot SDK runner.
type CopilotConfig struct {
	Provider            *copilot.ProviderConfig
	CLIPath             string
	GitHubToken         string
	Model               string
	DefaultAllowedTools []string
	TimeoutSecsDefault  int
}

// CopilotRunner uses the GitHub Copilot Go SDK (native, no subprocess).
// Manages a Copilot CLI process via the SDK's auto-start lifecycle.
type CopilotRunner struct {
	client *copilot.Client
	config CopilotConfig
}

// NewCopilotRunner creates and starts a Copilot SDK client.
func NewCopilotRunner(cfg CopilotConfig) (*CopilotRunner, error) {
	opts := &copilot.ClientOptions{}
	if cfg.CLIPath != "" {
		opts.CLIPath = cfg.CLIPath
	}
	if cfg.GitHubToken != "" {
		opts.GitHubToken = cfg.GitHubToken
	}

	client := copilot.NewClient(opts)
	if err := client.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("copilot: failed to start client: %w", err)
	}

	return &CopilotRunner{client: client, config: cfg}, nil
}

func (r *CopilotRunner) RunnerName() string { return "copilot" }

func (r *CopilotRunner) Run(ctx context.Context, req AgentRequest) (AgentResult, error) {
	sessionCfg := r.buildSessionConfig(req)

	session, err := r.client.CreateSession(ctx, sessionCfg)
	if err != nil {
		return AgentResult{}, fmt.Errorf("copilot: create session: %w", err)
	}
	defer func() { _ = session.Destroy() }()

	// Collect usage from assistant.usage events
	var usage AgentUsage
	var mu sync.Mutex
	unsubscribe := session.On(func(event copilot.SessionEvent) {
		if event.Type == copilot.AssistantUsage {
			mu.Lock()
			defer mu.Unlock()
			if event.Data.InputTokens != nil {
				usage.InputTokens += int(*event.Data.InputTokens)
			}
			if event.Data.OutputTokens != nil {
				usage.OutputTokens += int(*event.Data.OutputTokens)
			}
		}
	})
	defer unsubscribe()

	taskCtx, cancel := context.WithTimeout(ctx, r.resolveTimeout(req))
	defer cancel()

	result, err := session.SendAndWait(taskCtx, copilot.MessageOptions{
		Prompt: req.Prompt,
	})
	if err != nil {
		return AgentResult{}, fmt.Errorf("copilot: %w", err)
	}

	output := ""
	if result != nil && result.Data.Content != nil {
		output = *result.Data.Content
	}

	usage.Model = r.config.Model
	return enrichResult(AgentResult{Output: output, Usage: usage}), nil
}

func (r *CopilotRunner) HealthCheck(ctx context.Context) error {
	_, err := r.client.Ping(ctx, "foreman-health")
	return err
}

func (r *CopilotRunner) ConfiguredModel() string { return r.config.Model }

func (r *CopilotRunner) Close() error {
	if r.client != nil {
		return r.client.Stop()
	}
	return nil
}

// buildSessionConfig assembles a SessionConfig from request + runner config.
func (r *CopilotRunner) buildSessionConfig(req AgentRequest) *copilot.SessionConfig {
	cfg := &copilot.SessionConfig{
		OnPermissionRequest: copilot.PermissionHandler.ApproveAll,
		Model:               r.config.Model,
		WorkingDirectory:    req.WorkDir,
		InfiniteSessions:    &copilot.InfiniteSessionConfig{Enabled: copilot.Bool(false)},
	}

	tools := req.AllowedTools
	if len(tools) == 0 {
		tools = r.config.DefaultAllowedTools
	}
	cfg.AvailableTools = tools

	if r.config.Provider != nil {
		cfg.Provider = r.config.Provider
	}

	if req.SystemPrompt != "" {
		cfg.SystemMessage = &copilot.SystemMessageConfig{
			Mode:    "append",
			Content: req.SystemPrompt,
		}
	}

	return cfg
}

// resolveTimeout returns the effective timeout for a request.
func (r *CopilotRunner) resolveTimeout(req AgentRequest) time.Duration {
	if req.TimeoutSecs > 0 {
		return time.Duration(req.TimeoutSecs) * time.Second
	}
	if r.config.TimeoutSecsDefault > 0 {
		return time.Duration(r.config.TimeoutSecsDefault) * time.Second
	}
	return 120 * time.Second
}
