package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

const MaxBatchCalls = 25

type BatchCall struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
}

type BatchInput struct {
	ToolCalls []BatchCall `json:"tool_calls"`
}

type BatchResult struct {
	Tool    string `json:"tool"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
	Success bool   `json:"success"`
}

func ParseBatchInput(raw string) ([]BatchCall, error) {
	var input BatchInput
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return nil, fmt.Errorf("invalid batch input: %w", err)
	}
	for _, call := range input.ToolCalls {
		if strings.EqualFold(call.Tool, "Batch") {
			return nil, fmt.Errorf("cannot nest Batch tool calls")
		}
	}
	if err := ValidateBatchCalls(input.ToolCalls); err != nil {
		return nil, err
	}
	return input.ToolCalls, nil
}

func ValidateBatchCalls(calls []BatchCall) error {
	if len(calls) > MaxBatchCalls {
		return fmt.Errorf("maximum %d tool calls per batch, got %d", MaxBatchCalls, len(calls))
	}
	return nil
}

// ExecuteBatch runs multiple tool calls in parallel using the provided executor.
func ExecuteBatch(calls []BatchCall, executor func(tool string, input json.RawMessage) (string, error)) []BatchResult {
	results := make([]BatchResult, len(calls))
	var wg sync.WaitGroup

	for i, call := range calls {
		wg.Add(1)
		go func(idx int, c BatchCall) {
			defer wg.Done()
			output, err := executor(c.Tool, c.Input)
			if err != nil {
				results[idx] = BatchResult{Tool: c.Tool, Success: false, Error: err.Error()}
			} else {
				results[idx] = BatchResult{Tool: c.Tool, Success: true, Output: output}
			}
		}(i, call)
	}
	wg.Wait()
	return results
}

// batchTool implements the Batch tool for parallel execution of multiple tools.
type batchTool struct {
	reg *Registry
}

func (t *batchTool) Name() string { return "Batch" }
func (t *batchTool) Description() string {
	return "Execute multiple tool calls in parallel (max 25). Cannot nest Batch calls."
}
func (t *batchTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"tool_calls":{"type":"array","description":"List of tool calls to execute in parallel","items":{"type":"object","properties":{"tool":{"type":"string","description":"Tool name"},"input":{"type":"object","description":"Tool input parameters"}},"required":["tool","input"]},"maxItems":25}},"required":["tool_calls"]}`)
}
func (t *batchTool) Execute(ctx context.Context, workDir string, input json.RawMessage) (string, error) {
	calls, err := ParseBatchInput(string(input))
	if err != nil {
		return "", fmt.Errorf("batch: %w", err)
	}

	executor := func(tool string, inp json.RawMessage) (string, error) {
		return t.reg.Execute(ctx, workDir, tool, inp)
	}

	results := ExecuteBatch(calls, executor)

	out, err := json.Marshal(results)
	if err != nil {
		return "", fmt.Errorf("batch: failed to marshal results: %w", err)
	}
	return string(out), nil
}
