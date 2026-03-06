package channel

import (
	"context"
	"testing"
)

func TestClassifier_PrefixCommands(t *testing.T) {
	c := NewClassifier(nil) // no LLM needed for prefix tests

	tests := []struct {
		body    string
		kind    string
		command string
	}{
		{"/status", "command", "status"},
		{"/pause", "command", "pause"},
		{"/resume", "command", "resume"},
		{"/cost", "command", "cost"},
		{"/STATUS", "command", "status"},
		{"/pause please", "command", "pause"},
		{"Build a login page", "new_ticket", ""},
		{"", "new_ticket", ""},
	}

	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			result := c.Classify(context.Background(), tt.body)
			if result.Kind != tt.kind {
				t.Errorf("Classify(%q).Kind = %q, want %q", tt.body, result.Kind, tt.kind)
			}
			if result.Command != tt.command {
				t.Errorf("Classify(%q).Command = %q, want %q", tt.body, result.Command, tt.command)
			}
		})
	}
}
