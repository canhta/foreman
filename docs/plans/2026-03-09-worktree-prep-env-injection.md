# Worktree Prep & Env File Injection Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Before each ticket, reset the main workdir to a clean state on the configured default branch with a fresh pull; copy user-managed `.env` files into every worktree and load them into the process environment.

**Architecture:** New `internal/envloader` package handles `.env` parsing, process-env loading, and file copying. Two new `GitProvider` methods (`Checkout`, `Pull`) support the pre-ticket cleanup flow in the orchestrator. The DAG adapter injects env files after worktree creation.

**Tech Stack:** Go stdlib (`os`, `bufio`, `strings`, `path/filepath`), zerolog, existing `git.GitProvider` interface, viper config.

---

### Task 1: Add `EnvFiles` to config model

**Files:**
- Modify: `internal/models/config.go:60-71`
- Modify: `internal/config/config.go:183-197`

**Step 1: Add field to `DaemonConfig`**

In `internal/models/config.go`, add to `DaemonConfig` struct after `LockTTLSeconds`:

```go
// EnvFiles maps worktree-relative destination paths to absolute source paths
// on disk (outside the repo). Each file is copied into every task worktree
// and all vars are loaded into the process environment.
// Example: {".env": "~/.foreman/envs/myproject.env"}
EnvFiles map[string]string `mapstructure:"env_files"`
```

**Step 2: Expand `~` in env file values at config load time**

In `internal/config/config.go`, inside `expandEnvVars`, after `cfg.Daemon.WorkDir = expandTilde(...)`:

```go
for dest, src := range cfg.Daemon.EnvFiles {
    cfg.Daemon.EnvFiles[dest] = expandTilde(src)
}
```

**Step 3: Build and check**

```bash
cd /Users/canh/Projects/Indies/Foreman && go build ./...
```
Expected: no errors.

**Step 4: Commit**

```bash
git add internal/models/config.go internal/config/config.go
git commit -m "feat: add EnvFiles config field to DaemonConfig"
```

---

### Task 2: Implement `internal/envloader` package

**Files:**
- Create: `internal/envloader/envloader.go`
- Create: `internal/envloader/envloader_test.go`

**Step 1: Write failing tests**

Create `internal/envloader/envloader_test.go`:

```go
package envloader_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/canhta/foreman/internal/envloader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_SetsEnvVars(t *testing.T) {
	f := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(f, []byte("FOO=bar\nBAZ=qux\n"), 0o600))

	t.Setenv("FOO", "") // ensure clean slate
	t.Setenv("BAZ", "")

	require.NoError(t, envloader.Load(map[string]string{".env": f}))
	assert.Equal(t, "bar", os.Getenv("FOO"))
	assert.Equal(t, "qux", os.Getenv("BAZ"))
}

func TestLoad_IgnoresComentsAndBlanks(t *testing.T) {
	f := filepath.Join(t.TempDir(), ".env")
	content := "# comment\n\nKEY=value\n"
	require.NoError(t, os.WriteFile(f, []byte(content), 0o600))

	t.Setenv("KEY", "")
	require.NoError(t, envloader.Load(map[string]string{".env": f}))
	assert.Equal(t, "value", os.Getenv("KEY"))
}

func TestLoad_StripsQuotes(t *testing.T) {
	f := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(f, []byte(`QUOTED="hello world"`+"\n"), 0o600))

	t.Setenv("QUOTED", "")
	require.NoError(t, envloader.Load(map[string]string{".env": f}))
	assert.Equal(t, "hello world", os.Getenv("QUOTED"))
}

func TestLoad_MissingFileIsSkipped(t *testing.T) {
	err := envloader.Load(map[string]string{".env": "/does/not/exist/.env"})
	assert.NoError(t, err) // missing file is a warning, not an error
}

func TestCopyInto_CopiesFilesToWorktree(t *testing.T) {
	src := filepath.Join(t.TempDir(), "project.env")
	require.NoError(t, os.WriteFile(src, []byte("KEY=val\n"), 0o600))

	worktreeDir := t.TempDir()
	require.NoError(t, envloader.CopyInto(map[string]string{".env": src}, worktreeDir))

	dest := filepath.Join(worktreeDir, ".env")
	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "KEY=val\n", string(data))
}

func TestCopyInto_CreatesSubdirs(t *testing.T) {
	src := filepath.Join(t.TempDir(), "api.env")
	require.NoError(t, os.WriteFile(src, []byte("DB=postgres\n"), 0o600))

	worktreeDir := t.TempDir()
	require.NoError(t, envloader.CopyInto(map[string]string{"packages/api/.env": src}, worktreeDir))

	dest := filepath.Join(worktreeDir, "packages", "api", ".env")
	_, err := os.Stat(dest)
	assert.NoError(t, err)
}

func TestCopyInto_MissingSourceIsSkipped(t *testing.T) {
	err := envloader.CopyInto(map[string]string{".env": "/no/such/file"}, t.TempDir())
	assert.NoError(t, err)
}
```

**Step 2: Run tests to verify they fail**

```bash
cd /Users/canh/Projects/Indies/Foreman && go test ./internal/envloader/... 2>&1 | head -5
```
Expected: `cannot find package` or `no Go files`.

**Step 3: Implement `envloader.go`**

Create `internal/envloader/envloader.go`:

```go
// Package envloader loads .env files into process environment and copies
// them into git worktrees.
package envloader

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// Load parses each source file in files (map[dest]src) and sets the
// KEY=VALUE pairs as process environment variables via os.Setenv.
// Missing source files are logged as warnings and skipped (not errors).
// Later files in iteration order win on key conflicts.
func Load(files map[string]string) error {
	for _, src := range files {
		if err := loadFile(src); err != nil {
			return err
		}
	}
	return nil
}

// CopyInto copies each source file to <worktreeDir>/<dest>, creating
// intermediate directories as needed. Missing source files are skipped.
func CopyInto(files map[string]string, worktreeDir string) error {
	for dest, src := range files {
		destPath := filepath.Join(worktreeDir, filepath.FromSlash(dest))
		if err := copyFile(src, destPath); err != nil {
			return fmt.Errorf("copy env file %s → %s: %w", src, destPath, err)
		}
	}
	return nil
}

func loadFile(src string) error {
	f, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn().Str("src", src).Msg("env file not found, skipping")
			return nil
		}
		return fmt.Errorf("open env file %s: %w", src, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		// Strip surrounding quotes.
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if err := os.Setenv(key, val); err != nil {
			return fmt.Errorf("setenv %s: %w", key, err)
		}
	}
	return scanner.Err()
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn().Str("src", src).Msg("env source file not found, skipping copy")
			return nil
		}
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
```

**Step 4: Run tests**

```bash
cd /Users/canh/Projects/Indies/Foreman && go test ./internal/envloader/... -v
```
Expected: all PASS.

**Step 5: Commit**

```bash
git add internal/envloader/
git commit -m "feat: add envloader package for .env file loading and worktree injection"
```

---

### Task 3: Add `Checkout` and `Pull` to `GitProvider`

**Files:**
- Modify: `internal/git/git.go`
- Modify: `internal/git/native.go`
- Modify: `internal/git/gogit.go`
- Modify: `internal/git/native_test.go`

**Step 1: Add to interface**

In `internal/git/git.go`, add to `GitProvider` interface after `CleanWorkingTree`:

```go
Checkout(ctx context.Context, workDir, branch string) error
Pull(ctx context.Context, workDir string) error
```

**Step 2: Implement in `NativeGitProvider`**

In `internal/git/native.go`, add after `CleanWorkingTree`:

```go
// Checkout switches the working tree to an existing branch.
func (g *NativeGitProvider) Checkout(ctx context.Context, workDir, branch string) error {
	_, err := g.run(ctx, workDir, "git", "checkout", branch)
	return err
}

// Pull fast-forwards the current branch from its upstream.
func (g *NativeGitProvider) Pull(ctx context.Context, workDir string) error {
	_, err := g.run(ctx, workDir, "git", "pull")
	return err
}
```

**Step 3: Add stubs to `GoGitProvider`**

In `internal/git/gogit.go`, add:

```go
func (g *GoGitProvider) Checkout(_ context.Context, _, _ string) error {
	return fmt.Errorf("go-git: Checkout not supported")
}

func (g *GoGitProvider) Pull(_ context.Context, _ string) error {
	return fmt.Errorf("go-git: Pull not supported")
}
```

**Step 4: Add stubs to any mock `GitProvider` in tests**

Search for mock implementations:
```bash
grep -rn "func.*GitProvider\|CleanWorkingTree" /Users/canh/Projects/Indies/Foreman --include="*_test.go" -l
```

For each file found, add no-op stubs:
```go
func (m *mockGit) Checkout(_ context.Context, _, _ string) error { return nil }
func (m *mockGit) Pull(_ context.Context, _ string) error        { return nil }
```

**Step 5: Build and test**

```bash
cd /Users/canh/Projects/Indies/Foreman && go build ./... && go test ./internal/git/... -v
```
Expected: all pass.

**Step 6: Commit**

```bash
git add internal/git/
git commit -m "feat: add Checkout and Pull to GitProvider interface"
```

---

### Task 4: Orchestrator — clean + pull before ticket branch creation

**Files:**
- Modify: `internal/daemon/orchestrator.go:337-412`

**Step 1: Add pre-branch reset in normal ticket path**

In `orchestrator.ProcessTicket`, replace the `// Create feature branch.` block (currently at ~line 407):

```go
// Reset workdir to a clean, up-to-date state on the default branch
// before creating the ticket branch.
if err := o.git.CleanWorkingTree(ctx, o.config.WorkDir); err != nil {
    log.Warn().Err(err).Msg("clean working tree failed, continuing")
}
if err := o.git.Checkout(ctx, o.config.WorkDir, o.config.DefaultBranch); err != nil {
    returnErr = fmt.Errorf("checkout default branch %s: %w", o.config.DefaultBranch, err)
    return returnErr
}
if err := o.git.Pull(ctx, o.config.WorkDir); err != nil {
    log.Warn().Err(err).Msg("git pull failed, continuing with current HEAD")
}

// Create feature branch from fresh HEAD.
if err := o.git.CreateBranch(ctx, o.config.WorkDir, branchName); err != nil {
    returnErr = fmt.Errorf("create branch %s: %w", branchName, err)
    return returnErr
}
```

Note: `Pull` failure is a warning (remote may be unreachable) — proceed anyway.

**Step 2: Build**

```bash
cd /Users/canh/Projects/Indies/Foreman && go build ./...
```
Expected: no errors.

**Step 3: Commit**

```bash
git add internal/daemon/orchestrator.go
git commit -m "feat: reset workdir to default branch before creating ticket branch"
```

---

### Task 5: Load env files at daemon startup

**Files:**
- Modify: `cmd/start.go`

**Step 1: Add envloader call after git provider init**

In `cmd/start.go`, after the `// 4. Initialize git provider and ensure the work repo is ready.` block, add:

```go
// 4b. Load user env files into process environment.
if len(cfg.Daemon.EnvFiles) > 0 {
    if err := envloader.Load(cfg.Daemon.EnvFiles); err != nil {
        log.Warn().Err(err).Msg("failed to load env files at startup")
    } else {
        log.Info().Int("count", len(cfg.Daemon.EnvFiles)).Msg("env files loaded into process environment")
    }
}
```

**Step 2: Add import**

Add to imports in `cmd/start.go`:
```go
"github.com/canhta/foreman/internal/envloader"
```

**Step 3: Build**

```bash
cd /Users/canh/Projects/Indies/Foreman && go build ./...
```
Expected: no errors.

**Step 4: Commit**

```bash
git add cmd/start.go
git commit -m "feat: load env files into process environment at daemon startup"
```

---

### Task 6: Inject env files into worktrees

**Files:**
- Modify: `internal/daemon/orchestrator.go` (TaskRunnerFactoryInput)
- Modify: `internal/pipeline/dag_adapter.go` (DAGTaskAdapter)
- Modify: `cmd/start.go` (factory Create method)

**Step 1: Add `EnvFiles` to `TaskRunnerFactoryInput`**

In `internal/daemon/orchestrator.go`, add to `TaskRunnerFactoryInput`:

```go
// EnvFiles maps worktree-relative dest paths to source paths on disk.
// Loaded into process env and copied into each task worktree.
EnvFiles map[string]string
```

**Step 2: Pass `EnvFiles` when creating the factory input**

In `orchestrator.ProcessTicket`, find the `dagRunner := o.runnerFactory.Create(TaskRunnerFactoryInput{...})` call and add:

```go
EnvFiles: o.config.EnvFiles,
```

**Step 3: Add `EnvFiles` to `OrchestratorConfig`**

In `orchestrator.go`, add to `OrchestratorConfig`:

```go
EnvFiles map[string]string
```

**Step 4: Wire from `start.go`**

In `cmd/start.go`, in the `daemon.OrchestratorConfig{...}` block, add:

```go
EnvFiles: cfg.Daemon.EnvFiles,
```

**Step 5: Add `envFiles` to `DAGTaskAdapter` and inject in `Run`**

In `internal/pipeline/dag_adapter.go`:

Add field to struct:
```go
envFiles map[string]string
```

Pass it in `NewDAGTaskAdapterWithConsistency` (add parameter `envFiles map[string]string`):
```go
envFiles: envFiles,
```

In `Run`, after the successful `AddWorktree` block (after `runnerToUse = a.runner.CloneWithWorkDir(worktreeDir)`), add:

```go
// Reload env vars from disk and copy files into the worktree.
if len(a.envFiles) > 0 {
    if err := envloader.Load(a.envFiles); err != nil {
        log.Warn().Err(err).Str("task_id", taskID).Msg("env reload failed for worktree")
    }
    if err := envloader.CopyInto(a.envFiles, worktreeDir); err != nil {
        log.Warn().Err(err).Str("task_id", taskID).Msg("env file copy into worktree failed")
    }
}
```

Add import: `"github.com/canhta/foreman/internal/envloader"`

**Step 6: Wire `envFiles` through the factory in `start.go`**

In `cmd/start.go`, `taskRunnerFactory.Create`:

```go
return pipeline.NewDAGTaskAdapterWithConsistency(tr, f.db, input.TicketID, f.llm, f.db, f.gitProv, cfg, input.BranchName, input.EnvFiles)
```

Update `taskRunnerFactory` struct to store `envFiles` or pass through `input`. Since `input` already carries it, passing directly is cleanest.

**Step 7: Build and test**

```bash
cd /Users/canh/Projects/Indies/Foreman && go build ./... && go test ./internal/pipeline/... ./internal/daemon/... -v 2>&1 | tail -20
```
Expected: all pass.

**Step 8: Commit**

```bash
git add internal/daemon/orchestrator.go internal/pipeline/dag_adapter.go cmd/start.go
git commit -m "feat: inject env files into task worktrees and reload at worktree creation"
```

---

### Task 7: Example config and smoke test

**Files:**
- Modify: `foreman.example.yaml` (or equivalent example config)

**Step 1: Add example `env_files` section**

Find the example config:
```bash
ls /Users/canh/Projects/Indies/Foreman/*.yaml /Users/canh/Projects/Indies/Foreman/*.example* 2>/dev/null
```

Add under `daemon:`:
```yaml
daemon:
  # Map of worktree-relative destination → absolute source path on disk.
  # Source files live outside the repo (e.g. ~/.foreman/envs/) so they
  # are never accidentally committed.
  env_files:
    ".env": "~/.foreman/envs/myproject.env"
    # "packages/api/.env": "~/.foreman/envs/myproject-api.env"
```

**Step 2: Final build + full test run**

```bash
cd /Users/canh/Projects/Indies/Foreman && go build ./... && go test ./... 2>&1 | tail -20
```
Expected: all pass.

**Step 3: Commit**

```bash
git add .
git commit -m "docs: add env_files example to foreman.example.yaml"
```
