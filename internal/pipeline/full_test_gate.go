package pipeline

import (
	"context"
	"strings"

	"github.com/canhta/foreman/internal/runner"
)

type FullTestResult struct {
	Output   string
	ExitCode int
	Passed   bool
	TimedOut bool
}

// RunFullTestSuite executes the full test suite as a pre-PR gate.
// This runs after all tasks are committed and rebased onto the target branch.
func RunFullTestSuite(ctx context.Context, cmdRunner runner.CommandRunner, workDir, testCommand string, timeoutSecs int) (*FullTestResult, error) {
	parts := strings.Fields(testCommand)
	if len(parts) == 0 {
		return &FullTestResult{Passed: true, Output: "no test command configured"}, nil
	}

	cmd := parts[0]
	args := parts[1:]

	output, err := cmdRunner.Run(ctx, workDir, cmd, args, timeoutSecs)
	if err != nil {
		return nil, err
	}

	result := &FullTestResult{
		Output:   output.Stdout + "\n" + output.Stderr,
		ExitCode: output.ExitCode,
		TimedOut: output.TimedOut,
	}

	result.Passed = output.ExitCode == 0 && !output.TimedOut
	return result, nil
}
