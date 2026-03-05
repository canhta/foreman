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

func TestDockerRunner_ParseContainerList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
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
