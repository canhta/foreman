package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/agent/tools"
	"github.com/canhta/foreman/internal/runner"
)

// mockCmdRunner satisfies runner.CommandRunner for testing.
type mockCmdRunner struct {
	stdout string
	stderr string
}

func (m *mockCmdRunner) Run(_ context.Context, _, _ string, _ []string, _ int) (*runner.CommandOutput, error) {
	return &runner.CommandOutput{Stdout: m.stdout, Stderr: m.stderr}, nil
}
func (m *mockCmdRunner) CommandExists(_ context.Context, _ string) bool { return true }

func newCodeRegistry(t *testing.T, cmd runner.CommandRunner) (*tools.Registry, string) {
	t.Helper()
	reg := tools.NewRegistry(nil, cmd, tools.ToolHooks{})
	return reg, t.TempDir()
}

func TestGetSymbol_FindsFunction(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc MyHandler() {}\nfunc other() {}"), 0644)
	b, _ := json.Marshal(map[string]string{"symbol": "MyHandler", "kind": "func"})
	out, err := reg.Execute(context.Background(), dir, "GetSymbol", b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "MyHandler") {
		t.Errorf("expected MyHandler in output, got %q", out)
	}
}

func TestGetSymbol_NotFound(t *testing.T) {
	reg, dir := newFSRegistry(t)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	b, _ := json.Marshal(map[string]string{"symbol": "NonExistent"})
	out, err := reg.Execute(context.Background(), dir, "GetSymbol", b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "not found") {
		t.Errorf("expected 'not found' message, got %q", out)
	}
}

func TestGetErrors_ParsesLintOutput(t *testing.T) {
	cmd := &mockCmdRunner{
		stdout: "main.go:10:5: undefined: foo\nmain.go:20:1: unused import\n",
	}
	reg, dir := newCodeRegistry(t, cmd)
	out := execTool(t, reg, dir, "GetErrors", map[string]string{"tool": "golangci-lint"})
	if !strings.Contains(out, "main.go") || !strings.Contains(out, "undefined") {
		t.Errorf("expected lint issues, got %q", out)
	}
}

func TestGetErrors_NilRunner(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	b, _ := json.Marshal(map[string]string{"tool": "golangci-lint"})
	_, err := reg.Execute(context.Background(), t.TempDir(), "GetErrors", b)
	if err == nil {
		t.Fatal("expected error when runner is nil")
	}
}
