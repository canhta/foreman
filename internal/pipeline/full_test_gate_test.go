package pipeline

import (
	"context"
	"testing"

	"github.com/canhta/foreman/internal/runner"
)

type mockGateRunner struct {
	output *runner.CommandOutput
}

func (m *mockGateRunner) Run(_ context.Context, _, _ string, _ []string, _ int) (*runner.CommandOutput, error) {
	return m.output, nil
}

func (m *mockGateRunner) CommandExists(_ context.Context, _ string) bool { return true }

func TestFullTestGate_Pass(t *testing.T) {
	r := &mockGateRunner{output: &runner.CommandOutput{
		Stdout:   "PASS\nok example.com 0.5s",
		ExitCode: 0,
	}}

	result, err := RunFullTestSuite(context.Background(), r, "/work", "go test ./...", 600)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected passed")
	}
}

func TestFullTestGate_Fail(t *testing.T) {
	r := &mockGateRunner{output: &runner.CommandOutput{
		Stdout:   "FAIL\nFAIL example.com 0.5s",
		Stderr:   "test failed",
		ExitCode: 1,
	}}

	result, err := RunFullTestSuite(context.Background(), r, "/work", "go test ./...", 600)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected failed")
	}
}

func TestFullTestGate_Timeout(t *testing.T) {
	r := &mockGateRunner{output: &runner.CommandOutput{
		TimedOut: true,
		ExitCode: -1,
	}}

	result, err := RunFullTestSuite(context.Background(), r, "/work", "go test ./...", 600)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected failed on timeout")
	}
	if !result.TimedOut {
		t.Error("expected timeout flag")
	}
}
