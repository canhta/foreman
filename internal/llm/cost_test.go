package llm

import (
	"testing"

	"github.com/canhta/foreman/internal/models"
)

func TestCalculateCost(t *testing.T) {
	pricing := map[string]models.PricingConfig{
		"anthropic:claude-sonnet-4-5-20250929": {Input: 3.00, Output: 15.00},
	}

	tests := []struct {
		name         string
		model        string
		tokensInput  int
		tokensOutput int
		wantCost     float64
	}{
		{
			name:         "basic cost calculation",
			model:        "anthropic:claude-sonnet-4-5-20250929",
			tokensInput:  1000,
			tokensOutput: 500,
			wantCost:     0.0105, // (1000/1M)*3.00 + (500/1M)*15.00
		},
		{
			name:         "unknown model uses zero",
			model:        "unknown:model",
			tokensInput:  1000,
			tokensOutput: 500,
			wantCost:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateCost(tt.model, tt.tokensInput, tt.tokensOutput, pricing)
			if got != tt.wantCost {
				t.Errorf("CalculateCost() = %v, want %v", got, tt.wantCost)
			}
		})
	}
}
