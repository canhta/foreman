package runner

import (
	"context"
	"testing"
)

func TestLocalRunner_Run(t *testing.T) {
	r := NewLocalRunner(nil)
	ctx := context.Background()

	t.Run("successful command", func(t *testing.T) {
		out, err := r.Run(ctx, "/tmp", "echo", []string{"hello"}, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.ExitCode != 0 {
			t.Errorf("expected exit code 0, got %d", out.ExitCode)
		}
		if out.Stdout != "hello\n" {
			t.Errorf("expected stdout 'hello\\n', got %q", out.Stdout)
		}
	})

	t.Run("failing command", func(t *testing.T) {
		out, err := r.Run(ctx, "/tmp", "false", nil, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.ExitCode == 0 {
			t.Error("expected non-zero exit code")
		}
	})

	t.Run("command not found", func(t *testing.T) {
		_, err := r.Run(ctx, "/tmp", "nonexistent_cmd_xyz", nil, 10)
		if err == nil {
			t.Error("expected error for nonexistent command")
		}
	})
}

func TestLocalRunner_CommandExists(t *testing.T) {
	r := NewLocalRunner(nil)
	ctx := context.Background()

	if !r.CommandExists(ctx, "echo") {
		t.Error("echo should exist")
	}
	if r.CommandExists(ctx, "nonexistent_cmd_xyz") {
		t.Error("nonexistent command should not exist")
	}
}
