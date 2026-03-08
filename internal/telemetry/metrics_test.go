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

	m.RecordLlmCall("implementer", "anthropic:claude-sonnet-4-6", "success", 1000, 500, 0.015, 2500)

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

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}
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

func TestMetrics_RecordDAGExecution(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.RecordDAGExecution(5, 2, 3, 45000)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	names := make(map[string]bool)
	for _, f := range families {
		names[f.GetName()] = true
	}

	dagMetrics := []string{
		"foreman_dag_tasks_completed_total",
		"foreman_dag_tasks_failed_total",
		"foreman_dag_tasks_skipped_total",
		"foreman_dag_execution_duration_seconds",
	}
	for _, name := range dagMetrics {
		if !names[name] {
			t.Errorf("missing DAG metric: %s", name)
		}
	}
}

func TestMetrics_NewMetrics_RegistersAllCounters(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	if m.TaskFailuresTotal == nil {
		t.Error("expected non-nil TaskFailuresTotal")
	}
	if m.RetryTriggeredTotal == nil {
		t.Error("expected non-nil RetryTriggeredTotal")
	}
	if m.PlanConfidenceScore == nil {
		t.Error("expected non-nil PlanConfidenceScore")
	}
	if m.ContextCacheHitRatio == nil {
		t.Error("expected non-nil ContextCacheHitRatio")
	}
	if m.MCPToolCallsTotal == nil {
		t.Error("expected non-nil MCPToolCallsTotal")
	}
}

func TestMetrics_TaskFailuresTotal_CanBeIncremented(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.TaskFailuresTotal.WithLabelValues("compile_error", "builtin").Inc()

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}

	var found bool
	for _, f := range families {
		if f.GetName() == "foreman_task_failures_total" {
			found = true
			mets := f.GetMetric()
			if len(mets) == 0 {
				t.Fatal("expected at least one metric sample")
			}
			val := mets[0].GetCounter().GetValue()
			if val != 1 {
				t.Errorf("expected task_failures_total counter = 1, got %v", val)
			}
		}
	}
	if !found {
		t.Fatal("metric foreman_task_failures_total not found")
	}
}

func TestMetrics_RecordCacheSavings(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Zero read tokens should not update the counter (no-op).
	m.RecordCacheSavings(0)

	// Positive read tokens should increment the counter.
	m.RecordCacheSavings(200)
	m.RecordCacheSavings(50)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	var found bool
	for _, f := range families {
		if f.GetName() == "foreman_anthropic_cache_savings_tokens_total" {
			found = true
			mets := f.GetMetric()
			if len(mets) == 0 {
				t.Fatal("expected at least one metric sample")
			}
			val := mets[0].GetCounter().GetValue()
			if val != 250 {
				t.Errorf("expected cache savings counter = 250, got %v", val)
			}
		}
	}
	if !found {
		t.Fatal("metric foreman_anthropic_cache_savings_tokens_total not found")
	}
}
