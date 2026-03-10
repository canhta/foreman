# Prompt Architecture & Instruction Design: OpenCode vs Foreman

**Scope:** Comparative analysis of how each codebase constructs, layers, and dynamically updates the prompts sent to LLMs.

---

## 1. OpenCode's Prompt Architecture

### 1.1 Provider-Specific Base Prompts

**File:** `packages/opencode/src/session/system.ts` (`SystemPrompt.provider()`)

OpenCode selects a base system prompt at runtime based on the model ID, not a generic one-size-fits-all prompt:

| Model family | File | Key characteristics |
|---|---|---|
| `claude-*` | `prompt/anthropic.txt` | Persona definition, todo management, task workflow |
| `gemini-*` | `prompt/gemini.txt` | Detailed 6-step workflow, output safety rules |
| `gpt-4*` / heavy | `prompt/beast.txt` | Ultra-autonomous, mandates web research |
| `gpt-5*` / `codex*` | `prompt/codex_header.txt` | Editing constraints, git hygiene, frontend guidelines |
| `qwen*` (fallback) | `prompt/qwen.txt` | Anthropic minus todo management |
| Trinity | `prompt/trinity.txt` | Trinity-specific persona |

**Key insight:** Each model gets a prompt that matches its known strengths and weaknesses. The Claude prompt leans on tool-use patterns; the Gemini prompt enforces step-by-step workflow; the Codex prompt emphasizes editing precision.

### 1.2 Runtime Environment Injection

**File:** `packages/opencode/src/session/system.ts` (`SystemPrompt.environment()`)

Every call injects a structured runtime context block using XML-style tags:

```
<env>
  Working directory: /path/to/project
  Is directory a git repo: yes
  Platform: darwin
  Today's date: Wed Mar 11 2026
</env>
<directories>
  src/
  tests/
  ...
</directories>
```

This is assembled fresh per call — the model always knows *exactly* where it is and what date it is.

### 1.3 Instruction Discovery (AGENTS.md Walking)

**File:** `packages/opencode/src/session/instruction.ts` (`InstructionPrompt.system()`)

At session start, OpenCode walks **upward** from the working directory to the repo root, collecting every `AGENTS.md`, `CLAUDE.md`, and `CONTEXT.md` file it finds. It also reads:
- `~/.claude/CLAUDE.md` — global user-level instructions
- URL-fetched instructions from config

Each file is labeled: `"Instructions from: /path/to/file\n"` before its content.

**Reactive injection** (`InstructionPrompt.resolve()`): When a tool accesses a file, OpenCode walks up from that file's directory and injects any *new* AGENTS.md files it hasn't loaded yet. Instructions flow in as the model explores — it never has stale context.

### 1.4 Final System Prompt Assembly

**File:** `packages/opencode/src/session/llm.ts:67–93`

The final system is a flat array of strings, structured in two blocks for Anthropic prompt caching:

```
Block 1 (cached header):
  [base provider prompt]      ← rarely changes
  [user.system override]      ← if any

Block 2 (dynamic):
  [environment block]         ← changes per call
  [AGENTS.md instructions]    ← changes as files are explored
```

### 1.5 Mid-Loop `<system-reminder>` Injection

**File:** `packages/opencode/src/session/prompt.ts`

OpenCode injects contextual reminders *into user messages* (not the system prompt) during the tool loop:

| Trigger | Injected content |
|---|---|
| Plan mode active | `prompt/plan.txt` — CRITICAL read-only constraint + 6-phase workflow |
| Plan → Build transition | `prompt/build-switch.txt` — "you are no longer in read-only mode" |
| Max steps reached | `prompt/max-steps.txt` — tools disabled, text-only response required |
| Self-queued messages (step > 1) | Wrapped with `<system-reminder>` tags |
| Reflection turns (every N) | Reflection prompt injected as user message |
| Doom loop detected | Directive message injected |

This is the key mechanism for **stateful mode switching** without rebuilding the system prompt.

### 1.6 Agent Persona System

**File:** `packages/opencode/src/agent/agent.ts`

Named agents (`build`, `plan`, `general`, `explore`, `compaction`, `title`, `summary`) each carry:
- A **permission ruleset** (which tools are allowed)
- A **mode** (primary or subagent)
- An optional **prompt override** (replaces the provider base prompt entirely)
- A **model** (agents can use different models)
- A **temperature**

Custom agents can be defined in `opencode.json` and merged with defaults. This means the prompt *and* tool access *and* model *and* sampling parameters all change together when the agent switches — a holistic persona swap.

---

## 2. Foreman's Current Prompt Architecture

### 2.1 Prompt Registry

**File:** `internal/prompts/registry.go`

A Go struct that loads all prompt files from `prompts/` at startup:
- `prompts/roles/*/ROLE.md` — pipeline stage prompts (planner, implementer, reviewers)
- `prompts/agents/*/AGENT.md` — Claude Code agent personas
- `prompts/skills/*/SKILL.md` — workflow hook definitions
- `prompts/fragments/*.md` — reusable blocks included via pongo2 `{% include %}`

Rendering: `Registry.Render(kind, name, vars)` renders a pongo2 (Jinja2-compatible) template with a variable map. This is correct and well-structured.

**However:** The registry is only used by the *Claude Code runner* path. The builtin runner (`internal/agent/builtin.go`) does NOT use the registry — it receives a pre-assembled `SystemPrompt` string in `AgentRequest`.

### 2.2 Context Assembly (The Hardcoded Parallel System)

**File:** `internal/context/assembler.go`

`AssemblePlannerContext()`, `AssembleImplementerContext()`, `AssembleSpecReviewerContext()`, `AssembleQualityReviewerContext()` all build system + user prompts using **hardcoded Go string literals**, not registry renders.

This creates two divergent prompt systems:
- Registry ROLE.md files (used by Claude Code runner)
- assembler.go string literals (used by builtin runner)

They contain similar content but are maintained separately and can drift out of sync.

### 2.3 Context Injection Layers (Builtin Runner)

**File:** `internal/agent/builtin.go:119–143`

```go
// Layer 1: project-level context files
if fc := loadForemanContext(req.WorkDir); fc != "" {
    systemPrompt = fc + "\n\n" + systemPrompt
}
// Layer 2: call-site system prompt
if req.SystemPrompt != "" {
    systemPrompt = systemPrompt + "\n\n" + req.SystemPrompt
}
```

**File:** `internal/context/walk_context_files.go`

Walks up from `startDir` to `workDir`, collects `AGENTS.md`, `.foreman-rules.md`, `.foreman/context.md`. More specific (deeper) files returned first — correct precedence ordering.

**Reactive injection:** `ContextProvider.OnFilesAccessed()` — after each tool result, walks up from accessed file paths and injects any new context files found. This mirrors OpenCode's `InstructionPrompt.resolve()`.

### 2.4 Dynamic Injections (Mid-Loop)

**File:** `internal/agent/builtin.go`

- **Reflection prompt** every N turns (default 5): injected as a user message to prompt self-correction
- **Doom loop detection** (3 consecutive identical calls): injects a directive user message
- **Context window management**: prune at 70%, LLM summarization at 85%, compaction fallback
- **Deduplication warnings**: warns when identical tool calls are detected

These are good mechanisms but they are less sophisticated than OpenCode's — no mode-switching, no step-limit enforcement with explicit text-only mode, no plan/build transition messaging.

### 2.5 ClaudeCode Runner Prompt Injection

**File:** `internal/agent/claudecode.go:96–98`

```go
if req.SystemPrompt != "" {
    args = append(args, "--append-system-prompt", req.SystemPrompt)
}
```

System prompt is appended via CLI flag. The `registry.ForClaude()` call writes the `.claude/agents/` and `.claude/commands/` directory structure before execution — so the Claude Code runner gets both the agent persona (from AGENT.md) and the command set.

### 2.6 No Runtime Environment Block

Foreman does not inject a structured `<env>` block with working directory, git status, platform, and date. The model must infer or discover this information through tool use. Repo analysis is done via `internal/context/repo_analyzer.go` but its results are not consistently injected into every call's system prompt.

---

## 3. Gaps and Weaknesses in Foreman

### Gap 1: Dual Prompt System (Critical)

**Files:** `internal/context/assembler.go` vs `prompts/roles/*/ROLE.md`

The builtin runner uses hardcoded Go strings in `assembler.go`; the Claude Code runner uses ROLE.md files. These two representations of the same prompts will diverge over time. Every prompt improvement must be applied in two places.

**Impact:** Prompt quality drift between runner backends; maintenance burden; no single source of truth.

### Gap 2: No Runtime Environment Injection

**Missing from:** Every call in `builtin.go` and `assembler.go`

The model is never told: working directory, current date, git status, detected language, detected test command. It must either infer this or call tools to discover it — wasting tokens and turns.

**Impact:** Extra tool calls (3-5) per task just for orientation. Higher cost; slower execution.

### Gap 3: No Model-Specific Prompt Variants

**Missing from:** `internal/prompts/registry.go`, `internal/context/assembler.go`

All models receive the same prompt. OpenCode tailors each prompt to the model's known characteristics. Foreman's prompts were written for Claude and are sent to all models including OpenRouter-proxied GPT-4 variants.

**Impact:** Suboptimal behavior when non-Claude models are used.

### Gap 4: No Mode-Switch Reminders

**Missing from:** `internal/agent/builtin.go`

When the pipeline transitions between phases (e.g., spec review → quality review → implementation retry), there is no mid-loop injection to signal the mode change. The system prompt is rebuilt on each new `AgentRequest`, but within a multi-turn builtin run, the agent has no mechanism to receive "your mode has changed" signals.

**Impact:** Model may continue in old behavioral mode; no way to enforce read-only phases inside the turn loop.

### Gap 5: No Step-Limit Enforcement Prompt

**Missing from:** `internal/agent/builtin.go`

When `MaxSteps` is exceeded in the builtin runner, the loop terminates, but there is no injected "tools are now disabled, respond with text only" message. The model is simply cut off. OpenCode injects `max-steps.txt` as a fake assistant message to elicit a graceful summary.

**Impact:** Abrupt termination without useful summary output; may lose work context.

### Gap 6: No Prompt Caching Strategy

**Missing from:** `internal/agent/builtin.go`, `internal/llm/`

OpenCode explicitly structures its system array into a stable "header block" (rarely changes — eligible for Anthropic prompt caching) and a "dynamic block" (changes per call). Foreman concatenates everything into one string with no caching hints.

**Impact:** Higher token costs for Anthropic API calls (prompt caching can save 90% on repeated prefix tokens).

### Gap 7: No Per-Role Agent Persona (in Builtin Runner)

**Missing from:** `internal/agent/builtin.go`

The builtin runner accepts a single `SystemPrompt` string with no notion of an "agent persona" (distinct tool permission set + model + temperature + prompt). The AGENT.md files exist for Claude Code runner only. Builtin tasks like "spec reviewer" and "quality reviewer" run with whatever tools were configured at call site.

**Impact:** Spec reviewer could accidentally use write tools; no enforcement of read-only behavior for reviewer roles.

### Gap 8: Instruction File Discovery is Pre-Assembly Only

**File:** `internal/context/walk_context_files.go`

Context files are walked and injected before the agent run starts. While `ContextProvider.OnFilesAccessed()` provides reactive injection, it is wired as an interface that callers must implement. In the pipeline's planner and implementer calls, this interface is not always used. OpenCode's reactive injection is always-on.

**Impact:** If the implementer navigates into a subdirectory with a local `AGENTS.md`, those instructions may not be loaded.

---

## 4. Specific Improvements Foreman Could Adopt

### Improvement A: Unify Prompt Storage — Registry as Single Source of Truth

**Priority: Critical**

Eliminate `assembler.go`'s hardcoded strings. All roles should render from ROLE.md files. The builtin runner should call `registry.Render("role", "planner", vars)` the same way the Claude Code runner does.

**Approach:**
1. Move the hardcoded planner/implementer/reviewer system prompts into their respective ROLE.md files (or verify the existing ROLE.md files are complete enough to replace assembler.go)
2. Thread `*prompts.Registry` into the pipeline stage functions
3. Replace `assembler.go` calls with `registry.Render()` calls
4. Delete `assembler.go`

### Improvement B: Runtime Environment Block

**Priority: High**

Inject a structured block at the start of every system prompt containing: working directory, git branch, platform, date, detected language, detected test command, detected lint command.

**Approach:**
```go
// internal/context/environment.go
type EnvBlock struct {
    WorkDir     string
    GitBranch   string
    Platform    string
    Date        string
    Language    string
    TestCmd     string
    LintCmd     string
}

func (e EnvBlock) Render() string {
    return fmt.Sprintf(
        "<env>\n  Working directory: %s\n  Git branch: %s\n  Platform: %s\n  Date: %s\n  Language: %s\n  Test command: %s\n  Lint command: %s\n</env>",
        e.WorkDir, e.GitBranch, e.Platform, e.Date, e.Language, e.TestCmd, e.LintCmd,
    )
}
```

Wire into `builtin.go` — prepend to the system prompt before first turn.

### Improvement C: Model-Specific Prompt Selection

**Priority: Medium**

Add a `model_variant` field to ROLE.md frontmatter (or a separate file per model family), and select the appropriate base prompt in the registry renderer.

Minimal approach: Add a `prompt_variants` map to the registry, keyed by model family prefix:

```go
// internal/prompts/registry.go
func (r *Registry) RenderForModel(kind EntryKind, name, modelID string, vars map[string]any) (string, error) {
    // Try model-specific variant first: "implementer-openai", "implementer-claude"
    family := modelFamily(modelID) // "claude", "gpt", "gemini", etc.
    if variant, ok := r.get(kind, name+"-"+family); ok {
        return r.render(variant, vars)
    }
    return r.Render(kind, name, vars)
}
```

### Improvement D: Mode-Switch Injections in Builtin Runner

**Priority: Medium**

Add a `InjectUserMessage(msg string)` method to the builtin runner's turn loop, callable from the pipeline to signal phase transitions. Add a `ModeReminder` field to `AgentRequest` that gets injected at turn 0 as a `<system-reminder>` tagged user message.

```go
// AgentRequest addition
type AgentRequest struct {
    // ... existing fields ...
    ModeReminder string // injected as <system-reminder> at start
}
```

Pipeline usage:
```go
req := AgentRequest{
    SystemPrompt: registry.Render("role", "spec-reviewer", vars),
    ModeReminder: "You are in READ-ONLY review mode. Do NOT modify any files.",
    // ...
}
```

### Improvement E: Graceful Max-Steps Termination

**Priority: Medium**

When the builtin runner hits `MaxSteps`, inject a final user message before stopping the loop:

```go
// internal/agent/builtin.go — in the step limit check
const maxStepsMsg = `CRITICAL - MAXIMUM STEPS REACHED

Tools are now disabled. Respond with text only.

Your response must include:
- Summary of work completed so far
- List of remaining tasks not yet completed
- Recommended next steps`

// inject as fake user turn, set toolChoice = "none"
messages = append(messages, llm.UserMessage(maxStepsMsg))
// do one final LLM call with no tools, capture output as result
```

### Improvement F: Prompt Caching Structure

**Priority: Medium (cost savings)**

Split the system prompt into two parts for Anthropic API calls: a stable cache-eligible prefix and a dynamic suffix.

```go
// internal/llm/anthropic.go
type CachedSystemPrompt struct {
    CacheableHeader string // role instructions — rarely changes
    DynamicBlock    string // env block + context files — changes per call
}

func (p CachedSystemPrompt) ToAnthropicMessages() []anthropic.SystemMessage {
    return []anthropic.SystemMessage{
        {Content: p.CacheableHeader, CacheControl: &anthropic.CacheControl{Type: "ephemeral"}},
        {Content: p.DynamicBlock},
    }
}
```

### Improvement G: Role-Based Tool Permission Enforcement

**Priority: Medium**

Encode each role's allowed tools in its ROLE.md frontmatter:

```yaml
---
name: spec-reviewer
allowed_tools: [read, readrange, listdir, glob, grep, getsymbol]
denied_tools: [write, edit, multiedit, bash, runtest]
---
```

The builtin runner reads this from the rendered role's frontmatter and filters `tools.Registry` before each turn.

---

## 5. Concrete Go Implementation Suggestions

### 5.1 Unified Context Builder

Replace the scattered `assembler.go` + `registry.Render()` calls with a single `ContextBuilder`:

```go
// internal/context/builder.go
package context

import (
    "fmt"
    "time"

    "github.com/canhta/foreman/internal/prompts"
)

type ContextBuilder struct {
    registry *prompts.Registry
    analyzer *RepoAnalyzer
}

func NewContextBuilder(reg *prompts.Registry, analyzer *RepoAnalyzer) *ContextBuilder {
    return &ContextBuilder{registry: reg, analyzer: analyzer}
}

type BuildResult struct {
    SystemPrompt string
    UserPrompt   string
}

func (b *ContextBuilder) Build(roleName, workDir string, vars map[string]any) (BuildResult, error) {
    // 1. Render role system prompt from registry
    systemPrompt, err := b.registry.Render(prompts.RoleKind, roleName, vars)
    if err != nil {
        return BuildResult{}, fmt.Errorf("render role %q: %w", roleName, err)
    }

    // 2. Prepend environment block
    env := b.buildEnvBlock(workDir)
    systemPrompt = env + "\n\n" + systemPrompt

    // 3. Prepend project context files (AGENTS.md etc.)
    if ctxFiles := LoadContextFiles(workDir, workDir); len(ctxFiles) > 0 {
        systemPrompt = ctxFiles + "\n\n" + systemPrompt
    }

    // 4. Build user prompt from vars (ticket info, file context, etc.)
    userPrompt := b.buildUserPrompt(roleName, vars)

    return BuildResult{SystemPrompt: systemPrompt, UserPrompt: userPrompt}, nil
}

func (b *ContextBuilder) buildEnvBlock(workDir string) string {
    info := b.analyzer.Analyze(workDir)
    return fmt.Sprintf(
        "<env>\n  Working directory: %s\n  Platform: %s\n  Date: %s\n  Language: %s\n  Test command: %s\n  Lint command: %s\n</env>",
        workDir,
        detectPlatform(),
        time.Now().Format("Mon Jan 02 2006"),
        info.Language,
        info.TestCommand,
        info.LintCommand,
    )
}
```

### 5.2 System-Reminder Injection in Builtin Runner

```go
// internal/agent/builtin.go — add to turn loop

func injectSystemReminder(messages []llm.Message, content string) []llm.Message {
    reminder := fmt.Sprintf("<system-reminder>\n%s\n</system-reminder>", content)
    return append(messages, llm.UserMessage(reminder))
}

// In turn loop, before max-steps cutoff:
if stepCount >= maxSteps-1 {
    messages = injectSystemReminder(messages, maxStepsReminder)
    // do one final call with toolChoice = "none"
}
```

### 5.3 Role Permission Filter

```go
// internal/agent/builtin.go

func filterToolsByRole(registry *tools.Registry, role *prompts.Entry) *tools.Registry {
    if role == nil || len(role.Frontmatter.AllowedTools) == 0 {
        return registry
    }
    allowed := make(map[string]bool)
    for _, t := range role.Frontmatter.AllowedTools {
        allowed[t] = true
    }
    return registry.Filter(func(name string) bool {
        return allowed[name]
    })
}
```

### 5.4 Model Family Selector

```go
// internal/prompts/registry.go

func modelFamily(modelID string) string {
    switch {
    case strings.HasPrefix(modelID, "claude"):
        return "claude"
    case strings.HasPrefix(modelID, "gpt"), strings.HasPrefix(modelID, "o1"), strings.HasPrefix(modelID, "o3"):
        return "openai"
    case strings.HasPrefix(modelID, "gemini"):
        return "gemini"
    default:
        return ""
    }
}

func (r *Registry) RenderForModel(kind EntryKind, name, modelID string, vars map[string]any) (string, error) {
    family := modelFamily(modelID)
    if family != "" {
        variantName := name + "-" + family
        if _, ok := r.entries[kind][variantName]; ok {
            return r.Render(kind, variantName, vars)
        }
    }
    return r.Render(kind, name, vars)
}
```

---

## Summary Table

| Capability | OpenCode | Foreman (Current) | Foreman Gap |
|---|---|---|---|
| Provider-specific base prompts | Yes — 6 variants by model ID | No — one prompt for all models | Gap 3 |
| Runtime environment block | Yes — `<env>` tag with dir/date/git/platform | No | Gap 2 |
| AGENTS.md discovery + labeling | Yes — walks up to repo root, labels each source | Partial — walks but no labeling | Gap 8 |
| Reactive AGENTS.md injection | Yes — on every file access | Interface exists, not always wired | Gap 8 |
| Mid-loop mode-switch reminders | Yes — plan/build/max-steps via `<system-reminder>` | Partial — reflection + doom loop only | Gap 4, 5 |
| Single source of truth for prompts | Yes — prompt files only | No — registry + assembler.go both exist | Gap 1 |
| Prompt caching structure | Yes — two-block Anthropic split | No | Gap 6 |
| Role-based tool permissions | Yes — per-agent permission ruleset | No | Gap 7 |
| Agent persona system | Yes — full persona (prompt+tools+model+temp) | Partial — Claude Code only | Gap 7 |
| Graceful step-limit termination | Yes — text-only mode injection | No — hard cutoff | Gap 5 |
