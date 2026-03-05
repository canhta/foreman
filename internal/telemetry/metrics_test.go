package telemetry

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	if m == nil {
		t.Fatal("expected non-nil Metrics")
	}

	// Verify counters are registered by gathering
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	if len(families) == 0 {
		t.Fatal("expected registered metrics, got none")
	}
}

func TestMetricsRecordLlmCall(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.RecordLlmCall("implementer", "anthropic:claude-sonnet-4-5-20250929", "success", 1000, 500, 0.015, 2500)

	families, _ := reg.Gather()
	found := false
	for _, f := range families {
		if f.GetName() == "foreman_llm_calls_total" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected foreman_llm_calls_total metric")
	}
}

func TestMetricsRecordTicket(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.RecordTicket("completed")
	m.RecordTicket("failed")

	families, _ := reg.Gather()
	found := false
	for _, f := range families {
		if f.GetName() == "foreman_tickets_total" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected foreman_tickets_total metric")
	}
}

func TestMetrics_AllCountersRegistered(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Prime the ProviderOutages CounterVec so it appears in Gather output.
	m.ProviderOutages.WithLabelValues("test").Inc()

	families, _ := reg.Gather()
	names := make(map[string]bool)
	for _, f := range families {
		names[f.GetName()] = true
	}

	required := []string{
		"foreman_clarification_timeouts_total",
		"foreman_file_reservation_conflicts_total",
		"foreman_search_block_fuzzy_matches_total",
		"foreman_search_block_misses_total",
		"foreman_provider_outages_total",
		"foreman_crash_recoveries_total",
	}
	for _, name := range required {
		if !names[name] {
			t.Errorf("missing metric: %s", name)
		}
	}
}
