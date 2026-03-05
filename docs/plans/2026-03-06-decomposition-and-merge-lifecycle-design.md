# Ticket Auto-Decomposition & PR Merge Lifecycle

**Date:** 2026-03-06
**Status:** Approved

## Problem

Foreman has two architectural gaps:

1. **Large tickets produce unreviable PRs.** A ticket like "implement user authentication" generates 40 tasks in a single PR. The correct output is 4-6 focused PRs, which requires decomposing the ticket into child tracker issues.

2. **Broken ticket lifecycle.** After PR creation, Foreman never learns whether the PR was merged, closed, or abandoned. Every ticket stays in `PR_CREATED` limbo permanently. Dashboard counts are wrong, cost accounting is incomplete, and post-merge automation is impossible.

## Solution

One integrated deliverable with three components:

1. **Ticket Decomposition** — new pipeline stage that detects oversized tickets, uses LLM to generate child issue specs, creates them in the tracker, and waits for human approval before processing children.
2. **PR Merge Polling** — dedicated daemon goroutine that polls open PRs for merge/close status, updates ticket lifecycle, and fires `post_merge` hooks.
3. **Parent Auto-Completion** — when all child PRs merge, the parent ticket is automatically closed.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Child issues vs internal decomposition | Child issues in tracker (Option A) | Each child = one PR = one reviewable unit. `CreateTicket()` unlocks future features (auto-filed bug tickets, partial completion follow-ups). |
| Merge detection method | Poll-based (Option B) | Fits existing daemon architecture. No new HTTP surface. Latency acceptable (5min default). |
| Deployment skill model | Raw `post_merge` hook trigger (Option C) | Users compose their own flow via skill YAML. No environment abstraction. Same pattern as `post_lint`/`pre_pr`/`post_pr`. |
| PR tracking scope | Track all PRs (Option A) | Lifecycle completion is independently valuable. Polling cost is negligible (bounded by open PR count). |
| Ship strategy | Single integrated deliverable (Option A) | Parent auto-closes when all children merge — requires both features wired together. |
| LLM decomposition output | Structured JSON with schema enforcement (Option 1) | Consistent with existing `PlannedTask[]` pattern. `OutputSchema` already supported in `AgentRequest`. |
| Merge polling implementation | Dedicated goroutine (Option 1) | Decoupled from ticket polling cadence. Same pattern as existing clarification timeout checker. |

## Data Model Changes

### New Ticket Statuses

```go
// internal/models/pipeline.go
TicketStatusDecomposing    // LLM generating child tickets
TicketStatusDecomposed     // Children created, awaiting human approval
TicketStatusAwaitingMerge  // PR open, polling for merge
TicketStatusMerged         // PR merged, post_merge hooks fired
TicketStatusPRClosed       // PR closed without merge
```

Complete lifecycle:

```
QUEUED → CLARIFICATION_NEEDED (if vague)
       → DECOMPOSING → DECOMPOSED (if too large — parent stops here until children all merge)
       → PLANNING → IMPLEMENTING → PR_CREATED → AWAITING_MERGE → MERGED | PR_CLOSED
```

### Ticket Model Additions

```go
// internal/models/ticket.go — new fields on Ticket
ParentTicketID    string   // empty for top-level, set for decomposed children
ChildTicketIDs    []string // populated when parent is decomposed
DecomposeDepth    int      // 0 = top-level, 1 = child (never decompose depth > 0)
```

### Tracker Interface Addition

```go
// internal/tracker/tracker.go
CreateTicket(ctx context.Context, req CreateTicketRequest) (*Ticket, error)

type CreateTicketRequest struct {
    Title              string
    Description        string
    Labels             []string
    ParentID           string            // links child to parent in tracker
    AcceptanceCriteria string
    Metadata           map[string]string // includes "foreman_depth": "1"
}
```

### Git Provider Addition

```go
// internal/git/ — new interface
type PRChecker interface {
    GetPRStatus(ctx context.Context, prNumber int) (PRMergeStatus, error)
}

type PRMergeStatus struct {
    State    string     // "open", "merged", "closed"
    MergedAt *time.Time
    ClosedAt *time.Time
}
```

### Config Additions

```go
// internal/models/config.go
type DecomposeConfig struct {
    Enabled           bool   `mapstructure:"enabled"`
    MaxTicketWords    int    `mapstructure:"max_ticket_words"`    // default 150
    MaxScopeKeywords  int    `mapstructure:"max_scope_keywords"`  // default 2
    ApprovalLabel     string `mapstructure:"approval_label"`      // default "foreman-ready"
    ParentLabel       string `mapstructure:"parent_label"`        // default "foreman-decomposed"
}

// HooksConfig — add PostMerge
PostMerge []string `mapstructure:"post_merge"`

// DaemonConfig — add merge check interval
MergeCheckIntervalSecs int `mapstructure:"merge_check_interval_secs"` // default 300
```

### Database Schema

```sql
ALTER TABLE tickets ADD COLUMN parent_ticket_id TEXT DEFAULT '';
ALTER TABLE tickets ADD COLUMN decompose_depth INTEGER DEFAULT 0;

CREATE TABLE open_prs (
    ticket_id   TEXT PRIMARY KEY,
    pr_number   INTEGER NOT NULL,
    pr_url      TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL,
    FOREIGN KEY (ticket_id) REFERENCES tickets(id)
);
```

### Database Interface Additions

```go
ListTicketsByStatus(ctx context.Context, status models.TicketStatus) ([]models.Ticket, error)
GetTicketByExternalID(ctx context.Context, externalID string) (*models.Ticket, error)
GetChildTickets(ctx context.Context, parentID string) ([]models.Ticket, error)
```

## Component 1: Ticket Decomposition

### Scope Detection (Deterministic)

```go
// internal/pipeline/decompose.go
func NeedsDecomposition(ticket *models.Ticket, cfg *models.DecomposeConfig) bool {
    if !cfg.Enabled || ticket.DecomposeDepth > 0 {
        return false
    }
    wordCount := len(strings.Fields(ticket.Description))
    scopeWords := countScopeKeywords(ticket.Description)
    vagueAndLong := len(ticket.AcceptanceCriteria) == 0 && wordCount > 100
    return wordCount > cfg.MaxTicketWords || scopeWords > cfg.MaxScopeKeywords || vagueAndLong
}
```

Runs after `CheckTicketClarity` returns CLEAR:

```
CheckTicketClarity
  → VAGUE: clarification loop (existing)
  → CLEAR + NeedsDecomposition: decompose stage
  → CLEAR + !NeedsDecomposition: plan stage (existing, unchanged)
```

### Decomposition LLM Call

```go
type DecompositionResult struct {
    Children  []ChildTicketSpec `json:"children"`
    Rationale string            `json:"rationale"`
}

type ChildTicketSpec struct {
    Title              string   `json:"title"`
    Description        string   `json:"description"`
    AcceptanceCriteria []string `json:"acceptance_criteria"`
    EstimatedComplexity string  `json:"estimated_complexity"`
    DependsOn          []string `json:"depends_on"`
}
```

LLM prompt includes parent title, description, existing acceptance criteria. Instruction: decompose into 3-6 child tickets, each representing one reviewable PR, each independently testable. Output schema enforced via `AgentRequest.OutputSchema`.

### Execution Flow

```go
func (d *Decomposer) Execute(ctx context.Context, ticket *models.Ticket) error {
    // 1. LLM generates child specs
    result := d.generateChildSpecs(ctx, ticket)

    // 2. Create each child in tracker
    var childIDs []string
    for _, spec := range result.Children {
        created, _ := d.tracker.CreateTicket(ctx, tracker.CreateTicketRequest{
            Title:              spec.Title,
            Description:        spec.Description,
            AcceptanceCriteria: strings.Join(spec.AcceptanceCriteria, "\n"),
            Labels:             []string{d.cfg.ApprovalLabel + "-pending"},
            ParentID:           ticket.ExternalID,
            Metadata:           map[string]string{"foreman_depth": "1"},
        })
        childIDs = append(childIDs, created.ExternalID)
    }

    // 3. Update parent
    ticket.ChildTicketIDs = childIDs
    ticket.Status = models.TicketStatusDecomposed
    d.tracker.AddLabel(ctx, ticket.ExternalID, d.cfg.ParentLabel)

    // 4. Comment on parent
    d.tracker.AddComment(ctx, ticket.ExternalID, formatDecompositionComment(result, childIDs))

    // 5. Emit event
    d.events.Emit(ctx, ticket.ID, "", "ticket_decomposed", map[string]string{
        "child_count": strconv.Itoa(len(childIDs)),
        "rationale":   result.Rationale,
    })
    return nil
}
```

### Child Approval

Children are created with a pending label. Human changes label to the pickup label to approve. Existing `FetchReadyTickets` picks them up — no new polling logic needed.

### Recursion Guard

`DecomposeDepth > 0` prevents children from being decomposed. Children are created with `foreman_depth: 1` in metadata. Hard boundary, no recursive explosion.

## Component 2: PR Merge Polling

### Merge Checker

```go
// internal/daemon/merge_checker.go
type MergeChecker struct {
    db         db.Database
    prChecker  git.PRChecker
    hookRunner *skills.HookRunner
    tracker    tracker.IssueTracker
    events     *telemetry.EventEmitter
    interval   time.Duration
    log        zerolog.Logger
}

func (m *MergeChecker) Start(ctx context.Context) {
    ticker := time.NewTicker(m.interval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            m.checkAll(ctx)
        }
    }
}
```

### Check Cycle

Fetches all tickets in `AwaitingMerge` status, queries PR status for each:

- **merged** → update status to `Merged`, fire `post_merge` hooks, check parent completion
- **closed** → update status to `PRClosed`, emit event, stop polling
- **open** → no-op

### Parent Completion

When a child ticket's PR merges, check if all siblings are also merged. If yes, mark parent as `Done`, close it in tracker, post summary comment.

### Status Transition

Existing PR creation flow changes: after creating a PR, set status to `AwaitingMerge` instead of `PRCreated`.

### Daemon Wiring

```go
// internal/daemon/daemon.go — in Start()
mergeChecker := &MergeChecker{
    db:         d.db,
    prChecker:  d.gitProvider,
    hookRunner: d.hookRunner,
    tracker:    d.tracker,
    events:     d.events,
    interval:   time.Duration(d.cfg.MergeCheckIntervalSecs) * time.Second,
    log:        d.log.With().Str("component", "merge_checker").Logger(),
}
go mergeChecker.Start(ctx)
```

## Component 3: Post-Merge Hooks

### Skills Engine Change

Add `"post_merge"` to valid triggers in `internal/skills/loader.go`:

```go
var validTriggers = map[string]bool{
    "post_lint":   true,
    "pre_pr":      true,
    "post_pr":     true,
    "post_merge":  true,
}
```

### Example Deployment Skill

```yaml
# skills/deploy-staging.yml
id: deploy-staging
description: Deploy merged PR to Fly.io staging
trigger: post_merge
steps:
  - id: deploy
    type: run_command
    command: flyctl deploy --app {{ ticket.Metadata.app_name }}-staging --image-label {{ ticket.BranchName }}
    timeout_secs: 300
    allow_failure: false
  - id: healthcheck
    type: run_command
    command: curl -sf https://{{ ticket.Metadata.app_name }}-staging.fly.dev/health || exit 1
    timeout_secs: 30
    allow_failure: false
  - id: notify
    type: run_command
    command: |
      curl -X POST "$SLACK_WEBHOOK_URL" \
        -H 'Content-Type: application/json' \
        -d '{"text": "Deployed {{ ticket.ExternalID }} to staging"}'
    allow_failure: true
```

### Config

```toml
[decompose]
enabled = true
max_ticket_words = 150
max_scope_keywords = 2
approval_label = "foreman-ready"
parent_label = "foreman-decomposed"

[pipeline.hooks]
post_merge = ["deploy-staging"]

[daemon]
merge_check_interval_secs = 300
```

### Error Handling

- If a `post_merge` skill step fails with `allow_failure: false`, ticket stays `Merged` (merge is irreversible) but event is emitted with severity `error`
- No automatic retry — deployment failures surface in dashboard for human intervention
- `allow_failure: true` steps (notifications) log warnings but don't affect ticket state

## Testing Strategy

**Unit tests:**
- `NeedsDecomposition()` — table-driven: word count, scope keywords, depth guard
- `MergeChecker.checkAll()` — mock PRChecker returning open/merged/closed, verify status transitions
- `checkParentCompletion()` — mock DB with N children in various states, verify parent closes only when all merged
- `Decomposer.Execute()` — mock LLM + tracker, verify CreateTicket calls and parent labeling

**Integration tests:**
- End-to-end: ticket triggers decomposition → children created in mock tracker → approve → simulate merge → parent closes
- Merge checker with real SQLite: insert AwaitingMerge tickets, mock PR status, verify DB transitions

No new test infrastructure needed — existing mocks for `IssueTracker`, `Database`, and `LlmProvider` cover all seams.

## Key Files to Create/Modify

| Action | File |
|--------|------|
| Create | `internal/pipeline/decompose.go` |
| Create | `internal/pipeline/decompose_test.go` |
| Create | `internal/daemon/merge_checker.go` |
| Create | `internal/daemon/merge_checker_test.go` |
| Modify | `internal/models/pipeline.go` — new statuses |
| Modify | `internal/models/ticket.go` — parent/child fields |
| Modify | `internal/models/config.go` — DecomposeConfig, PostMerge hook, merge interval |
| Modify | `internal/tracker/tracker.go` — CreateTicket interface method |
| Modify | `internal/tracker/github_issues.go` — CreateTicket implementation |
| Modify | `internal/tracker/jira.go` — CreateTicket implementation |
| Modify | `internal/tracker/linear.go` — CreateTicket implementation |
| Modify | `internal/tracker/local_file.go` — CreateTicket implementation |
| Modify | `internal/git/github_pr.go` — PRChecker interface + implementation |
| Modify | `internal/skills/loader.go` — post_merge trigger |
| Modify | `internal/daemon/daemon.go` — wire merge checker goroutine |
| Modify | `internal/db/db.go` — new query methods |
| Modify | `internal/db/sqlite.go` (or equivalent) — schema migration + implementations |
| Modify | `internal/config/config.go` — defaults for new config fields |
