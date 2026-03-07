package telemetry

import (
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/canhta/foreman/internal/models"
)

// CostController enforces cost budgets at ticket, daily, and monthly levels.
type CostController struct {
	config models.CostConfig
}

// NewCostController creates a cost controller.
func NewCostController(config models.CostConfig) *CostController {
	return &CostController{config: config}
}

// CheckTicketBudget returns an error if the ticket cost exceeds the per-ticket budget.
func (c *CostController) CheckTicketBudget(currentCost float64) error {
	if c.config.MaxCostPerTicketUSD > 0 && currentCost > c.config.MaxCostPerTicketUSD {
		return fmt.Errorf("ticket budget exceeded: $%.2f > $%.2f limit", currentCost, c.config.MaxCostPerTicketUSD)
	}
	return nil
}

// CheckDailyBudget returns an error if the daily cost exceeds the per-day budget.
func (c *CostController) CheckDailyBudget(currentCost float64) error {
	if c.config.MaxCostPerDayUSD > 0 && currentCost > c.config.MaxCostPerDayUSD {
		return fmt.Errorf("daily budget exceeded: $%.2f > $%.2f limit", currentCost, c.config.MaxCostPerDayUSD)
	}
	return nil
}

// CheckMonthlyBudget returns an error if the monthly cost exceeds the per-month budget.
func (c *CostController) CheckMonthlyBudget(currentCost float64) error {
	if c.config.MaxCostPerMonthUSD > 0 && currentCost > c.config.MaxCostPerMonthUSD {
		return fmt.Errorf("monthly budget exceeded: $%.2f > $%.2f limit", currentCost, c.config.MaxCostPerMonthUSD)
	}
	return nil
}

// ShouldAlert returns true if the current cost exceeds the alert threshold percentage of the limit.
func (c *CostController) ShouldAlert(currentCost, limit float64) bool {
	if limit <= 0 || c.config.AlertThresholdPct <= 0 {
		return false
	}
	threshold := limit * float64(c.config.AlertThresholdPct) / 100.0
	return currentCost >= threshold
}

// CalculateCost computes the USD cost for a given model and token counts.
func (c *CostController) CalculateCost(model string, inputTokens, outputTokens int) float64 {
	pricing, ok := c.config.Pricing[model]
	if !ok {
		if c.config.FallbackPricing != nil {
			pricing = *c.config.FallbackPricing
		} else {
			// Default hardcoded fallback — warn prominently that costs may be inaccurate.
			log.Warn().Str("model", model).
				Msg("unknown model for cost calculation, using default fallback pricing ($3/$15); " +
					"configure [cost.fallback_pricing] in foreman.toml for accurate estimates")
			pricing = models.PricingConfig{Input: 3.0, Output: 15.0}
		}
	}
	return (float64(inputTokens)/1_000_000)*pricing.Input +
		(float64(outputTokens)/1_000_000)*pricing.Output
}

// CheckTaskCallCap returns an error if the task has reached the LLM call cap.
func (c *CostController) CheckTaskCallCap(currentCalls int) error {
	if c.config.MaxLlmCallsPerTask > 0 && currentCalls >= c.config.MaxLlmCallsPerTask {
		return fmt.Errorf("task LLM call cap reached: %d >= %d", currentCalls, c.config.MaxLlmCallsPerTask)
	}
	return nil
}
