package agent

import "sync"

type CostEntry struct {
	Model        string
	InputTokens  int
	OutputTokens int
	CachedTokens int
	CostUSD      float64
}

type CostSummary struct {
	ByModel           map[string]float64
	TotalCostUSD      float64
	TotalInputTokens  int
	TotalOutputTokens int
	TotalCachedTokens int
	LLMCalls          int
}

type CostTracker struct {
	entries []CostEntry
	budget  float64
	mu      sync.Mutex
}

func NewCostTracker() *CostTracker { return &CostTracker{} }

func (ct *CostTracker) SetBudget(usd float64) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.budget = usd
}

func (ct *CostTracker) Record(entry CostEntry) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.entries = append(ct.entries, entry)
}

func (ct *CostTracker) Summary() CostSummary {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	s := CostSummary{ByModel: make(map[string]float64)}
	for _, e := range ct.entries {
		s.TotalInputTokens += e.InputTokens
		s.TotalOutputTokens += e.OutputTokens
		s.TotalCachedTokens += e.CachedTokens
		s.TotalCostUSD += e.CostUSD
		s.LLMCalls++
		s.ByModel[e.Model] += e.CostUSD
	}
	return s
}

func (ct *CostTracker) BudgetExceeded() bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if ct.budget <= 0 {
		return false
	}
	var total float64
	for _, e := range ct.entries {
		total += e.CostUSD
	}
	return total >= ct.budget
}
