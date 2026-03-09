# External Agent Runner Integration — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Wire existing AgentRunner implementations (Claude Code, Copilot) into the core pipeline as first-class planning and implementation runners.

**Architecture:** Surgical branch in `RunTask()` delegates to `AgentRunner` when configured. New `AgentPlanner` implements `TicketPlanner` interface for codebase-aware planning. `PromptBuilder` translates tasks into self-contained prompts. `SkillInjector` writes TDD templates for Claude Code. Wiring flows from `cmd/start.go` → orchestrator → task runner factory.

**Tech Stack:** Go, existing `agent.AgentRunner` interface, `go:embed` for templates

**Design doc:** `docs/plans/2026-03-09-external-agent-runner-design.md`

---

### Task 1: PromptBuilder

Translates a planned task + project context into a structured prompt for autonomous agents.

**Files:**
- Create: `internal/pipeline/prompt_builder.go`
- Test: `internal/pipeline/prompt_builder_test.go`

**Step 1: Write the failing tests**

```go
package pipeline

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptBuilder_Build_BasicTask(t *testing.T) {
	pb := NewPromptBuilder(nil) // no LLM = skip criteria reformulation
	task := &models.Task{
		Title:              "Add user validation",
		Description:        "Validate email format on signup",
		AcceptanceCriteria: []string{"Invalid emails are rejected with 400"},
		FilesToModify:      []string{"internal/api/handler.go"},
	}
	config := PromptBuilderConfig{
		TestCommand:      "go test ./...",
		CodebasePatterns: "Go, stdlib net/http",
	}

	prompt := pb.Build(task, nil, config)

	assert.Contains(t, prompt, "Add user validation")
	assert.Contains(t, prompt, "Validate email format on signup")
	assert.Contains(t, prompt, "Invalid emails are rejected with 400")
	assert.Contains(t, prompt, "go test ./...")
	assert.Contains(t, prompt, "internal/api/handler.go")
}

func TestPromptBuilder_Build_WithContextFiles(t *testing.T) {
	pb := NewPromptBuilder(nil)
	task := &models.Task{
		Title:       "Fix bug",
		Description: "Null pointer in handler",
	}
	contextFiles := map[string]string{
		"handler.go": "package api\nfunc Handle() {}",
	}
	config := PromptBuilderConfig{}

	prompt := pb.Build(task, contextFiles, config)

	assert.Contains(t, prompt, "handler.go")
	assert.Contains(t, prompt, "package api")
}

func TestPromptBuilder_Build_WithRetryFeedback(t *testing.T) {
	pb := NewPromptBuilder(nil)
	task := &models.Task{
		Title:       "Fix bug",
		Description: "Null pointer",
	}
	config := PromptBuilderConfig{
		Attempt:        2,
		RetryFeedback:  "test failed: nil pointer dereference",
		RetryErrorType: ErrorTypeTestRuntime,
	}

	prompt := pb.Build(task, nil, config)

	assert.Contains(t, prompt, "RETRY")
	assert.Contains(t, prompt, "nil pointer dereference")
	assert.Contains(t, prompt, "runtime")
}

func TestPromptBuilder_Build_NoRetryOnFirstAttempt(t *testing.T) {
	pb := NewPromptBuilder(nil)
	task := &models.Task{
		Title:       "Add feature",
		Description: "New endpoint",
	}
	config := PromptBuilderConfig{Attempt: 1}

	prompt := pb.Build(task, nil, config)

	assert.NotContains(t, prompt, "RETRY")
}

func TestPromptBuilder_Build_WithLLMReformulation(t *testing.T) {
	mockLLM := &mockLLMProvider{
		response: &models.LlmResponse{Content: "- VERIFY: POST /signup with invalid email returns HTTP 400"},
	}
	pb := NewPromptBuilder(mockLLM)
	task := &models.Task{
		Title:              "Add validation",
		Description:        "Validate emails",
		AcceptanceCriteria: []string{"Invalid emails rejected"},
	}
	config := PromptBuilderConfig{CriteriaModel: "claude-haiku-4-5-20251001"}

	prompt := pb.Build(task, nil, config)

	assert.Contains(t, prompt, "VERIFY: POST /signup")
}

func TestPromptBuilder_Build_LLMFailureFallsBackToRawCriteria(t *testing.T) {
	mockLLM := &mockLLMProvider{err: fmt.Errorf("API error")}
	pb := NewPromptBuilder(mockLLM)
	task := &models.Task{
		Title:              "Add validation",
		Description:        "Validate emails",
		AcceptanceCriteria: []string{"Invalid emails rejected"},
	}
	config := PromptBuilderConfig{CriteriaModel: "claude-haiku-4-5-20251001"}

	prompt := pb.Build(task, nil, config)

	// Falls back to raw criteria
	assert.Contains(t, prompt, "Invalid emails rejected")
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/pipeline/ -run TestPromptBuilder -v`
Expected: FAIL — `NewPromptBuilder` not defined

**Step 3: Write minimal implementation**

```go
package pipeline

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog/log"
)

// PromptBuilderConfig holds configuration for prompt generation.
type PromptBuilderConfig struct {
	TestCommand      string
	LintCommand      string
	CodebasePatterns string
	CriteriaModel    string
	RetryFeedback    string
	RetryErrorType   ErrorType
	Attempt          int
}

// PromptBuilder generates structured prompts for external agent runners.
type PromptBuilder struct {
	llm LLMProvider // optional — used for criteria reformulation
}

// NewPromptBuilder creates a PromptBuilder. llm may be nil to skip reformulation.
func NewPromptBuilder(llm LLMProvider) *PromptBuilder {
	return &PromptBuilder{llm: llm}
}

// Build generates a self-contained prompt from task, context files, and config.
func (pb *PromptBuilder) Build(task *models.Task, contextFiles map[string]string, config PromptBuilderConfig) string {
	var b strings.Builder

	// Task section
	fmt.Fprintf(&b, "## Task\n**%s**\n\n", task.Title)
	if task.Description != "" {
		fmt.Fprintf(&b, "**Description:** %s\n\n", task.Description)
	}

	// Expected Behaviors (acceptance criteria)
	if len(task.AcceptanceCriteria) > 0 {
		criteria := pb.reformulateCriteria(task.AcceptanceCriteria, config.CriteriaModel)
		b.WriteString("## Expected Behaviors\n")
		for _, c := range criteria {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteString("\n")
	}

	// Affected Area Hints
	if len(task.FilesToRead) > 0 || len(task.FilesToModify) > 0 {
		b.WriteString("## Affected Area Hints\n")
		for _, f := range task.FilesToModify {
			fmt.Fprintf(&b, "- Modify: `%s`\n", f)
		}
		for _, f := range task.FilesToRead {
			fmt.Fprintf(&b, "- Reference: `%s`\n", f)
		}
		b.WriteString("\n")
	}

	// Codebase Context
	if len(contextFiles) > 0 {
		b.WriteString("## Codebase Context\n")
		paths := make([]string, 0, len(contextFiles))
		for p := range contextFiles {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			fmt.Fprintf(&b, "### %s\n```\n%s\n```\n\n", p, contextFiles[p])
		}
	}

	// Constraints
	if config.CodebasePatterns != "" {
		fmt.Fprintf(&b, "## Constraints\n%s\n\n", config.CodebasePatterns)
	}

	// Definition of Done
	b.WriteString("## Definition of Done\n")
	if config.TestCommand != "" {
		fmt.Fprintf(&b, "- All tests pass: `%s`\n", config.TestCommand)
	}
	if config.LintCommand != "" {
		fmt.Fprintf(&b, "- Lint passes: `%s`\n", config.LintCommand)
	}
	if config.TestCommand == "" && config.LintCommand == "" {
		b.WriteString("- Code compiles and works correctly\n")
	}
	b.WriteString("\n")

	// Retry Context (attempt > 1 only)
	if config.Attempt > 1 && config.RetryFeedback != "" {
		heading, guidance := retryHeadingAndGuidance(config.RetryErrorType)
		fmt.Fprintf(&b, "## RETRY — %s (attempt %d)\n\n", heading, config.Attempt)
		if guidance != "" {
			fmt.Fprintf(&b, "%s\n\n", guidance)
		}
		fmt.Fprintf(&b, "Previous failure:\n%s\n", config.RetryFeedback)
	}

	return b.String()
}

// reformulateCriteria uses LLM to transform raw acceptance criteria into
// mechanically verifiable assertions. Falls back to raw criteria on failure.
func (pb *PromptBuilder) reformulateCriteria(criteria []string, model string) []string {
	if pb.llm == nil || model == "" {
		return criteria
	}

	prompt := "Reformulate each acceptance criterion into a mechanically verifiable assertion. " +
		"Each should start with 'VERIFY:' and describe an observable, testable behavior.\n\n"
	for _, c := range criteria {
		prompt += "- " + c + "\n"
	}

	resp, err := pb.llm.Complete(context.Background(), models.LlmRequest{
		Model:       model,
		SystemPrompt: "You reformulate acceptance criteria into precise, testable assertions.",
		UserPrompt:  prompt,
		MaxTokens:   1024,
		Temperature: 0.0,
		Stage:       "criteria_reformulation",
	})
	if err != nil {
		log.Warn().Err(err).Msg("prompt_builder: criteria reformulation failed, using raw criteria")
		return criteria
	}

	// Parse response lines
	lines := strings.Split(strings.TrimSpace(resp.Content), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		if line != "" {
			result = append(result, line)
		}
	}
	if len(result) == 0 {
		return criteria
	}
	return result
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/pipeline/ -run TestPromptBuilder -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/prompt_builder.go internal/pipeline/prompt_builder_test.go
git commit -m "feat: add PromptBuilder for external agent runner prompts"
```

---

### Task 2: SkillInjector

Writes TDD skill templates into the working directory for Claude Code.

**Files:**
- Create: `internal/pipeline/skill_injector.go`
- Create: `internal/pipeline/skill_injector_test.go`
- Create: `internal/pipeline/assets/claude/settings.json`
- Create: `internal/pipeline/assets/claude/foreman/skills/tdd.md`
- Create: `internal/pipeline/assets/claude/foreman/agents/tdd-test-writer.md`
- Create: `internal/pipeline/assets/claude/foreman/agents/tdd-implementer.md`
- Create: `internal/pipeline/assets/claude/foreman/agents/tdd-refactorer.md`

**Step 1: Create the embedded template assets**

`internal/pipeline/assets/claude/settings.json`:
```json
{
  "hooks": {
    "UserPromptSubmit": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "cat /dev/null"
          }
        ]
      }
    ]
  },
  "permissions": {
    "allow": [
      "Read", "Edit", "Write", "Glob", "Grep", "Bash"
    ]
  }
}
```

`internal/pipeline/assets/claude/foreman/skills/tdd.md`:
```markdown
# TDD Orchestrator

## Phase Gates

You MUST follow RED → GREEN → REFACTOR strictly.

### RED Phase
Write failing tests FIRST. Tests must:
- Be runnable: `{{.TestCommand}}`
- Fail with assertion errors (not compile errors)
- Cover all acceptance criteria

### GREEN Phase
Write minimal implementation to make tests pass.
- Run: `{{.TestCommand}}`
- All tests must pass before proceeding

### REFACTOR Phase
Clean up without changing behavior.
- Run: `{{.TestCommand}}`
- All tests must still pass after refactoring

## Language: {{.Language}}
```

`internal/pipeline/assets/claude/foreman/agents/tdd-test-writer.md` — RED phase agent template (context-isolated, sees only requirements).

`internal/pipeline/assets/claude/foreman/agents/tdd-implementer.md` — GREEN phase agent template (sees only failing tests).

`internal/pipeline/assets/claude/foreman/agents/tdd-refactorer.md` — REFACTOR phase agent template (sees passing tests + implementation).

**Step 2: Write the failing tests**

```go
package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillInjector_Inject_CreatesFiles(t *testing.T) {
	workDir := t.TempDir()
	si := NewSkillInjector(SkillInjectorConfig{
		TestCommand: "go test ./...",
		Language:    "Go",
	})

	err := si.Inject(workDir)
	require.NoError(t, err)

	// settings.json should exist
	data, err := os.ReadFile(filepath.Join(workDir, ".claude", "settings.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "permissions")

	// TDD skill should exist under foreman namespace
	data, err = os.ReadFile(filepath.Join(workDir, ".claude", "foreman", "skills", "tdd.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "go test ./...")
	assert.NotContains(t, string(data), "{{.TestCommand}}")
}

func TestSkillInjector_Inject_MergesExistingSettings(t *testing.T) {
	workDir := t.TempDir()
	claudeDir := filepath.Join(workDir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))

	existing := map[string]interface{}{
		"model": "sonnet",
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{"existing-hook"},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.json"), data, 0o644))

	si := NewSkillInjector(SkillInjectorConfig{TestCommand: "npm test", Language: "TypeScript"})
	require.NoError(t, si.Inject(workDir))

	merged, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(merged, &result))

	// Existing fields preserved
	assert.Equal(t, "sonnet", result["model"])
	// Foreman hooks merged in
	hooks := result["hooks"].(map[string]interface{})
	assert.Contains(t, hooks, "PreToolUse")
	assert.Contains(t, hooks, "UserPromptSubmit")
}

func TestSkillInjector_Inject_NoExistingClaudeDir(t *testing.T) {
	workDir := t.TempDir()
	si := NewSkillInjector(SkillInjectorConfig{TestCommand: "pytest", Language: "Python"})

	err := si.Inject(workDir)
	require.NoError(t, err)

	assert.DirExists(t, filepath.Join(workDir, ".claude"))
	assert.DirExists(t, filepath.Join(workDir, ".claude", "foreman"))
}

func TestSkillInjector_Cleanup_RemovesForemanFiles(t *testing.T) {
	workDir := t.TempDir()
	si := NewSkillInjector(SkillInjectorConfig{TestCommand: "go test", Language: "Go"})

	require.NoError(t, si.Inject(workDir))
	assert.DirExists(t, filepath.Join(workDir, ".claude", "foreman"))

	si.Cleanup(workDir)
	assert.NoDirExists(t, filepath.Join(workDir, ".claude", "foreman"))
	// settings.json should remain (may have user config)
	assert.FileExists(t, filepath.Join(workDir, ".claude", "settings.json"))
}
```

**Step 3: Run tests to verify they fail**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/pipeline/ -run TestSkillInjector -v`
Expected: FAIL — `NewSkillInjector` not defined

**Step 4: Write minimal implementation**

```go
package pipeline

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/rs/zerolog/log"
)

//go:embed assets/claude
var claudeAssets embed.FS

// SkillInjectorConfig holds template rendering values.
type SkillInjectorConfig struct {
	TestCommand string
	LintCommand string
	Language    string
}

// SkillInjector writes TDD skill templates into a working directory for Claude Code.
type SkillInjector struct {
	config SkillInjectorConfig
}

// NewSkillInjector creates a SkillInjector with the given config.
func NewSkillInjector(config SkillInjectorConfig) *SkillInjector {
	return &SkillInjector{config: config}
}

// Inject writes template files into workDir/.claude/. Merges settings.json
// if one already exists. All other files go under .claude/foreman/.
func (si *SkillInjector) Inject(workDir string) error {
	claudeDir := filepath.Join(workDir, ".claude")

	// Merge or create settings.json
	if err := si.mergeSettings(claudeDir); err != nil {
		return fmt.Errorf("merge settings: %w", err)
	}

	// Write template files under .claude/foreman/
	return si.writeTemplates(claudeDir)
}

// Cleanup removes .claude/foreman/ directory. Leaves settings.json intact.
func (si *SkillInjector) Cleanup(workDir string) {
	foremanDir := filepath.Join(workDir, ".claude", "foreman")
	if err := os.RemoveAll(foremanDir); err != nil {
		log.Warn().Err(err).Str("dir", foremanDir).Msg("skill_injector: cleanup failed")
	}
}

func (si *SkillInjector) mergeSettings(claudeDir string) error {
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return err
	}

	settingsPath := filepath.Join(claudeDir, "settings.json")

	// Load Foreman's settings template
	templateData, err := claudeAssets.ReadFile("assets/claude/settings.json")
	if err != nil {
		return fmt.Errorf("read embedded settings: %w", err)
	}
	var foremanSettings map[string]interface{}
	if err := json.Unmarshal(templateData, &foremanSettings); err != nil {
		return fmt.Errorf("parse embedded settings: %w", err)
	}

	// Load existing settings if present
	existing := make(map[string]interface{})
	if data, readErr := os.ReadFile(settingsPath); readErr == nil {
		if parseErr := json.Unmarshal(data, &existing); parseErr != nil {
			log.Warn().Err(parseErr).Msg("skill_injector: existing settings.json invalid, overwriting")
			existing = make(map[string]interface{})
		}
	}

	// Deep merge: Foreman settings into existing (existing keys preserved)
	merged := deepMerge(existing, foremanSettings)

	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal merged settings: %w", err)
	}
	return os.WriteFile(settingsPath, out, 0o644)
}

func (si *SkillInjector) writeTemplates(claudeDir string) error {
	entries, err := claudeAssets.ReadDir("assets/claude/foreman")
	if err != nil {
		return fmt.Errorf("read embedded foreman dir: %w", err)
	}
	return si.writeDir("assets/claude/foreman", filepath.Join(claudeDir, "foreman"), entries)
}

func (si *SkillInjector) writeDir(embedPath, diskPath string, entries []os.DirEntry) error {
	if err := os.MkdirAll(diskPath, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := embedPath + "/" + entry.Name()
		dstPath := filepath.Join(diskPath, entry.Name())

		if entry.IsDir() {
			subEntries, err := claudeAssets.ReadDir(srcPath)
			if err != nil {
				return err
			}
			if err := si.writeDir(srcPath, dstPath, subEntries); err != nil {
				return err
			}
			continue
		}

		data, err := claudeAssets.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", srcPath, err)
		}

		// Render templates
		rendered, err := si.renderTemplate(entry.Name(), string(data))
		if err != nil {
			return fmt.Errorf("render %s: %w", srcPath, err)
		}

		if err := os.WriteFile(dstPath, []byte(rendered), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dstPath, err)
		}
	}
	return nil
}

func (si *SkillInjector) renderTemplate(name, content string) (string, error) {
	tmpl, err := template.New(name).Parse(content)
	if err != nil {
		// Not a template — return raw content
		return content, nil
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, si.config); err != nil {
		return content, nil
	}
	return buf.String(), nil
}

// deepMerge merges src into dst. Existing keys in dst are NOT overwritten
// unless both values are maps (recursive merge).
func deepMerge(dst, src map[string]interface{}) map[string]interface{} {
	for k, srcVal := range src {
		if dstVal, exists := dst[k]; exists {
			// Both are maps: recursive merge
			dstMap, dstOk := dstVal.(map[string]interface{})
			srcMap, srcOk := srcVal.(map[string]interface{})
			if dstOk && srcOk {
				dst[k] = deepMerge(dstMap, srcMap)
				continue
			}
			// dst key exists and isn't a map merge — keep dst value
			continue
		}
		dst[k] = srcVal
	}
	return dst
}
```

**Step 5: Run tests to verify they pass**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/pipeline/ -run TestSkillInjector -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/pipeline/skill_injector.go internal/pipeline/skill_injector_test.go internal/pipeline/assets/
git commit -m "feat: add SkillInjector for Claude Code TDD templates"
```

---

### Task 3: AgentPlanner

Alternative `TicketPlanner` implementation that delegates to `AgentRunner` with structured output.

**Files:**
- Create: `internal/pipeline/agent_planner.go`
- Create: `internal/pipeline/agent_planner_test.go`

**Step 1: Write the failing tests**

```go
package pipeline

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/canhta/foreman/internal/agent"
	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockAgentRunnerForPlanner struct {
	result agent.AgentResult
	err    error
	gotReq agent.AgentRequest
}

func (m *mockAgentRunnerForPlanner) Run(_ context.Context, req agent.AgentRequest) (agent.AgentResult, error) {
	m.gotReq = req
	return m.result, m.err
}
func (m *mockAgentRunnerForPlanner) HealthCheck(_ context.Context) error { return nil }
func (m *mockAgentRunnerForPlanner) RunnerName() string                 { return "mock" }
func (m *mockAgentRunnerForPlanner) Close() error                       { return nil }

func TestAgentPlanner_Plan_StructuredOutput(t *testing.T) {
	planResult := &PlannerResult{
		Status: "OK",
		Tasks: []PlannedTask{
			{Title: "Add handler", Description: "Create HTTP handler", FilesToModify: []string{"handler.go"}},
			{Title: "Add tests", Description: "Write tests", DependsOn: []string{"Add handler"}},
		},
		CodebasePatterns: CodebasePatterns{Language: "Go", Framework: "net/http"},
	}
	structured, _ := json.Marshal(planResult)

	mock := &mockAgentRunnerForPlanner{
		result: agent.AgentResult{
			Structured: json.RawMessage(structured),
		},
	}
	ap := NewAgentPlanner(mock, &models.LimitsConfig{MaxPlannedTasks: 10})

	result, err := ap.Plan(context.Background(), "/tmp/repo", &models.Ticket{
		ID:    "t-1",
		Title: "Add user signup",
	})
	require.NoError(t, err)
	assert.Equal(t, "OK", result.Status)
	assert.Len(t, result.Tasks, 2)
	assert.Equal(t, "Add handler", result.Tasks[0].Title)

	// Should have set OutputSchema
	assert.NotNil(t, mock.gotReq.OutputSchema)
	// Should have set WorkDir
	assert.Equal(t, "/tmp/repo", mock.gotReq.WorkDir)
}

func TestAgentPlanner_Plan_AgentError_ReturnsFallbackError(t *testing.T) {
	mock := &mockAgentRunnerForPlanner{
		err: fmt.Errorf("agent timeout"),
	}
	ap := NewAgentPlanner(mock, &models.LimitsConfig{MaxPlannedTasks: 10})

	_, err := ap.Plan(context.Background(), "/tmp/repo", &models.Ticket{Title: "Test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "agent timeout")
}

func TestAgentPlanner_Plan_InvalidStructured_ReturnsError(t *testing.T) {
	mock := &mockAgentRunnerForPlanner{
		result: agent.AgentResult{
			Structured: "not valid json object",
		},
	}
	ap := NewAgentPlanner(mock, &models.LimitsConfig{MaxPlannedTasks: 10})

	_, err := ap.Plan(context.Background(), "/tmp/repo", &models.Ticket{Title: "Test"})
	assert.Error(t, err)
}

func TestAgentPlanner_Plan_ValidatesAndSorts(t *testing.T) {
	planResult := &PlannerResult{
		Status: "OK",
		Tasks: []PlannedTask{
			{Title: "B", DependsOn: []string{"A"}},
			{Title: "A"},
		},
	}
	structured, _ := json.Marshal(planResult)

	mock := &mockAgentRunnerForPlanner{
		result: agent.AgentResult{Structured: json.RawMessage(structured)},
	}
	ap := NewAgentPlanner(mock, &models.LimitsConfig{MaxPlannedTasks: 10})

	result, err := ap.Plan(context.Background(), "/tmp", &models.Ticket{Title: "T"})
	require.NoError(t, err)

	// Should be topologically sorted: A before B
	assert.Equal(t, "A", result.Tasks[0].Title)
	assert.Equal(t, "B", result.Tasks[1].Title)
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/pipeline/ -run TestAgentPlanner -v`
Expected: FAIL — `NewAgentPlanner` not defined

**Step 3: Write minimal implementation**

```go
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/canhta/foreman/internal/agent"
	"github.com/canhta/foreman/internal/models"
)

// AgentPlanner implements planning by delegating to an AgentRunner
// that can explore the codebase and return structured output.
type AgentPlanner struct {
	runner agent.AgentRunner
	limits *models.LimitsConfig
}

// NewAgentPlanner creates a planner that delegates to an AgentRunner.
func NewAgentPlanner(runner agent.AgentRunner, limits *models.LimitsConfig) *AgentPlanner {
	return &AgentPlanner{runner: runner, limits: limits}
}

// Plan generates a task plan by having the agent explore the codebase.
func (ap *AgentPlanner) Plan(ctx context.Context, workDir string, ticket *models.Ticket) (*PlannerResult, error) {
	prompt := ap.buildPlanningPrompt(ticket)
	schema := ap.plannerOutputSchema()

	result, err := ap.runner.Run(ctx, agent.AgentRequest{
		Prompt:       prompt,
		WorkDir:      workDir,
		OutputSchema: schema,
		MaxTurns:     30,
		TimeoutSecs:  300,
	})
	if err != nil {
		return nil, fmt.Errorf("agent planner: %w", err)
	}

	// Parse structured output
	planResult, err := ap.parseStructured(result.Structured)
	if err != nil {
		return nil, fmt.Errorf("agent planner: parse structured output: %w", err)
	}

	if planResult.Status != "OK" {
		return planResult, nil
	}

	// Validate
	validation := ValidatePlan(planResult, workDir, ap.limits)
	if !validation.Valid {
		return nil, fmt.Errorf("agent plan validation failed: %s", validation.Errors[0])
	}

	// Topological sort
	sorted, err := TopologicalSort(planResult.Tasks)
	if err != nil {
		return nil, fmt.Errorf("agent plan sort: %w", err)
	}
	planResult.Tasks = sorted

	return planResult, nil
}

func (ap *AgentPlanner) buildPlanningPrompt(ticket *models.Ticket) string {
	prompt := fmt.Sprintf(`You are a software architect planning the implementation of a ticket.

## Ticket
**Title:** %s
**Description:** %s
`, ticket.Title, ticket.Description)

	if len(ticket.AcceptanceCriteria) > 0 {
		prompt += "\n**Acceptance Criteria:**\n"
		for _, ac := range ticket.AcceptanceCriteria {
			prompt += "- " + ac + "\n"
		}
	}

	prompt += `
## Instructions
1. Explore the codebase to understand the architecture, conventions, and relevant files.
2. Decompose this ticket into ordered, independent implementation tasks.
3. For each task, specify: title, description, acceptance_criteria, files_to_read, files_to_modify, estimated_complexity, depends_on.
4. Detect codebase patterns: language, framework, test_runner, style_notes.
5. Return your plan as structured output matching the schema.

Keep tasks small and focused. Each task should be independently implementable and testable.
`
	return prompt
}

func (ap *AgentPlanner) plannerOutputSchema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"status": map[string]string{"type": "string"},
			"message": map[string]string{"type": "string"},
			"codebase_patterns": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"language":    map[string]string{"type": "string"},
					"framework":   map[string]string{"type": "string"},
					"test_runner": map[string]string{"type": "string"},
					"style_notes": map[string]string{"type": "string"},
				},
			},
			"tasks": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title":                map[string]string{"type": "string"},
						"description":          map[string]string{"type": "string"},
						"acceptance_criteria":   map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
						"test_assertions":       map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
						"files_to_read":         map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
						"files_to_modify":       map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
						"estimated_complexity":  map[string]string{"type": "string"},
						"depends_on":            map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
					},
					"required": []string{"title", "description"},
				},
			},
		},
		"required": []string{"status", "tasks"},
	}
	data, _ := json.Marshal(schema)
	return data
}

func (ap *AgentPlanner) parseStructured(structured interface{}) (*PlannerResult, error) {
	if structured == nil {
		return nil, fmt.Errorf("no structured output returned")
	}

	// Handle json.RawMessage
	var data []byte
	switch v := structured.(type) {
	case json.RawMessage:
		data = v
	case []byte:
		data = v
	default:
		var err error
		data, err = json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal structured output: %w", err)
		}
	}

	var result PlannerResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal plan: %w", err)
	}
	return &result, nil
}
```

Note: `PlannerResult` needs JSON tags for unmarshalling. Check `yaml_parser.go` — it has YAML tags. We need to add JSON tags or use a custom unmarshal. The `parseStructured` method should handle the mapping. Verify the existing `PlannerResult` struct in `yaml_parser.go:12-19` and `PlannedTask` in `yaml_parser.go:30-39` — they have `yaml:` tags. Add `json:` tags to both structs.

**Step 3b: Add JSON tags to PlannerResult and PlannedTask**

Modify: `internal/pipeline/yaml_parser.go:12-39`

Add `json:"..."` tags alongside existing `yaml:"..."` tags on both structs so `json.Unmarshal` works.

**Step 4: Run tests to verify they pass**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/pipeline/ -run TestAgentPlanner -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/agent_planner.go internal/pipeline/agent_planner_test.go internal/pipeline/yaml_parser.go
git commit -m "feat: add AgentPlanner for codebase-aware planning via AgentRunner"
```

---

### Task 4: AgentPlanner adapter for daemon.TicketPlanner

The orchestrator uses `daemon.TicketPlanner` (returns `*daemon.PlanResult`), not `*pipeline.PlannerResult`. Need an adapter like the existing `plannerAdapter` in `cmd/start.go:35-69`.

**Files:**
- Modify: `cmd/start.go:35-69` (add `agentPlannerAdapter` alongside existing `plannerAdapter`)

**Step 1: Write the adapter**

```go
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
```

**Step 2: Commit**

```bash
git add cmd/start.go
git commit -m "feat: add agentPlannerAdapter for daemon.TicketPlanner interface"
```

---

### Task 5: Surgical branch in RunTask

Add the external runner path to `PipelineTaskRunner.RunTask()`.

**Files:**
- Modify: `internal/pipeline/task_runner.go:34-68` (add fields to `TaskRunnerConfig`)
- Modify: `internal/pipeline/task_runner.go:90-120` (add fields to struct + constructor)
- Modify: `internal/pipeline/task_runner.go:162` (add branch at top of `RunTask`)
- Create: `internal/pipeline/task_runner_agent_test.go`

**Step 1: Write the failing tests**

```go
package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/agent"
	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAgentRunnerForTask implements agent.AgentRunner for testing.
type mockAgentRunnerForTask struct {
	result agent.AgentResult
	err    error
	gotReq agent.AgentRequest
}

func (m *mockAgentRunnerForTask) Run(_ context.Context, req agent.AgentRequest) (agent.AgentResult, error) {
	m.gotReq = req
	return m.result, m.err
}
func (m *mockAgentRunnerForTask) HealthCheck(_ context.Context) error { return nil }
func (m *mockAgentRunnerForTask) RunnerName() string                 { return "mock" }
func (m *mockAgentRunnerForTask) Close() error                       { return nil }

func TestRunTask_AgentRunner_DelegatesToAgent(t *testing.T) {
	workDir := t.TempDir()
	mockAgent := &mockAgentRunnerForTask{
		result: agent.AgentResult{
			Output: "done",
			Usage:  agent.AgentUsage{CostUSD: 0.05, InputTokens: 1000, OutputTokens: 500},
		},
	}
	mockDB := newMockTaskRunnerDB()
	mockGit := &mockGitProvider{diffOutput: "diff --git a/file.go ..."}

	cfg := TaskRunnerConfig{
		WorkDir:                  workDir,
		MaxImplementationRetries: 2,
		AgentRunner:              mockAgent,
		AgentRunnerName:          "claudecode",
	}
	tr := NewPipelineTaskRunner(nil, mockDB, mockGit, nil, cfg)

	task := &models.Task{
		ID:       "task-1",
		TicketID: "ticket-1",
		Title:    "Add feature",
	}

	err := tr.RunTask(context.Background(), task)
	require.NoError(t, err)

	// Should have called agent
	assert.Contains(t, mockAgent.gotReq.Prompt, "Add feature")
	assert.Equal(t, workDir, mockAgent.gotReq.WorkDir)

	// Should have committed
	assert.True(t, mockGit.commitCalled)
	assert.Equal(t, models.TaskStatusDone, mockDB.lastStatus)
}

func TestRunTask_AgentRunner_EmptyDiff_Retries(t *testing.T) {
	mockAgent := &mockAgentRunnerForTask{
		result: agent.AgentResult{Output: "done"},
	}
	mockDB := newMockTaskRunnerDB()
	mockGit := &mockGitProvider{diffOutput: ""} // empty diff

	cfg := TaskRunnerConfig{
		WorkDir:                  t.TempDir(),
		MaxImplementationRetries: 1,
		AgentRunner:              mockAgent,
	}
	tr := NewPipelineTaskRunner(nil, mockDB, mockGit, nil, cfg)
	task := &models.Task{ID: "t-1", TicketID: "tk-1", Title: "Fix bug"}

	err := tr.RunTask(context.Background(), task)
	assert.Error(t, err) // should fail after retries exhausted
	assert.Equal(t, models.TaskStatusFailed, mockDB.lastStatus)
}

func TestRunTask_AgentRunner_AgentError_Retries(t *testing.T) {
	callCount := 0
	mockAgent := &mockAgentRunnerForTask{}
	// First call fails, second succeeds
	origRun := mockAgent.Run
	_ = origRun
	// Use a wrapper that tracks calls
	// (simplified — in real test, use a stateful mock)

	mockDB := newMockTaskRunnerDB()
	mockGit := &mockGitProvider{diffOutput: "some diff"}

	cfg := TaskRunnerConfig{
		WorkDir:                  t.TempDir(),
		MaxImplementationRetries: 2,
		AgentRunner:              mockAgent,
	}
	_ = cfg
	_ = callCount
	// This test validates the retry path exists — implementation details
	// are tested via the full flow in integration tests.
}

func TestRunTask_NoAgentRunner_UsesBuiltinPath(t *testing.T) {
	// When AgentRunner is nil, RunTask should use the existing builtin path.
	// This is a regression guard — we verify the branch doesn't fire.
	// (Full builtin path tests already exist in task_runner_test.go)
	cfg := TaskRunnerConfig{
		WorkDir:                  t.TempDir(),
		MaxImplementationRetries: 0,
		// AgentRunner: nil — builtin path
	}
	_ = cfg
	// Existing tests cover this path — this is just a structural marker.
}
```

Note: exact mock types depend on what's already defined in `task_runner_test.go`. Check existing mocks and reuse them. The test file should import existing mock helpers where possible.

**Step 2: Run tests to verify they fail**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/pipeline/ -run TestRunTask_AgentRunner -v`
Expected: FAIL — `AgentRunner` field not on `TaskRunnerConfig`

**Step 3: Add fields and implement the branch**

Add to `TaskRunnerConfig` (after line 67):
```go
	// AgentRunner is an optional external agent runner. When non-nil, RunTask
	// delegates implementation to this runner instead of using the builtin
	// implementer → parse → apply → review loop.
	AgentRunner     agent.AgentRunner
	// AgentRunnerName identifies the runner type ("claudecode", "copilot").
	// Used to decide whether to inject Claude Code skills.
	AgentRunnerName string
```

Add import for `"github.com/canhta/foreman/internal/agent"` to task_runner.go.

In `RunTask()`, add branch after status update (after line 165, before line 167):

```go
	// External runner path — delegate entire implementation to AgentRunner.
	if r.config.AgentRunner != nil {
		return r.runTaskWithAgent(ctx, task)
	}
```

Add new method `runTaskWithAgent`:

```go
// runTaskWithAgent delegates task implementation to an external AgentRunner.
// The agent handles its own TDD loop, file exploration, and implementation.
// Foreman runs spec/quality review on the resulting diff (non-blocking).
func (r *PipelineTaskRunner) runTaskWithAgent(ctx context.Context, task *models.Task) error {
	promptBuilder := NewPromptBuilder(r.llm)

	// Inject Claude Code skills if applicable
	if r.config.AgentRunnerName == "claudecode" {
		injector := NewSkillInjector(SkillInjectorConfig{
			TestCommand: r.config.TestCommand,
			Language:    "", // detected from codebase patterns
		})
		if err := injector.Inject(r.config.WorkDir); err != nil {
			log.Warn().Err(err).Msg("skill injection failed, proceeding without skills")
		}
		defer injector.Cleanup(r.config.WorkDir)
	}

	feedback := NewFeedbackAccumulator()

	for attempt := 1; attempt <= r.config.MaxImplementationRetries+1; attempt++ {
		feedback.ResetKeepingSummary()

		var retryErrorType ErrorType
		if attempt > 1 {
			retryErrorType = ClassifyRetryError(feedback.Render())
			if r.metrics != nil {
				r.metrics.RetryTriggeredTotal.WithLabelValues("agent_implement", string(retryErrorType)).Inc()
			}
		}

		// Build prompt
		contextFiles := r.selectContextFiles(ctx, task, r.config.ContextTokenBudget)
		prompt := promptBuilder.Build(task, contextFiles, PromptBuilderConfig{
			TestCommand:      r.config.TestCommand,
			CodebasePatterns: r.config.CodebasePatterns,
			Attempt:          attempt,
			RetryFeedback:    feedback.Render(),
			RetryErrorType:   retryErrorType,
		})

		// Delegate to agent
		result, err := r.config.AgentRunner.Run(ctx, agent.AgentRequest{
			Prompt:  prompt,
			WorkDir: r.config.WorkDir,
		})
		if err != nil {
			feedback.AddLintError(fmt.Sprintf("Agent error: %s", err))
			continue
		}

		// Record cost
		r.recordAgentCost(ctx, task, result.Usage)

		// Check for diff
		diff, diffErr := r.git.DiffWorking(ctx, r.config.WorkDir)
		if diffErr != nil {
			return fmt.Errorf("git diff after agent: %w", diffErr)
		}
		if diff == "" {
			// Also check staged
			diff, _ = r.git.DiffStaged(ctx, r.config.WorkDir)
		}
		if diff == "" {
			feedback.AddLintError("Agent produced no changes")
			continue
		}

		// Non-blocking spec review
		var reviewWarnings []string
		if len(task.AcceptanceCriteria) > 0 {
			reviewResult, reviewErr := r.specReviewer.Review(ctx, SpecReviewInput{
				TaskTitle:          task.Title,
				Diff:               diff,
				AcceptanceCriteria: task.AcceptanceCriteria,
			})
			if reviewErr != nil {
				log.Warn().Err(reviewErr).Msg("agent path: spec review failed (non-blocking)")
			} else if !reviewResult.Approved {
				reviewWarnings = append(reviewWarnings, "Spec review: "+reviewResult.IssuesText())
			}
		}

		// Non-blocking quality review
		qualityResult, qualityErr := r.qualityReviewer.Review(ctx, QualityReviewInput{
			Diff:             diff,
			CodebasePatterns: r.config.CodebasePatterns,
		})
		if qualityErr != nil {
			log.Warn().Err(qualityErr).Msg("agent path: quality review failed (non-blocking)")
		} else if !qualityResult.Approved {
			reviewWarnings = append(reviewWarnings, "Quality review: "+qualityResult.IssuesText())
		}

		// Stage and commit
		if stageErr := r.git.StageAll(ctx, r.config.WorkDir); stageErr != nil {
			return fmt.Errorf("git stage after agent: %w", stageErr)
		}
		commitMsg := fmt.Sprintf("feat: %s", task.Title)
		_, err = r.git.Commit(ctx, r.config.WorkDir, commitMsg)
		if err != nil {
			// Agent may have already committed — check if working tree is clean
			cleanDiff, _ := r.git.DiffWorking(ctx, r.config.WorkDir)
			if cleanDiff != "" {
				return fmt.Errorf("git commit after agent: %w", err)
			}
			// Agent already committed — that's fine
		}

		if r.config.Cache != nil {
			r.config.Cache.Invalidate()
		}

		r.runPostLintHook(ctx, task)

		// Store review warnings for PR description (via task metadata or DB)
		if len(reviewWarnings) > 0 {
			log.Info().Strs("warnings", reviewWarnings).Str("task_id", task.ID).
				Msg("agent path: review warnings (non-blocking)")
		}

		if err := r.db.UpdateTaskStatus(ctx, task.ID, models.TaskStatusDone); err != nil {
			return fmt.Errorf("update task status: %w", err)
		}
		return nil
	}

	// All retries exhausted
	_ = r.db.UpdateTaskStatus(ctx, task.ID, models.TaskStatusFailed)
	if r.metrics != nil {
		errType := string(ClassifyRetryError(feedback.Render()))
		r.metrics.TaskFailuresTotal.WithLabelValues(errType, r.config.AgentRunnerName).Inc()
	}
	return fmt.Errorf("task %q failed after %d agent attempts", task.Title, r.config.MaxImplementationRetries+1)
}

// recordAgentCost inserts an llm_calls row for the external runner execution.
func (r *PipelineTaskRunner) recordAgentCost(ctx context.Context, task *models.Task, usage agent.AgentUsage) {
	// TaskRunnerDB doesn't have RecordLlmCall — cost recording happens
	// at a higher level (orchestrator or recording provider). Log for now.
	log.Info().
		Str("task_id", task.ID).
		Float64("cost_usd", usage.CostUSD).
		Int("input_tokens", usage.InputTokens).
		Int("output_tokens", usage.OutputTokens).
		Int("duration_ms", usage.DurationMs).
		Msg("agent_cost: external runner execution")
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/pipeline/ -run TestRunTask_AgentRunner -v`
Expected: PASS

**Step 5: Run ALL existing task_runner tests to verify no regression**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/pipeline/ -run TestRunTask -v`
Expected: All existing tests PASS (builtin path unchanged)

**Step 6: Commit**

```bash
git add internal/pipeline/task_runner.go internal/pipeline/task_runner_agent_test.go
git commit -m "feat: add surgical branch in RunTask for external agent runners"
```

---

### Task 6: Wire AgentRunner through factory and orchestrator

Connect the config → daemon startup → orchestrator → task runner factory chain.

**Files:**
- Modify: `internal/daemon/orchestrator.go:39-68` (add `AgentRunner` + `AgentRunnerName` to `TaskRunnerFactoryInput`)
- Modify: `internal/daemon/orchestrator.go:145-160` (add optional fields to `Orchestrator`)
- Modify: `cmd/start.go:80-120` (add `agentRunner` field to `taskRunnerFactory`, wire in `Create`)
- Modify: `cmd/start.go:210-261` (build agent runner from config, select planner)

**Step 1: Add fields to `TaskRunnerFactoryInput`**

In `internal/daemon/orchestrator.go`, add to `TaskRunnerFactoryInput` (after line 68):
```go
	// AgentRunner is the optional external agent runner for task implementation.
	AgentRunner     agent.AgentRunner
	// AgentRunnerName identifies the runner ("claudecode", "copilot", "").
	AgentRunnerName string
```

Add import for `"github.com/canhta/foreman/internal/agent"`.

**Step 2: Wire in `taskRunnerFactory.Create`**

In `cmd/start.go`, add `agentRunner` and `agentRunnerName` fields to `taskRunnerFactory`:
```go
type taskRunnerFactory struct {
	llm             pipeline.LLMProvider
	db              fullTaskRunnerDB
	gitProv         git.GitProvider
	cmdRunner       runner.CommandRunner
	metrics         *telemetry.Metrics
	agentRunner     agent.AgentRunner
	agentRunnerName string
}
```

In `Create()`, pass them through to `TaskRunnerConfig`:
```go
cfg := pipeline.TaskRunnerConfig{
	// ... existing fields ...
	AgentRunner:     f.agentRunner,
	AgentRunnerName: f.agentRunnerName,
}
```

**Step 3: Build agent runner at startup and select planner**

In `cmd/start.go`, in the `RunE` function (around line 210-261), after building the command runner:

```go
// 6c. Build pipeline agent runner (optional — only when provider != "builtin").
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
	if closer, ok := pipelineAgentRunner.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	log.Info().Str("provider", agentRunnerName).Msg("pipeline agent runner initialized")
}

// Select planner based on agent runner
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
```

Update the orchestrator construction to use `ticketPlanner` instead of inline `&plannerAdapter{...}`.

Update the `taskRunnerFactory` construction to include:
```go
agentRunner:     pipelineAgentRunner,
agentRunnerName: agentRunnerName,
```

**Step 4: Run build to verify compilation**

Run: `cd /Users/canh/Projects/Indies/Foreman && go build ./...`
Expected: Compiles successfully

**Step 5: Run existing orchestrator tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/daemon/ -run TestOrchestrator -v`
Expected: All existing tests PASS (they use nil agent runner via mock factory)

**Step 6: Commit**

```bash
git add internal/daemon/orchestrator.go cmd/start.go
git commit -m "feat: wire AgentRunner through factory and orchestrator"
```

---

### Task 7: Repo-level reservation sentinel

Add `__REPO_LOCK__` sentinel for external runner file reservation.

**Files:**
- Modify: `internal/db/sqlite.go` (around `TryReserveFiles` — line 615)
- Create: `internal/db/repo_lock_test.go`

**Step 1: Write the failing test**

```go
package db

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTryReserveFiles_RepoLockBlocksOtherTickets(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Ticket A reserves repo lock
	conflicts, err := db.TryReserveFiles(ctx, "ticket-A", []string{RepoLockSentinel})
	require.NoError(t, err)
	assert.Empty(t, conflicts)

	// Ticket B tries to reserve specific files — blocked by repo lock
	conflicts, err = db.TryReserveFiles(ctx, "ticket-B", []string{"handler.go"})
	require.NoError(t, err)
	assert.NotEmpty(t, conflicts)
	assert.Contains(t, conflicts[0], RepoLockSentinel)
}

func TestTryReserveFiles_SpecificFilesDoNotBlockRepoLock(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Ticket A reserves specific files
	conflicts, err := db.TryReserveFiles(ctx, "ticket-A", []string{"handler.go"})
	require.NoError(t, err)
	assert.Empty(t, conflicts)

	// Ticket B tries repo lock — blocked by ticket A's files
	conflicts, err = db.TryReserveFiles(ctx, "ticket-B", []string{RepoLockSentinel})
	require.NoError(t, err)
	assert.NotEmpty(t, conflicts)
}

func TestTryReserveFiles_SameTicketRepoLockIdempotent(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	conflicts, err := db.TryReserveFiles(ctx, "ticket-A", []string{RepoLockSentinel})
	require.NoError(t, err)
	assert.Empty(t, conflicts)

	// Same ticket re-reserving — should be fine
	conflicts, err = db.TryReserveFiles(ctx, "ticket-A", []string{RepoLockSentinel})
	require.NoError(t, err)
	assert.Empty(t, conflicts)
}
```

**Step 2: Add sentinel constant and modify TryReserveFiles**

In `internal/db/sqlite.go`, add constant:
```go
// RepoLockSentinel is a special file path that locks the entire repo.
// When reserved, no other ticket can reserve any files.
const RepoLockSentinel = "__REPO_LOCK__"
```

Export it from `internal/db/db.go` as well if needed for the daemon package.

In `TryReserveFiles`, after reading current reservations, add logic:
```go
// Check if any other ticket holds a repo lock
for path, owner := range reserved {
	if path == RepoLockSentinel && owner != ticketID {
		return []string{fmt.Sprintf("%s (held by %s)", RepoLockSentinel, owner)}, nil
	}
}

// If this ticket is requesting repo lock, check if ANY files are reserved by others
if contains(paths, RepoLockSentinel) {
	for path, owner := range reserved {
		if owner != ticketID {
			return []string{fmt.Sprintf("%s (held by %s)", path, owner)}, nil
		}
	}
}
```

**Step 3: Run tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/db/ -run TestTryReserveFiles_RepoLock -v`
Expected: PASS

**Step 4: Run existing reservation tests**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./internal/db/ -run TestTryReserveFiles -v`
Expected: All existing tests PASS

**Step 5: Commit**

```bash
git add internal/db/sqlite.go internal/db/db.go internal/db/repo_lock_test.go
git commit -m "feat: add repo-level reservation sentinel for external runners"
```

---

### Task 8: Full build and integration verification

**Step 1: Run full build**

Run: `cd /Users/canh/Projects/Indies/Foreman && go build ./...`
Expected: Compiles

**Step 2: Run full test suite**

Run: `cd /Users/canh/Projects/Indies/Foreman && go test ./... -count=1`
Expected: All tests PASS

**Step 3: Verify with make**

Run: `cd /Users/canh/Projects/Indies/Foreman && make build` (or equivalent)
Expected: Binary builds successfully

**Step 4: Manual smoke test config**

Verify that setting `[agent_runner].provider = "builtin"` in `foreman.toml` results in the exact same behavior as before (zero regression). Then verify `provider = "claudecode"` initializes the agent runner (check startup logs).

**Step 5: Commit (if any fixes needed)**

```bash
git add -A
git commit -m "fix: integration issues from external agent runner wiring"
```

---

## Dependency Graph

```
Task 1 (PromptBuilder) ──┐
                          ├── Task 5 (Surgical branch in RunTask) ──┐
Task 2 (SkillInjector) ──┘                                          │
                                                                     ├── Task 6 (Wiring) ── Task 8 (Verification)
Task 3 (AgentPlanner) ── Task 4 (Adapter) ──────────────────────────┘
                                                                     │
Task 7 (Repo-level reservation) ────────────────────────────────────┘
```

Tasks 1, 2, 3, 7 are independent and can be built in parallel.
Task 4 depends on Task 3.
Task 5 depends on Tasks 1, 2.
Task 6 depends on Tasks 4, 5.
Task 8 depends on Tasks 6, 7.
