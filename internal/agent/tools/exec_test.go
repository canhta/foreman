package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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
	var fn tools.RunFn = func(ctx context.Context, task, workDir string, toolNames []string, maxTurns int) (string, error) {
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
