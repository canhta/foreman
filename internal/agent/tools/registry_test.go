package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/canhta/foreman/internal/agent/tools"
)

// stubTool is a minimal Tool for testing.
type stubTool struct {
	name   string
	output string
}

func (s *stubTool) Name() string            { return s.name }
func (s *stubTool) Description() string     { return "stub" }
func (s *stubTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (s *stubTool) Execute(_ context.Context, _ string, _ json.RawMessage) (string, error) {
	return s.output, nil
}

func TestRegistry_Execute_KnownTool(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	reg.Register(&stubTool{name: "Stub", output: "hello"})

	out, err := reg.Execute(context.Background(), "/work", "Stub", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello" {
		t.Errorf("expected 'hello', got %q", out)
	}
}

func TestRegistry_Execute_UnknownTool(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	_, err := reg.Execute(context.Background(), "/work", "Missing", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestRegistry_Defs_ReturnsRequested(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	reg.Register(&stubTool{name: "A"})
	reg.Register(&stubTool{name: "B"})

	defs := reg.Defs([]string{"A"})
	if len(defs) != 1 || defs[0].Name != "A" {
		t.Errorf("expected 1 def for A, got %+v", defs)
	}
}

func TestRegistry_Defs_SkipsUnknown(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	reg.Register(&stubTool{name: "A"})

	defs := reg.Defs([]string{"A", "Unknown"})
	if len(defs) != 1 {
		t.Errorf("expected 1 def (unknown skipped), got %d", len(defs))
	}
}

func TestRegistry_PreHook_CanBlock(t *testing.T) {
	blocked := false
	hooks := tools.ToolHooks{
		PreToolUse: func(_ context.Context, name string, _ json.RawMessage) error {
			if name == "Stub" {
				blocked = true
				return fmt.Errorf("blocked by hook")
			}
			return nil
		},
	}
	reg := tools.NewRegistry(nil, nil, hooks)
	reg.Register(&stubTool{name: "Stub", output: "should not reach"})

	_, err := reg.Execute(context.Background(), "/work", "Stub", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected pre-hook to block execution")
	}
	if !blocked {
		t.Error("expected pre-hook to be called")
	}
}

func TestRegistry_PostHook_CalledAfterExecution(t *testing.T) {
	var postCalled atomic.Bool
	hooks := tools.ToolHooks{
		PostToolUse: func(_ context.Context, name, output string, err error) {
			postCalled.Store(true)
		},
	}
	reg := tools.NewRegistry(nil, nil, hooks)
	reg.Register(&stubTool{name: "Stub", output: "ok"})

	reg.Execute(context.Background(), "/work", "Stub", json.RawMessage(`{}`))
	if !postCalled.Load() {
		t.Error("expected post-hook to be called")
	}
}

func TestRegistry_Has(t *testing.T) {
	reg := tools.NewRegistry(nil, nil, tools.ToolHooks{})
	reg.Register(&stubTool{name: "A"})
	if !reg.Has("A") {
		t.Error("expected Has('A') to be true")
	}
	if reg.Has("B") {
		t.Error("expected Has('B') to be false")
	}
}
