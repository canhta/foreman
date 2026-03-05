package runner

import (
	"context"
	"testing"
)

func TestDockerRunner_FormatRunArgs(t *testing.T) {
	r := &DockerRunner{
		image:       "node:22-slim",
		network:     "none",
		cpuLimit:    "2.0",
		memoryLimit: "4g",
	}

	args := r.formatRunArgs("/work", "t1")
	expected := []string{
		"run", "--rm",
		"--label", "foreman-ticket=t1",
		"--network", "none",
		"--cpus", "2.0",
		"--memory", "4g",
		"-v", "/work:/work",
		"-w", "/work",
		"node:22-slim",
	}

	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, a := range expected {
		if args[i] != a {
			t.Errorf("arg[%d]: expected %q, got %q", i, a, args[i])
		}
	}
}

func TestDockerRunner_CommandExists(t *testing.T) {
	r := NewDockerRunner("node:22-slim", false, "none", "2.0", "4g", false)
	// CommandExists for Docker always returns true — commands are inside the container
	if !r.CommandExists(context.Background(), "npm") {
		t.Error("expected CommandExists to return true for Docker runner")
	}
}
