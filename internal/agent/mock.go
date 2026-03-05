package agent

import "context"

// MockRunner is a test double for AgentRunner.
type MockRunner struct {
	RunFunc         func(ctx context.Context, req AgentRequest) (AgentResult, error)
	HealthCheckFunc func(ctx context.Context) error
	Name            string
}

func (m *MockRunner) Run(ctx context.Context, req AgentRequest) (AgentResult, error) {
	if m.RunFunc != nil {
		return m.RunFunc(ctx, req)
	}
	return AgentResult{Output: "mock output"}, nil
}

func (m *MockRunner) HealthCheck(ctx context.Context) error {
	if m.HealthCheckFunc != nil {
		return m.HealthCheckFunc(ctx)
	}
	return nil
}

func (m *MockRunner) RunnerName() string {
	if m.Name != "" {
		return m.Name
	}
	return "mock"
}

func (m *MockRunner) Close() error { return nil }
