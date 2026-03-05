package context

import "testing"

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantMin int
		wantMax int
	}{
		{"empty", "", 0, 1},
		{"short", "hello world", 2, 5},
		{"code block", "func main() {\n\tfmt.Println(\"hello\")\n}", 5, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokens(tt.content)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("EstimateTokens() = %d, want between %d and %d", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestTokenBudget_Add(t *testing.T) {
	tb := NewTokenBudget(100)

	if !tb.CanFit(50) {
		t.Error("should fit 50 tokens in 100 budget")
	}

	tb.Add(60)

	if tb.CanFit(50) {
		t.Error("should not fit 50 more tokens (60 used of 100)")
	}

	if tb.CanFit(40) {
		// 60 + 40 = 100, which should be exactly at limit
	}

	if tb.Remaining() != 40 {
		t.Errorf("Remaining() = %d, want 40", tb.Remaining())
	}
}
