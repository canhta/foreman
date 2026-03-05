// internal/pipeline/prompt_renderer_test.go
package pipeline

import (
	"testing"
)

func TestRenderPrompt_Planner(t *testing.T) {
	ctx := PromptContext{
		TicketTitle:       "Add login page",
		TicketDescription: "Build a login page with email and password.",
		FileTree:          "src/\n  main.go\n  auth/\n    login.go",
	}

	result, err := RenderPrompt("planner", ctx)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty rendered prompt")
	}
	if !promptContainsAll(result, "Add login page", "login page with email") {
		t.Error("expected ticket context in rendered prompt")
	}
}

func TestRenderPrompt_Implementer(t *testing.T) {
	ctx := PromptContext{
		TaskTitle:       "Implement login handler",
		TaskDescription: "Handle POST /login with email/password validation.",
		ContextFiles: map[string]string{
			"auth/login.go": "package auth\n",
		},
	}

	result, err := RenderPrompt("implementer", ctx)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty rendered prompt")
	}
}

func TestRenderPrompt_Unknown(t *testing.T) {
	_, err := RenderPrompt("nonexistent", PromptContext{})
	if err == nil {
		t.Error("expected error for unknown template")
	}
}

func promptContainsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}
