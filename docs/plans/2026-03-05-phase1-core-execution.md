# Phase 1: Core Execution — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the foundation: give Foreman one task + files → get working TDD code back. File selector returns relevant files. Secrets scanner excludes sensitive files.

**Architecture:** Bottom-up build of domain models, interfaces, and core implementations. Each package is independently testable via Go interfaces. SQLite for persistence, native git CLI for git ops, Anthropic as first LLM provider.

**Tech Stack:** Go 1.23+, SQLite (go-sqlite3), cobra/viper (CLI/config), zerolog (logging), pongo2 (templates), tiktoken-go (token counting), strutil (fuzzy matching)

---

### Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `Makefile`
- Create: `foreman.example.toml`

**Step 1: Initialize Go module and install dependencies**

```bash
cd /Users/canh/Projects/Indies/Foreman
go mod init github.com/canhta/foreman
```

**Step 2: Create main.go entry point**

```go
package main

import (
	"fmt"
	"os"

	"github.com/canhta/foreman/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

**Step 3: Create minimal cmd/root.go**

```go
package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "foreman",
	Short: "Autonomous software development daemon",
	Long:  "An autonomous coding daemon that turns issue tracker tickets into tested, reviewed pull requests.",
}

func Execute() error {
	return rootCmd.Execute()
}
```

**Step 4: Create Makefile**

```makefile
.PHONY: build test lint clean

BINARY := foreman

build:
	go build -o $(BINARY) .

test:
	go test ./... -v -race

lint:
	go vet ./...
	golangci-lint run

clean:
	rm -f $(BINARY)
```

**Step 5: Install core dependencies and verify build**

```bash
go get github.com/spf13/cobra@v1.8.0
go get github.com/spf13/viper@v1.18.0
go get github.com/rs/zerolog@v1.32.0
go get github.com/google/uuid@v1.6.0
go mod tidy
go build -o foreman .
./foreman --help
```

Expected: Help text prints successfully.

**Step 6: Commit**

```bash
git add go.mod go.sum main.go cmd/root.go Makefile
git commit -m "feat: project scaffolding with cobra CLI"
```

---

### Task 2: Domain Models

**Files:**
- Create: `internal/models/ticket.go`
- Create: `internal/models/pipeline.go`
- Create: `internal/models/config.go`

**Step 1: Create ticket and task domain models**

```go
// internal/models/ticket.go
package models

import "time"

type Ticket struct {
	ID                       string
	ExternalID               string
	Title                    string
	Description              string
	AcceptanceCriteria       string
	Labels                   []string
	Priority                 string
	Assignee                 string
	Reporter                 string
	Comments                 []TicketComment
	Status                   TicketStatus
	ExternalStatus           string
	RepoURL                  string
	BranchName               string
	PRURL                    string
	PRNumber                 int
	IsPartial                bool
	CostUSD                  float64
	TokensInput              int
	TokensOutput             int
	TotalLlmCalls            int
	ClarificationRequestedAt *time.Time
	ErrorMessage             string
	LastCompletedTaskSeq     int
	CreatedAt                time.Time
	StartedAt                *time.Time
	CompletedAt              *time.Time
	UpdatedAt                time.Time
}

type TicketComment struct {
	Author    string
	Body      string
	CreatedAt time.Time
}

type Task struct {
	ID                     string
	TicketID               string
	Sequence               int
	Title                  string
	Description            string
	AcceptanceCriteria     []string
	FilesToRead            []string
	FilesToModify          []string
	TestAssertions         []string
	EstimatedComplexity    string
	DependsOn              []string
	Status                 TaskStatus
	ImplementationAttempts int
	SpecReviewAttempts     int
	QualityReviewAttempts  int
	TotalLlmCalls          int
	CommitSHA              string
	CostUSD                float64
	CreatedAt              time.Time
	StartedAt              *time.Time
	CompletedAt            *time.Time
}

type LlmCallRecord struct {
	ID              string
	TicketID        string
	TaskID          string
	Role            string
	Provider        string
	Model           string
	Attempt         int
	TokensInput     int
	TokensOutput    int
	CostUSD         float64
	DurationMs      int64
	PromptHash      string
	ResponseSummary string
	Status          string
	ErrorMessage    string
	CreatedAt       time.Time
}

type HandoffRecord struct {
	ID        string
	TicketID  string
	FromRole  string
	ToRole    string
	Key       string
	Value     string
	CreatedAt time.Time
}

type ProgressPattern struct {
	ID               string
	TicketID         string
	PatternKey       string
	PatternValue     string
	Directories      []string
	DiscoveredByTask string
	CreatedAt        time.Time
}

type EventRecord struct {
	ID        string
	TicketID  string
	TaskID    string
	EventType string
	Severity  string
	Message   string
	Details   string
	CreatedAt time.Time
}

type TicketFilter struct {
	Status   string
	StatusIn []string
}
```

**Step 2: Create pipeline state enums**

```go
// internal/models/pipeline.go
package models

type TicketStatus string

const (
	TicketStatusQueued                TicketStatus = "queued"
	TicketStatusClarificationNeeded  TicketStatus = "clarification_needed"
	TicketStatusPlanning             TicketStatus = "planning"
	TicketStatusPlanValidating       TicketStatus = "plan_validating"
	TicketStatusImplementing         TicketStatus = "implementing"
	TicketStatusReviewing            TicketStatus = "reviewing"
	TicketStatusPRCreated            TicketStatus = "pr_created"
	TicketStatusDone                 TicketStatus = "done"
	TicketStatusPartial              TicketStatus = "partial"
	TicketStatusFailed               TicketStatus = "failed"
	TicketStatusBlocked              TicketStatus = "blocked"
)

type TaskStatus string

const (
	TaskStatusPending       TaskStatus = "pending"
	TaskStatusImplementing  TaskStatus = "implementing"
	TaskStatusTDDVerifying  TaskStatus = "tdd_verifying"
	TaskStatusTesting       TaskStatus = "testing"
	TaskStatusSpecReview    TaskStatus = "spec_review"
	TaskStatusQualityReview TaskStatus = "quality_review"
	TaskStatusDone          TaskStatus = "done"
	TaskStatusFailed        TaskStatus = "failed"
	TaskStatusSkipped       TaskStatus = "skipped"
)

type StopReason string

const (
	StopReasonEndTurn      StopReason = "end_turn"
	StopReasonMaxTokens    StopReason = "max_tokens"
	StopReasonStopSequence StopReason = "stop_sequence"
)
```

**Step 3: Create config structs**

```go
// internal/models/config.go
package models

type Config struct {
	Daemon    DaemonConfig    `mapstructure:"daemon"`
	Dashboard DashboardConfig `mapstructure:"dashboard"`
	Tracker   TrackerConfig   `mapstructure:"tracker"`
	Git       GitConfig       `mapstructure:"git"`
	LLM       LLMConfig       `mapstructure:"llm"`
	Models    ModelsConfig    `mapstructure:"models"`
	Cost      CostConfig      `mapstructure:"cost"`
	Limits    LimitsConfig    `mapstructure:"limits"`
	Secrets   SecretsConfig   `mapstructure:"secrets"`
	RateLimit RateLimitConfig `mapstructure:"rate_limit"`
	Runner    RunnerConfig    `mapstructure:"runner"`
	Database  DatabaseConfig  `mapstructure:"database"`
	Pipeline  PipelineConfig  `mapstructure:"pipeline"`
}

type DaemonConfig struct {
	PollIntervalSecs     int    `mapstructure:"poll_interval_secs"`
	IdlePollIntervalSecs int    `mapstructure:"idle_poll_interval_secs"`
	MaxParallelTickets   int    `mapstructure:"max_parallel_tickets"`
	WorkDir              string `mapstructure:"work_dir"`
	LogLevel             string `mapstructure:"log_level"`
	LogFormat            string `mapstructure:"log_format"`
}

type DashboardConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	Port      int    `mapstructure:"port"`
	Host      string `mapstructure:"host"`
	AuthToken string `mapstructure:"auth_token"`
}

type TrackerConfig struct {
	Provider                    string `mapstructure:"provider"`
	PickupLabel                 string `mapstructure:"pickup_label"`
	ClarificationLabel          string `mapstructure:"clarification_label"`
	ClarificationTimeoutHours   int    `mapstructure:"clarification_timeout_hours"`
}

type GitConfig struct {
	Provider      string   `mapstructure:"provider"`
	Backend       string   `mapstructure:"backend"`
	CloneURL      string   `mapstructure:"clone_url"`
	DefaultBranch string   `mapstructure:"default_branch"`
	AutoPush      bool     `mapstructure:"auto_push"`
	PRDraft       bool     `mapstructure:"pr_draft"`
	PRReviewers   []string `mapstructure:"pr_reviewers"`
	BranchPrefix  string   `mapstructure:"branch_prefix"`
	RebaseBeforePR bool    `mapstructure:"rebase_before_pr"`
}

type LLMConfig struct {
	DefaultProvider string              `mapstructure:"default_provider"`
	Anthropic       LLMProviderConfig   `mapstructure:"anthropic"`
	OpenAI          LLMProviderConfig   `mapstructure:"openai"`
	OpenRouter      LLMProviderConfig   `mapstructure:"openrouter"`
	Local           LLMProviderConfig   `mapstructure:"local"`
	Outage          LLMOutageConfig     `mapstructure:"outage"`
}

type LLMProviderConfig struct {
	APIKey  string `mapstructure:"api_key"`
	BaseURL string `mapstructure:"base_url"`
}

type LLMOutageConfig struct {
	MaxConnectionRetries    int    `mapstructure:"max_connection_retries"`
	ConnectionRetryDelaySecs int   `mapstructure:"connection_retry_delay_secs"`
	FallbackProvider        string `mapstructure:"fallback_provider"`
}

type ModelsConfig struct {
	Planner         string `mapstructure:"planner"`
	Implementer     string `mapstructure:"implementer"`
	SpecReviewer    string `mapstructure:"spec_reviewer"`
	QualityReviewer string `mapstructure:"quality_reviewer"`
	FinalReviewer   string `mapstructure:"final_reviewer"`
	Clarifier       string `mapstructure:"clarifier"`
}

type CostConfig struct {
	MaxCostPerTicketUSD  float64                    `mapstructure:"max_cost_per_ticket_usd"`
	MaxCostPerDayUSD     float64                    `mapstructure:"max_cost_per_day_usd"`
	MaxCostPerMonthUSD   float64                    `mapstructure:"max_cost_per_month_usd"`
	AlertThresholdPct    int                        `mapstructure:"alert_threshold_percent"`
	MaxLlmCallsPerTask   int                        `mapstructure:"max_llm_calls_per_task"`
	Pricing              map[string]PricingConfig   `mapstructure:"pricing"`
}

type PricingConfig struct {
	Input  float64 `mapstructure:"input"`
	Output float64 `mapstructure:"output"`
}

type LimitsConfig struct {
	MaxTasksPerTicket         int     `mapstructure:"max_tasks_per_ticket"`
	MaxImplementationRetries  int     `mapstructure:"max_implementation_retries"`
	MaxSpecReviewCycles       int     `mapstructure:"max_spec_review_cycles"`
	MaxQualityReviewCycles    int     `mapstructure:"max_quality_review_cycles"`
	MaxTaskDurationSecs       int     `mapstructure:"max_task_duration_secs"`
	MaxTotalDurationSecs      int     `mapstructure:"max_total_duration_secs"`
	ContextTokenBudget        int     `mapstructure:"context_token_budget"`
	EnablePartialPR           bool    `mapstructure:"enable_partial_pr"`
	EnableClarification       bool    `mapstructure:"enable_clarification"`
	EnableTDDVerification     bool    `mapstructure:"enable_tdd_verification"`
	SearchReplaceSimilarity   float64 `mapstructure:"search_replace_similarity"`
	SearchReplaceMinContext   int     `mapstructure:"search_replace_min_context_lines"`
}

type SecretsConfig struct {
	Enabled       bool     `mapstructure:"enabled"`
	ExtraPatterns []string `mapstructure:"extra_patterns"`
	AlwaysExclude []string `mapstructure:"always_exclude"`
}

type RateLimitConfig struct {
	RequestsPerMinute int `mapstructure:"requests_per_minute"`
	BurstSize         int `mapstructure:"burst_size"`
	BackoffBaseMs     int `mapstructure:"backoff_base_ms"`
	BackoffMaxMs      int `mapstructure:"backoff_max_ms"`
	JitterPercent     int `mapstructure:"jitter_percent"`
}

type RunnerConfig struct {
	Mode   string            `mapstructure:"mode"`
	Docker DockerRunnerConfig `mapstructure:"docker"`
	Local  LocalRunnerConfig  `mapstructure:"local"`
}

type DockerRunnerConfig struct {
	Image            string `mapstructure:"image"`
	PersistPerTicket bool   `mapstructure:"persist_per_ticket"`
	Network          string `mapstructure:"network"`
	CPULimit         string `mapstructure:"cpu_limit"`
	MemoryLimit      string `mapstructure:"memory_limit"`
	AutoReinstallDeps bool  `mapstructure:"auto_reinstall_deps"`
}

type LocalRunnerConfig struct {
	AllowedCommands []string `mapstructure:"allowed_commands"`
	ForbiddenPaths  []string `mapstructure:"forbidden_paths"`
}

type DatabaseConfig struct {
	Driver   string         `mapstructure:"driver"`
	SQLite   SQLiteConfig   `mapstructure:"sqlite"`
	Postgres PostgresConfig `mapstructure:"postgres"`
}

type SQLiteConfig struct {
	Path               string `mapstructure:"path"`
	BusyTimeoutMs      int    `mapstructure:"busy_timeout_ms"`
	WALMode            bool   `mapstructure:"wal_mode"`
	EventFlushInterval int    `mapstructure:"event_flush_interval_ms"`
}

type PostgresConfig struct {
	URL            string `mapstructure:"url"`
	MaxConnections int    `mapstructure:"max_connections"`
}

type PipelineConfig struct {
	Hooks HooksConfig `mapstructure:"hooks"`
}

type HooksConfig struct {
	PostLint []string `mapstructure:"post_lint"`
	PrePR    []string `mapstructure:"pre_pr"`
	PostPR   []string `mapstructure:"post_pr"`
}
```

**Step 4: Verify it compiles**

Run: `go build ./...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/
git commit -m "feat: add domain models for tickets, tasks, pipeline states, and config"
```

---

### Task 3: LLM Provider Interface and Types

**Files:**
- Create: `internal/llm/provider.go`
- Create: `internal/llm/cost.go`
- Test: `internal/llm/cost_test.go`

**Step 1: Write the failing test for cost calculation**

```go
// internal/llm/cost_test.go
package llm

import (
	"testing"

	"github.com/canhta/foreman/internal/models"
)

func TestCalculateCost(t *testing.T) {
	pricing := map[string]models.PricingConfig{
		"anthropic:claude-sonnet-4-5-20250929": {Input: 3.00, Output: 15.00},
	}

	tests := []struct {
		name         string
		model        string
		tokensInput  int
		tokensOutput int
		wantCost     float64
	}{
		{
			name:         "basic cost calculation",
			model:        "anthropic:claude-sonnet-4-5-20250929",
			tokensInput:  1000,
			tokensOutput: 500,
			wantCost:     0.0105, // (1000/1M)*3.00 + (500/1M)*15.00
		},
		{
			name:         "unknown model uses zero",
			model:        "unknown:model",
			tokensInput:  1000,
			tokensOutput: 500,
			wantCost:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateCost(tt.model, tt.tokensInput, tt.tokensOutput, pricing)
			if got != tt.wantCost {
				t.Errorf("CalculateCost() = %v, want %v", got, tt.wantCost)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run TestCalculateCost -v`
Expected: FAIL — function not defined

**Step 3: Create LLM provider interface**

```go
// internal/llm/provider.go
package llm

import "context"

// LlmProvider is implemented by each LLM backend (Anthropic, OpenAI, etc.)
// Every call is stateless — no conversation memory.
type LlmProvider interface {
	Complete(ctx context.Context, req LlmRequest) (*LlmResponse, error)
	ProviderName() string
	HealthCheck(ctx context.Context) error
}

type LlmRequest struct {
	Model         string   `json:"model"`
	SystemPrompt  string   `json:"system_prompt"`
	UserPrompt    string   `json:"user_prompt"`
	MaxTokens     int      `json:"max_tokens"`
	Temperature   float64  `json:"temperature"`
	StopSequences []string `json:"stop_sequences,omitempty"`
}

type LlmResponse struct {
	Content      string `json:"content"`
	TokensInput  int    `json:"tokens_input"`
	TokensOutput int    `json:"tokens_output"`
	Model        string `json:"model"`
	DurationMs   int64  `json:"duration_ms"`
	StopReason   string `json:"stop_reason"`
}

// Error types for structured error handling.
type RateLimitError struct {
	RetryAfterSecs int
}

func (e *RateLimitError) Error() string {
	return "rate limit exceeded"
}

type AuthError struct {
	Message string
}

func (e *AuthError) Error() string {
	return "authentication error: " + e.Message
}

type BudgetExceededError struct {
	Current float64
	Limit   float64
}

func (e *BudgetExceededError) Error() string {
	return "budget exceeded"
}

type ConnectionError struct {
	Attempt int
	Err     error
}

func (e *ConnectionError) Error() string {
	return e.Err.Error()
}

func (e *ConnectionError) Unwrap() error {
	return e.Err
}
```

**Step 4: Implement cost calculation**

```go
// internal/llm/cost.go
package llm

import "github.com/canhta/foreman/internal/models"

// CalculateCost returns the cost in USD for a given model and token counts.
// Pricing is per 1M tokens.
func CalculateCost(model string, tokensInput, tokensOutput int, pricing map[string]models.PricingConfig) float64 {
	p, ok := pricing[model]
	if !ok {
		return 0.0
	}
	inputCost := (float64(tokensInput) / 1_000_000) * p.Input
	outputCost := (float64(tokensOutput) / 1_000_000) * p.Output
	return inputCost + outputCost
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/llm/ -run TestCalculateCost -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/llm/
git commit -m "feat: add LLM provider interface, error types, and cost calculation"
```

---

### Task 4: Command Runner Interface + Local Implementation

**Files:**
- Create: `internal/runner/runner.go`
- Create: `internal/runner/local.go`
- Create: `internal/runner/runner_test.go`

**Step 1: Write the failing test**

```go
// internal/runner/runner_test.go
package runner

import (
	"context"
	"testing"
)

func TestLocalRunner_Run(t *testing.T) {
	r := NewLocalRunner(nil)
	ctx := context.Background()

	t.Run("successful command", func(t *testing.T) {
		out, err := r.Run(ctx, "/tmp", "echo", []string{"hello"}, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.ExitCode != 0 {
			t.Errorf("expected exit code 0, got %d", out.ExitCode)
		}
		if out.Stdout != "hello\n" {
			t.Errorf("expected stdout 'hello\\n', got %q", out.Stdout)
		}
	})

	t.Run("failing command", func(t *testing.T) {
		out, err := r.Run(ctx, "/tmp", "false", nil, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.ExitCode == 0 {
			t.Error("expected non-zero exit code")
		}
	})

	t.Run("command not found", func(t *testing.T) {
		_, err := r.Run(ctx, "/tmp", "nonexistent_cmd_xyz", nil, 10)
		if err == nil {
			t.Error("expected error for nonexistent command")
		}
	})
}

func TestLocalRunner_CommandExists(t *testing.T) {
	r := NewLocalRunner(nil)
	ctx := context.Background()

	if !r.CommandExists(ctx, "echo") {
		t.Error("echo should exist")
	}
	if r.CommandExists(ctx, "nonexistent_cmd_xyz") {
		t.Error("nonexistent command should not exist")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/runner/ -v`
Expected: FAIL

**Step 3: Create runner interface and local implementation**

```go
// internal/runner/runner.go
package runner

import (
	"context"
	"time"
)

type CommandRunner interface {
	Run(ctx context.Context, workDir, command string, args []string, timeoutSecs int) (*CommandOutput, error)
	CommandExists(ctx context.Context, command string) bool
}

type CommandOutput struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	TimedOut bool
}
```

```go
// internal/runner/local.go
package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/canhta/foreman/internal/models"
)

type LocalRunner struct {
	config *models.LocalRunnerConfig
}

func NewLocalRunner(config *models.LocalRunnerConfig) *LocalRunner {
	return &LocalRunner{config: config}
}

func (r *LocalRunner) Run(ctx context.Context, workDir, command string, args []string, timeoutSecs int) (*CommandOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	output := &CommandOutput{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	if ctx.Err() == context.DeadlineExceeded {
		output.TimedOut = true
		output.ExitCode = -1
		return output, nil
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			output.ExitCode = exitErr.ExitCode()
			return output, nil
		}
		return nil, fmt.Errorf("failed to execute command %s: %w", command, err)
	}

	return output, nil
}

func (r *LocalRunner) CommandExists(ctx context.Context, command string) bool {
	_, err := exec.LookPath(command)
	return err == nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/runner/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/runner/
git commit -m "feat: add command runner interface and local implementation"
```

---

### Task 5: Secrets Scanner

**Files:**
- Create: `internal/context/secrets_scanner.go`
- Create: `internal/context/secrets_scanner_test.go`

**Step 1: Write the failing test**

```go
// internal/context/secrets_scanner_test.go
package context

import (
	"testing"

	"github.com/canhta/foreman/internal/models"
)

func TestSecretsScanner_ScanFile(t *testing.T) {
	scanner := NewSecretsScanner(&models.SecretsConfig{
		AlwaysExclude: []string{".env", ".env.*", "*.pem"},
	})

	tests := []struct {
		name       string
		path       string
		content    string
		hasSecrets bool
	}{
		{
			name:       "clean file",
			path:       "main.go",
			content:    "package main\nfunc main() {}",
			hasSecrets: false,
		},
		{
			name:       "AWS access key",
			path:       "config.go",
			content:    "key := \"AKIAIOSFODNN7EXAMPLE\"",
			hasSecrets: true,
		},
		{
			name:       "always excluded .env",
			path:       ".env",
			content:    "FOO=bar",
			hasSecrets: true,
		},
		{
			name:       "always excluded .env.local",
			path:       ".env.local",
			content:    "FOO=bar",
			hasSecrets: true,
		},
		{
			name:       "private key header",
			path:       "cert.go",
			content:    "-----BEGIN RSA PRIVATE KEY-----\ndata",
			hasSecrets: true,
		},
		{
			name:       "always excluded pem file",
			path:       "server.pem",
			content:    "cert data",
			hasSecrets: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scanner.ScanFile(tt.path, tt.content)
			if result.HasSecrets != tt.hasSecrets {
				t.Errorf("ScanFile(%q) HasSecrets = %v, want %v", tt.path, result.HasSecrets, tt.hasSecrets)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/context/ -run TestSecretsScanner -v`
Expected: FAIL

**Step 3: Implement secrets scanner**

```go
// internal/context/secrets_scanner.go
package context

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/canhta/foreman/internal/models"
)

var builtinSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(AKIA[0-9A-Z]{16})`),
	regexp.MustCompile(`(?i)(sk-[a-zA-Z0-9]{20,})`),
	regexp.MustCompile(`(?i)(ghp_[a-zA-Z0-9]{36})`),
	regexp.MustCompile(`(?i)(glpat-[a-zA-Z0-9\-]{20,})`),
	regexp.MustCompile(`(?i)-----BEGIN (RSA |EC |DSA )?PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)(xox[bprs]-[a-zA-Z0-9\-]+)`),
	regexp.MustCompile(`(?i)(api[_-]?key|api[_-]?secret|api[_-]?token)\s*[:=]\s*["']?[a-zA-Z0-9\-._]{16,}`),
	regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*["'][^"']{8,}["']`),
}

type ScanResult struct {
	Path       string
	HasSecrets bool
	Matches    []SecretMatch
}

type SecretMatch struct {
	Line    int
	Pattern string
	Snippet string
}

type SecretsScanner struct {
	patterns      []*regexp.Regexp
	alwaysExclude []string
}

func NewSecretsScanner(config *models.SecretsConfig) *SecretsScanner {
	patterns := make([]*regexp.Regexp, len(builtinSecretPatterns))
	copy(patterns, builtinSecretPatterns)
	for _, extra := range config.ExtraPatterns {
		compiled, err := regexp.Compile(extra)
		if err == nil {
			patterns = append(patterns, compiled)
		}
	}
	return &SecretsScanner{
		patterns:      patterns,
		alwaysExclude: config.AlwaysExclude,
	}
}

func (s *SecretsScanner) ScanFile(path, content string) *ScanResult {
	for _, pattern := range s.alwaysExclude {
		matched, _ := filepath.Match(pattern, filepath.Base(path))
		if matched {
			return &ScanResult{
				Path: path, HasSecrets: true,
				Matches: []SecretMatch{{Pattern: "always_exclude", Snippet: path}},
			}
		}
	}

	result := &ScanResult{Path: path}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		for _, pat := range s.patterns {
			if pat.MatchString(line) {
				result.HasSecrets = true
				match := pat.FindString(line)
				redacted := match
				if len(redacted) > 4 {
					redacted = redacted[:4] + "***"
				}
				result.Matches = append(result.Matches, SecretMatch{
					Line: i + 1, Pattern: pat.String(), Snippet: redacted,
				})
			}
		}
	}
	return result
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/context/ -run TestSecretsScanner -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/context/
git commit -m "feat: add secrets scanner with builtin patterns and always-exclude"
```

---

### Task 6: Token Budget Management

**Files:**
- Create: `internal/context/token_budget.go`
- Create: `internal/context/token_budget_test.go`

**Step 1: Write the failing test**

```go
// internal/context/token_budget_test.go
package context

import "testing"

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantMin int
		wantMax int
	}{
		{"empty", "", 0, 1},
		{"short", "hello world", 2, 5},
		{"code block", "func main() {\n\tfmt.Println(\"hello\")\n}", 5, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokens(tt.content)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("EstimateTokens() = %d, want between %d and %d", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestTokenBudget_Add(t *testing.T) {
	tb := NewTokenBudget(100)

	if !tb.CanFit(50) {
		t.Error("should fit 50 tokens in 100 budget")
	}

	tb.Add(60)

	if tb.CanFit(50) {
		t.Error("should not fit 50 more tokens (60 used of 100)")
	}

	if tb.CanFit(40) {
		// 60 + 40 = 100, which should be exactly at limit
	}

	if tb.Remaining() != 40 {
		t.Errorf("Remaining() = %d, want 40", tb.Remaining())
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/context/ -run TestEstimateTokens -v`
Expected: FAIL

**Step 3: Implement token budget**

```go
// internal/context/token_budget.go
package context

// EstimateTokens provides a rough token estimate.
// Approximation: ~4 characters per token for English/code.
func EstimateTokens(content string) int {
	if len(content) == 0 {
		return 0
	}
	return (len(content) + 3) / 4
}

// TokenBudget tracks token usage against a limit.
type TokenBudget struct {
	limit int
	used  int
}

func NewTokenBudget(limit int) *TokenBudget {
	return &TokenBudget{limit: limit}
}

func (tb *TokenBudget) CanFit(tokens int) bool {
	return tb.used+tokens <= tb.limit
}

func (tb *TokenBudget) Add(tokens int) {
	tb.used += tokens
}

func (tb *TokenBudget) Remaining() int {
	r := tb.limit - tb.used
	if r < 0 {
		return 0
	}
	return r
}

func (tb *TokenBudget) Used() int {
	return tb.used
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/context/ -run "TestEstimateTokens|TestTokenBudget" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/context/token_budget.go internal/context/token_budget_test.go
git commit -m "feat: add token estimation and budget management"
```

---

### Task 7: Implementer Output Parser

**Files:**
- Create: `internal/pipeline/output_parser.go`
- Create: `internal/pipeline/output_parser_test.go`

**Step 1: Write the failing test**

```go
// internal/pipeline/output_parser_test.go
package pipeline

import (
	"testing"
)

func TestParseImplementerOutput_NewFile(t *testing.T) {
	raw := `=== NEW FILE: src/hello.ts ===
export function hello(): string {
  return "hello";
}
=== END FILE ===`

	result, err := ParseImplementerOutput(raw, 0.92, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.Files))
	}
	if !result.Files[0].IsNew {
		t.Error("expected file to be new")
	}
	if result.Files[0].Path != "src/hello.ts" {
		t.Errorf("expected path 'src/hello.ts', got %q", result.Files[0].Path)
	}
}

func TestParseImplementerOutput_ModifyFile(t *testing.T) {
	raw := `=== MODIFY FILE: src/app.ts ===
<<<< SEARCH
import { Router } from 'express';
import { authMiddleware } from '../lib/auth';
const app = express();
>>>>
<<<< REPLACE
import { Router } from 'express';
import { authMiddleware } from '../lib/auth';
import { validateInput } from '../lib/validation';
const app = express();
>>>>
=== END FILE ===`

	result, err := ParseImplementerOutput(raw, 0.92, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.Files))
	}
	if result.Files[0].IsNew {
		t.Error("expected file to be a modification")
	}
	if len(result.Files[0].Patches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(result.Files[0].Patches))
	}
}

func TestParseImplementerOutput_MultipleFiles(t *testing.T) {
	raw := `=== NEW FILE: src/test.ts ===
test content
=== END FILE ===

=== NEW FILE: src/impl.ts ===
impl content
=== END FILE ===`

	result, err := ParseImplementerOutput(raw, 0.92, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(result.Files))
	}
}

func TestParseImplementerOutput_PermissiveParsing(t *testing.T) {
	// LLM wraps in markdown fences
	raw := "```\n=== NEW FILE: src/hello.ts ===\nhello content\n=== END FILE ===\n```"

	result, err := ParseImplementerOutput(raw, 0.92, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.Files))
	}
}

func TestApplySearchReplace_ExactMatch(t *testing.T) {
	content := "line1\nline2\nline3\nline4"
	sr := &SearchReplace{
		Search:  "line2\nline3",
		Replace: "lineA\nlineB",
	}

	result, err := ApplySearchReplace(content, sr, 0.92)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "line1\nlineA\nlineB\nline4"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestParseImplementerOutput -v`
Expected: FAIL

**Step 3: Implement output parser**

```go
// internal/pipeline/output_parser.go
package pipeline

import (
	"fmt"
	"regexp"
	"strings"
)

type ParsedOutput struct {
	Files       []FileChange
	ParseErrors []string
}

type FileChange struct {
	Path    string
	IsNew   bool
	Content string
	Patches []SearchReplace
}

type SearchReplace struct {
	Search     string
	Replace    string
	FuzzyMatch bool
	Similarity float64
}

var (
	newFileRe    = regexp.MustCompile(`===\s*NEW FILE:\s*(.+?)\s*===`)
	modifyFileRe = regexp.MustCompile(`===\s*MODIFY FILE:\s*(.+?)\s*===`)
	endFileRe    = regexp.MustCompile(`===\s*END FILE\s*===`)
	searchRe     = regexp.MustCompile(`<<<<\s*SEARCH`)
	replaceRe    = regexp.MustCompile(`<<<<\s*REPLACE`)
	endBlockRe   = regexp.MustCompile(`>>>>`)
)

func ParseImplementerOutput(raw string, similarityThreshold float64, minContextLines int) (*ParsedOutput, error) {
	// Strategy 1: strict
	result, err := parseStrict(raw)
	if err == nil && len(result.Files) > 0 {
		return result, nil
	}

	// Strategy 2: permissive (strip markdown fences, commentary)
	cleaned := stripMarkdownFences(raw)
	result, err = parseStrict(cleaned)
	if err == nil && len(result.Files) > 0 {
		return result, nil
	}

	return nil, fmt.Errorf("failed to parse implementer output (all strategies failed). Raw length: %d", len(raw))
}

func parseStrict(raw string) (*ParsedOutput, error) {
	result := &ParsedOutput{}
	lines := strings.Split(raw, "\n")
	i := 0

	for i < len(lines) {
		line := lines[i]

		if m := newFileRe.FindStringSubmatch(line); m != nil {
			path := strings.TrimSpace(m[1])
			i++
			var contentLines []string
			for i < len(lines) && !endFileRe.MatchString(lines[i]) {
				contentLines = append(contentLines, lines[i])
				i++
			}
			if i < len(lines) {
				i++ // skip END FILE
			}
			result.Files = append(result.Files, FileChange{
				Path:    path,
				IsNew:   true,
				Content: strings.Join(contentLines, "\n"),
			})
			continue
		}

		if m := modifyFileRe.FindStringSubmatch(line); m != nil {
			path := strings.TrimSpace(m[1])
			i++
			var patches []SearchReplace
			for i < len(lines) && !endFileRe.MatchString(lines[i]) {
				if searchRe.MatchString(lines[i]) {
					i++
					var searchLines []string
					for i < len(lines) && !endBlockRe.MatchString(lines[i]) {
						searchLines = append(searchLines, lines[i])
						i++
					}
					if i < len(lines) {
						i++ // skip >>>>
					}

					if i < len(lines) && replaceRe.MatchString(lines[i]) {
						i++
						var replaceLines []string
						for i < len(lines) && !endBlockRe.MatchString(lines[i]) {
							replaceLines = append(replaceLines, lines[i])
							i++
						}
						if i < len(lines) {
							i++ // skip >>>>
						}
						patches = append(patches, SearchReplace{
							Search:  strings.Join(searchLines, "\n"),
							Replace: strings.Join(replaceLines, "\n"),
						})
					}
				} else {
					i++
				}
			}
			if i < len(lines) {
				i++ // skip END FILE
			}
			result.Files = append(result.Files, FileChange{
				Path:    path,
				IsNew:   false,
				Patches: patches,
			})
			continue
		}

		i++
	}

	if len(result.Files) == 0 {
		return nil, fmt.Errorf("no files found in output")
	}
	return result, nil
}

func stripMarkdownFences(raw string) string {
	lines := strings.Split(raw, "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			continue
		}
		if strings.HasPrefix(trimmed, "~~~") {
			continue
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

// ApplySearchReplace applies a search/replace patch to file content.
func ApplySearchReplace(content string, sr *SearchReplace, threshold float64) (string, error) {
	// Try exact match first
	if idx := strings.Index(content, sr.Search); idx != -1 {
		return content[:idx] + sr.Replace + content[idx+len(sr.Search):], nil
	}

	// Fuzzy match: slide a window over lines
	searchLines := strings.Split(sr.Search, "\n")
	contentLines := strings.Split(content, "\n")
	windowSize := len(searchLines)

	if windowSize > len(contentLines) {
		return "", fmt.Errorf("SEARCH block (%d lines) larger than file (%d lines)", windowSize, len(contentLines))
	}

	bestSimilarity := 0.0
	bestStart := -1

	for i := 0; i <= len(contentLines)-windowSize; i++ {
		candidate := strings.Join(contentLines[i:i+windowSize], "\n")
		sim := normalizedSimilarity(sr.Search, candidate)
		if sim > bestSimilarity {
			bestSimilarity = sim
			bestStart = i
		}
	}

	if bestSimilarity >= threshold {
		sr.FuzzyMatch = true
		sr.Similarity = bestSimilarity
		var result []string
		result = append(result, contentLines[:bestStart]...)
		result = append(result, strings.Split(sr.Replace, "\n")...)
		result = append(result, contentLines[bestStart+windowSize:]...)
		return strings.Join(result, "\n"), nil
	}

	return "", fmt.Errorf("SEARCH block not found (best similarity: %.2f, threshold: %.2f)", bestSimilarity, threshold)
}

// normalizedSimilarity computes a simple character-level similarity ratio.
func normalizedSimilarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	if maxLen == 0 {
		return 1.0
	}
	dist := levenshtein(a, b)
	return 1.0 - float64(dist)/float64(maxLen)
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/pipeline/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/
git commit -m "feat: add implementer output parser with strict/permissive strategies and fuzzy matching"
```

---

### Task 8: Anthropic LLM Provider

**Files:**
- Create: `internal/llm/anthropic.go`
- Create: `internal/llm/anthropic_test.go`

**Step 1: Write the failing test (uses httptest mock server)**

```go
// internal/llm/anthropic_test.go
package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicProvider_Complete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			t.Error("missing or wrong API key header")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version header")
		}

		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		resp := map[string]interface{}{
			"id":   "msg_test",
			"type": "message",
			"role": "assistant",
			"content": []map[string]interface{}{
				{"type": "text", "text": "Hello from Claude"},
			},
			"model":          "claude-sonnet-4-5-20250929",
			"stop_reason":    "end_turn",
			"usage": map[string]interface{}{
				"input_tokens":  100,
				"output_tokens": 20,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL)
	resp, err := provider.Complete(context.Background(), LlmRequest{
		Model:        "claude-sonnet-4-5-20250929",
		SystemPrompt: "You are helpful.",
		UserPrompt:   "Say hello",
		MaxTokens:    1024,
		Temperature:  0.3,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello from Claude" {
		t.Errorf("expected 'Hello from Claude', got %q", resp.Content)
	}
	if resp.TokensInput != 100 {
		t.Errorf("expected 100 input tokens, got %d", resp.TokensInput)
	}
	if resp.TokensOutput != 20 {
		t.Errorf("expected 20 output tokens, got %d", resp.TokensOutput)
	}
}

func TestAnthropicProvider_ProviderName(t *testing.T) {
	p := NewAnthropicProvider("key", "url")
	if p.ProviderName() != "anthropic" {
		t.Errorf("expected 'anthropic', got %q", p.ProviderName())
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run TestAnthropicProvider -v`
Expected: FAIL

**Step 3: Implement Anthropic provider**

```go
// internal/llm/anthropic.go
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type AnthropicProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewAnthropicProvider(apiKey, baseURL string) *AnthropicProvider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &AnthropicProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 5 * time.Minute},
	}
}

func (p *AnthropicProvider) ProviderName() string {
	return "anthropic"
}

type anthropicRequest struct {
	Model       string              `json:"model"`
	MaxTokens   int                 `json:"max_tokens"`
	System      string              `json:"system,omitempty"`
	Messages    []anthropicMessage  `json:"messages"`
	Temperature float64             `json:"temperature,omitempty"`
	Stop        []string            `json:"stop_sequences,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (p *AnthropicProvider) Complete(ctx context.Context, req LlmRequest) (*LlmResponse, error) {
	body := anthropicRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		System:      req.SystemPrompt,
		Temperature: req.Temperature,
		Stop:        req.StopSequences,
		Messages: []anthropicMessage{
			{Role: "user", Content: req.UserPrompt},
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	start := time.Now()
	httpResp, err := p.client.Do(httpReq)
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		return nil, &ConnectionError{Attempt: 1, Err: err}
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if httpResp.StatusCode == 429 {
		return nil, &RateLimitError{RetryAfterSecs: 60}
	}

	if httpResp.StatusCode == 401 || httpResp.StatusCode == 403 {
		var apiErr anthropicError
		json.Unmarshal(respBody, &apiErr)
		return nil, &AuthError{Message: apiErr.Error.Message}
	}

	if httpResp.StatusCode != 200 {
		var apiErr anthropicError
		json.Unmarshal(respBody, &apiErr)
		return nil, fmt.Errorf("anthropic API error (status %d): %s", httpResp.StatusCode, apiErr.Error.Message)
	}

	var resp anthropicResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	var content string
	for _, c := range resp.Content {
		if c.Type == "text" {
			content += c.Text
		}
	}

	return &LlmResponse{
		Content:      content,
		TokensInput:  resp.Usage.InputTokens,
		TokensOutput: resp.Usage.OutputTokens,
		Model:        resp.Model,
		DurationMs:   durationMs,
		StopReason:   resp.StopReason,
	}, nil
}

func (p *AnthropicProvider) HealthCheck(ctx context.Context) error {
	_, err := p.Complete(ctx, LlmRequest{
		Model:     "claude-haiku-4-5-20251001",
		UserPrompt: "ping",
		MaxTokens: 5,
	})
	return err
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/llm/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/llm/anthropic.go internal/llm/anthropic_test.go
git commit -m "feat: add Anthropic LLM provider with Messages API"
```

---

### Task 9: Database Interface + SQLite Schema

**Files:**
- Create: `internal/db/db.go`
- Create: `internal/db/schema.go`
- Create: `internal/db/sqlite.go`
- Create: `internal/db/sqlite_test.go`

**Step 1: Write the failing test**

```go
// internal/db/sqlite_test.go
package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
)

func setupTestDB(t *testing.T) (Database, func()) {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "foreman-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	db, err := NewSQLiteDB(tmpFile.Name())
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatal(err)
	}

	return db, func() {
		db.Close()
		os.Remove(tmpFile.Name())
	}
}

func TestSQLiteDB_CreateAndGetTicket(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	ticket := &models.Ticket{
		ID:          "t-1",
		ExternalID:  "PROJ-123",
		Title:       "Test ticket",
		Description: "Test description",
		Status:      models.TicketStatusQueued,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	err := db.CreateTicket(ctx, ticket)
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	got, err := db.GetTicketByExternalID(ctx, "PROJ-123")
	if err != nil {
		t.Fatalf("GetTicketByExternalID: %v", err)
	}
	if got.Title != "Test ticket" {
		t.Errorf("expected title 'Test ticket', got %q", got.Title)
	}
}

func TestSQLiteDB_RecordEvent(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create ticket first (FK constraint)
	db.CreateTicket(ctx, &models.Ticket{
		ID: "t-1", ExternalID: "X-1", Title: "t", Description: "d",
		Status: models.TicketStatusQueued, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	err := db.RecordEvent(ctx, &models.EventRecord{
		ID:        "e-1",
		TicketID:  "t-1",
		EventType: "test_event",
		Severity:  "info",
		Message:   "test message",
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	events, err := db.GetEvents(ctx, "t-1", 10)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -v`
Expected: FAIL

**Step 3: Create database interface**

```go
// internal/db/db.go
package db

import (
	"context"
	"io"

	"github.com/canhta/foreman/internal/models"
)

type Database interface {
	// Tickets
	CreateTicket(ctx context.Context, t *models.Ticket) error
	UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error
	GetTicket(ctx context.Context, id string) (*models.Ticket, error)
	GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error)
	ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error)
	SetLastCompletedTask(ctx context.Context, ticketID string, taskSeq int) error

	// Tasks
	CreateTasks(ctx context.Context, ticketID string, tasks []models.Task) error
	UpdateTaskStatus(ctx context.Context, id string, status models.TaskStatus) error
	IncrementTaskLlmCalls(ctx context.Context, id string) (int, error)

	// LLM calls
	RecordLlmCall(ctx context.Context, call *models.LlmCallRecord) error

	// Handoffs
	SetHandoff(ctx context.Context, h *models.HandoffRecord) error
	GetHandoffs(ctx context.Context, ticketID, forRole string) ([]models.HandoffRecord, error)

	// Progress patterns
	SaveProgressPattern(ctx context.Context, p *models.ProgressPattern) error
	GetProgressPatterns(ctx context.Context, ticketID string, directories []string) ([]models.ProgressPattern, error)

	// File reservations
	ReserveFiles(ctx context.Context, ticketID string, paths []string) error
	ReleaseFiles(ctx context.Context, ticketID string) error
	GetReservedFiles(ctx context.Context) (map[string]string, error)

	// Cost
	GetTicketCost(ctx context.Context, ticketID string) (float64, error)
	GetDailyCost(ctx context.Context, date string) (float64, error)
	RecordDailyCost(ctx context.Context, date string, amount float64) error

	// Events
	RecordEvent(ctx context.Context, e *models.EventRecord) error
	GetEvents(ctx context.Context, ticketID string, limit int) ([]models.EventRecord, error)

	// Auth
	CreateAuthToken(ctx context.Context, tokenHash, name string) error
	ValidateAuthToken(ctx context.Context, tokenHash string) (bool, error)

	io.Closer
}
```

**Step 4: Create schema**

```go
// internal/db/schema.go
package db

const schema = `
CREATE TABLE IF NOT EXISTS tickets (
    id TEXT PRIMARY KEY,
    external_id TEXT NOT NULL UNIQUE,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    acceptance_criteria TEXT,
    labels TEXT,
    priority TEXT,
    status TEXT NOT NULL DEFAULT 'queued',
    external_status TEXT,
    repo_url TEXT,
    branch_name TEXT,
    pr_url TEXT,
    pr_number INTEGER,
    is_partial BOOLEAN DEFAULT FALSE,
    cost_usd REAL DEFAULT 0.0,
    tokens_input INTEGER DEFAULT 0,
    tokens_output INTEGER DEFAULT 0,
    total_llm_calls INTEGER DEFAULT 0,
    clarification_requested_at TIMESTAMP,
    error_message TEXT,
    last_completed_task_seq INTEGER DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    sequence INTEGER NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    acceptance_criteria TEXT NOT NULL,
    files_to_read TEXT,
    files_to_modify TEXT,
    test_assertions TEXT,
    estimated_complexity TEXT,
    depends_on TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    implementation_attempts INTEGER DEFAULT 0,
    spec_review_attempts INTEGER DEFAULT 0,
    quality_review_attempts INTEGER DEFAULT 0,
    total_llm_calls INTEGER DEFAULT 0,
    commit_sha TEXT,
    cost_usd REAL DEFAULT 0.0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP,
    completed_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS llm_calls (
    id TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    task_id TEXT REFERENCES tasks(id),
    role TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    attempt INTEGER NOT NULL DEFAULT 1,
    tokens_input INTEGER NOT NULL,
    tokens_output INTEGER NOT NULL,
    cost_usd REAL NOT NULL,
    duration_ms INTEGER NOT NULL,
    prompt_hash TEXT,
    response_summary TEXT,
    status TEXT NOT NULL,
    error_message TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS handoffs (
    id TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    from_role TEXT NOT NULL,
    to_role TEXT,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS progress_patterns (
    id TEXT PRIMARY KEY,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    pattern_key TEXT NOT NULL,
    pattern_value TEXT NOT NULL,
    directories TEXT,
    discovered_by_task TEXT REFERENCES tasks(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS file_reservations (
    file_path TEXT NOT NULL,
    ticket_id TEXT NOT NULL REFERENCES tickets(id),
    reserved_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    released_at TIMESTAMP,
    PRIMARY KEY (file_path, ticket_id)
);

CREATE TABLE IF NOT EXISTS cost_daily (
    date TEXT PRIMARY KEY,
    total_usd REAL DEFAULT 0.0,
    total_input_tokens INTEGER DEFAULT 0,
    total_output_tokens INTEGER DEFAULT 0,
    ticket_count INTEGER DEFAULT 0,
    task_count INTEGER DEFAULT 0,
    llm_call_count INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    ticket_id TEXT REFERENCES tickets(id),
    task_id TEXT REFERENCES tasks(id),
    event_type TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info',
    message TEXT NOT NULL,
    details TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS auth_tokens (
    token_hash TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP,
    revoked BOOLEAN DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_tickets_status ON tickets(status);
CREATE INDEX IF NOT EXISTS idx_tickets_external_id ON tickets(external_id);
CREATE INDEX IF NOT EXISTS idx_tasks_ticket_id ON tasks(ticket_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_llm_calls_ticket_id ON llm_calls(ticket_id);
CREATE INDEX IF NOT EXISTS idx_events_ticket_id ON events(ticket_id);
CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at);
CREATE INDEX IF NOT EXISTS idx_file_reservations_ticket ON file_reservations(ticket_id);
`
```

**Step 5: Implement SQLite database (core methods)**

```go
// internal/db/sqlite.go
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/canhta/foreman/internal/models"
	_ "github.com/mattn/go-sqlite3"
)

type SQLiteDB struct {
	db *sql.DB
}

func NewSQLiteDB(path string) (*SQLiteDB, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return &SQLiteDB{db: db}, nil
}

func (s *SQLiteDB) Close() error {
	return s.db.Close()
}

func (s *SQLiteDB) CreateTicket(ctx context.Context, t *models.Ticket) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tickets (id, external_id, title, description, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ExternalID, t.Title, t.Description, string(t.Status), t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (s *SQLiteDB) UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tickets SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), time.Now(), id,
	)
	return err
}

func (s *SQLiteDB) GetTicket(ctx context.Context, id string) (*models.Ticket, error) {
	return s.scanTicket(s.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, created_at, updated_at FROM tickets WHERE id = ?`, id))
}

func (s *SQLiteDB) GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error) {
	return s.scanTicket(s.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, created_at, updated_at FROM tickets WHERE external_id = ?`, externalID))
}

func (s *SQLiteDB) scanTicket(row *sql.Row) (*models.Ticket, error) {
	var t models.Ticket
	var status string
	err := row.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	t.Status = models.TicketStatus(status)
	return &t, nil
}

func (s *SQLiteDB) ListTickets(ctx context.Context, filter models.TicketFilter) ([]models.Ticket, error) {
	query := `SELECT id, external_id, title, description, status, created_at, updated_at FROM tickets WHERE 1=1`
	var args []interface{}

	if filter.Status != "" {
		query += ` AND status = ?`
		args = append(args, filter.Status)
	}
	if len(filter.StatusIn) > 0 {
		placeholders := ""
		for i, s := range filter.StatusIn {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, s)
		}
		query += ` AND status IN (` + placeholders + `)`
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tickets []models.Ticket
	for rows.Next() {
		var t models.Ticket
		var status string
		if err := rows.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Status = models.TicketStatus(status)
		tickets = append(tickets, t)
	}
	return tickets, rows.Err()
}

func (s *SQLiteDB) SetLastCompletedTask(ctx context.Context, ticketID string, taskSeq int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tickets SET last_completed_task_seq = ?, updated_at = ? WHERE id = ?`,
		taskSeq, time.Now(), ticketID,
	)
	return err
}

func (s *SQLiteDB) CreateTasks(ctx context.Context, ticketID string, tasks []models.Task) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO tasks (id, ticket_id, sequence, title, description, acceptance_criteria,
		 files_to_read, files_to_modify, test_assertions, estimated_complexity, depends_on, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, t := range tasks {
		_, err := stmt.ExecContext(ctx, t.ID, ticketID, t.Sequence, t.Title, t.Description,
			"[]", "[]", "[]", "[]", t.EstimatedComplexity, "[]",
			string(models.TaskStatusPending), time.Now())
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteDB) UpdateTaskStatus(ctx context.Context, id string, status models.TaskStatus) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET status = ? WHERE id = ?`, string(status), id)
	return err
}

func (s *SQLiteDB) IncrementTaskLlmCalls(ctx context.Context, id string) (int, error) {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET total_llm_calls = total_llm_calls + 1 WHERE id = ?`, id)
	if err != nil {
		return 0, err
	}
	var count int
	err = s.db.QueryRowContext(ctx, `SELECT total_llm_calls FROM tasks WHERE id = ?`, id).Scan(&count)
	return count, err
}

func (s *SQLiteDB) RecordLlmCall(ctx context.Context, call *models.LlmCallRecord) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO llm_calls (id, ticket_id, task_id, role, provider, model, attempt,
		 tokens_input, tokens_output, cost_usd, duration_ms, prompt_hash, response_summary, status, error_message, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		call.ID, call.TicketID, call.TaskID, call.Role, call.Provider, call.Model, call.Attempt,
		call.TokensInput, call.TokensOutput, call.CostUSD, call.DurationMs,
		call.PromptHash, call.ResponseSummary, call.Status, call.ErrorMessage, call.CreatedAt,
	)
	return err
}

func (s *SQLiteDB) SetHandoff(ctx context.Context, h *models.HandoffRecord) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO handoffs (id, ticket_id, from_role, to_role, key, value, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		h.ID, h.TicketID, h.FromRole, h.ToRole, h.Key, h.Value, h.CreatedAt,
	)
	return err
}

func (s *SQLiteDB) GetHandoffs(ctx context.Context, ticketID, forRole string) ([]models.HandoffRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, from_role, to_role, key, value, created_at FROM handoffs
		 WHERE ticket_id = ? AND (to_role = ? OR to_role IS NULL OR to_role = '')
		 ORDER BY created_at`, ticketID, forRole)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var handoffs []models.HandoffRecord
	for rows.Next() {
		var h models.HandoffRecord
		if err := rows.Scan(&h.ID, &h.TicketID, &h.FromRole, &h.ToRole, &h.Key, &h.Value, &h.CreatedAt); err != nil {
			return nil, err
		}
		handoffs = append(handoffs, h)
	}
	return handoffs, rows.Err()
}

func (s *SQLiteDB) SaveProgressPattern(ctx context.Context, p *models.ProgressPattern) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO progress_patterns (id, ticket_id, pattern_key, pattern_value, directories, discovered_by_task, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.TicketID, p.PatternKey, p.PatternValue, "[]", p.DiscoveredByTask, p.CreatedAt,
	)
	return err
}

func (s *SQLiteDB) GetProgressPatterns(ctx context.Context, ticketID string, directories []string) ([]models.ProgressPattern, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, pattern_key, pattern_value, directories, discovered_by_task, created_at
		 FROM progress_patterns WHERE ticket_id = ?`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var patterns []models.ProgressPattern
	for rows.Next() {
		var p models.ProgressPattern
		var dirs string
		if err := rows.Scan(&p.ID, &p.TicketID, &p.PatternKey, &p.PatternValue, &dirs, &p.DiscoveredByTask, &p.CreatedAt); err != nil {
			return nil, err
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}

func (s *SQLiteDB) ReserveFiles(ctx context.Context, ticketID string, paths []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, p := range paths {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO file_reservations (file_path, ticket_id, reserved_at) VALUES (?, ?, ?)`,
			p, ticketID, time.Now())
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteDB) ReleaseFiles(ctx context.Context, ticketID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE file_reservations SET released_at = ? WHERE ticket_id = ? AND released_at IS NULL`,
		time.Now(), ticketID)
	return err
}

func (s *SQLiteDB) GetReservedFiles(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT file_path, ticket_id FROM file_reservations WHERE released_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var path, ticketID string
		if err := rows.Scan(&path, &ticketID); err != nil {
			return nil, err
		}
		result[path] = ticketID
	}
	return result, rows.Err()
}

func (s *SQLiteDB) GetTicketCost(ctx context.Context, ticketID string) (float64, error) {
	var cost float64
	err := s.db.QueryRowContext(ctx, `SELECT cost_usd FROM tickets WHERE id = ?`, ticketID).Scan(&cost)
	return cost, err
}

func (s *SQLiteDB) GetDailyCost(ctx context.Context, date string) (float64, error) {
	var cost float64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(total_usd, 0) FROM cost_daily WHERE date = ?`, date).Scan(&cost)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return cost, err
}

func (s *SQLiteDB) RecordDailyCost(ctx context.Context, date string, amount float64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cost_daily (date, total_usd) VALUES (?, ?)
		 ON CONFLICT(date) DO UPDATE SET total_usd = total_usd + ?`,
		date, amount, amount)
	return err
}

func (s *SQLiteDB) RecordEvent(ctx context.Context, e *models.EventRecord) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (id, ticket_id, task_id, event_type, severity, message, details, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.TicketID, e.TaskID, e.EventType, e.Severity, e.Message, e.Details, e.CreatedAt,
	)
	return err
}

func (s *SQLiteDB) GetEvents(ctx context.Context, ticketID string, limit int) ([]models.EventRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ticket_id, task_id, event_type, severity, message, details, created_at
		 FROM events WHERE ticket_id = ? ORDER BY created_at DESC LIMIT ?`,
		ticketID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.EventRecord
	for rows.Next() {
		var e models.EventRecord
		var taskID, details sql.NullString
		if err := rows.Scan(&e.ID, &e.TicketID, &taskID, &e.EventType, &e.Severity, &e.Message, &details, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.TaskID = taskID.String
		e.Details = details.String
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *SQLiteDB) CreateAuthToken(ctx context.Context, tokenHash, name string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_tokens (token_hash, name, created_at) VALUES (?, ?, ?)`,
		tokenHash, name, time.Now())
	return err
}

func (s *SQLiteDB) ValidateAuthToken(ctx context.Context, tokenHash string) (bool, error) {
	var revoked bool
	err := s.db.QueryRowContext(ctx,
		`SELECT revoked FROM auth_tokens WHERE token_hash = ?`, tokenHash).Scan(&revoked)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !revoked {
		s.db.ExecContext(ctx, `UPDATE auth_tokens SET last_used_at = ? WHERE token_hash = ?`, time.Now(), tokenHash)
	}
	return !revoked, nil
}
```

**Step 6: Install go-sqlite3 and run tests**

```bash
go get github.com/mattn/go-sqlite3@v1.14.22
go mod tidy
```

Run: `go test ./internal/db/ -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/db/
git commit -m "feat: add database interface and SQLite implementation with full schema"
```

---

### Task 10: Config Loading

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `foreman.example.toml`

**Step 1: Write the failing test**

```go
// internal/config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}

	if cfg.Daemon.PollIntervalSecs != 60 {
		t.Errorf("expected default poll_interval_secs=60, got %d", cfg.Daemon.PollIntervalSecs)
	}
	if cfg.Daemon.MaxParallelTickets != 3 {
		t.Errorf("expected default max_parallel_tickets=3, got %d", cfg.Daemon.MaxParallelTickets)
	}
	if cfg.Cost.MaxLlmCallsPerTask != 8 {
		t.Errorf("expected default max_llm_calls_per_task=8, got %d", cfg.Cost.MaxLlmCallsPerTask)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Errorf("expected default driver=sqlite, got %q", cfg.Database.Driver)
	}
}

func TestLoadConfig_FromFile(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "foreman.toml")
	err := os.WriteFile(configFile, []byte(`
[daemon]
poll_interval_secs = 120
log_level = "debug"

[cost]
max_cost_per_ticket_usd = 25.0
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(configFile)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	if cfg.Daemon.PollIntervalSecs != 120 {
		t.Errorf("expected poll_interval_secs=120, got %d", cfg.Daemon.PollIntervalSecs)
	}
	if cfg.Daemon.LogLevel != "debug" {
		t.Errorf("expected log_level=debug, got %q", cfg.Daemon.LogLevel)
	}
	if cfg.Cost.MaxCostPerTicketUSD != 25.0 {
		t.Errorf("expected max_cost_per_ticket_usd=25.0, got %f", cfg.Cost.MaxCostPerTicketUSD)
	}
}

func TestValidateConfig_SQLiteMaxParallel(t *testing.T) {
	cfg, _ := LoadDefaults()
	cfg.Database.Driver = "sqlite"
	cfg.Daemon.MaxParallelTickets = 10

	errs := Validate(cfg)
	found := false
	for _, e := range errs {
		if e.Error() != "" {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error for SQLite with max_parallel_tickets > 3")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL

**Step 3: Implement config loading**

```go
// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/canhta/foreman/internal/models"
	"github.com/spf13/viper"
)

func LoadDefaults() (*models.Config, error) {
	v := viper.New()
	setDefaults(v)

	var cfg models.Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return &cfg, nil
}

func LoadFromFile(path string) (*models.Config, error) {
	v := viper.New()
	setDefaults(v)

	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg models.Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	expandEnvVars(&cfg)
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("daemon.poll_interval_secs", 60)
	v.SetDefault("daemon.idle_poll_interval_secs", 300)
	v.SetDefault("daemon.max_parallel_tickets", 3)
	v.SetDefault("daemon.work_dir", "~/.foreman/work")
	v.SetDefault("daemon.log_level", "info")
	v.SetDefault("daemon.log_format", "json")

	v.SetDefault("dashboard.enabled", true)
	v.SetDefault("dashboard.port", 3333)
	v.SetDefault("dashboard.host", "127.0.0.1")

	v.SetDefault("tracker.provider", "local_file")
	v.SetDefault("tracker.pickup_label", "foreman-ready")
	v.SetDefault("tracker.clarification_label", "foreman-needs-info")
	v.SetDefault("tracker.clarification_timeout_hours", 72)

	v.SetDefault("git.provider", "github")
	v.SetDefault("git.backend", "native")
	v.SetDefault("git.default_branch", "main")
	v.SetDefault("git.auto_push", true)
	v.SetDefault("git.pr_draft", true)
	v.SetDefault("git.branch_prefix", "foreman")
	v.SetDefault("git.rebase_before_pr", true)

	v.SetDefault("llm.default_provider", "anthropic")
	v.SetDefault("llm.outage.max_connection_retries", 3)
	v.SetDefault("llm.outage.connection_retry_delay_secs", 30)

	v.SetDefault("models.planner", "anthropic:claude-sonnet-4-5-20250929")
	v.SetDefault("models.implementer", "anthropic:claude-sonnet-4-5-20250929")
	v.SetDefault("models.spec_reviewer", "anthropic:claude-haiku-4-5-20251001")
	v.SetDefault("models.quality_reviewer", "anthropic:claude-haiku-4-5-20251001")
	v.SetDefault("models.final_reviewer", "anthropic:claude-sonnet-4-5-20250929")
	v.SetDefault("models.clarifier", "anthropic:claude-haiku-4-5-20251001")

	v.SetDefault("cost.max_cost_per_ticket_usd", 15.0)
	v.SetDefault("cost.max_cost_per_day_usd", 150.0)
	v.SetDefault("cost.max_cost_per_month_usd", 3000.0)
	v.SetDefault("cost.alert_threshold_percent", 80)
	v.SetDefault("cost.max_llm_calls_per_task", 8)

	v.SetDefault("limits.max_tasks_per_ticket", 20)
	v.SetDefault("limits.max_implementation_retries", 2)
	v.SetDefault("limits.max_spec_review_cycles", 2)
	v.SetDefault("limits.max_quality_review_cycles", 1)
	v.SetDefault("limits.max_task_duration_secs", 600)
	v.SetDefault("limits.max_total_duration_secs", 7200)
	v.SetDefault("limits.context_token_budget", 80000)
	v.SetDefault("limits.enable_partial_pr", true)
	v.SetDefault("limits.enable_clarification", true)
	v.SetDefault("limits.enable_tdd_verification", true)
	v.SetDefault("limits.search_replace_similarity", 0.92)
	v.SetDefault("limits.search_replace_min_context_lines", 3)

	v.SetDefault("secrets.enabled", true)
	v.SetDefault("secrets.always_exclude", []string{".env", ".env.*", "*.pem", "*.key", "*.p12"})

	v.SetDefault("rate_limit.requests_per_minute", 50)
	v.SetDefault("rate_limit.burst_size", 10)
	v.SetDefault("rate_limit.backoff_base_ms", 1000)
	v.SetDefault("rate_limit.backoff_max_ms", 60000)
	v.SetDefault("rate_limit.jitter_percent", 25)

	v.SetDefault("runner.mode", "local")
	v.SetDefault("runner.local.allowed_commands", []string{"npm", "yarn", "pnpm", "cargo", "go", "pytest", "make", "bun"})

	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("database.sqlite.path", "~/.foreman/foreman.db")
	v.SetDefault("database.sqlite.busy_timeout_ms", 5000)
	v.SetDefault("database.sqlite.wal_mode", true)
	v.SetDefault("database.sqlite.event_flush_interval_ms", 100)
}

func expandEnvVars(cfg *models.Config) {
	cfg.LLM.Anthropic.APIKey = expandEnv(cfg.LLM.Anthropic.APIKey)
	cfg.LLM.OpenAI.APIKey = expandEnv(cfg.LLM.OpenAI.APIKey)
	cfg.LLM.OpenRouter.APIKey = expandEnv(cfg.LLM.OpenRouter.APIKey)
	cfg.Dashboard.AuthToken = expandEnv(cfg.Dashboard.AuthToken)
	cfg.Database.Postgres.URL = expandEnv(cfg.Database.Postgres.URL)
}

func expandEnv(s string) string {
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		envVar := s[2 : len(s)-1]
		return os.Getenv(envVar)
	}
	return s
}

func Validate(cfg *models.Config) []error {
	var errs []error

	if cfg.Database.Driver == "sqlite" && cfg.Daemon.MaxParallelTickets > 3 {
		errs = append(errs, fmt.Errorf("max_parallel_tickets cannot exceed 3 with SQLite (got %d), use PostgreSQL for higher concurrency", cfg.Daemon.MaxParallelTickets))
	}

	return errs
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/ foreman.example.toml
git commit -m "feat: add config loading with viper, defaults, env var expansion, and validation"
```

---

## Phase 1 Verification

After completing all 10 tasks, verify the full build:

```bash
go build -o foreman .
go test ./... -v -race
go vet ./...
```

Expected: All tests pass, binary builds, no race conditions.

**Phase 1 delivers:**
- Domain models for the entire system
- LLM provider interface + Anthropic implementation
- Command runner (local)
- Implementer output parser with fuzzy matching
- Secrets scanner
- Token budget management
- SQLite database with full schema
- Config loading with sensible defaults
- CLI scaffolding

**Next:** Create Phase 2 plan (Full Pipeline: orchestrator, planner, plan validator, TDD verifier, feedback loops, git provider, context assembler).
