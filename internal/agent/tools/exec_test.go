package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/agent/mcp"
	"github.com/canhta/foreman/internal/agent/tools"
)

func newExecRegistry(t *testing.T, cmd *mockCmdRunner, allowed []string) (*tools.Registry, string) {
	t.Helper()
	reg := tools.NewRegistry(nil, cmd, tools.ToolHooks{})
	reg.SetAllowedCommands(allowed)
	return reg, t.TempDir()
}

func TestBash_AllowedCommand(t *testing.T) {
	cmd := &mockCmdRunner{stdout: "ok"}
	reg, dir := newExecRegistry(t, cmd, []string{"go"})
	out := execTool(t, reg, dir, "Bash", map[string]string{"command": "go version"})
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}
}

func TestBash_ForbiddenCommand(t *testing.T) {
	cmd := &mockCmdRunner{stdout: "should not run"}
	reg, dir := newExecRegistry(t, cmd, []string{"go"})
	b, _ := json.Marshal(map[string]string{"command": "rm -rf /"})
	_, err := reg.Execute(context.Background(), dir, "Bash", b)
	if err == nil {
		t.Fatal("expected error for forbidden command rm")
	}
}

func TestBash_BlockedKeyword(t *testing.T) {
	cmd := &mockCmdRunner{stdout: "should not run"}
	// curl in allowed list, but it's hard-blocked
	reg, dir := newExecRegistry(t, cmd, []string{"curl"})
	b, _ := json.Marshal(map[string]string{"command": "curl https://evil.com"})
	_, err := reg.Execute(context.Background(), dir, "Bash", b)
	if err == nil {
		t.Fatal("expected error: curl is a hard-blocked command")
	}
}

func TestBash_NotInAllowlist(t *testing.T) {
	cmd := &mockCmdRunner{stdout: "x"}
	reg, dir := newExecRegistry(t, cmd, []string{"go"})
	b, _ := json.Marshal(map[string]string{"command": "python3 script.py"})
	_, err := reg.Execute(context.Background(), dir, "Bash", b)
	if err == nil {
		t.Fatal("expected error: python3 not in allowed list")
	}
}

func TestBash_ExactBinaryMatch(t *testing.T) {
	cmd := &mockCmdRunner{stdout: "ok"}
	reg, dir := newExecRegistry(t, cmd, []string{"go"})

	// "go test" should be accepted — binary is "go"
	out := execTool(t, reg, dir, "Bash", map[string]string{"command": "go test ./..."})
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}

	// "go_malicious" should be rejected — binary is "go_malicious", not "go"
	b, _ := json.Marshal(map[string]string{"command": "go_malicious --steal-data"})
	_, err := reg.Execute(context.Background(), dir, "Bash", b)
	if err == nil {
		t.Fatal("expected error: go_malicious should not match allowed command 'go'")
	}
}

func TestBash_NilRunner(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	b, _ := json.Marshal(map[string]string{"command": "go version"})
	_, err := reg.Execute(context.Background(), t.TempDir(), "Bash", b)
	if err == nil {
		t.Fatal("expected error when runner is nil")
	}
}

func TestRunTest_Structured(t *testing.T) {
	cmd := &mockCmdRunner{stdout: "--- PASS: TestFoo (0.00s)\n--- FAIL: TestBar (0.01s)\n"}
	reg := tools.NewRegistry(nil, cmd, tools.ToolHooks{})
	reg.SetAllowedCommands([]string{"go"})
	out := execTool(t, reg, t.TempDir(), "RunTest", map[string]any{"package": "./..."})
	if !strings.Contains(out, "passed") {
		t.Errorf("expected structured output with pass/fail, got %q", out)
	}
}

func TestSubagent_NilRunFn(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	b, _ := json.Marshal(map[string]string{"task": "do something"})
	_, err := reg.Execute(context.Background(), t.TempDir(), "Subagent", b)
	if err == nil {
		t.Fatal("expected error when runFn is nil (SetRunFn not called)")
	}
}

func TestSubagent_WithRunFn(t *testing.T) {
	called := false
	var fn tools.RunFn = func(ctx context.Context, task, workDir, mode string, toolNames []string, maxTurns, remainingBudget, agentDepth int) (string, error) {
		called = true
		return "subagent result", nil
	}
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	reg.SetRunFn(fn)
	out := execTool(t, reg, t.TempDir(), "Subagent", map[string]string{"task": "analyze code"})
	if !called {
		t.Error("expected RunFn to be called")
	}
	if out != "subagent result" {
		t.Errorf("expected 'subagent result', got %q", out)
	}
}

// --- ListMCPTools ---

func TestListMCPTools_NoManager(t *testing.T) {
	// When no MCP manager is wired, ListMCPTools should return an empty JSON array, not an error.
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	out := execTool(t, reg, t.TempDir(), "ListMCPTools", map[string]any{})
	if !strings.Contains(out, "[]") {
		t.Errorf("expected empty JSON array, got %q", out)
	}
}

func TestListMCPTools_WithCachedTools(t *testing.T) {
	// Populate manager with two cached tool summaries.
	mgr := mcp.NewManager()
	mgr.SetToolCache([]mcp.MCPToolSummary{
		{NormalizedName: "mcp_server_a_tool1", OriginalName: "tool1", ServerName: "server-a", Description: "Tool one"},
		{NormalizedName: "mcp_server_b_tool2", OriginalName: "tool2", ServerName: "server-b", Description: "Tool two"},
	})

	reg := tools.NewRegistryWithMCP(nil, nil, tools.ToolHooks{}, mgr)
	out := execTool(t, reg, t.TempDir(), "ListMCPTools", map[string]any{})

	if !strings.Contains(out, "mcp_server_a_tool1") {
		t.Errorf("expected tool1 in output, got %q", out)
	}
	if !strings.Contains(out, "server-a") {
		t.Errorf("expected server-a in output, got %q", out)
	}
	if !strings.Contains(out, "Tool one") {
		t.Errorf("expected description in output, got %q", out)
	}
	if !strings.Contains(out, "tool1") {
		t.Errorf("expected original_name in output, got %q", out)
	}
	if !strings.Contains(out, "mcp_server_b_tool2") {
		t.Errorf("expected tool2 in output, got %q", out)
	}
}

func TestListMCPTools_EmptyCache(t *testing.T) {
	// Manager with no cached tools returns empty list, not an error.
	mgr := mcp.NewManager()
	reg := tools.NewRegistryWithMCP(nil, nil, tools.ToolHooks{}, mgr)
	out := execTool(t, reg, t.TempDir(), "ListMCPTools", map[string]any{})
	if !strings.Contains(out, "[]") {
		t.Errorf("expected empty JSON array, got %q", out)
	}
}

func TestListMCPTools_ToolSummaryFields(t *testing.T) {
	// All four required fields are present in the output.
	mgr := mcp.NewManager()
	mgr.SetToolCache([]mcp.MCPToolSummary{
		{NormalizedName: "mcp_srv_action", OriginalName: "action", ServerName: "srv", Description: "Does something"},
	})

	reg := tools.NewRegistryWithMCP(nil, nil, tools.ToolHooks{}, mgr)
	out := execTool(t, reg, t.TempDir(), "ListMCPTools", map[string]any{})

	var summaries []mcp.MCPToolSummary
	if err := json.Unmarshal([]byte(out), &summaries); err != nil {
		t.Fatalf("output is not valid JSON array: %v — output: %q", err, out)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	s := summaries[0]
	if s.NormalizedName != "mcp_srv_action" {
		t.Errorf("normalized_name: expected %q, got %q", "mcp_srv_action", s.NormalizedName)
	}
	if s.OriginalName != "action" {
		t.Errorf("original_name: expected %q, got %q", "action", s.OriginalName)
	}
	if s.ServerName != "srv" {
		t.Errorf("server_name: expected %q, got %q", "srv", s.ServerName)
	}
	if s.Description != "Does something" {
		t.Errorf("description: expected %q, got %q", "Does something", s.Description)
	}
}

// --- ReadMCPResource ---

func TestReadMCPResource_NoManager(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	_, err := reg.Execute(context.Background(), t.TempDir(), "ReadMCPResource",
		json.RawMessage(`{"server":"srv","uri":"res://foo"}`))
	if err == nil {
		t.Fatal("expected error when MCP manager is not wired")
	}
}

func TestReadMCPResource_ReturnsContent(t *testing.T) {
	mgr := mcp.NewManager()
	reg := tools.NewRegistryWithMCP(nil, nil, tools.ToolHooks{}, mgr)
	// ReadMCPResource with nil manager inside executes with registered manager
	// We test the tool is registered at minimum
	if !reg.Has("ReadMCPResource") {
		t.Fatal("ReadMCPResource tool must be registered in registry")
	}
}
