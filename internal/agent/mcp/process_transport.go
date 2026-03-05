package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"

	"github.com/rs/zerolog/log"
)

// ProcessTransport implements Transport by spawning a subprocess and
// communicating over its stdin/stdout using newline-delimited JSON.
type ProcessTransport struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	scanner    *bufio.Scanner
	serverName string
}

// NewProcessTransport spawns a subprocess from the given config.
// Only cfg.Env is passed (not inherited from the parent process).
// Returns an error if the command cannot be started.
func NewProcessTransport(cfg MCPServerConfig) (*ProcessTransport, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)

	// Build env from config only (no inheritance)
	env := make([]string, 0, len(cfg.Env))
	for k, v := range cfg.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %q: %w", cfg.Command, err)
	}

	// Capture stderr with [mcp:serverName] prefix
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Warn().Str("source", fmt.Sprintf("[mcp:%s]", cfg.Name)).Msg(scanner.Text())
		}
	}()

	pt := &ProcessTransport{
		cmd:        cmd,
		stdin:      stdin,
		scanner:    bufio.NewScanner(stdout),
		serverName: cfg.Name,
	}
	return pt, nil
}

// Send writes a JSON message followed by a newline to the process stdin.
// Callers (StdioClient) serialize writes via their own writeMu.
func (p *ProcessTransport) Send(msg json.RawMessage) error {
	_, err := fmt.Fprintf(p.stdin, "%s\n", msg)
	return err
}

// Receive reads a line from the process stdout and returns it as a JSON message.
func (p *ProcessTransport) Receive() (json.RawMessage, error) {
	if p.scanner.Scan() {
		return json.RawMessage(p.scanner.Bytes()), nil
	}
	if err := p.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("process stdout closed")
}

// Close kills the subprocess and cleans up.
func (p *ProcessTransport) Close() error {
	p.stdin.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return p.cmd.Wait()
}
