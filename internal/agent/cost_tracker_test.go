package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCostTracker_RecordAndTotal(t *testing.T) {
	ct := NewCostTracker()
	ct.Record(CostEntry{
		Model: "claude-sonnet-4-20250514", InputTokens: 1000, OutputTokens: 500, CachedTokens: 200, CostUSD: 0.005,
	})
	ct.Record(CostEntry{
		Model: "claude-sonnet-4-20250514", InputTokens: 2000, OutputTokens: 1000, CachedTokens: 500, CostUSD: 0.010,
	})
	summary := ct.Summary()
	assert.Equal(t, 3000, summary.TotalInputTokens)
	assert.Equal(t, 1500, summary.TotalOutputTokens)
	assert.Equal(t, 700, summary.TotalCachedTokens)
	assert.InDelta(t, 0.015, summary.TotalCostUSD, 0.001)
	assert.Equal(t, 2, summary.LLMCalls)
}

func TestCostTracker_BudgetExceeded(t *testing.T) {
	ct := NewCostTracker()
	ct.SetBudget(0.01)
	ct.Record(CostEntry{CostUSD: 0.008})
	assert.False(t, ct.BudgetExceeded())
	ct.Record(CostEntry{CostUSD: 0.005})
	assert.True(t, ct.BudgetExceeded())
}
