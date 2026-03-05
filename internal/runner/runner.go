package runner

import (
	"context"
	"time"
)

type CommandRunner interface {
	Run(ctx context.Context, workDir, command string, args []string, timeoutSecs int) (*CommandOutput, error)
	CommandExists(ctx context.Context, command string) bool
}

type CommandOutput struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	TimedOut bool
}
