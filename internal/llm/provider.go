package llm

import "context"

// LlmProvider is implemented by each LLM backend (Anthropic, OpenAI, etc.)
// Every call is stateless — no conversation memory.
type LlmProvider interface {
	Complete(ctx context.Context, req LlmRequest) (*LlmResponse, error)
	ProviderName() string
	HealthCheck(ctx context.Context) error
}

type LlmRequest struct {
	Model         string   `json:"model"`
	SystemPrompt  string   `json:"system_prompt"`
	UserPrompt    string   `json:"user_prompt"`
	MaxTokens     int      `json:"max_tokens"`
	Temperature   float64  `json:"temperature"`
	StopSequences []string `json:"stop_sequences,omitempty"`
}

type LlmResponse struct {
	Content      string `json:"content"`
	TokensInput  int    `json:"tokens_input"`
	TokensOutput int    `json:"tokens_output"`
	Model        string `json:"model"`
	DurationMs   int64  `json:"duration_ms"`
	StopReason   string `json:"stop_reason"`
}

// Error types for structured error handling.
type RateLimitError struct {
	RetryAfterSecs int
}

func (e *RateLimitError) Error() string {
	return "rate limit exceeded"
}

type AuthError struct {
	Message string
}

func (e *AuthError) Error() string {
	return "authentication error: " + e.Message
}

type BudgetExceededError struct {
	Current float64
	Limit   float64
}

func (e *BudgetExceededError) Error() string {
	return "budget exceeded"
}

type ConnectionError struct {
	Attempt int
	Err     error
}

func (e *ConnectionError) Error() string {
	return e.Err.Error()
}

func (e *ConnectionError) Unwrap() error {
	return e.Err
}
