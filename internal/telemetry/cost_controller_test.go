package telemetry

import (
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCostController_CheckTicketBudget_WithinLimit(t *testing.T) {
	cc := NewCostController(models.CostConfig{
		MaxCostPerTicketUSD: 15.0,
		AlertThresholdPct:   80,
	})

	err := cc.CheckTicketBudget(5.0)
	assert.NoError(t, err)
}

func TestCostController_CheckTicketBudget_Exceeded(t *testing.T) {
	cc := NewCostController(models.CostConfig{
		MaxCostPerTicketUSD: 15.0,
		AlertThresholdPct:   80,
	})

	err := cc.CheckTicketBudget(16.0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "budget exceeded")
}

func TestCostController_CheckTicketBudget_AlertThreshold(t *testing.T) {
	cc := NewCostController(models.CostConfig{
		MaxCostPerTicketUSD: 10.0,
		AlertThresholdPct:   80,
	})

	alert := cc.ShouldAlert(8.5, 10.0) // 85% > 80%
	assert.True(t, alert)
}

func TestCostController_CheckTicketBudget_BelowAlert(t *testing.T) {
	cc := NewCostController(models.CostConfig{
		MaxCostPerTicketUSD: 10.0,
		AlertThresholdPct:   80,
	})

	alert := cc.ShouldAlert(7.0, 10.0) // 70% < 80%
	assert.False(t, alert)
}

func TestCostController_CheckDailyBudget(t *testing.T) {
	cc := NewCostController(models.CostConfig{
		MaxCostPerDayUSD: 150.0,
	})

	assert.NoError(t, cc.CheckDailyBudget(100.0))
	assert.Error(t, cc.CheckDailyBudget(160.0))
}

func TestCostController_CheckMonthlyBudget(t *testing.T) {
	cc := NewCostController(models.CostConfig{
		MaxCostPerMonthUSD: 3000.0,
	})

	assert.NoError(t, cc.CheckMonthlyBudget(2500.0))
	assert.Error(t, cc.CheckMonthlyBudget(3100.0))
}

func TestCostController_CalculateCost(t *testing.T) {
	cc := NewCostController(models.CostConfig{
		Pricing: map[string]models.PricingConfig{
			"anthropic:claude-sonnet-4-6": {Input: 3.0, Output: 15.0},
		},
	})

	cost := cc.CalculateCost("anthropic:claude-sonnet-4-6", 10000, 2000)
	// (10000/1M)*3.0 + (2000/1M)*15.0 = 0.03 + 0.03 = 0.06
	require.InDelta(t, 0.06, cost, 0.001)
}

func TestCostController_CalculateCost_UnknownModel(t *testing.T) {
	cc := NewCostController(models.CostConfig{})
	cost := cc.CalculateCost("unknown:model", 10000, 2000)
	// Unknown model should use fallback pricing
	assert.True(t, cost > 0)
}

func TestCostController_CheckTaskCallCap(t *testing.T) {
	cc := NewCostController(models.CostConfig{
		MaxLlmCallsPerTask: 8,
	})

	assert.NoError(t, cc.CheckTaskCallCap(7))
	assert.Error(t, cc.CheckTaskCallCap(8))
	assert.Error(t, cc.CheckTaskCallCap(10))
}

// TestCostController_ConfigurableFallbackPricing verifies BUG-M12:
// When a FallbackPricing is configured in CostConfig, unknown models must use
// those rates instead of the hardcoded $3/$15 values.
func TestCostController_ConfigurableFallbackPricing(t *testing.T) {
	customInput := 1.0
	customOutput := 5.0
	cc := NewCostController(models.CostConfig{
		FallbackPricing: &models.PricingConfig{Input: customInput, Output: customOutput},
	})

	// 1M input tokens at $1/M + 1M output tokens at $5/M = $6.00
	cost := cc.CalculateCost("some-unknown-model", 1_000_000, 1_000_000)
	assert.InDelta(t, 6.0, cost, 0.001, "custom fallback pricing should be used for unknown models")
}

// TestCostController_DefaultFallbackPricing verifies that the default hardcoded
// fallback pricing ($3 input / $15 output) is still used when no FallbackPricing
// is configured.
func TestCostController_DefaultFallbackPricing(t *testing.T) {
	cc := NewCostController(models.CostConfig{})

	// 1M input tokens at $3/M + 1M output tokens at $15/M = $18.00
	cost := cc.CalculateCost("unknown-model-default", 1_000_000, 1_000_000)
	assert.InDelta(t, 18.0, cost, 0.001, "default fallback pricing $3/$15 should be used when none configured")
}
