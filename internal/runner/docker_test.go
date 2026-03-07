package runner

import (
	"context"
	"testing"
)

func TestDockerRunner_FormatRunArgs(t *testing.T) {
	r := &DockerRunner{
		image:        "node:22-slim",
		network:      "none",
		cpuLimit:     "2.0",
		memoryLimit:  "4g",
		allowNetwork: true,
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
	r := NewDockerRunner("node:22-slim", false, "none", "2.0", "4g", false, false)
	// CommandExists for Docker always returns true — commands are inside the container
	if !r.CommandExists(context.Background(), "npm") {
		t.Error("expected CommandExists to return true for Docker runner")
	}
}

func TestDockerRunner_ParseContainerList(t *testing.T) {
	tests := []struct {
		expected map[string]string
		name     string
		input    string
	}{
		{
			name:     "empty output",
			input:    "",
			expected: map[string]string{},
		},
		{
			name:     "single valid line",
			input:    "abc123\tticket-1",
			expected: map[string]string{"abc123": "ticket-1"},
		},
		{
			name:     "line with no tab is skipped",
			input:    "abc123",
			expected: map[string]string{},
		},
		{
			name:  "multiple valid lines",
			input: "abc123\tticket-1\ndef456\tticket-2\nghi789\tticket-3",
			expected: map[string]string{
				"abc123": "ticket-1",
				"def456": "ticket-2",
				"ghi789": "ticket-3",
			},
		},
		{
			name:  "mixed valid and invalid lines",
			input: "abc123\tticket-1\nbadline\ndef456\tticket-2",
			expected: map[string]string{
				"abc123": "ticket-1",
				"def456": "ticket-2",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseContainerList([]byte(tc.input))
			if len(result) != len(tc.expected) {
				t.Fatalf("expected %d entries, got %d: %v", len(tc.expected), len(result), result)
			}
			for k, v := range tc.expected {
				got, ok := result[k]
				if !ok {
					t.Errorf("missing key %q", k)
				} else if got != v {
					t.Errorf("key %q: expected %q, got %q", k, v, got)
				}
			}
		})
	}
}

func TestDockerRunner_NetworkNoneByDefault(t *testing.T) {
	r := NewDockerRunner("image", false, "", "", "", false, false)
	args := r.formatRunArgs("/workdir", "ticket123")

	idx := -1
	for i, a := range args {
		if a == "--network" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("expected --network flag in args, not found")
	}
	if args[idx+1] != "none" {
		t.Errorf("expected --network none, got --network %s", args[idx+1])
	}
}

func TestDockerRunner_NetworkAllowedWithCustomNet(t *testing.T) {
	r := NewDockerRunner("image", false, "my-net", "", "", false, true)
	args := r.formatRunArgs("/workdir", "ticket123")

	found := false
	for _, a := range args {
		if a == "my-net" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected custom network 'my-net' in args, got: %v", args)
	}
}

func TestDockerRunner_NetworkDefaultBridgeWhenAllowedNoCustomNet(t *testing.T) {
	r := NewDockerRunner("image", false, "", "", "", false, true)
	args := r.formatRunArgs("/workdir", "ticket123")

	for i, a := range args {
		if a == "--network" {
			if args[i+1] == "none" {
				t.Errorf("expected no --network none when allow_network=true and no custom network, got: %v", args)
			}
		}
	}
}
