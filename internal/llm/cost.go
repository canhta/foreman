package llm

import "github.com/canhta/foreman/internal/models"

// CalculateCost returns the cost in USD for a given model and token counts.
// Pricing is per 1M tokens.
func CalculateCost(model string, tokensInput, tokensOutput int, pricing map[string]models.PricingConfig) float64 {
	p, ok := pricing[model]
	if !ok {
		return 0.0
	}
	inputCost := (float64(tokensInput) / 1_000_000) * p.Input
	outputCost := (float64(tokensOutput) / 1_000_000) * p.Output
	return inputCost + outputCost
}
