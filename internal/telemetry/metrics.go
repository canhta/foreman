package telemetry

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all Prometheus metrics for Foreman.
type Metrics struct {
	TicketsTotal        *prometheus.CounterVec
	TicketsActive       prometheus.Gauge
	TasksTotal          *prometheus.CounterVec
	LlmCallsTotal       *prometheus.CounterVec
	LlmTokensTotal      *prometheus.CounterVec
	LlmDuration         *prometheus.HistogramVec
	CostUSDTotal        *prometheus.CounterVec
	PipelineDuration    prometheus.Histogram
	TestRunsTotal       *prometheus.CounterVec
	RetriesTotal        *prometheus.CounterVec
	RateLimitsTotal     *prometheus.CounterVec
	TDDVerifyTotal      *prometheus.CounterVec
	PartialPRsTotal     prometheus.Counter
	ClarificationsTotal prometheus.Counter
	SecretsDetected     prometheus.Counter
	HookExecutions      *prometheus.CounterVec
	SkillExecutions     *prometheus.CounterVec

	ClarificationTimeouts    prometheus.Counter
	FileReservationConflicts prometheus.Counter
	SearchBlockFuzzyMatches  prometheus.Counter
	SearchBlockMisses        prometheus.Counter
	ProviderOutages          *prometheus.CounterVec
	CrashRecoveries          prometheus.Counter

	DAGTasksCompleted prometheus.Counter
	DAGTasksFailed    prometheus.Counter
	DAGTasksSkipped   prometheus.Counter
	DAGDuration       prometheus.Histogram

	AnthropicCacheSavingsTotal prometheus.Counter

	TaskFailuresTotal    *prometheus.CounterVec // labels: error_type, runner
	RetryTriggeredTotal  *prometheus.CounterVec // labels: stage, error_type
	PlanConfidenceScore  prometheus.Histogram
	ContextCacheHitRatio prometheus.Gauge
	MCPToolCallsTotal    *prometheus.CounterVec // labels: server, tool, status
}

// NewMetrics creates and registers all Prometheus metrics with the given registerer.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		TicketsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_tickets_total",
			Help: "Total tickets by status",
		}, []string{"status"}),
		TicketsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "foreman_tickets_active",
			Help: "Currently active tickets",
		}),
		TasksTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_tasks_total",
			Help: "Total tasks by status",
		}, []string{"status"}),
		LlmCallsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_llm_calls_total",
			Help: "Total LLM calls by role, model, and status",
		}, []string{"role", "model", "status"}),
		LlmTokensTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_llm_tokens_total",
			Help: "Total LLM tokens by direction and model",
		}, []string{"direction", "model"}),
		LlmDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "foreman_llm_duration_seconds",
			Help:    "LLM call duration by role and model",
			Buckets: prometheus.DefBuckets,
		}, []string{"role", "model"}),
		CostUSDTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_cost_usd_total",
			Help: "Total cost in USD by model",
		}, []string{"model"}),
		PipelineDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "foreman_pipeline_duration_seconds",
			Help:    "Pipeline duration in seconds",
			Buckets: []float64{60, 120, 300, 600, 1200, 1800, 3600, 7200},
		}),
		TestRunsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_test_runs_total",
			Help: "Total test runs by result",
		}, []string{"result"}),
		RetriesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_retries_total",
			Help: "Total retries by role",
		}, []string{"role"}),
		RateLimitsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_rate_limits_total",
			Help: "Total rate limits by provider",
		}, []string{"provider"}),
		TDDVerifyTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_tdd_verify_total",
			Help: "TDD verification results",
		}, []string{"result"}),
		PartialPRsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "foreman_partial_prs_total",
			Help: "Total partial PRs created",
		}),
		ClarificationsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "foreman_clarifications_total",
			Help: "Total clarification requests",
		}),
		SecretsDetected: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "foreman_secrets_detected_total",
			Help: "Total secrets detected and excluded",
		}),
		HookExecutions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_hook_executions_total",
			Help: "Hook executions by hook point",
		}, []string{"hook"}),
		SkillExecutions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_skill_executions_total",
			Help: "Skill executions by skill and status",
		}, []string{"skill", "status"}),
		ClarificationTimeouts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "foreman_clarification_timeouts_total",
			Help: "Total clarification timeouts",
		}),
		FileReservationConflicts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "foreman_file_reservation_conflicts_total",
			Help: "Total file reservation conflicts",
		}),
		SearchBlockFuzzyMatches: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "foreman_search_block_fuzzy_matches_total",
			Help: "Total fuzzy matches in SEARCH blocks",
		}),
		SearchBlockMisses: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "foreman_search_block_misses_total",
			Help: "Total SEARCH block misses",
		}),
		ProviderOutages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_provider_outages_total",
			Help: "Total provider outages by provider",
		}, []string{"provider"}),
		CrashRecoveries: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "foreman_crash_recoveries_total",
			Help: "Total crash recoveries",
		}),
		DAGTasksCompleted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "foreman_dag_tasks_completed_total",
			Help: "Total DAG tasks completed successfully",
		}),
		DAGTasksFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "foreman_dag_tasks_failed_total",
			Help: "Total DAG tasks failed",
		}),
		DAGTasksSkipped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "foreman_dag_tasks_skipped_total",
			Help: "Total DAG tasks skipped due to dependency failure",
		}),
		DAGDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "foreman_dag_execution_duration_seconds",
			Help:    "DAG execution duration in seconds",
			Buckets: []float64{10, 30, 60, 120, 300, 600, 1200, 3600},
		}),
		AnthropicCacheSavingsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "foreman_anthropic_cache_savings_tokens_total",
			Help: "Total tokens served from Anthropic prompt cache (cache_read_input_tokens)",
		}),
		TaskFailuresTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_task_failures_total",
			Help: "Total number of task failures by error type and runner.",
		}, []string{"error_type", "runner"}),
		RetryTriggeredTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_retry_triggered_total",
			Help: "Total number of retries triggered by stage and error type.",
		}, []string{"stage", "error_type"}),
		PlanConfidenceScore: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "foreman_plan_confidence_score",
			Help:    "Distribution of plan confidence scores.",
			Buckets: prometheus.LinearBuckets(0.1, 0.1, 10),
		}),
		ContextCacheHitRatio: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "foreman_context_cache_hit_ratio",
			Help: "Ratio of context cache hits to total context lookups.",
		}),
		MCPToolCallsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "foreman_mcp_tool_calls_total",
			Help: "Total number of MCP tool calls by server, tool name, and status.",
		}, []string{"server", "tool", "status"}),
	}

	reg.MustRegister(
		m.TicketsTotal, m.TicketsActive, m.TasksTotal,
		m.LlmCallsTotal, m.LlmTokensTotal, m.LlmDuration,
		m.CostUSDTotal, m.PipelineDuration, m.TestRunsTotal,
		m.RetriesTotal, m.RateLimitsTotal, m.TDDVerifyTotal,
		m.PartialPRsTotal, m.ClarificationsTotal, m.SecretsDetected,
		m.HookExecutions, m.SkillExecutions,
		m.ClarificationTimeouts, m.FileReservationConflicts,
		m.SearchBlockFuzzyMatches, m.SearchBlockMisses,
		m.ProviderOutages, m.CrashRecoveries,
		m.DAGTasksCompleted, m.DAGTasksFailed, m.DAGTasksSkipped, m.DAGDuration,
		m.AnthropicCacheSavingsTotal,
		m.TaskFailuresTotal, m.RetryTriggeredTotal, m.PlanConfidenceScore,
		m.ContextCacheHitRatio, m.MCPToolCallsTotal,
	)

	return m
}

// RecordLlmCall records a single LLM call with token counts, cost, and duration.
func (m *Metrics) RecordLlmCall(role, model, status string, tokensIn, tokensOut int, costUSD float64, durationMs int64) {
	m.LlmCallsTotal.WithLabelValues(role, model, status).Inc()
	m.LlmTokensTotal.WithLabelValues("input", model).Add(float64(tokensIn))
	m.LlmTokensTotal.WithLabelValues("output", model).Add(float64(tokensOut))
	m.CostUSDTotal.WithLabelValues(model).Add(costUSD)
	m.LlmDuration.WithLabelValues(role, model).Observe(float64(durationMs) / float64(time.Second/time.Millisecond))
}

// RecordCacheSavings records Anthropic prompt cache read tokens when cache hits occur.
func (m *Metrics) RecordCacheSavings(cacheReadTokens int) {
	if cacheReadTokens > 0 {
		m.AnthropicCacheSavingsTotal.Add(float64(cacheReadTokens))
	}
}

// RecordTicket increments the tickets counter for the given status.
func (m *Metrics) RecordTicket(status string) {
	m.TicketsTotal.WithLabelValues(status).Inc()
}

// RecordTask increments the tasks counter for the given status.
func (m *Metrics) RecordTask(status string) {
	m.TasksTotal.WithLabelValues(status).Inc()
}

// RecordTestRun increments the test runs counter for the given result.
func (m *Metrics) RecordTestRun(result string) {
	m.TestRunsTotal.WithLabelValues(result).Inc()
}

// RecordTDDVerify increments the TDD verification counter for the given result.
func (m *Metrics) RecordTDDVerify(result string) {
	m.TDDVerifyTotal.WithLabelValues(result).Inc()
}

// RecordRetry increments the retries counter for the given role.
func (m *Metrics) RecordRetry(role string) {
	m.RetriesTotal.WithLabelValues(role).Inc()
}

// RecordRateLimit increments the rate limits counter for the given provider.
func (m *Metrics) RecordRateLimit(provider string) {
	m.RateLimitsTotal.WithLabelValues(provider).Inc()
}

// RecordDAGExecution records the outcome of a DAG execution run.
func (m *Metrics) RecordDAGExecution(completed, failed, skipped int, durationMs int64) {
	m.DAGTasksCompleted.Add(float64(completed))
	m.DAGTasksFailed.Add(float64(failed))
	m.DAGTasksSkipped.Add(float64(skipped))
	m.DAGDuration.Observe(float64(durationMs) / float64(time.Second/time.Millisecond))
}
