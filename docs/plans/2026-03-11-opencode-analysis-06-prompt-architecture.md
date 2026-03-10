# OpenCode Analysis 06: Prompt Architecture & Instruction Design

**Date:** 2026-03-11  
**Domain:** System prompt composition, runtime environment injection, context file discovery, model-specific prompts, permission-gated tools  
**Source:** Comparative analysis of `opencode/` vs `Foreman/`. See also `docs/prompt-architecture-analysis.md` for additional depth.

---

## How OpenCode Solves This

### Model-Specific Base Prompts

OpenCode maintains 6 distinct base prompt variants, one per model family (Claude, GPT, Gemini, Qwen, DeepSeek, LLaMA). The prompt structure, tool usage instructions, and example formats are tailored to each model's characteristics. This is not a template with substitutions — each variant is a fundamentally different document.

### Runtime Environment Block

Every system prompt includes a dynamically assembled `<env>` block injected at runtime:

```xml
<env>
  Working directory: /path/to/project
  Is directory a git repo: yes
  Platform: darwin
  Today's date: Mon Jan 06 2025
</env>
```

This prevents the LLM from wasting turns just to orient itself. The LLM knows the context from turn 1.

### Context File Discovery (AGENTS.md)

OpenCode walks up the directory tree from the working directory, collecting any `AGENTS.md` files it finds. Each file is labeled with its source path and concatenated into the system prompt. This supports layered configuration: org-wide rules in `~/.config`, repo-wide rules in the root, and project-specific rules in a subdirectory.

### Reactive Injection on File Access

When the agent accesses a file, OpenCode checks for `.agents.md` files in the same directory. If found, it injects a `<system-reminder>` into the conversation mid-loop. This allows per-directory guidance to be surfaced exactly when relevant.

### Two-Block Prompt Caching Structure

For Anthropic, OpenCode splits the system prompt into two parts:
1. **Static header** (model + env + AGENTS.md — rarely changes) → gets a long-lived cache breakpoint
2. **Dynamic block** (current task instructions) → gets a short-lived breakpoint

This maximizes cache hit rates because the expensive static content is cached across multiple turns.

### Mid-Loop System Reminders

OpenCode can inject `<system-reminder>` messages mid-conversation (e.g., "You are now in build mode, you can edit files" when transitioning from planning to building). These are visible to the LLM but distinguished from regular user messages.

### Agent Persona System

Each named agent (`build`, `plan`, `explore`, `compaction`) carries: system prompt, allowed tools, model override, and temperature. The complete agent definition is assembled before the first LLM call. No late-binding.

---

## Issues in Our Current Approach

### G1 — Dual Prompt System: Registry vs `assembler.go` (Critical)

Foreman has two independent prompt systems:
1. `internal/prompt/registry.go` — `PromptRegistry` with typed `PromptTemplate` structs (only used by the Claude Code runner)
2. `internal/context/assembler.go` — hardcoded Go string templates assembled at runtime (used by `BuiltinRunner`)

These two systems evolve independently. Improvements to one do not propagate to the other. The `BuiltinRunner` (the primary runner) does not use the `PromptRegistry` at all.

### G2 — No Runtime Environment Block (High)

Foreman does not inject a dynamic `<env>` block into the system prompt. The LLM does not know the working directory, platform, or current date unless it calls a tool. This wastes turns on orientation.

### G3 — No Model-Specific Prompts (Medium)

All agents receive the same prompt regardless of the underlying model. Claude, GPT, and Gemini have different strengths, idioms, and failure modes. Generic prompts are suboptimal for non-Claude models.

### G4 — No Mid-Loop System Reminders (Medium)

Mode transitions (e.g., planner unlocking write access after a plan is approved) are not communicated to the LLM mid-loop. The LLM may continue behaving as if it has only read-only access.

### G5 — No Graceful Max-Steps Termination Message (Medium)

When `maxTurns` is reached, `builtin.go` returns with a hard cutoff. The LLM receives no "you are running out of steps" warning. OpenCode injects a warning at `stepsLeft <= 3` so the LLM can wrap up cleanly.

### G6 — No Prompt Caching Structure Optimization (Medium — cost)

The system prompt is assembled as a single block. For Anthropic, this means the entire system prompt is re-uploaded on every turn. Splitting into a static header + dynamic block would allow the expensive static content to be cached.

### G7 — Reactive Context Injection Not Always Wired (Low)

The `ContextProvider` interface exists and works in the builtin runner, but pipeline/daemon callers do not always wire a context provider. Some call paths get no reactive context injection.

---

## Specific Improvements to Adopt

### I1: Consolidate to a Single Prompt System
Deprecate `assembler.go` as the authoritative prompt system. Move all prompt templates into `PromptRegistry` (or a new unified `ContextBuilder`). The `BuiltinRunner` should use the registry.

**Effort:** Medium. **Impact:** Eliminates prompt drift between runners.

### I2: Add Runtime Environment Block
Inject `<env>` block at system prompt assembly time: working dir, git status (bool), platform, current date.

**Effort:** Very low. **Impact:** Eliminates orientation turns.

### I3: Add Max-Steps Warning Injection
When `turnsRemaining <= 3`, inject a `<system-reminder>` warning: "You have N steps remaining. Wrap up or ask for clarification."

**Effort:** Very low.

### I4: Add Model-Variant Prompt Selection
Add a `RenderForModel(modelFamily string)` method that selects a model-appropriate prompt variant. Start with Claude vs generic. Expand as needed.

**Effort:** Low.

### I5: Split System Prompt for Anthropic Caching
Identify the stable/static portion of the system prompt (rules, AGENTS.md, env block) and the dynamic portion (current task). Emit them as two separate system blocks so Anthropic can cache the static one.

**Effort:** Low.

---

## Concrete Implementation Suggestions

### I1 + I2 — `ContextBuilder` Replacing `assembler.go`

```go
// internal/context/builder.go
package context

import (
    "fmt"
    "os"
    "path/filepath"
    "runtime"
    "strings"
    "time"
)

type ContextBuilder struct {
    WorkDir    string
    ModelFamily string // "claude" | "gpt" | "gemini" | "generic"
    AgentsFiles []string // found by walking up from WorkDir
}

func NewContextBuilder(workDir, modelFamily string) (*ContextBuilder, error) {
    b := &ContextBuilder{
        WorkDir:    workDir,
        ModelFamily: modelFamily,
    }
    b.AgentsFiles = discoverAgentsFiles(workDir)
    return b, nil
}

// EnvBlock returns the dynamic <env> block
func (b *ContextBuilder) EnvBlock() string {
    isGit := "no"
    if _, err := os.Stat(filepath.Join(b.WorkDir, ".git")); err == nil {
        isGit = "yes"
    }
    return fmt.Sprintf(`<env>
  Working directory: %s
  Is directory a git repo: %s
  Platform: %s
  Today's date: %s
</env>`,
        b.WorkDir,
        isGit,
        runtime.GOOS,
        time.Now().Format("Mon Jan 02 2006"),
    )
}

// AgentsContext returns concatenated AGENTS.md content with source labels
func (b *ContextBuilder) AgentsContext() string {
    if len(b.AgentsFiles) == 0 { return "" }
    var sb strings.Builder
    for _, f := range b.AgentsFiles {
        content, err := os.ReadFile(f)
        if err != nil { continue }
        rel, _ := filepath.Rel(b.WorkDir, f)
        fmt.Fprintf(&sb, "<!-- Source: %s -->\n%s\n\n", rel, strings.TrimSpace(string(content)))
    }
    return sb.String()
}

// Build assembles the complete system prompt
func (b *ContextBuilder) Build(basePrompt, taskInstructions string) string {
    parts := []string{
        basePrompt,
        b.EnvBlock(),
    }
    if ctx := b.AgentsContext(); ctx != "" {
        parts = append(parts, ctx)
    }
    parts = append(parts, taskInstructions)
    return strings.Join(parts, "\n\n")
}

func discoverAgentsFiles(workDir string) []string {
    var files []string
    dir := workDir
    for {
        candidate := filepath.Join(dir, "AGENTS.md")
        if _, err := os.Stat(candidate); err == nil {
            files = append([]string{candidate}, files...) // prepend (outermost first)
        }
        parent := filepath.Dir(dir)
        if parent == dir { break }
        dir = parent
    }
    // Also check config dirs
    if home, err := os.UserHomeDir(); err == nil {
        for _, configPath := range []string{
            filepath.Join(home, ".config", "foreman", "AGENTS.md"),
            filepath.Join(home, ".foreman", "AGENTS.md"),
        } {
            if _, err := os.Stat(configPath); err == nil {
                files = append([]string{configPath}, files...)
            }
        }
    }
    return files
}
```

### I3 — Max-Steps Warning Injection

```go
// internal/agent/builtin.go — inside the turn loop
const stepsWarnAt = 3

turnsRemaining := maxTurns - turn
if turnsRemaining <= stepsWarnAt && turnsRemaining > 0 {
    reminderMsg := fmt.Sprintf(
        "<system-reminder>You have %d step(s) remaining. "+
            "Wrap up your current work, summarize what was accomplished, "+
            "and stop or ask for clarification if needed.</system-reminder>",
        turnsRemaining,
    )
    messages = append(messages, models.Message{
        Role:    "user",
        Content: reminderMsg,
    })
}
```

### I4 — Model-Variant Prompt Selection

```go
// internal/prompt/registry.go — add RenderForModel
type PromptVariants struct {
    Claude  string
    GPT     string
    Gemini  string
    Generic string
}

func (v PromptVariants) RenderForModel(modelID string) string {
    switch {
    case strings.Contains(modelID, "claude"):
        return v.Claude
    case strings.Contains(modelID, "gpt") || strings.Contains(modelID, "o1") || strings.Contains(modelID, "o3"):
        if v.GPT != "" { return v.GPT }
    case strings.Contains(modelID, "gemini"):
        if v.Gemini != "" { return v.Gemini }
    }
    if v.Generic != "" { return v.Generic }
    return v.Claude // default
}
```

### I5 — Two-Block Anthropic Caching

```go
// internal/llm/anthropic.go — split system prompt into static + dynamic blocks
type anthropicSystemBlock struct {
    Type         string            `json:"type"`
    Text         string            `json:"text"`
    CacheControl *anthropicCache   `json:"cache_control,omitempty"`
}

func buildAnthropicSystemBlocks(staticPart, dynamicPart string) []anthropicSystemBlock {
    blocks := []anthropicSystemBlock{
        {
            Type: "text",
            Text: staticPart,
            // Cache static content (env block, AGENTS.md, base prompt)
            CacheControl: &anthropicCache{Type: "ephemeral"},
        },
    }
    if dynamicPart != "" {
        blocks = append(blocks, anthropicSystemBlock{
            Type: "text",
            Text: dynamicPart,
            // Cache dynamic block too (changes per turn, but caching still helps)
            CacheControl: &anthropicCache{Type: "ephemeral"},
        })
    }
    return blocks
}
```

---

## Recommended Implementation Order

| # | Improvement | Effort | Priority |
|---|-------------|--------|----------|
| 1 | Add runtime `<env>` block injection (I2) | Very low | High — eliminates wasted orientation turns |
| 2 | Add max-steps warning at N≤3 turns remaining (I3) | Very low | High — graceful termination |
| 3 | Two-block system prompt for Anthropic caching (I5) | Low | High — cost reduction |
| 4 | Consolidate to single prompt system via `ContextBuilder` (I1) | Medium | High — eliminates prompt drift |
| 5 | Model-variant prompt selection (I4) | Low | Medium |
