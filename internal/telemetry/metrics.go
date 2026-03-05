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
	}

	reg.MustRegister(
		m.TicketsTotal, m.TicketsActive, m.TasksTotal,
		m.LlmCallsTotal, m.LlmTokensTotal, m.LlmDuration,
		m.CostUSDTotal, m.PipelineDuration, m.TestRunsTotal,
		m.RetriesTotal, m.RateLimitsTotal, m.TDDVerifyTotal,
		m.PartialPRsTotal, m.ClarificationsTotal, m.SecretsDetected,
		m.HookExecutions, m.SkillExecutions,
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
