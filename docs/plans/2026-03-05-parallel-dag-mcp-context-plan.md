# Parallel DAG + MCP Client + Context Generate — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship three compounding features — parallel DAG task execution, stdio MCP client, and LLM-powered context generation — that transform Foreman from "great pipeline" to "fast, connectable, easy to adopt."

**Architecture:** DAG executor replaces sequential task iteration with a coordinator/worker pool pattern. MCP client implements JSON-RPC 2.0 over stdin/stdout with concurrent request multiplexing. Context generator scans repos and uses LLM to produce agent-optimized AGENTS.md files.

**Tech Stack:** Go 1.24, errgroup, sync/atomic, encoding/json (JSON-RPC 2.0), os/exec (subprocess management), cobra (CLI), zerolog (logging), prometheus (metrics)

---

## Feature 1: Parallel DAG Executor

### Task 1: Add config fields for parallel task execution

**Files:**
- Modify: `internal/models/config.go:20-27`
- Modify: `internal/config/config.go:41-48` (setDefaults)
- Modify: `internal/config/config.go:147-155` (Validate)
- Modify: `internal/daemon/daemon.go:16-21` (DaemonConfig)
- Test: `internal/config/config_test.go`

**Step 1: Write the failing test**

```go
// internal/config/config_test.go — add to existing file or create
func TestDefaults_DaemonParallelTasks(t *testing.T) {
	cfg, err := LoadDefaults()
	require.NoError(t, err)
	assert.Equal(t, 3, cfg.Daemon.MaxParallelTasks)
	assert.Equal(t, 15, cfg.Daemon.TaskTimeoutMinutes)
}

func TestValidate_MaxParallelTasksZero(t *testing.T) {
	cfg, _ := LoadDefaults()
	cfg.Daemon.MaxParallelTasks = 0
	errs := Validate(cfg)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "max_parallel_tasks must be at least 1")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestDefaults_DaemonParallelTasks -v`
Expected: FAIL — `MaxParallelTasks` field does not exist

**Step 3: Write minimal implementation**

Add to `internal/models/config.go` DaemonConfig struct (after line 26):
```go
MaxParallelTasks   int `mapstructure:"max_parallel_tasks"`
TaskTimeoutMinutes int `mapstructure:"task_timeout_minutes"`
```

Add to `internal/config/config.go` setDefaults (after line 44):
```go
v.SetDefault("daemon.max_parallel_tasks", 3)
v.SetDefault("daemon.task_timeout_minutes", 15)
```

Add to `internal/config/config.go` Validate (after line 151):
```go
if cfg.Daemon.MaxParallelTasks < 1 {
	errs = append(errs, fmt.Errorf("max_parallel_tasks must be at least 1 (got %d)", cfg.Daemon.MaxParallelTasks))
}
if cfg.Daemon.TaskTimeoutMinutes < 1 {
	errs = append(errs, fmt.Errorf("task_timeout_minutes must be at least 1 (got %d)", cfg.Daemon.TaskTimeoutMinutes))
}
```

Add to `internal/daemon/daemon.go` DaemonConfig struct (after line 20):
```go
MaxParallelTasks   int
TaskTimeoutMinutes int
```

Update DefaultDaemonConfig():
```go
func DefaultDaemonConfig() DaemonConfig {
	return DaemonConfig{
		PollIntervalSecs:     60,
		IdlePollIntervalSecs: 300,
		MaxParallelTickets:   3,
		MaxParallelTasks:     3,
		TaskTimeoutMinutes:   15,
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestDefaults_Daemon -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/config.go internal/config/config.go internal/config/config_test.go internal/daemon/daemon.go
git commit -m "feat(config): add max_parallel_tasks and task_timeout_minutes daemon settings"
```

---

### Task 2: Create TaskRunner interface and DAG executor core

**Files:**
- Create: `internal/daemon/dag_executor.go`
- Create: `internal/daemon/dag_executor_test.go`

**Step 1: Write the failing test — single task DAG**

```go
// internal/daemon/dag_executor_test.go
package daemon

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTaskRunner struct {
	mu          sync.Mutex
	execOrder   []string
	maxConcur   int
	activeCnt   int
	failTasks   map[string]bool
	runDuration time.Duration
}

func newMockRunner() *mockTaskRunner {
	return &mockTaskRunner{failTasks: make(map[string]bool)}
}

func (m *mockTaskRunner) Run(ctx context.Context, taskID string) TaskResult {
	m.mu.Lock()
	m.activeCnt++
	if m.activeCnt > m.maxConcur {
		m.maxConcur = m.activeCnt
	}
	m.mu.Unlock()

	if m.runDuration > 0 {
		time.Sleep(m.runDuration)
	}

	m.mu.Lock()
	m.execOrder = append(m.execOrder, taskID)
	shouldFail := m.failTasks[taskID]
	m.activeCnt--
	m.mu.Unlock()

	if shouldFail {
		return TaskResult{TaskID: taskID, Status: TaskStatusFailed, Error: fmt.Errorf("task %s failed", taskID)}
	}
	return TaskResult{TaskID: taskID, Status: TaskStatusDone}
}

func TestDAGExecutor_SingleTask(t *testing.T) {
	runner := newMockRunner()
	tasks := []DAGTask{{ID: "A"}}

	exec := NewDAGExecutor(runner, 3, 15*time.Minute)
	results := exec.Execute(context.Background(), tasks)

	require.Len(t, results, 1)
	assert.Equal(t, TaskStatusDone, results["A"].Status)
	assert.Equal(t, []string{"A"}, runner.execOrder)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestDAGExecutor_SingleTask -v`
Expected: FAIL — `DAGExecutor` type does not exist

**Step 3: Write minimal implementation**

```go
// internal/daemon/dag_executor.go
package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/canhta/foreman/internal/models"
)

// TaskStatusDone and TaskStatusFailed mirror models constants for DAG results.
var (
	TaskStatusDone    = models.TaskStatusDone
	TaskStatusFailed  = models.TaskStatusFailed
	TaskStatusSkipped = models.TaskStatusSkipped
)

// TaskRunner executes a single task. Injected for testability.
type TaskRunner interface {
	Run(ctx context.Context, taskID string) TaskResult
}

// TaskResult holds the outcome of a single task execution.
type TaskResult struct {
	TaskID string
	Status models.TaskStatus
	Error  error
}

// DAGTask describes a task node in the dependency graph.
type DAGTask struct {
	ID        string
	DependsOn []string
}

// DAGExecutor runs tasks in parallel respecting dependency edges.
// A single coordinator goroutine owns all mutable state (zero mutexes on DAG state).
type DAGExecutor struct {
	runner     TaskRunner
	maxWorkers int
	timeout    time.Duration
}

// NewDAGExecutor creates a DAG executor with bounded parallelism.
func NewDAGExecutor(runner TaskRunner, maxWorkers int, taskTimeout time.Duration) *DAGExecutor {
	return &DAGExecutor{
		runner:     runner,
		maxWorkers: maxWorkers,
		timeout:    taskTimeout,
	}
}

// Execute runs all tasks respecting the DAG. Returns a map of taskID -> result.
// On failure, the full transitive closure of dependents is marked skipped (BFS).
func (e *DAGExecutor) Execute(ctx context.Context, tasks []DAGTask) map[string]TaskResult {
	if len(tasks) == 0 {
		return map[string]TaskResult{}
	}

	// Build adjacency list and in-degree map
	adjacency := make(map[string][]string)   // task -> dependents
	inDegree := make(map[string]int)
	taskSet := make(map[string]bool)

	for _, t := range tasks {
		taskSet[t.ID] = true
		if _, ok := inDegree[t.ID]; !ok {
			inDegree[t.ID] = 0
		}
		for _, dep := range t.DependsOn {
			adjacency[dep] = append(adjacency[dep], t.ID)
			inDegree[t.ID]++
		}
	}

	results := make(map[string]TaskResult)
	readyChan := make(chan string, len(tasks))
	resultChan := make(chan TaskResult, len(tasks))
	remaining := len(tasks)

	// Seed ready queue with root tasks (in-degree 0)
	for _, t := range tasks {
		if inDegree[t.ID] == 0 {
			readyChan <- t.ID
		}
	}

	// Launch worker pool
	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()

	for i := 0; i < e.maxWorkers; i++ {
		go func() {
			for {
				select {
				case <-workerCtx.Done():
					return
				case taskID, ok := <-readyChan:
					if !ok {
						return
					}
					taskCtx, cancel := context.WithTimeout(workerCtx, e.timeout)
					result := e.runner.Run(taskCtx, taskID)
					cancel()
					// Check if timeout caused failure
					if taskCtx.Err() == context.DeadlineExceeded && result.Status != TaskStatusDone {
						result = TaskResult{
							TaskID: taskID,
							Status: TaskStatusFailed,
							Error:  fmt.Errorf("task %s timed out after %v", taskID, e.timeout),
						}
					}
					resultChan <- result
				}
			}
		}()
	}

	// Coordinator loop — single goroutine owns all DAG state
	for remaining > 0 {
		select {
		case <-ctx.Done():
			// Mark all remaining as skipped
			for _, t := range tasks {
				if _, done := results[t.ID]; !done {
					results[t.ID] = TaskResult{TaskID: t.ID, Status: TaskStatusSkipped, Error: ctx.Err()}
				}
			}
			close(readyChan)
			return results
		case result := <-resultChan:
			results[result.TaskID] = result
			remaining--

			if result.Status == TaskStatusDone {
				// Check dependents — push newly ready tasks
				for _, depID := range adjacency[result.TaskID] {
					inDegree[depID]--
					if inDegree[depID] == 0 {
						readyChan <- depID
					}
				}
			} else {
				// BFS: mark full transitive closure as skipped
				queue := []string{result.TaskID}
				for len(queue) > 0 {
					curr := queue[0]
					queue = queue[1:]
					for _, depID := range adjacency[curr] {
						if _, done := results[depID]; !done {
							results[depID] = TaskResult{
								TaskID: depID,
								Status: TaskStatusSkipped,
								Error:  fmt.Errorf("skipped: dependency %q failed", result.TaskID),
							}
							remaining--
							queue = append(queue, depID)
						}
					}
				}
			}
		}
	}

	close(readyChan)
	return results
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/ -run TestDAGExecutor_SingleTask -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/dag_executor.go internal/daemon/dag_executor_test.go
git commit -m "feat(daemon): add DAGExecutor with coordinator/worker pool pattern"
```

---

### Task 3: Test DAG executor — parallel independent tasks

**Files:**
- Modify: `internal/daemon/dag_executor_test.go`

**Step 1: Write the failing test**

```go
func TestDAGExecutor_ParallelIndependent(t *testing.T) {
	runner := newMockRunner()
	runner.runDuration = 50 * time.Millisecond
	tasks := []DAGTask{
		{ID: "A"},
		{ID: "B"},
		{ID: "C"},
	}

	exec := NewDAGExecutor(runner, 3, 15*time.Minute)
	results := exec.Execute(context.Background(), tasks)

	require.Len(t, results, 3)
	for _, id := range []string{"A", "B", "C"} {
		assert.Equal(t, TaskStatusDone, results[id].Status)
	}
	// All 3 should have run concurrently
	assert.Equal(t, 3, runner.maxConcur)
}
```

**Step 2: Run test to verify it passes (implementation already supports this)**

Run: `go test ./internal/daemon/ -run TestDAGExecutor_ParallelIndependent -v`
Expected: PASS (already implemented in Task 2)

**Step 3: Commit**

```bash
git add internal/daemon/dag_executor_test.go
git commit -m "test(daemon): verify parallel execution of independent DAG tasks"
```

---

### Task 4: Test DAG executor — dependency ordering

**Files:**
- Modify: `internal/daemon/dag_executor_test.go`

**Step 1: Write the test**

```go
func TestDAGExecutor_DependencyChain(t *testing.T) {
	runner := newMockRunner()
	tasks := []DAGTask{
		{ID: "A"},
		{ID: "B", DependsOn: []string{"A"}},
		{ID: "C", DependsOn: []string{"B"}},
	}

	exec := NewDAGExecutor(runner, 3, 15*time.Minute)
	results := exec.Execute(context.Background(), tasks)

	require.Len(t, results, 3)
	// A must execute before B, B before C
	aIdx, bIdx, cIdx := -1, -1, -1
	for i, id := range runner.execOrder {
		switch id {
		case "A":
			aIdx = i
		case "B":
			bIdx = i
		case "C":
			cIdx = i
		}
	}
	assert.Less(t, aIdx, bIdx)
	assert.Less(t, bIdx, cIdx)
}
```

**Step 2: Run test**

Run: `go test ./internal/daemon/ -run TestDAGExecutor_DependencyChain -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/daemon/dag_executor_test.go
git commit -m "test(daemon): verify DAG dependency ordering A->B->C"
```

---

### Task 5: Test DAG executor — BFS failure propagation

**Files:**
- Modify: `internal/daemon/dag_executor_test.go`

**Step 1: Write the test — the validation trace from the design doc**

```go
func TestDAGExecutor_BFSFailurePropagation(t *testing.T) {
	// DAG: A->B->D, A->C->D, E(independent). A fails.
	// Expected: B skipped, C skipped, D skipped, E completes.
	runner := newMockRunner()
	runner.failTasks["A"] = true
	runner.runDuration = 10 * time.Millisecond

	tasks := []DAGTask{
		{ID: "A"},
		{ID: "B", DependsOn: []string{"A"}},
		{ID: "C", DependsOn: []string{"A"}},
		{ID: "D", DependsOn: []string{"B", "C"}},
		{ID: "E"},
	}

	exec := NewDAGExecutor(runner, 3, 15*time.Minute)
	results := exec.Execute(context.Background(), tasks)

	require.Len(t, results, 5)
	assert.Equal(t, TaskStatusFailed, results["A"].Status)
	assert.Equal(t, TaskStatusSkipped, results["B"].Status)
	assert.Equal(t, TaskStatusSkipped, results["C"].Status)
	assert.Equal(t, TaskStatusSkipped, results["D"].Status)
	assert.Equal(t, TaskStatusDone, results["E"].Status)
}
```

**Step 2: Run test**

Run: `go test ./internal/daemon/ -run TestDAGExecutor_BFSFailurePropagation -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/daemon/dag_executor_test.go
git commit -m "test(daemon): verify BFS failure propagation across transitive deps"
```

---

### Task 6: Test DAG executor — bounded concurrency

**Files:**
- Modify: `internal/daemon/dag_executor_test.go`

**Step 1: Write the test**

```go
func TestDAGExecutor_BoundedConcurrency(t *testing.T) {
	runner := newMockRunner()
	runner.runDuration = 50 * time.Millisecond
	// 6 independent tasks but only 2 workers
	tasks := []DAGTask{
		{ID: "A"}, {ID: "B"}, {ID: "C"},
		{ID: "D"}, {ID: "E"}, {ID: "F"},
	}

	exec := NewDAGExecutor(runner, 2, 15*time.Minute)
	results := exec.Execute(context.Background(), tasks)

	require.Len(t, results, 6)
	for _, id := range []string{"A", "B", "C", "D", "E", "F"} {
		assert.Equal(t, TaskStatusDone, results[id].Status)
	}
	// Max concurrent should never exceed worker count
	assert.LessOrEqual(t, runner.maxConcur, 2)
}
```

**Step 2: Run test**

Run: `go test ./internal/daemon/ -run TestDAGExecutor_BoundedConcurrency -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/daemon/dag_executor_test.go
git commit -m "test(daemon): verify worker pool bounds max concurrency"
```

---

### Task 7: Test DAG executor — context cancellation

**Files:**
- Modify: `internal/daemon/dag_executor_test.go`

**Step 1: Write the test**

```go
func TestDAGExecutor_ContextCancellation(t *testing.T) {
	runner := newMockRunner()
	runner.runDuration = 500 * time.Millisecond
	tasks := []DAGTask{{ID: "A"}, {ID: "B"}, {ID: "C"}}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	exec := NewDAGExecutor(runner, 3, 15*time.Minute)
	results := exec.Execute(ctx, tasks)

	// All tasks should be present in results (either done or skipped)
	require.Len(t, results, 3)
}
```

**Step 2: Run test**

Run: `go test ./internal/daemon/ -run TestDAGExecutor_ContextCancellation -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/daemon/dag_executor_test.go
git commit -m "test(daemon): verify context cancellation propagates to all workers"
```

---

### Task 8: Add DAG metrics to telemetry

**Files:**
- Modify: `internal/telemetry/metrics.go`
- Test: `internal/telemetry/metrics_test.go`

**Step 1: Write the failing test**

```go
func TestMetrics_RecordDAGExecution(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	m.RecordDAGExecution(3, 1, 1, 2500) // completed, failed, skipped, durationMs
	// Verify metric exists by gathering
	families, err := reg.Gather()
	require.NoError(t, err)
	found := false
	for _, f := range families {
		if f.GetName() == "foreman_dag_execution_duration_seconds" {
			found = true
		}
	}
	assert.True(t, found)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/telemetry/ -run TestMetrics_RecordDAGExecution -v`
Expected: FAIL — method does not exist

**Step 3: Write minimal implementation**

Add to `internal/telemetry/metrics.go` Metrics struct:
```go
DAGTasksCompleted prometheus.Counter
DAGTasksFailed    prometheus.Counter
DAGTasksSkipped   prometheus.Counter
DAGDuration       prometheus.Histogram
```

Add to NewMetrics():
```go
DAGTasksCompleted: prometheus.NewCounter(prometheus.CounterOpts{
	Name: "foreman_dag_tasks_completed_total",
	Help: "Total DAG tasks completed successfully",
}),
DAGTasksFailed: prometheus.NewCounter(prometheus.CounterOpts{
	Name: "foreman_dag_tasks_failed_total",
	Help: "Total DAG tasks failed",
}),
DAGTasksSkipped: prometheus.NewCounter(prometheus.CounterOpts{
	Name: "foreman_dag_tasks_skipped_total",
	Help: "Total DAG tasks skipped due to dependency failure",
}),
DAGDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
	Name:    "foreman_dag_execution_duration_seconds",
	Help:    "DAG execution duration in seconds",
	Buckets: []float64{10, 30, 60, 120, 300, 600, 1200, 3600},
}),
```

Register them in the MustRegister call.

Add helper method:
```go
func (m *Metrics) RecordDAGExecution(completed, failed, skipped int, durationMs int64) {
	m.DAGTasksCompleted.Add(float64(completed))
	m.DAGTasksFailed.Add(float64(failed))
	m.DAGTasksSkipped.Add(float64(skipped))
	m.DAGDuration.Observe(float64(durationMs) / float64(time.Second/time.Millisecond))
}
```

**Step 4: Run test**

Run: `go test ./internal/telemetry/ -run TestMetrics_RecordDAGExecution -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/telemetry/metrics.go internal/telemetry/metrics_test.go
git commit -m "feat(telemetry): add DAG execution metrics"
```

---

### Task 9: Update PR body formatting for completed/skipped checklist

**Files:**
- Modify: `internal/git/pr.go:56-67`
- Modify: `internal/git/pr_test.go` (or create if needed)

**Step 1: Write the failing test**

```go
func TestFormatPRBody_SkippedTasks(t *testing.T) {
	input := PRBodyInput{
		TicketExternalID: "PROJ-1",
		TicketTitle:      "Add auth",
		IsPartial:        true,
		TaskSummaries: []PRTaskSummary{
			{Title: "Add middleware", Status: "done"},
			{Title: "Add rate limiting", Status: "skipped"},
		},
	}
	body := FormatPRBody(input)
	assert.Contains(t, body, "- [x] Add middleware")
	assert.Contains(t, body, "- [ ] ~~Add rate limiting~~ (skipped)")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestFormatPRBody_SkippedTasks -v`
Expected: FAIL — current formatting uses emoji icons, not checkboxes

**Step 3: Update FormatPRBody**

Replace the task summary loop in `internal/git/pr.go` lines 56-67:
```go
sb.WriteString("### Tasks\n\n")
for _, t := range input.TaskSummaries {
	switch t.Status {
	case "done":
		sb.WriteString(fmt.Sprintf("- [x] %s\n", t.Title))
	case "failed":
		sb.WriteString(fmt.Sprintf("- [ ] ~~%s~~ (failed)\n", t.Title))
	case "skipped":
		sb.WriteString(fmt.Sprintf("- [ ] ~~%s~~ (skipped)\n", t.Title))
	default:
		sb.WriteString(fmt.Sprintf("- [ ] %s — %s\n", t.Title, t.Status))
	}
}
```

**Step 4: Run test**

Run: `go test ./internal/git/ -run TestFormatPRBody -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/pr.go internal/git/pr_test.go
git commit -m "feat(git): update PR body with checkbox-style completed/skipped task list"
```

---

## Feature 2: MCP Client (stdio transport)

### Task 10: Extend MCPServerConfig with restart policy fields

**Files:**
- Modify: `internal/agent/mcp/client.go:21-28`
- Test: `internal/agent/mcp/client_test.go`

**Step 1: Write the failing test**

```go
func TestMCPServerConfig_RestartPolicyDefaults(t *testing.T) {
	cfg := MCPServerConfig{Name: "test", Command: "/usr/bin/tool"}
	assert.Equal(t, "on-failure", cfg.EffectiveRestartPolicy())
	assert.Equal(t, 3, cfg.EffectiveMaxRestarts())
	assert.Equal(t, 2, cfg.EffectiveRestartDelaySecs())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/mcp/ -run TestMCPServerConfig_RestartPolicyDefaults -v`
Expected: FAIL — methods don't exist

**Step 3: Write minimal implementation**

Add fields to MCPServerConfig in `internal/agent/mcp/client.go`:
```go
type MCPServerConfig struct {
	Name              string            `json:"name"`
	URL               string            `json:"url,omitempty"`
	AuthToken         string            `json:"auth_token,omitempty"`
	AllowedTools      []string          `json:"allowed_tools,omitempty"`
	Command           string            `json:"command,omitempty"`
	Args              []string          `json:"args,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
	RestartPolicy     string            `json:"restart_policy,omitempty"`     // always | never | on-failure
	MaxRestarts       *int              `json:"max_restarts,omitempty"`       // default 3
	RestartDelaySecs  *int              `json:"restart_delay_secs,omitempty"` // default 2
}

func (c MCPServerConfig) EffectiveRestartPolicy() string {
	if c.RestartPolicy != "" {
		return c.RestartPolicy
	}
	return "on-failure"
}

func (c MCPServerConfig) EffectiveMaxRestarts() int {
	if c.MaxRestarts != nil {
		return *c.MaxRestarts
	}
	return 3
}

func (c MCPServerConfig) EffectiveRestartDelaySecs() int {
	if c.RestartDelaySecs != nil {
		return *c.RestartDelaySecs
	}
	return 2
}
```

**Step 4: Run test**

Run: `go test ./internal/agent/mcp/ -run TestMCPServerConfig_RestartPolicyDefaults -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/mcp/client.go internal/agent/mcp/client_test.go
git commit -m "feat(mcp): add restart policy, env, and timeout fields to MCPServerConfig"
```

---

### Task 11: Implement tool name normalization

**Files:**
- Create: `internal/agent/mcp/naming.go`
- Create: `internal/agent/mcp/naming_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/mcp/naming_test.go
package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMCPToolName_Simple(t *testing.T) {
	assert.Equal(t, "mcp_myserver_mytool", MCPToolName("myserver", "mytool"))
}

func TestMCPToolName_SpecialChars(t *testing.T) {
	assert.Equal(t, "mcp_my_server_my_tool", MCPToolName("my-server", "my.tool"))
}

func TestMCPToolName_Spaces(t *testing.T) {
	assert.Equal(t, "mcp_my_server_my_tool", MCPToolName("my server", "my tool"))
}

func TestMCPToolName_TruncatesLongNames(t *testing.T) {
	longServer := "abcdefghijklmnopqrstuvwxyz" // 26 chars
	longTool := "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz" // 52 chars
	name := MCPToolName(longServer, longTool)
	assert.LessOrEqual(t, len(name), 64)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/mcp/ -run TestMCPToolName -v`
Expected: FAIL — function doesn't exist

**Step 3: Write minimal implementation**

```go
// internal/agent/mcp/naming.go
package mcp

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

var nameReplacer = strings.NewReplacer("-", "_", ".", "_", " ", "_")

// MCPToolName generates a normalized tool name for the LLM tool registry.
// Capped at 64 characters (OpenAI limit) with hash disambiguation if truncated.
func MCPToolName(server, tool string) string {
	s := nameReplacer.Replace(server)
	t := nameReplacer.Replace(tool)
	name := "mcp_" + s + "_" + t

	if len(name) <= 64 {
		return name
	}

	// Truncate with hash suffix
	if len(s) > 20 {
		s = s[:20]
	}
	if len(t) > 30 {
		t = t[:30]
	}
	base := "mcp_" + s + "_" + t
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(server+"_"+tool)))[:6]
	return base + "_" + hash
}
```

**Step 4: Run test**

Run: `go test ./internal/agent/mcp/ -run TestMCPToolName -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/mcp/naming.go internal/agent/mcp/naming_test.go
git commit -m "feat(mcp): add tool name normalization with 64-char cap"
```

---

### Task 12: Implement StdioClient — JSON-RPC 2.0 transport

**Files:**
- Create: `internal/agent/mcp/stdio_client.go`
- Create: `internal/agent/mcp/stdio_client_test.go`

**Step 1: Write the failing test — initialize handshake**

```go
// internal/agent/mcp/stdio_client_test.go
package mcp

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransport simulates stdin/stdout for testing without subprocesses
type mockTransport struct {
	responses chan json.RawMessage
	requests  chan json.RawMessage
	closed    bool
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		responses: make(chan json.RawMessage, 10),
		requests:  make(chan json.RawMessage, 10),
	}
}

func (m *mockTransport) Send(msg json.RawMessage) error {
	m.requests <- msg
	return nil
}

func (m *mockTransport) Receive() (json.RawMessage, error) {
	msg, ok := <-m.responses
	if !ok {
		return nil, io.EOF
	}
	return msg, nil
}

func (m *mockTransport) Close() error {
	m.closed = true
	close(m.responses)
	return nil
}

func TestStdioClient_Initialize(t *testing.T) {
	transport := newMockTransport()

	// Queue the initialize response
	initResp, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":   map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":     map[string]interface{}{"name": "test-server", "version": "1.0"},
		},
	})
	transport.responses <- json.RawMessage(initResp)

	client := NewStdioClientWithTransport(transport, "test-server")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Initialize(ctx)
	require.NoError(t, err)
	assert.True(t, client.HasCapability("tools"))
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/mcp/ -run TestStdioClient_Initialize -v`
Expected: FAIL — StdioClient doesn't exist

**Step 3: Write implementation**

```go
// internal/agent/mcp/stdio_client.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog/log"
)

// Transport abstracts the stdin/stdout communication layer.
type Transport interface {
	Send(msg json.RawMessage) error
	Receive() (json.RawMessage, error)
	Close() error
}

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type pendingRequest struct {
	resp chan jsonRPCResponse
}

// StdioClient implements Client over a JSON-RPC 2.0 stdio transport.
type StdioClient struct {
	transport    Transport
	serverName   string
	capabilities map[string]interface{}
	tools        []models.ToolDef
	nextID       atomic.Int64
	pending      sync.Map // map[int64]*pendingRequest
	writeMu      sync.Mutex
	closed       atomic.Bool
}

// NewStdioClientWithTransport creates a StdioClient from a pre-built transport (for testing).
func NewStdioClientWithTransport(t Transport, serverName string) *StdioClient {
	c := &StdioClient{
		transport:  t,
		serverName: serverName,
	}
	go c.readLoop()
	return c
}

func (c *StdioClient) readLoop() {
	for {
		msg, err := c.transport.Receive()
		if err != nil {
			if !c.closed.Load() {
				log.Warn().Str("server", c.serverName).Err(err).Msg("[mcp] read loop error")
			}
			return
		}
		var resp jsonRPCResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			log.Warn().Str("server", c.serverName).Err(err).Msg("[mcp] invalid JSON-RPC response")
			continue
		}
		if val, ok := c.pending.LoadAndDelete(resp.ID); ok {
			pr := val.(*pendingRequest)
			pr.resp <- resp
		}
	}
}

func (c *StdioClient) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("[mcp:%s] marshal request: %w", c.serverName, err)
	}

	pr := &pendingRequest{resp: make(chan jsonRPCResponse, 1)}
	c.pending.Store(id, pr)
	defer c.pending.Delete(id)

	c.writeMu.Lock()
	err = c.transport.Send(data)
	c.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("[mcp:%s] send: %w", c.serverName, err)
	}

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("[mcp:%s] %s timed out: %w", c.serverName, method, ctx.Err())
	case resp := <-pr.resp:
		if resp.Error != nil {
			return nil, fmt.Errorf("[mcp:%s] %s error %d: %s", c.serverName, method, resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// Initialize performs the MCP initialize handshake.
func (c *StdioClient) Initialize(ctx context.Context) error {
	result, err := c.call(ctx, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "foreman", "version": "1.0"},
	})
	if err != nil {
		return err
	}

	var initResult struct {
		Capabilities map[string]interface{} `json:"capabilities"`
		ServerInfo   struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(result, &initResult); err != nil {
		return fmt.Errorf("[mcp:%s] unmarshal init result: %w", c.serverName, err)
	}
	c.capabilities = initResult.Capabilities
	return nil
}

// HasCapability checks if the server declared a capability.
func (c *StdioClient) HasCapability(name string) bool {
	_, ok := c.capabilities[name]
	return ok
}

// ListTools calls tools/list and returns discovered tool definitions.
func (c *StdioClient) ListTools(ctx context.Context) ([]models.ToolDef, error) {
	if !c.HasCapability("tools") {
		return nil, nil
	}
	result, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	var listResult struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &listResult); err != nil {
		return nil, fmt.Errorf("[mcp:%s] unmarshal tools/list: %w", c.serverName, err)
	}

	var defs []models.ToolDef
	for _, t := range listResult.Tools {
		defs = append(defs, models.ToolDef{
			Name:        MCPToolName(c.serverName, t.Name),
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	c.tools = defs
	return defs, nil
}

// Call invokes a tool on the MCP server.
func (c *StdioClient) Call(ctx context.Context, name string, input json.RawMessage) (string, error) {
	result, err := c.call(ctx, "tools/call", map[string]interface{}{
		"name":      name,
		"arguments": input,
	})
	if err != nil {
		return "", err
	}

	var callResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &callResult); err != nil {
		return "", fmt.Errorf("[mcp:%s] unmarshal tools/call: %w", c.serverName, err)
	}

	var texts []string
	for _, c := range callResult.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}
	return fmt.Sprintf("%s", joinStrings(texts)), nil
}

func joinStrings(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	result := ss[0]
	for _, s := range ss[1:] {
		result += "\n" + s
	}
	return result
}

// Close sends shutdown notification and closes the transport.
func (c *StdioClient) Close() error {
	c.closed.Store(true)
	// Send shutdown notification (no response expected)
	data, _ := json.Marshal(jsonRPCRequest{JSONRPC: "2.0", Method: "notifications/cancelled"})
	c.writeMu.Lock()
	_ = c.transport.Send(data)
	c.writeMu.Unlock()
	return c.transport.Close()
}
```

**Step 4: Run test**

Run: `go test ./internal/agent/mcp/ -run TestStdioClient -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/mcp/stdio_client.go internal/agent/mcp/stdio_client_test.go
git commit -m "feat(mcp): implement StdioClient with JSON-RPC 2.0 and concurrent request multiplexing"
```

---

### Task 13: Test StdioClient — concurrent tool calls

**Files:**
- Modify: `internal/agent/mcp/stdio_client_test.go`

**Step 1: Write the test**

```go
func TestStdioClient_ConcurrentCalls(t *testing.T) {
	transport := newMockTransport()
	// Queue init response
	initResp, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1,
		"result": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":   map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":     map[string]interface{}{"name": "test", "version": "1.0"},
		},
	})
	transport.responses <- json.RawMessage(initResp)

	client := NewStdioClientWithTransport(transport, "test")
	ctx := context.Background()
	_ = client.Initialize(ctx)

	// Send 3 concurrent calls — responses arrive out of order
	var wg sync.WaitGroup
	results := make([]string, 3)
	errs := make([]error, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = client.Call(ctx, "tool", json.RawMessage(`{}`))
		}(i)
	}

	// Wait for requests, then respond out of order (4, 3, 2)
	time.Sleep(50 * time.Millisecond)
	for _, id := range []int64{4, 3, 2} {
		resp, _ := json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0", "id": id,
			"result": map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": fmt.Sprintf("result-%d", id)},
				},
			},
		})
		transport.responses <- json.RawMessage(resp)
	}

	wg.Wait()
	for i := 0; i < 3; i++ {
		assert.NoError(t, errs[i])
		assert.NotEmpty(t, results[i])
	}
}
```

**Step 2: Run test**

Run: `go test ./internal/agent/mcp/ -run TestStdioClient_ConcurrentCalls -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/agent/mcp/stdio_client_test.go
git commit -m "test(mcp): verify concurrent tool calls with out-of-order responses"
```

---

### Task 14: Implement ProcessTransport — subprocess management

**Files:**
- Create: `internal/agent/mcp/process_transport.go`
- Create: `internal/agent/mcp/process_transport_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/mcp/process_transport_test.go
package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProcessTransport_InvalidCommand(t *testing.T) {
	cfg := MCPServerConfig{
		Name:    "bad",
		Command: "/nonexistent/binary",
	}
	_, err := NewProcessTransport(cfg)
	assert.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/mcp/ -run TestProcessTransport_InvalidCommand -v`
Expected: FAIL — NewProcessTransport doesn't exist

**Step 3: Write implementation**

```go
// internal/agent/mcp/process_transport.go
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/rs/zerolog/log"
)

// ProcessTransport manages an MCP server subprocess over stdin/stdout.
type ProcessTransport struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	scanner    *bufio.Scanner
	serverName string
	scanMu     sync.Mutex
}

// NewProcessTransport spawns an MCP server subprocess.
func NewProcessTransport(cfg MCPServerConfig) (*ProcessTransport, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)

	// Only pass explicitly configured env vars
	env := make([]string, 0, len(cfg.Env))
	for k, v := range cfg.Env {
		env = append(env, k+"="+v)
	}
	if len(env) > 0 {
		cmd.Env = env
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("[mcp:%s] stdin pipe: %w", cfg.Name, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("[mcp:%s] stdout pipe: %w", cfg.Name, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("[mcp:%s] stderr pipe: %w", cfg.Name, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("[mcp:%s] start: %w", cfg.Name, err)
	}

	// Capture stderr with server name prefix
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Warn().Str("server", cfg.Name).Msgf("[mcp:%s] %s", cfg.Name, scanner.Text())
		}
	}()

	return &ProcessTransport{
		cmd:        cmd,
		stdin:      stdin,
		scanner:    bufio.NewScanner(stdout),
		serverName: cfg.Name,
	}, nil
}

// Send writes a JSON-RPC message to the subprocess stdin (one line per message).
func (p *ProcessTransport) Send(msg json.RawMessage) error {
	line := append(msg, '\n')
	_, err := p.stdin.Write(line)
	return err
}

// Receive reads a JSON-RPC response line from stdout.
func (p *ProcessTransport) Receive() (json.RawMessage, error) {
	p.scanMu.Lock()
	defer p.scanMu.Unlock()
	if !p.scanner.Scan() {
		if err := p.scanner.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	return json.RawMessage(p.scanner.Bytes()), nil
}

// Close kills the subprocess.
func (p *ProcessTransport) Close() error {
	_ = p.stdin.Close()
	return p.cmd.Process.Kill()
}
```

**Step 4: Run test**

Run: `go test ./internal/agent/mcp/ -run TestProcessTransport -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/mcp/process_transport.go internal/agent/mcp/process_transport_test.go
git commit -m "feat(mcp): add ProcessTransport for subprocess management over stdin/stdout"
```

---

### Task 15: Implement MCP server manager with restart policy

**Files:**
- Create: `internal/agent/mcp/manager.go`
- Create: `internal/agent/mcp/manager_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/mcp/manager_test.go
package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_RegisterAndListTools(t *testing.T) {
	mgr := NewManager()
	// Register a mock client
	mockTransport := newMockTransport()
	initResp, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1,
		"result": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":   map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":     map[string]interface{}{"name": "test", "version": "1.0"},
		},
	})
	toolsResp, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 2,
		"result": map[string]interface{}{
			"tools": []map[string]interface{}{
				{"name": "query", "description": "Run SQL", "inputSchema": map[string]interface{}{"type": "object"}},
			},
		},
	})
	mockTransport.responses <- json.RawMessage(initResp)
	mockTransport.responses <- json.RawMessage(toolsResp)

	client := NewStdioClientWithTransport(mockTransport, "db")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Initialize(ctx))

	mgr.RegisterClient("db", client)
	tools := mgr.AllTools(ctx)
	assert.Len(t, tools, 1)
	assert.Equal(t, "mcp_db_query", tools[0].Name)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/mcp/ -run TestManager -v`
Expected: FAIL — Manager doesn't exist

**Step 3: Write implementation**

```go
// internal/agent/mcp/manager.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog/log"
)

// Manager coordinates multiple MCP server connections.
type Manager struct {
	mu      sync.RWMutex
	clients map[string]*StdioClient // server name -> client
	tools   map[string]string       // normalized tool name -> server name
}

// NewManager creates an MCP manager.
func NewManager() *Manager {
	return &Manager{
		clients: make(map[string]*StdioClient),
		tools:   make(map[string]string),
	}
}

// RegisterClient adds a connected MCP client.
func (m *Manager) RegisterClient(name string, client *StdioClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[name] = client
}

// AllTools returns all discovered tool definitions from all servers.
func (m *Manager) AllTools(ctx context.Context) []models.ToolDef {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var all []models.ToolDef
	for name, client := range m.clients {
		tools, err := client.ListTools(ctx)
		if err != nil {
			log.Warn().Str("server", name).Err(err).Msg("[mcp] failed to list tools")
			continue
		}
		for _, t := range tools {
			m.tools[t.Name] = name
		}
		all = append(all, tools...)
	}
	return all
}

// CallTool routes a tool call to the correct MCP server.
func (m *Manager) CallTool(ctx context.Context, toolName string, input json.RawMessage) (string, error) {
	m.mu.RLock()
	serverName, ok := m.tools[toolName]
	if !ok {
		m.mu.RUnlock()
		return "", fmt.Errorf("[mcp] unknown tool %q", toolName)
	}
	client, clientOk := m.clients[serverName]
	m.mu.RUnlock()
	if !clientOk {
		return "", fmt.Errorf("[mcp] server %q not connected", serverName)
	}

	// Strip the mcp_server_ prefix to get original tool name for the server
	originalName := toolName
	prefix := "mcp_" + nameReplacer.Replace(serverName) + "_"
	if strings.HasPrefix(toolName, prefix) {
		originalName = toolName[len(prefix):]
	}

	return client.Call(ctx, originalName, input)
}

// IsMCPTool checks if a tool name belongs to an MCP server.
func (m *Manager) IsMCPTool(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.tools[name]
	return ok
}

// Close shuts down all MCP server connections.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, client := range m.clients {
		if err := client.Close(); err != nil {
			log.Warn().Str("server", name).Err(err).Msg("[mcp] close error")
		}
	}
}
```

**Step 4: Run test**

Run: `go test ./internal/agent/mcp/ -run TestManager -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/mcp/manager.go internal/agent/mcp/manager_test.go
git commit -m "feat(mcp): add Manager for multi-server MCP tool routing"
```

---

### Task 16: Add MCP config to models and config defaults

**Files:**
- Modify: `internal/models/config.go` — add MCPConfig struct and field to Config
- Modify: `internal/config/config.go` — no defaults needed (MCP is opt-in)

**Step 1: Write the failing test**

```go
func TestConfig_MCPServersEmpty(t *testing.T) {
	cfg, err := LoadDefaults()
	require.NoError(t, err)
	assert.Empty(t, cfg.MCP.Servers)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestConfig_MCPServersEmpty -v`
Expected: FAIL — MCP field doesn't exist

**Step 3: Add to `internal/models/config.go`**

After line 17 (RateLimit field), add:
```go
MCP MCPConfig `mapstructure:"mcp"`
```

Add the struct:
```go
type MCPConfig struct {
	Servers []MCPServerEntry `mapstructure:"servers"`
}

type MCPServerEntry struct {
	Name             string            `mapstructure:"name"`
	Command          string            `mapstructure:"command"`
	Args             []string          `mapstructure:"args"`
	Env              map[string]string `mapstructure:"env"`
	AllowedTools     []string          `mapstructure:"allowed_tools"`
	RestartPolicy    string            `mapstructure:"restart_policy"`
	MaxRestarts      int               `mapstructure:"max_restarts"`
	RestartDelaySecs int               `mapstructure:"restart_delay_secs"`
}
```

**Step 4: Run test**

Run: `go test ./internal/config/ -run TestConfig_MCPServersEmpty -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/config.go internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add MCP server configuration section"
```

---

## Feature 3: Context Generate

### Task 17: Add tiered file scanner

**Files:**
- Create: `internal/context/file_scanner.go`
- Create: `internal/context/file_scanner_test.go`

**Step 1: Write the failing test**

```go
// internal/context/file_scanner_test.go
package context

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanFiles_Tier1AlwaysIncluded(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0o644)
	os.WriteFile(filepath.Join(dir, "random.txt"), []byte("hello"), 0o644)

	files := ScanFiles(dir, 32000)

	// go.mod and README.md should be in results
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	assert.Contains(t, paths, "go.mod")
	assert.Contains(t, paths, "README.md")
}

func TestScanFiles_RespectsTokenBudget(t *testing.T) {
	dir := t.TempDir()
	// Create a large file that would exceed a tiny budget
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0o644)
	bigContent := make([]byte, 200000) // ~50K tokens
	os.WriteFile(filepath.Join(dir, "big.go"), bigContent, 0o644)

	files := ScanFiles(dir, 100) // very small budget

	// Should include go.mod (small) but not the big file
	assert.LessOrEqual(t, len(files), 2)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/context/ -run TestScanFiles -v`
Expected: FAIL — ScanFiles doesn't exist

**Step 3: Write implementation**

```go
// internal/context/file_scanner.go
package context

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ScannedFile represents a file selected for context generation.
type ScannedFile struct {
	Path    string
	Content string
	Tier    int
	Tokens  int
}

// tier1Files are always included if they exist.
var tier1Files = []string{
	"go.mod", "package.json", "Cargo.toml", "pyproject.toml",
	"README.md", "CONTRIBUTING.md", "AGENTS.md", ".golangci.yml",
	"Gemfile", "requirements.txt", "setup.py",
}

// tier2Patterns are CI configs, Dockerfiles, and entry points.
var tier2Patterns = []string{
	".github/workflows/*.yml", ".github/workflows/*.yaml",
	"Dockerfile", "docker-compose.yml", "docker-compose.yaml",
	"Jenkinsfile", ".gitlab-ci.yml", ".circleci/config.yml",
	"Makefile", "justfile",
	"main.go", "cmd/main.go", "src/main.ts", "src/index.ts",
	"src/main.rs", "src/lib.rs", "app.py", "manage.py",
}

// ScanFiles collects files for context generation using tiered priority.
// Drops lower-tier files when the token budget is exceeded.
func ScanFiles(workDir string, maxTokens int) []ScannedFile {
	budget := NewTokenBudget(maxTokens)
	var result []ScannedFile

	// Tier 1: Always include
	for _, name := range tier1Files {
		f := tryReadFile(workDir, name, 1)
		if f != nil && budget.CanFit(f.Tokens) {
			budget.Add(f.Tokens)
			result = append(result, *f)
		}
	}

	// Tier 2: CI, Dockerfiles, entry points (up to 10)
	tier2Count := 0
	for _, pattern := range tier2Patterns {
		if tier2Count >= 10 {
			break
		}
		matches, _ := filepath.Glob(filepath.Join(workDir, pattern))
		for _, match := range matches {
			if tier2Count >= 10 {
				break
			}
			rel, _ := filepath.Rel(workDir, match)
			if alreadyIncluded(result, rel) {
				continue
			}
			f := tryReadFile(workDir, rel, 2)
			if f != nil && budget.CanFit(f.Tokens) {
				budget.Add(f.Tokens)
				result = append(result, *f)
				tier2Count++
			}
		}
	}

	// Tier 3: One file per top-level directory (up to 20)
	tier3Count := 0
	entries, _ := os.ReadDir(workDir)
	for _, entry := range entries {
		if tier3Count >= 20 {
			break
		}
		if !entry.IsDir() || skipDirs[entry.Name()] || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		// Pick the first non-test source file in the directory
		f := pickRepresentativeFile(workDir, entry.Name(), 3)
		if f != nil && !alreadyIncluded(result, f.Path) && budget.CanFit(f.Tokens) {
			budget.Add(f.Tokens)
			result = append(result, *f)
			tier3Count++
		}
	}

	return result
}

func tryReadFile(workDir, relPath string, tier int) *ScannedFile {
	absPath := filepath.Join(workDir, relPath)
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}
	tokens := EstimateTokens(string(content))
	return &ScannedFile{
		Path:    relPath,
		Content: string(content),
		Tier:    tier,
		Tokens:  tokens,
	}
}

func pickRepresentativeFile(workDir, dirName string, tier int) *ScannedFile {
	dirPath := filepath.Join(workDir, dirName)
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil
	}

	// Sort by name for determinism
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip test files and non-source files
		if isTestFile(name) || !isSourceFile(name) {
			continue
		}
		rel := filepath.Join(dirName, name)
		return tryReadFile(workDir, rel, tier)
	}
	return nil
}

func alreadyIncluded(files []ScannedFile, path string) bool {
	for _, f := range files {
		if f.Path == path {
			return true
		}
	}
	return false
}

func isTestFile(name string) bool {
	return strings.HasSuffix(name, "_test.go") ||
		strings.HasSuffix(name, ".test.ts") ||
		strings.HasSuffix(name, ".test.js") ||
		strings.HasSuffix(name, ".spec.ts") ||
		strings.HasSuffix(name, ".spec.js") ||
		strings.HasSuffix(name, "_test.rs") ||
		strings.HasPrefix(name, "test_")
}

func isSourceFile(name string) bool {
	sourceExts := []string{".go", ".ts", ".js", ".py", ".rs", ".rb", ".java", ".kt", ".swift", ".c", ".cpp", ".h"}
	for _, ext := range sourceExts {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}
```

**Step 4: Run test**

Run: `go test ./internal/context/ -run TestScanFiles -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/context/file_scanner.go internal/context/file_scanner_test.go
git commit -m "feat(context): add tiered file scanner with token budget enforcement"
```

---

### Task 18: Implement context generator with LLM call

**Files:**
- Create: `internal/context/generator.go`
- Create: `internal/context/generator_test.go`

**Step 1: Write the failing test**

```go
// internal/context/generator_test.go
package context

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockLLMProvider struct {
	lastReq    models.LlmRequest
	response   string
	err        error
}

func (m *mockLLMProvider) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	m.lastReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &models.LlmResponse{Content: m.response}, nil
}

func (m *mockLLMProvider) ProviderName() string         { return "mock" }
func (m *mockLLMProvider) HealthCheck(_ context.Context) error { return nil }

func TestGenerator_GenerateOnline(t *testing.T) {
	llm := &mockLLMProvider{response: "# Project\nA Go project."}
	gen := NewGenerator(llm, "test-model")

	dir := t.TempDir()
	writeTestFile(t, dir, "go.mod", "module example.com/test\n\ngo 1.24")
	writeTestFile(t, dir, "main.go", "package main\n\nfunc main() {}")

	result, err := gen.Generate(context.Background(), dir, GenerateOptions{MaxTokens: 32000})
	require.NoError(t, err)
	assert.Equal(t, "# Project\nA Go project.", result)

	// Verify system prompt mentions Foreman
	assert.Contains(t, llm.lastReq.SystemPrompt, "Foreman")
	assert.Contains(t, llm.lastReq.SystemPrompt, "autonomous")
}

func TestGenerator_GenerateOffline(t *testing.T) {
	gen := NewGenerator(nil, "") // no LLM

	dir := t.TempDir()
	writeTestFile(t, dir, "go.mod", "module example.com/test\n\ngo 1.24")

	result, err := gen.Generate(context.Background(), dir, GenerateOptions{Offline: true})
	require.NoError(t, err)
	assert.Contains(t, result, "go")
	assert.Contains(t, result, "go test ./...")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/context/ -run TestGenerator -v`
Expected: FAIL — Generator doesn't exist

**Step 3: Write implementation**

```go
// internal/context/generator.go
package context

import (
	"context"
	"fmt"
	"strings"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

const contextGenerateSystemPrompt = `You are generating an AGENTS.md for Foreman, a fully autonomous coding daemon.
This file is read by an LLM agent, not a human developer. Optimize for:
- Precise naming conventions (the agent will follow them literally)
- Exact test commands (the agent will run them verbatim)
- Explicit anti-patterns to avoid (the agent has no implicit human intuition)
- File organization rules (the agent must know where to create new files)
Omit marketing language, narrative prose, and generic best practices.

Output pure Markdown. Include these sections:
1. Project Overview (language, framework, purpose)
2. Architecture (key packages/modules, entry points)
3. Coding Conventions (naming, error handling, patterns)
4. Build & Test Commands (exact, copy-pasteable)
5. Key Dependencies
6. File Organization Rules`

// GenerateOptions configures context generation behavior.
type GenerateOptions struct {
	MaxTokens int
	Offline   bool
}

// Generator produces AGENTS.md content from codebase analysis.
type Generator struct {
	provider llm.LlmProvider
	model    string
}

// NewGenerator creates a context generator.
func NewGenerator(provider llm.LlmProvider, model string) *Generator {
	return &Generator{provider: provider, model: model}
}

// Generate produces AGENTS.md content for the given workDir.
func (g *Generator) Generate(ctx context.Context, workDir string, opts GenerateOptions) (string, error) {
	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = 32000
	}

	// Always scan repo
	repoInfo, err := AnalyzeRepo(workDir)
	if err != nil {
		return "", fmt.Errorf("analyze repo: %w", err)
	}

	if opts.Offline || g.provider == nil {
		return generateOffline(repoInfo), nil
	}

	// Scan files with tiered selection
	files := ScanFiles(workDir, maxTokens)

	// Build user prompt
	var sb strings.Builder
	sb.WriteString("## Repository Analysis\n\n")
	sb.WriteString(fmt.Sprintf("- Language: %s\n", repoInfo.Language))
	if repoInfo.Framework != "" {
		sb.WriteString(fmt.Sprintf("- Framework: %s\n", repoInfo.Framework))
	}
	sb.WriteString(fmt.Sprintf("- Test command: `%s`\n", repoInfo.TestCmd))
	if repoInfo.LintCmd != "" {
		sb.WriteString(fmt.Sprintf("- Lint command: `%s`\n", repoInfo.LintCmd))
	}
	if repoInfo.BuildCmd != "" {
		sb.WriteString(fmt.Sprintf("- Build command: `%s`\n", repoInfo.BuildCmd))
	}
	sb.WriteString("\n## Directory Structure\n\n```\n")
	sb.WriteString(repoInfo.FileTree)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Key Files\n\n")
	for _, f := range files {
		sb.WriteString(fmt.Sprintf("### %s\n```\n%s\n```\n\n", f.Path, f.Content))
	}

	resp, err := g.provider.Complete(ctx, models.LlmRequest{
		SystemPrompt: contextGenerateSystemPrompt,
		UserPrompt:   sb.String(),
		Model:        g.model,
		MaxTokens:    4096,
		Temperature:  0.2,
	})
	if err != nil {
		return "", fmt.Errorf("LLM call: %w", err)
	}

	return resp.Content, nil
}

func generateOffline(info *RepoInfo) string {
	var sb strings.Builder
	sb.WriteString("# Project Context\n\n")
	sb.WriteString(fmt.Sprintf("- **Language:** %s\n", info.Language))
	if info.Framework != "" {
		sb.WriteString(fmt.Sprintf("- **Framework:** %s\n", info.Framework))
	}
	sb.WriteString("\n## Commands\n\n")
	if info.TestCmd != "" {
		sb.WriteString(fmt.Sprintf("- Test: `%s`\n", info.TestCmd))
	}
	if info.LintCmd != "" {
		sb.WriteString(fmt.Sprintf("- Lint: `%s`\n", info.LintCmd))
	}
	if info.BuildCmd != "" {
		sb.WriteString(fmt.Sprintf("- Build: `%s`\n", info.BuildCmd))
	}
	sb.WriteString("\n## Directory Structure\n\n```\n")
	sb.WriteString(info.FileTree)
	sb.WriteString("\n```\n")
	return sb.String()
}
```

**Step 4: Run test**

Run: `go test ./internal/context/ -run TestGenerator -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/context/generator.go internal/context/generator_test.go
git commit -m "feat(context): add Generator with LLM-first and offline fallback"
```

---

### Task 19: Add `foreman context generate` CLI command

**Files:**
- Create: `cmd/context.go`
- Modify: `cmd/init.go:62-66` (wire --analyze)

**Step 1: Write the failing test**

```go
// cmd/context_test.go
package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextCmd_Exists(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"context", "--help"})
	err := rootCmd.Execute()
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "generate")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestContextCmd_Exists -v`
Expected: FAIL — "context" subcommand doesn't exist

**Step 3: Write implementation**

```go
// cmd/context.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	fcontext "github.com/canhta/foreman/internal/context"
	"github.com/canhta/foreman/internal/config"
	"github.com/canhta/foreman/internal/llm"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	ctxOffline  bool
	ctxDryRun   bool
	ctxForce    bool
	ctxOutput   string
)

func init() {
	contextCmd := &cobra.Command{
		Use:   "context",
		Short: "Manage AGENTS.md project context",
	}
	contextCmd.AddCommand(newContextGenerateCmd())
	rootCmd.AddCommand(contextCmd)
}

func newContextGenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate AGENTS.md from repository analysis",
		Long:  "Scans the codebase and uses your configured LLM to generate an agent-optimized AGENTS.md file.",
		RunE:  runContextGenerate,
	}

	cmd.Flags().BoolVar(&ctxOffline, "offline", false, "Static analysis only, no LLM call")
	cmd.Flags().BoolVar(&ctxDryRun, "dry-run", false, "Print to stdout without writing file")
	cmd.Flags().BoolVar(&ctxForce, "force", false, "Overwrite existing AGENTS.md without confirmation")
	cmd.Flags().StringVar(&ctxOutput, "output", "./AGENTS.md", "Output file path")

	return cmd
}

func runContextGenerate(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	outputPath := ctxOutput
	if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(workDir, outputPath)
	}

	// Check if file exists and handle non-interactive safety
	if !ctxForce && !ctxDryRun {
		if _, err := os.Stat(outputPath); err == nil {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				fmt.Fprintf(cmd.ErrOrStderr(), "AGENTS.md already exists. Use --force to overwrite or --dry-run to preview.\n")
				os.Exit(1)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "AGENTS.md already exists. Overwrite? [y/N] ")
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
		}
	}

	var provider llm.LlmProvider
	var model string

	if !ctxOffline {
		cfg, err := config.LoadFromFile("foreman.toml")
		if err != nil {
			// Try defaults
			cfg, err = config.LoadDefaults()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
		}
		provider, err = llm.NewProviderFromConfig(cfg.LLM.DefaultProvider, cfg.LLM)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not create LLM provider, falling back to offline mode: %v\n", err)
			ctxOffline = true
		} else {
			model = cfg.Models.Planner // reuse planner model
		}
	}

	gen := fcontext.NewGenerator(provider, model)
	opts := fcontext.GenerateOptions{
		MaxTokens: 32000,
		Offline:   ctxOffline,
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Scanning repository...")
	content, err := gen.Generate(cmd.Context(), workDir, opts)
	if err != nil {
		return fmt.Errorf("generate context: %w", err)
	}

	if ctxDryRun {
		fmt.Fprintln(cmd.OutOrStdout(), content)
		return nil
	}

	if err := os.WriteFile(outputPath, []byte(content+"\n"), 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Generated %s\n", outputPath)
	return nil
}
```

**Step 4: Run test**

Run: `go test ./cmd/ -run TestContextCmd_Exists -v`
Expected: PASS

**Step 5: Wire --analyze in cmd/init.go**

Replace lines 62-66 in `cmd/init.go`:
```go
if initAnalyze {
	fmt.Println("Analyzing repository...")
	gen := fcontext.NewGenerator(nil, "")
	content, err := gen.Generate(cmd.Context(), ".", fcontext.GenerateOptions{Offline: true})
	if err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	if err := os.WriteFile("AGENTS.md", []byte(content+"\n"), 0o644); err != nil {
		return fmt.Errorf("write AGENTS.md: %w", err)
	}
	fmt.Println("Generated AGENTS.md")
}
```

Add import for `fcontext "github.com/canhta/foreman/internal/context"`.

**Step 6: Commit**

```bash
git add cmd/context.go cmd/context_test.go cmd/init.go
git commit -m "feat(cli): add 'foreman context generate' command and wire init --analyze"
```

---

### Task 20: Add observation log infrastructure

**Files:**
- Create: `internal/context/observations.go`
- Create: `internal/context/observations_test.go`

**Step 1: Write the failing test**

```go
// internal/context/observations_test.go
package context

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObservationLog_AppendAndRead(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, ".foreman", "observations.jsonl")

	log := NewObservationLog(dir)
	err := log.Append(Observation{
		Type:    "naming_correction",
		Details: map[string]string{"original": "getUserData", "corrected": "fetchUser"},
		File:    "api/users.go",
		Time:    time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(logPath)
	require.NoError(t, err)

	// Read back
	obs, cursor, err := log.ReadFrom(0)
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "naming_correction", obs[0].Type)
	assert.Greater(t, cursor, int64(0))
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/context/ -run TestObservationLog -v`
Expected: FAIL — ObservationLog doesn't exist

**Step 3: Write implementation**

```go
// internal/context/observations.go
package context

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Observation represents a single learned pattern from pipeline execution.
type Observation struct {
	Type    string            `json:"type"` // naming_correction, test_pattern, convention_discovered, review_feedback
	Details map[string]string `json:"details,omitempty"`
	File    string            `json:"file,omitempty"`
	Time    time.Time         `json:"ts"`
}

// ObservationLog manages the .foreman/observations.jsonl file.
type ObservationLog struct {
	workDir string
}

// NewObservationLog creates an observation log for the given work directory.
func NewObservationLog(workDir string) *ObservationLog {
	return &ObservationLog{workDir: workDir}
}

func (o *ObservationLog) path() string {
	return filepath.Join(o.workDir, ".foreman", "observations.jsonl")
}

// Append adds an observation to the log.
func (o *ObservationLog) Append(obs Observation) error {
	dir := filepath.Dir(o.path())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create .foreman dir: %w", err)
	}

	f, err := os.OpenFile(o.path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open observations log: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(obs)
	if err != nil {
		return fmt.Errorf("marshal observation: %w", err)
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

// ReadFrom reads observations starting from the given byte offset.
// Returns the observations and the new cursor position.
func (o *ObservationLog) ReadFrom(cursor int64) ([]Observation, int64, error) {
	f, err := os.Open(o.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, cursor, err
	}
	defer f.Close()

	if cursor > 0 {
		if _, err := f.Seek(cursor, 0); err != nil {
			return nil, cursor, err
		}
	}

	var observations []Observation
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var obs Observation
		if err := json.Unmarshal(scanner.Bytes(), &obs); err != nil {
			continue // skip malformed lines
		}
		observations = append(observations, obs)
	}

	// Get final cursor position
	pos, _ := f.Seek(0, 1) // current position after reading
	// Since scanner consumed the file, get file size instead
	fi, _ := f.Stat()
	newCursor := fi.Size()

	return observations, newCursor, scanner.Err()
}
```

**Step 4: Run test**

Run: `go test ./internal/context/ -run TestObservationLog -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/context/observations.go internal/context/observations_test.go
git commit -m "feat(context): add observation log for post-merge pattern learning"
```

---

### Task 21: Add `foreman context update` command

**Files:**
- Modify: `cmd/context.go` — add update subcommand

**Step 1: Write the failing test**

```go
// cmd/context_test.go — add
func TestContextUpdateCmd_Exists(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"context", "update", "--help"})
	err := rootCmd.Execute()
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "Update AGENTS.md")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestContextUpdateCmd_Exists -v`
Expected: FAIL — "update" subcommand doesn't exist

**Step 3: Add update subcommand to `cmd/context.go`**

In the `init()` function, add after `contextCmd.AddCommand(newContextGenerateCmd())`:
```go
contextCmd.AddCommand(newContextUpdateCmd())
```

Add the function:
```go
func newContextUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update AGENTS.md from learned patterns",
		Long:  "Reads .foreman/observations.jsonl and updates AGENTS.md with newly discovered conventions.",
		RunE:  runContextUpdate,
	}
}

func runContextUpdate(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	agentsPath := filepath.Join(workDir, "AGENTS.md")
	existing, err := os.ReadFile(agentsPath)
	if err != nil {
		return fmt.Errorf("read AGENTS.md: %w (run 'foreman context generate' first)", err)
	}

	// Parse cursor from footer
	cursor := parseCursor(string(existing))

	obsLog := fcontext.NewObservationLog(workDir)
	observations, newCursor, err := obsLog.ReadFrom(cursor)
	if err != nil {
		return fmt.Errorf("read observations: %w", err)
	}
	if len(observations) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No new observations since last update.")
		return nil
	}

	cfg, err := config.LoadFromFile("foreman.toml")
	if err != nil {
		cfg, _ = config.LoadDefaults()
	}
	provider, err := llm.NewProviderFromConfig(cfg.LLM.DefaultProvider, cfg.LLM)
	if err != nil {
		return fmt.Errorf("create LLM provider: %w", err)
	}

	// Build update prompt
	var obsSummary strings.Builder
	for _, obs := range observations {
		data, _ := json.Marshal(obs)
		obsSummary.Write(data)
		obsSummary.WriteByte('\n')
	}

	resp, err := provider.Complete(cmd.Context(), models.LlmRequest{
		SystemPrompt: "You are updating an AGENTS.md file for Foreman, an autonomous coding daemon. Incorporate the new observations into the existing file. Keep the same structure. Only add or modify sections where observations provide new information. Output the complete updated AGENTS.md.",
		UserPrompt:   fmt.Sprintf("## Current AGENTS.md\n\n%s\n\n## New Observations\n\n%s", string(existing), obsSummary.String()),
		Model:        cfg.Models.Planner,
		MaxTokens:    4096,
		Temperature:  0.2,
	})
	if err != nil {
		return fmt.Errorf("LLM update call: %w", err)
	}

	// Write updated file with new cursor
	content := resp.Content
	content = stripCursor(content) // remove any existing cursor
	content += fmt.Sprintf("\n<!--foreman:last-update:%s:observations-cursor:%d-->\n",
		time.Now().UTC().Format(time.RFC3339), newCursor)

	if err := os.WriteFile(agentsPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write AGENTS.md: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Updated AGENTS.md with %d observations.\n", len(observations))
	return nil
}

func parseCursor(content string) int64 {
	// Look for <!--foreman:last-update:...:observations-cursor:NNN-->
	idx := strings.Index(content, "observations-cursor:")
	if idx == -1 {
		return 0
	}
	after := content[idx+len("observations-cursor:"):]
	end := strings.Index(after, "-->")
	if end == -1 {
		return 0
	}
	var cursor int64
	fmt.Sscanf(after[:end], "%d", &cursor)
	return cursor
}

func stripCursor(content string) string {
	idx := strings.Index(content, "\n<!--foreman:last-update:")
	if idx != -1 {
		return content[:idx]
	}
	return content
}
```

Add imports: `"encoding/json"`, `"strings"`, `"time"`, `"github.com/canhta/foreman/internal/models"`.

**Step 4: Run test**

Run: `go test ./cmd/ -run TestContextUpdateCmd_Exists -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/context.go cmd/context_test.go
git commit -m "feat(cli): add 'foreman context update' command with observation cursor"
```

---

### Task 22: Final integration — run all tests

**Step 1: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: All tests pass

**Step 2: Run linter**

Run: `go vet ./...`
Expected: No issues

**Step 3: Commit any fixes if needed**

```bash
git add -A
git commit -m "fix: address test and lint issues from integration"
```

---

## Implementation Order Summary

| Task | Feature | Description | Dependencies |
|------|---------|-------------|-------------|
| 1 | DAG | Config fields | None |
| 2 | DAG | Executor core | Task 1 |
| 3 | DAG | Parallel independent test | Task 2 |
| 4 | DAG | Dependency ordering test | Task 2 |
| 5 | DAG | BFS failure test | Task 2 |
| 6 | DAG | Bounded concurrency test | Task 2 |
| 7 | DAG | Context cancellation test | Task 2 |
| 8 | DAG | Metrics | Task 2 |
| 9 | DAG | PR body checklist | None |
| 10 | MCP | Config extensions | None |
| 11 | MCP | Name normalization | None |
| 12 | MCP | StdioClient core | Task 10 |
| 13 | MCP | Concurrent calls test | Task 12 |
| 14 | MCP | ProcessTransport | Task 12 |
| 15 | MCP | Manager | Task 12 |
| 16 | MCP | Config model | None |
| 17 | Context | File scanner | None |
| 18 | Context | Generator | Task 17 |
| 19 | Context | CLI command | Task 18 |
| 20 | Context | Observation log | None |
| 21 | Context | Update command | Task 18, 20 |
| 22 | All | Integration test | All |

**Parallelizable groups:**
- Tasks 1-9 (DAG) can run in parallel with Tasks 10-16 (MCP) and Tasks 17-20 (Context)
- Task 21 depends on Tasks 18 + 20
- Task 22 depends on all
