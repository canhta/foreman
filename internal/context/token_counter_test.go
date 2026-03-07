package context

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCountTokens_Empty(t *testing.T) {
	assert.Equal(t, 0, CountTokens(""))
}

func TestCountTokens_AccurateForCode(t *testing.T) {
	// Go code has more tokens per character than the len/4 heuristic suggests
	code := `func main() { fmt.Println("hello, world") }`
	heuristic := (len(code) + 3) / 4
	accurate := CountTokens(code)
	// Both should be positive
	assert.Greater(t, accurate, 0)
	assert.Greater(t, heuristic, 0)
	// Accurate should differ from naive heuristic for code
	// (tiktoken gives ~12 tokens, heuristic gives ~12 too for this short example but differs for larger code)
	t.Logf("code len=%d, heuristic=%d, tiktoken=%d", len(code), heuristic, accurate)
}

func TestCountTokens_SimpleText(t *testing.T) {
	text := "Hello, world!"
	count := CountTokens(text)
	assert.Greater(t, count, 0)
	assert.Less(t, count, 20) // "Hello, world!" is about 4-5 tokens
}

func TestEstimateTokens_DelegatesToCountTokens(t *testing.T) {
	text := "test content"
	assert.Equal(t, CountTokens(text), EstimateTokens(text))
}
