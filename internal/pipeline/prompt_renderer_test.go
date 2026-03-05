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

func TestRenderPrompt_SpecReviewer(t *testing.T) {
	ctx := PromptContext{
		TaskTitle:       "Implement auth middleware",
		TaskDescription: "JWT validation middleware for all protected routes.",
		AcceptanceCriteria: []string{
			"Returns 401 for missing token",
			"Returns 403 for invalid token",
		},
		Diff: "--- a/auth.go\n+++ b/auth.go\n@@ -0,0 +1,5 @@\n+func Middleware() {}",
	}

	result, err := RenderPrompt("spec_reviewer", ctx)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty rendered prompt")
	}
}

func TestRenderPrompt_QualityReviewer(t *testing.T) {
	ctx := PromptContext{
		Diff:             "--- a/auth.go\n+++ b/auth.go\n@@ -0,0 +1,5 @@\n+func Middleware() {}",
		CodebasePatterns: "Use zerolog for logging. Wrap errors with fmt.Errorf.",
	}

	result, err := RenderPrompt("quality_reviewer", ctx)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty rendered prompt")
	}
}

func TestRenderPrompt_FinalReviewer(t *testing.T) {
	ctx := PromptContext{
		TicketTitle:       "Add authentication",
		TicketDescription: "Implement JWT-based authentication.",
		FullDiff:          "--- a/auth.go\n+++ b/auth.go\n@@ -0,0 +1,5 @@\n+func Middleware() {}",
		CompletedTasks: []CompletedTask{
			{Title: "Implement auth middleware", Status: "done"},
			{Title: "Add login endpoint", Status: "done"},
		},
	}

	result, err := RenderPrompt("final_reviewer", ctx)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty rendered prompt")
	}
	if !promptContainsAll(result, "Implement auth middleware", "Add login endpoint") {
		t.Error("expected completed task titles in rendered prompt")
	}
}

func TestRenderPrompt_Clarifier(t *testing.T) {
	ctx := PromptContext{
		TicketTitle:       "Build something",
		TicketDescription: "It should do things.",
	}

	result, err := RenderPrompt("clarifier", ctx)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty rendered prompt")
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
