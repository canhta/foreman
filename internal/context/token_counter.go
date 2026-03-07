package context

import (
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

// defaultEncoding is used for all models as a good approximation.
// cl100k_base is used by GPT-4, Claude, and most modern LLMs.
const defaultEncoding = "cl100k_base"

var (
	encoderOnce sync.Once
	encoder     *tiktoken.Tiktoken
	encoderErr  error
)

func getEncoder() (*tiktoken.Tiktoken, error) {
	encoderOnce.Do(func() {
		encoder, encoderErr = tiktoken.GetEncoding(defaultEncoding)
	})
	return encoder, encoderErr
}

// CountTokens returns the accurate token count for text using tiktoken.
// Falls back to the len/4 heuristic if the encoder is unavailable.
func CountTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	enc, err := getEncoder()
	if err != nil {
		// Fallback to heuristic
		return (len(text) + 3) / 4
	}
	tokens := enc.Encode(text, nil, nil)
	return len(tokens)
}
