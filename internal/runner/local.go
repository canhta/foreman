package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/canhta/foreman/internal/models"
)

type LocalRunner struct {
	config *models.LocalRunnerConfig
}

func NewLocalRunner(config *models.LocalRunnerConfig) *LocalRunner {
	return &LocalRunner{config: config}
}

func (r *LocalRunner) Run(ctx context.Context, workDir, command string, args []string, timeoutSecs int) (*CommandOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	output := &CommandOutput{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	if ctx.Err() == context.DeadlineExceeded {
		output.TimedOut = true
		output.ExitCode = -1
		return output, nil
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			output.ExitCode = exitErr.ExitCode()
			return output, nil
		}
		return nil, fmt.Errorf("failed to execute command %s: %w", command, err)
	}

	return output, nil
}

func (r *LocalRunner) CommandExists(ctx context.Context, command string) bool {
	_, err := exec.LookPath(command)
	return err == nil
}
