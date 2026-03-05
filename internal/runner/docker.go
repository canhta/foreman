package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/rs/zerolog/log"
)

type DockerRunner struct {
	image            string
	network          string
	cpuLimit         string
	memoryLimit      string
	currentTicketID  string
	persistPerTicket bool
	autoReinstall    bool
}

func NewDockerRunner(image string, persistPerTicket bool, network, cpuLimit, memoryLimit string, autoReinstall bool) *DockerRunner {
	return &DockerRunner{
		image:            image,
		persistPerTicket: persistPerTicket,
		network:          network,
		cpuLimit:         cpuLimit,
		memoryLimit:      memoryLimit,
		autoReinstall:    autoReinstall,
	}
}

// SetTicketID sets the current ticket ID on the runner. One runner instance is
// created per ticket in the daemon; this allows orphan tracking via Docker labels.
func (r *DockerRunner) SetTicketID(id string) {
	r.currentTicketID = id
}

func (r *DockerRunner) formatRunArgs(workDir, ticketID string) []string {
	args := []string{"run", "--rm"}
	args = append(args, "--label", "foreman-ticket="+ticketID)
	if r.network != "" {
		args = append(args, "--network", r.network)
	}
	if r.cpuLimit != "" {
		args = append(args, "--cpus", r.cpuLimit)
	}
	if r.memoryLimit != "" {
		args = append(args, "--memory", r.memoryLimit)
	}
	args = append(args, "-v", workDir+":"+workDir, "-w", workDir)
	args = append(args, r.image)
	return args
}

func (r *DockerRunner) Run(ctx context.Context, workDir, command string, args []string, timeoutSecs int) (*CommandOutput, error) {
	if r.currentTicketID == "" {
		log.Warn().Msg("DockerRunner.Run called without a ticket ID set — container will not be labeled correctly")
	}

	if timeoutSecs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
		defer cancel()
	}

	// Build docker run command: docker run ... image command args...
	dockerArgs := r.formatRunArgs(workDir, r.currentTicketID)
	dockerArgs = append(dockerArgs, command)
	dockerArgs = append(dockerArgs, args...)

	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)

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
		return nil, fmt.Errorf("docker run failed: %w", err)
	}

	return output, nil
}

func (r *DockerRunner) CommandExists(_ context.Context, _ string) bool {
	// In Docker mode, we assume all commands are available inside the container.
	return true
}

// parseContainerList parses the output of `docker ps` with tab-separated
// containerID and ticketID fields and returns a map of containerID -> ticketID.
func parseContainerList(output []byte) map[string]string {
	result := make(map[string]string)
	for _, line := range bytes.Split(output, []byte("\n")) {
		parts := bytes.SplitN(line, []byte("\t"), 2)
		if len(parts) != 2 {
			continue
		}
		containerID := string(parts[0])
		ticketID := string(parts[1])
		if containerID == "" {
			continue
		}
		result[containerID] = ticketID
	}
	return result
}

// CleanupOrphanContainers removes Docker containers labeled with foreman-ticket
// that don't match any active ticket ID.
func (r *DockerRunner) CleanupOrphanContainers(ctx context.Context, activeTicketIDs map[string]bool) error {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--filter", "label=foreman-ticket", "--format", "{{.ID}}\t{{index .Labels \"foreman-ticket\"}}")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	for containerID, ticketID := range parseContainerList(out.Bytes()) {
		if !activeTicketIDs[ticketID] {
			if err := exec.CommandContext(ctx, "docker", "rm", "-f", containerID).Run(); err != nil {
				log.Warn().Err(err).Str("container_id", containerID).Msg("failed to remove orphan container")
			}
		}
	}
	return nil
}
