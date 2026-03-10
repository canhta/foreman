package project

import (
	"fmt"
	"sync"
)

// GlobalCostController aggregates costs across all projects.
// Workers push costs to this controller via ReportCost.
type GlobalCostController struct {
	maxDailyUSD   float64
	maxMonthlyUSD float64

	dailyCosts   map[string]float64 // project ID → cost today
	monthlyCosts map[string]float64 // project ID → cost this month
	mu           sync.RWMutex
}

// NewGlobalCostController creates a GlobalCostController with the given daily and monthly limits.
func NewGlobalCostController(maxDailyUSD, maxMonthlyUSD float64) *GlobalCostController {
	return &GlobalCostController{
		maxDailyUSD:   maxDailyUSD,
		maxMonthlyUSD: maxMonthlyUSD,
		dailyCosts:    make(map[string]float64),
		monthlyCosts:  make(map[string]float64),
	}
}

// ReportCost adds a cost amount for a project (called by ProjectWorker after each LLM call).
func (g *GlobalCostController) ReportCost(projectID string, amount float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dailyCosts[projectID] += amount
	g.monthlyCosts[projectID] += amount
}

// TotalToday returns total cost across all projects today.
func (g *GlobalCostController) TotalToday() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var total float64
	for _, c := range g.dailyCosts {
		total += c
	}
	return total
}

// TotalThisMonth returns total cost across all projects this month.
func (g *GlobalCostController) TotalThisMonth() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var total float64
	for _, c := range g.monthlyCosts {
		total += c
	}
	return total
}

// CheckDailyBudget returns an error if the daily budget is exceeded.
func (g *GlobalCostController) CheckDailyBudget() error {
	total := g.TotalToday()
	if total > g.maxDailyUSD {
		return fmt.Errorf("daily cost budget exceeded: $%.2f / $%.2f", total, g.maxDailyUSD)
	}
	return nil
}

// CheckMonthlyBudget returns an error if the monthly budget is exceeded.
func (g *GlobalCostController) CheckMonthlyBudget() error {
	total := g.TotalThisMonth()
	if total > g.maxMonthlyUSD {
		return fmt.Errorf("monthly cost budget exceeded: $%.2f / $%.2f", total, g.maxMonthlyUSD)
	}
	return nil
}

// ResetDaily clears daily costs (called at midnight by scheduler).
func (g *GlobalCostController) ResetDaily() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dailyCosts = make(map[string]float64)
}

// SeedFromDB initializes costs from project databases on startup.
func (g *GlobalCostController) SeedFromDB(projectID string, dailyCost, monthlyCost float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dailyCosts[projectID] = dailyCost
	g.monthlyCosts[projectID] = monthlyCost
}
