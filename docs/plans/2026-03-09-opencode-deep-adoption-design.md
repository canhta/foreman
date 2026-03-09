# OpenCode Deep Adoption — Design Document

## Problem

After a thorough deep-dive into OpenCode's architecture, we've identified significant gaps between Foreman and OpenCode that go beyond the initial adoption plan. OpenCode has 22+ tools vs Foreman's 14, a sophisticated permission system, tool output management, better edit strategies, session persistence, and richer agent patterns. This document covers everything the first two plans missed.

## Scope: What This Plan Covers (vs Previous Plans)

| Area | Previous Plans | This Plan |
|------|---------------|-----------|
| Prompt format | Unified registry (Plan 1) | — |
| Snapshot/undo | Core tracking (Plan 2) | — |
| Structured output | Schema validation (Plan 2) | — |
| Command registry | Basic registry (Plan 2) | — |
| Doom loop | Basic detection (Plan 2) | — |
| Compaction | Pruning-first (Plan 2) | — |
| Event bus | Basic pub/sub (Plan 2) | — |
| **New tools** | — | batch, webfetch, websearch, lsp, todo, question, skill |
| **Edit strategies** | — | 9 fallback matchers, Levenshtein similarity |
| **Tool output management** | — | Truncation, save-to-disk, hints |
| **Permission system** | — | Rule-based, per-agent, wildcard patterns |
| **Agent modes** | — | Plan mode (read-only), explore mode |
| **Skill discovery** | — | Multi-directory, hierarchy, auto-command registration |
| **Session persistence** | — | Session/message storage, resumption |
| **Subagent improvements** | — | Task resumption, better result handling |
| **File change tracking** | — | Per-session diff tracking, patch parts |
| **Cost tracking** | — | Per-session token/cost accounting |

## Analysis: Detailed Gap Analysis

### 1. Tool Gaps

**OpenCode tools Foreman is missing:**

| Tool | OpenCode | Foreman | Value |
|------|----------|---------|-------|
| `batch` | Parallel 25 tools in one call | None | HIGH — reduces LLM round-trips |
| `webfetch` | HTTP + HTML→markdown | None | MEDIUM — useful for docs/API reference |
| `websearch` | Exa API search | None | LOW — daemon doesn't need web search |
| `lsp` | 9 LSP operations | `GetErrors` only | HIGH — definitions, references, symbols |
| `todowrite/read` | Session task tracking | None | MEDIUM — agent can track own progress |
| `question` | Interactive user prompts | None | LOW — daemon is non-interactive |
| `skill` | Dynamic skill loading | None | MEDIUM — load context on demand |

**Existing tools that need improvement:**

| Tool | OpenCode Enhancement | Foreman Gap | Value |
|------|---------------------|-------------|-------|
| `Edit` | 9 fallback strategies | Exact match only | HIGH — reduces edit failures |
| `Read` | Binary/image detection, PDF support | Basic file read | MEDIUM |
| `Bash` | Tree-sitter command parsing, path extraction | Whitelist only | MEDIUM |
| `ApplyPatch` | Hunk validation before apply | Basic patch apply | HIGH |

### 2. Tool Output Management

**OpenCode pattern:**
- Automatic output truncation (2000 lines, 50KB max)
- Truncated content saved to disk at `~/.opencode/data/tool-output/`
- Hint message tells agent to use grep/read with offset for full content
- Tasks get special hint: "delegate to explore agent instead"

**Foreman gap:** No output management. Large tool outputs consume context directly.

**Adoption value:** HIGH — prevents context window waste on large file reads or grep results.

### 3. Edit Tool Strategies

**OpenCode's 9 fallback strategies (in order):**
1. **SimpleReplacer** — exact string match
2. **LineTrimmedReplacer** — trim whitespace per line
3. **BlockAnchorReplacer** — match first/last lines as anchors
4. **WhitespaceNormalizedReplacer** — ignore all whitespace differences
5. **IndentationFlexibleReplacer** — match with different indentation
6. **EscapeNormalizedReplacer** — handle escape sequence differences
7. **TrimmedBoundaryReplacer** — trim boundary whitespace
8. **ContextAwareReplacer** — context-aware block matching
9. **MultiOccurrenceReplacer** — find all occurrences

Plus Levenshtein distance scoring for "did you mean?" suggestions when no match found.

**Foreman gap:** Only exact match. LLM edit failures require full retry.

### 4. Permission System

**OpenCode pattern:**
- Rule-based: `{ permission, pattern, action }` where action = allow/deny/ask
- Per-agent rulesets with merge hierarchy: defaults → agent → user config
- Wildcard matching on both permission names and file patterns
- External directory gating (files outside project)
- Special mappings: edit/write/patch/multiedit all map to "edit" permission
- `DeniedError` vs `RejectedError` vs `CorrectedError`

**Foreman gap:** Simple tool whitelist (`default_allowed_tools` string set). No file-level or pattern-based permissions. No per-agent rules.

**Adoption value:** HIGH for production safety — prevents agents from modifying files outside their scope.

### 5. Agent Modes

**OpenCode agents:**
- **build** (primary) — full tool access, default agent
- **plan** (primary) — read-only, can only create plan files
- **explore** (subagent) — fast read-only codebase search
- **general** (subagent) — full multi-step research
- **compaction** (hidden) — internal context summarization
- **title** (hidden) — session title generation
- **summary** (hidden) — session summary generation

**Foreman gap:** No agent modes. All agents have same tool access. No read-only planning mode.

**Adoption value:** MEDIUM — plan mode useful for multi-step thinking before implementation.

### 6. Skill Discovery

**OpenCode pattern:**
- Multiple directories scanned: `~/.claude/skills/`, `./.opencode/skill/`, config paths
- Remote URLs with index.json for downloadable skills
- Skills auto-registered as invokable commands
- Skill tool for on-demand loading during sessions

**Foreman gap:** Skills are YAML files in `skills/` directory only. No hierarchy, no remote, no auto-command registration.

### 7. Session Persistence & Cost Tracking

**OpenCode pattern:**
- SQLite with Drizzle ORM for sessions, messages, parts
- Per-session cost tracking (input/output/cache tokens × pricing)
- Session forking, archiving, resumption
- Diff summary per session (files changed, additions, deletions)

**Foreman gap:** Has LLM call tracking but no session-level persistence. Cost tracked per-ticket but not per-session with detailed breakdown.

## Prioritized Adoption

### Phase 1: Tool Robustness (highest ROI)
1. **Edit tool strategies** — 9 fallback matchers reduce edit failures dramatically
2. **Tool output truncation** — prevent context waste on large outputs
3. **Batch tool** — parallel tool execution reduces round-trips
4. **ApplyPatch validation** — validate hunks before applying

### Phase 2: Agent Safety & Intelligence
5. **Permission system** — rule-based, per-agent, pattern matching
6. **Agent modes** — plan mode (read-only) and explore mode (fast search)
7. **LSP tool** — definitions, references, symbols for code navigation
8. **Todo tool** — session-specific progress tracking

### Phase 3: Skill & Session Infrastructure
9. **Skill discovery** — multi-directory, hierarchy, auto-command
10. **Session persistence** — store sessions/messages for resumption
11. **Per-session cost tracking** — detailed token/cost accounting
12. **Subagent improvements** — task resumption, better isolation

### Phase 4: Nice-to-Have Tools
13. **WebFetch tool** — HTTP fetch with HTML→markdown
14. **File change tracking** — per-session diff tracking
