# Design: Worktree Prep & Env File Injection

Date: 2026-03-09

## Problem

1. Worktrees are created from whatever state the main workdir happens to be in — stale commits, uncommitted changes, or a wrong branch can silently corrupt task work.
2. `.env` files are gitignored and never copied into worktrees, so agent tools (test runners, build scripts) can't find them.
3. Process environment is not populated from user-managed env files, so commands invoked by Foreman lack project credentials and config.

## Goals

- Before each ticket, reset the main workdir to a clean, up-to-date state on the configured default branch.
- Support per-project env file mapping stored outside the repo (`~/.foreman/envs/`).
- Load env vars into the process environment at startup (and reload at worktree creation).
- Copy env files into each worktree at the correct relative paths.

## Design

### 1. Worktree Prep Flow

Triggered once per ticket in `orchestrator.ProcessTicket`, before `CreateBranch`:

```
1. git checkout <config.DefaultBranch>   ← configured, never hardcoded
2. git checkout -- . && git clean -fd    ← discard all uncommitted changes
3. git pull                              ← fetch + fast-forward from origin
4. git checkout -b <ticketBranch>        ← ticket branch from fresh HEAD
5. per task: git worktree add -b <taskBranch> <worktreeDir> <ticketBranch>
```

Two new `GitProvider` methods:
- `Checkout(ctx context.Context, workDir, branch string) error`
- `Pull(ctx context.Context, workDir string) error`

### 2. Env Files Configuration

New field in `foreman.yaml` under `daemon`:

```yaml
daemon:
  env_files:
    ".env": "~/.foreman/envs/myproject.env"
    "packages/api/.env": "~/.foreman/envs/myproject-api.env"
    "services/auth/.env": "~/.foreman/envs/myproject-auth.env"
```

- **Key**: destination path relative to worktree root
- **Value**: source path on disk (`~` expanded at config load time)

### 3. Env Loading Behavior

**At daemon startup** (once):
- Parse all source files, call `os.Setenv` for each `KEY=VALUE`.
- Process-wide availability immediately.

**At worktree creation** (per task):
- Reload from disk (picks up edits since startup).
- Copy each source file to `<worktreeDir>/<destPath>`, creating parent dirs.

### 4. New Package: `internal/envloader`

```go
// Load parses each source file and sets env vars via os.Setenv.
// Files are processed in iteration order; later files win on conflicts.
func Load(files map[string]string) error

// CopyInto copies each source file to <worktreeDir>/<dest>, creating dirs.
func CopyInto(files map[string]string, worktreeDir string) error
```

Supports standard `.env` format: `KEY=VALUE`, `# comments`, quoted values, blank lines ignored.

## Files Changed

| File | Change |
|------|--------|
| `internal/models/config.go` | Add `EnvFiles map[string]string` to `DaemonConfig` |
| `internal/config/config.go` | Default empty map, expand `~` in values at load |
| `internal/envloader/envloader.go` | New package: `Load` + `CopyInto` |
| `internal/git/git.go` | Add `Checkout`, `Pull` to `GitProvider` interface |
| `internal/git/native.go` | Implement `Checkout`, `Pull` |
| `internal/git/gogit.go` | Stub `Checkout`, `Pull` |
| `cmd/start.go` | Call `envloader.Load(cfg.Daemon.EnvFiles)` at startup |
| `internal/daemon/orchestrator.go` | Before `CreateBranch`: clean → checkout default → pull |
| `internal/daemon/orchestrator.go` | Add `EnvFiles` to `TaskRunnerFactoryInput` |
| `internal/pipeline/dag_adapter.go` | After `AddWorktree`: reload env + `CopyInto` worktree |

## Non-Goals

- No glob patterns for env file discovery (explicit paths only).
- No env var interpolation within `.env` files.
- No per-ticket or per-task env file overrides (global config only).
