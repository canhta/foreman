// internal/pipeline/implementer_test.go
package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/models"
)

type mockImplLLM struct {
	capturedRequest *models.LlmRequest
	response        string
}

func (m *mockImplLLM) Complete(_ context.Context, req models.LlmRequest) (*models.LlmResponse, error) {
	m.capturedRequest = &req
	return &models.LlmResponse{
		Content:      m.response,
		TokensInput:  500,
		TokensOutput: 300,
		Model:        req.Model,
		DurationMs:   1000,
		StopReason:   models.StopReasonEndTurn,
	}, nil
}

func (m *mockImplLLM) ProviderName() string                { return "mock" }
func (m *mockImplLLM) HealthCheck(_ context.Context) error { return nil }

func TestImplementer_Execute(t *testing.T) {
	llmProvider := &mockImplLLM{
		response: `--- SEARCH/REPLACE ---
<<<< SEARCH
func Add(a, b int) int {
	return 0
}
==== REPLACE
func Add(a, b int) int {
	return a + b
}
>>>> END

--- TEST ---
` + "```" + `go
func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Error("expected 5")
	}
}
` + "```",
	}

	impl := NewImplementer(llmProvider)
	result, err := impl.Execute(context.Background(), ImplementerInput{
		Task: &models.Task{
			ID:    "task-1",
			Title: "Implement Add function",
		},
		ContextFiles: map[string]string{
			"math.go": "package main\n\nfunc Add(a, b int) int {\n\treturn 0\n}\n",
		},
		Model:     "test-model",
		MaxTokens: 4096,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response == nil {
		t.Fatal("expected response")
	}
	if result.Response.Content == "" {
		t.Error("expected non-empty content")
	}
}

func TestImplementer_ExecuteRetry(t *testing.T) {
	llmProvider := &mockImplLLM{response: "retry response with fixes"}

	impl := NewImplementer(llmProvider)
	result, err := impl.Execute(context.Background(), ImplementerInput{
		Task:         &models.Task{ID: "task-1", Title: "Fix bug"},
		ContextFiles: map[string]string{"main.go": "package main"},
		Model:        "test-model",
		MaxTokens:    4096,
		Attempt:      2,
		Feedback:     "Tests failed: expected 5 got 0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response == nil {
		t.Fatal("expected response")
	}

	req := llmProvider.capturedRequest
	if req == nil {
		t.Fatal("expected LLM request to be captured")
	}

	// Verify retry section appears in user prompt with correct attempt number and feedback.
	if !strings.Contains(req.UserPrompt, "RETRY (attempt 2)") {
		t.Errorf("expected user prompt to contain 'RETRY (attempt 2)', got:\n%s", req.UserPrompt)
	}
	if !strings.Contains(req.UserPrompt, "Tests failed: expected 5 got 0") {
		t.Errorf("expected user prompt to contain feedback text, got:\n%s", req.UserPrompt)
	}
}

// TestBuildImplementerUserPrompt_NoRetryOnFirstAttempt verifies no retry section
// appears when Attempt == 1.
func TestBuildImplementerUserPrompt_NoRetryOnFirstAttempt(t *testing.T) {
	input := ImplementerInput{
		Task:     &models.Task{ID: "t1", Title: "Some task"},
		Attempt:  1,
		Feedback: "some feedback",
	}
	prompt := buildImplementerUserPrompt(input)
	if strings.Contains(prompt, "RETRY") {
		t.Errorf("expected no RETRY section on first attempt, got:\n%s", prompt)
	}
}

// TestBuildImplementerUserPrompt_RetryGuidancePerErrorType verifies that each of
// the 7 classified error types produces the correct heading, guidance text, and
// that the guidance appears before the raw feedback text.
func TestBuildImplementerUserPrompt_RetryGuidancePerErrorType(t *testing.T) {
	cases := []struct {
		name             string
		errType          ErrorType
		feedbackText     string
		wantHeading      string
		wantGuidanceSnip string
	}{
		{
			name:             "compile error",
			errType:          ErrorTypeCompile,
			feedbackText:     "syntax error: unexpected token",
			wantHeading:      "Compile Error",
			wantGuidanceSnip: "build error",
		},
		{
			name:             "type error",
			errType:          ErrorTypeTypeError,
			feedbackText:     "cannot use int as string",
			wantHeading:      "Type Error",
			wantGuidanceSnip: "type mismatch",
		},
		{
			name:             "lint/style",
			errType:          ErrorTypeLintStyle,
			feedbackText:     "line too long: 120 chars",
			wantHeading:      "Lint/Style",
			wantGuidanceSnip: "lint/style",
		},
		{
			name:             "test assertion",
			errType:          ErrorTypeTestAssertion,
			feedbackText:     "expected: 5, got: 0",
			wantHeading:      "Test Assertion",
			wantGuidanceSnip: "failing test assertions",
		},
		{
			name:             "test runtime",
			errType:          ErrorTypeTestRuntime,
			feedbackText:     "panic: runtime error: index out of range",
			wantHeading:      "Test Runtime",
			wantGuidanceSnip: "runtime panic",
		},
		{
			name:             "spec violation",
			errType:          ErrorTypeSpecViolation,
			feedbackText:     "acceptance criterion not met: endpoint returns 404",
			wantHeading:      "Spec Violation",
			wantGuidanceSnip: "acceptance criteria",
		},
		{
			name:             "quality concern",
			errType:          ErrorTypeQualityConcern,
			feedbackText:     "function is too complex (cyclomatic complexity 15)",
			wantHeading:      "Quality Concern",
			wantGuidanceSnip: "quality concerns",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := ImplementerInput{
				Task:           &models.Task{ID: "t1", Title: "Fix something"},
				Attempt:        2,
				Feedback:       tc.feedbackText,
				RetryErrorType: tc.errType,
			}
			prompt := buildImplementerUserPrompt(input)

			if !strings.Contains(prompt, tc.wantHeading) {
				t.Errorf("expected heading to contain %q, got:\n%s", tc.wantHeading, prompt)
			}
			if !strings.Contains(prompt, tc.wantGuidanceSnip) {
				t.Errorf("expected guidance to contain %q, got:\n%s", tc.wantGuidanceSnip, prompt)
			}
			if !strings.Contains(prompt, tc.feedbackText) {
				t.Errorf("expected prompt to contain feedback text %q, got:\n%s", tc.feedbackText, prompt)
			}
			// Guidance must appear BEFORE the raw feedback text.
			guidanceIdx := strings.Index(prompt, tc.wantGuidanceSnip)
			feedbackIdx := strings.Index(prompt, tc.feedbackText)
			if guidanceIdx == -1 || feedbackIdx == -1 || guidanceIdx > feedbackIdx {
				t.Errorf("guidance must appear before feedback text; guidanceIdx=%d feedbackIdx=%d", guidanceIdx, feedbackIdx)
			}
		})
	}
}

// TestBuildImplementerUserPrompt_UnknownTypeGenericHeader verifies that the
// unknown / zero-value error type still produces the old generic ## RETRY header.
func TestBuildImplementerUserPrompt_UnknownTypeGenericHeader(t *testing.T) {
	input := ImplementerInput{
		Task:           &models.Task{ID: "t1", Title: "Fix unknown"},
		Attempt:        2,
		Feedback:       "something went wrong",
		RetryErrorType: ErrorTypeUnknown,
	}
	prompt := buildImplementerUserPrompt(input)

	if !strings.Contains(prompt, "## RETRY (attempt 2)") {
		t.Errorf("expected generic '## RETRY (attempt 2)' header for unknown type, got:\n%s", prompt)
	}
}

// TestBuildImplementerUserPrompt_ZeroValueTypeGenericHeader verifies backward
// compatibility when RetryErrorType is the zero value (empty string).
func TestBuildImplementerUserPrompt_ZeroValueTypeGenericHeader(t *testing.T) {
	input := ImplementerInput{
		Task:     &models.Task{ID: "t1", Title: "Fix unknown"},
		Attempt:  2,
		Feedback: "something went wrong",
		// RetryErrorType intentionally left as zero value
	}
	prompt := buildImplementerUserPrompt(input)

	if !strings.Contains(prompt, "## RETRY (attempt 2)") {
		t.Errorf("expected generic '## RETRY (attempt 2)' header for zero-value type, got:\n%s", prompt)
	}
}
