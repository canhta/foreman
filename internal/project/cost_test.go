package project

import (
	"testing"
)

func TestGlobalCostController_ReportAndCheck(t *testing.T) {
	ctrl := NewGlobalCostController(100.0, 2000.0)

	// Report costs from two projects
	ctrl.ReportCost("proj-1", 5.0)
	ctrl.ReportCost("proj-2", 3.0)

	total := ctrl.TotalToday()
	if total != 8.0 {
		t.Errorf("total = %f, want 8.0", total)
	}

	// Under budget
	if err := ctrl.CheckDailyBudget(); err != nil {
		t.Errorf("unexpected budget exceeded: %v", err)
	}

	// Push over budget
	ctrl.ReportCost("proj-1", 95.0)
	if err := ctrl.CheckDailyBudget(); err == nil {
		t.Error("expected budget exceeded error")
	}
}

func TestGlobalCostController_Monthly(t *testing.T) {
	ctrl := NewGlobalCostController(100.0, 200.0)

	ctrl.ReportCost("proj-1", 150.0)
	ctrl.ReportCost("proj-2", 60.0)

	total := ctrl.TotalThisMonth()
	if total != 210.0 {
		t.Errorf("monthly total = %f, want 210.0", total)
	}

	if err := ctrl.CheckMonthlyBudget(); err == nil {
		t.Error("expected monthly budget exceeded error")
	}
}

func TestGlobalCostController_ResetDaily(t *testing.T) {
	ctrl := NewGlobalCostController(100.0, 2000.0)
	ctrl.ReportCost("proj-1", 50.0)
	ctrl.ResetDaily()

	if total := ctrl.TotalToday(); total != 0.0 {
		t.Errorf("after reset, total = %f, want 0.0", total)
	}
}

func TestGlobalCostController_SeedFromDB(t *testing.T) {
	ctrl := NewGlobalCostController(100.0, 2000.0)
	ctrl.SeedFromDB("proj-1", 30.0, 300.0)

	if total := ctrl.TotalToday(); total != 30.0 {
		t.Errorf("daily total after seed = %f, want 30.0", total)
	}
	if total := ctrl.TotalThisMonth(); total != 300.0 {
		t.Errorf("monthly total after seed = %f, want 300.0", total)
	}
}
