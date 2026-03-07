package context

// EstimateTokens returns the token count for content.
// Uses tiktoken for accuracy; falls back to len/4 heuristic if unavailable.
func EstimateTokens(content string) int {
	return CountTokens(content)
}

// TokenBudget tracks token usage against a limit.
type TokenBudget struct {
	limit int
	used  int
}

func NewTokenBudget(limit int) *TokenBudget {
	return &TokenBudget{limit: limit}
}

func (tb *TokenBudget) CanFit(tokens int) bool {
	return tb.used+tokens <= tb.limit
}

func (tb *TokenBudget) Add(tokens int) {
	tb.used += tokens
}

func (tb *TokenBudget) Remaining() int {
	r := tb.limit - tb.used
	if r < 0 {
		return 0
	}
	return r
}

func (tb *TokenBudget) Used() int {
	return tb.used
}
