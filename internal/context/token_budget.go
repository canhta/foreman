package context

// EstimateTokens provides a rough token estimate.
// Approximation: ~4 characters per token for English/code.
func EstimateTokens(content string) int {
	if len(content) == 0 {
		return 0
	}
	return (len(content) + 3) / 4
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
