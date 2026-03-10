package project

import (
	"testing"

	"github.com/canhta/foreman/internal/models"
)

func TestMergeConfigs_ProjectOverridesGlobal(t *testing.T) {
	global := &models.Config{}
	global.LLM.Anthropic.APIKey = "global-key"
	global.LLM.DefaultProvider = "anthropic"
	global.Cost.MaxCostPerDayUSD = 150.0
	global.Cost.MaxCostPerMonthUSD = 3000.0

	proj := &ProjectConfig{}
	proj.Project.Name = "Test"
	proj.Cost.MaxCostPerTicketUSD = 10.0
	proj.Limits.MaxParallelTickets = 2

	merged := MergeConfigs(global, proj, "/tmp/project")

	// Project values should be set
	if merged.Cost.MaxCostPerTicketUSD != 10.0 {
		t.Errorf("ticket cost = %f, want 10.0", merged.Cost.MaxCostPerTicketUSD)
	}

	// Global LLM key should be used (project didn't set one)
	if merged.LLM.Anthropic.APIKey != "global-key" {
		t.Errorf("api key = %q, want global-key", merged.LLM.Anthropic.APIKey)
	}

	// Global daily/monthly limits should carry through
	if merged.Cost.MaxCostPerDayUSD != 150.0 {
		t.Errorf("daily cost = %f, want 150.0", merged.Cost.MaxCostPerDayUSD)
	}
}

func TestMergeConfigs_ProjectOverridesAPIKey(t *testing.T) {
	global := &models.Config{}
	global.LLM.Anthropic.APIKey = "global-key"

	proj := &ProjectConfig{}
	proj.LLM.Anthropic.APIKey = "project-key"

	merged := MergeConfigs(global, proj, "/tmp/project")

	if merged.LLM.Anthropic.APIKey != "project-key" {
		t.Errorf("api key = %q, want project-key", merged.LLM.Anthropic.APIKey)
	}
}
