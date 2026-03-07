// internal/skills/pipeline_context_test.go
package skills

import (
	"context"
	"fmt"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/canhta/foreman/internal/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Minimal mock implementations of the new interfaces ---

type mockHandoffAccessor struct {
	setHandoffCalls []mockSetHandoffCall
	handoffs        []models.HandoffRecord
	err             error
}

type mockSetHandoffCall struct {
	ticketID string
	fromRole string
	toRole   string
	key      string
	value    string
}

func (m *mockHandoffAccessor) SetHandoff(_ context.Context, h *models.HandoffRecord) error {
	m.setHandoffCalls = append(m.setHandoffCalls, mockSetHandoffCall{
		ticketID: h.TicketID,
		fromRole: h.FromRole,
		toRole:   h.ToRole,
		key:      h.Key,
		value:    h.Value,
	})
	return m.err
}

func (m *mockHandoffAccessor) GetHandoffs(_ context.Context, ticketID, forRole string) ([]models.HandoffRecord, error) {
	return m.handoffs, m.err
}

type mockProgressAccessor struct {
	saved []*models.ProgressPattern
	err   error
}

func (m *mockProgressAccessor) SaveProgressPattern(_ context.Context, p *models.ProgressPattern) error {
	m.saved = append(m.saved, p)
	return m.err
}

type mockSkillEventEmitter struct {
	emitted []mockEmittedEvent
}

type mockEmittedEvent struct {
	ticketID  string
	taskID    string
	eventType string
	severity  string
	message   string
}

func (m *mockSkillEventEmitter) Emit(_ context.Context, ticketID, taskID, eventType, severity, message string, metadata map[string]string) {
	m.emitted = append(m.emitted, mockEmittedEvent{
		ticketID:  ticketID,
		taskID:    taskID,
		eventType: eventType,
		severity:  severity,
		message:   message,
	})
}

// --- Tests ---

// TestSkillContext_CarriesPipelineCtx verifies that SkillContext can hold a PipelineContext.
func TestSkillContext_CarriesPipelineCtx(t *testing.T) {
	pCtx := &telemetry.PipelineContext{
		TraceID:  "trace-abc",
		TicketID: "ticket-1",
		TaskID:   "task-1",
		Stage:    "post_merge",
		Attempt:  1,
	}

	sCtx := NewSkillContext()
	sCtx.PipelineCtx = pCtx

	require.NotNil(t, sCtx.PipelineCtx)
	assert.Equal(t, "trace-abc", sCtx.PipelineCtx.TraceID)
	assert.Equal(t, "ticket-1", sCtx.PipelineCtx.TicketID)
	assert.Equal(t, "task-1", sCtx.PipelineCtx.TaskID)
	assert.Equal(t, "post_merge", sCtx.PipelineCtx.Stage)
	assert.Equal(t, 1, sCtx.PipelineCtx.Attempt)
}

// TestSkillContext_HandoffAndProgressFields verifies all new fields are assignable.
func TestSkillContext_HandoffAndProgressFields(t *testing.T) {
	sCtx := NewSkillContext()
	sCtx.HandoffDB = &mockHandoffAccessor{}
	sCtx.ProgressDB = &mockProgressAccessor{}
	sCtx.EventEmitter = &mockSkillEventEmitter{}

	assert.NotNil(t, sCtx.HandoffDB)
	assert.NotNil(t, sCtx.ProgressDB)
	assert.NotNil(t, sCtx.EventEmitter)
}

// TestExecuteAgentSDK_InjectsHandoffDataIntoSystemPrompt verifies that when SkillContext.HandoffDB
// is set and contains handoffs for the ticket, the handoff data is injected into the agent
// system prompt.
func TestExecuteAgentSDK_InjectsHandoffDataIntoSystemPrompt(t *testing.T) {
	mockAgent := &mockAgentRunner{output: "ok"}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mockAgent)

	handoffs := []models.HandoffRecord{
		{TicketID: "ticket-1", FromRole: "planner", ToRole: "implementer", Key: "approach", Value: "use TDD"},
		{TicketID: "ticket-1", FromRole: "planner", ToRole: "implementer", Key: "test_framework", Value: "go test"},
	}
	handoffDB := &mockHandoffAccessor{handoffs: handoffs}

	sCtx := NewSkillContext()
	sCtx.PipelineCtx = &telemetry.PipelineContext{
		TicketID: "ticket-1",
		Stage:    "post_lint",
	}
	sCtx.HandoffDB = handoffDB

	step := SkillStep{
		ID:      "audit",
		Type:    "agentsdk",
		Content: "Review the code",
	}

	_, err := e.executeStep(context.Background(), step, sCtx)
	require.NoError(t, err)

	// System prompt must contain handoff data
	assert.Contains(t, mockAgent.lastReq.SystemPrompt, "approach")
	assert.Contains(t, mockAgent.lastReq.SystemPrompt, "use TDD")
	assert.Contains(t, mockAgent.lastReq.SystemPrompt, "test_framework")
	assert.Contains(t, mockAgent.lastReq.SystemPrompt, "go test")
}

// TestExecuteAgentSDK_NoHandoffInjectionWithoutDB verifies that agentsdk steps behave
// normally when no HandoffDB is set (backward compatibility).
func TestExecuteAgentSDK_NoHandoffInjectionWithoutDB(t *testing.T) {
	mockAgent := &mockAgentRunner{output: "ok"}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mockAgent)

	sCtx := NewSkillContext()
	// No HandoffDB or PipelineCtx set

	step := SkillStep{
		ID:      "audit",
		Type:    "agentsdk",
		Content: "Review the code",
	}

	result, err := e.executeStep(context.Background(), step, sCtx)
	require.NoError(t, err)
	// Step output should be the mock's output
	assert.Equal(t, "ok", result.Output)
	// Prompt should be unchanged (no handoff annotation injected)
	assert.Equal(t, "Review the code", mockAgent.lastReq.Prompt)
	// System prompt should be empty (no AGENTS.md in temp dir, no handoff injection)
	assert.NotContains(t, mockAgent.lastReq.SystemPrompt, "Available Handoffs")
}

// TestExecuteAgentSDK_SavesProgressPattern verifies that when SkillContext.ProgressDB is set,
// the agent step output is saved as a progress pattern.
func TestExecuteAgentSDK_SavesProgressPattern(t *testing.T) {
	agentOutput := "Pattern: always use context.WithTimeout for LLM calls"
	mockAgent := &mockAgentRunner{output: agentOutput}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mockAgent)

	progressDB := &mockProgressAccessor{}

	sCtx := NewSkillContext()
	sCtx.PipelineCtx = &telemetry.PipelineContext{
		TicketID: "ticket-99",
		Stage:    "post_merge",
	}
	sCtx.ProgressDB = progressDB

	step := SkillStep{
		ID:      "analyze",
		Type:    "agentsdk",
		Content: "Analyze patterns",
	}

	_, err := e.executeStep(context.Background(), step, sCtx)
	require.NoError(t, err)

	// Progress pattern should be saved
	require.Len(t, progressDB.saved, 1)
	assert.Equal(t, "ticket-99", progressDB.saved[0].TicketID)
	assert.Equal(t, agentOutput, progressDB.saved[0].PatternValue)
}

// TestExecuteAgentSDK_NoProgressSaveWithoutDB verifies backward compatibility:
// no progress saved when ProgressDB is nil.
func TestExecuteAgentSDK_NoProgressSaveWithoutDB(t *testing.T) {
	mockAgent := &mockAgentRunner{output: "some output"}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mockAgent)

	sCtx := NewSkillContext()
	// No ProgressDB set

	step := SkillStep{
		ID:      "analyze",
		Type:    "agentsdk",
		Content: "Analyze",
	}

	result, err := e.executeStep(context.Background(), step, sCtx)
	require.NoError(t, err)
	assert.Equal(t, "some output", result.Output)
}

// TestExecuteAgentSDK_HandoffGetErrorDoesNotBlockExecution verifies that a failure
// in GetHandoffs is logged but does not fail the step.
func TestExecuteAgentSDK_HandoffGetErrorDoesNotBlockExecution(t *testing.T) {
	mockAgent := &mockAgentRunner{output: "ok"}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mockAgent)

	handoffDB := &mockHandoffAccessor{err: fmt.Errorf("DB connection error")}

	sCtx := NewSkillContext()
	sCtx.PipelineCtx = &telemetry.PipelineContext{
		TicketID: "ticket-1",
	}
	sCtx.HandoffDB = handoffDB

	step := SkillStep{
		ID:      "audit",
		Type:    "agentsdk",
		Content: "Review",
	}

	// Should not fail even if HandoffDB returns error
	_, err := e.executeStep(context.Background(), step, sCtx)
	require.NoError(t, err)
}

// TestExecuteAgentSDK_EmitsEventAfterStep verifies that when EventEmitter and PipelineCtx
// are set, a structured event is emitted after agentsdk step completion.
func TestExecuteAgentSDK_EmitsEventAfterStep(t *testing.T) {
	mockAgent := &mockAgentRunner{output: "agent output"}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mockAgent)

	emitter := &mockSkillEventEmitter{}

	sCtx := NewSkillContext()
	sCtx.PipelineCtx = &telemetry.PipelineContext{
		TicketID: "ticket-42",
		TaskID:   "task-7",
		Stage:    "post_lint",
	}
	sCtx.EventEmitter = emitter

	step := SkillStep{
		ID:      "my-step",
		Type:    "agentsdk",
		Content: "Do something",
	}

	_, err := e.executeStep(context.Background(), step, sCtx)
	require.NoError(t, err)

	// EventEmitter.Emit must be called once for the agentsdk step
	require.Len(t, emitter.emitted, 1)
	assert.Equal(t, "ticket-42", emitter.emitted[0].ticketID)
	assert.Equal(t, "task-7", emitter.emitted[0].taskID)
	assert.Equal(t, "skill_step_completed", emitter.emitted[0].eventType)
	assert.Equal(t, "info", emitter.emitted[0].severity)
	assert.Contains(t, emitter.emitted[0].message, "my-step")
}

// TestExecuteLLMCall_EmitsEventAfterStep verifies that when EventEmitter and PipelineCtx
// are set, a structured event is emitted after llm_call step completion.
func TestExecuteLLMCall_EmitsEventAfterStep(t *testing.T) {
	e := NewEngine(&mockLLMProvider{response: "llm result"}, nil, t.TempDir(), "main")

	emitter := &mockSkillEventEmitter{}

	sCtx := NewSkillContext()
	sCtx.PipelineCtx = &telemetry.PipelineContext{
		TicketID: "ticket-5",
		TaskID:   "task-2",
		Stage:    "pre_pr",
	}
	sCtx.EventEmitter = emitter

	step := SkillStep{
		ID:      "llm-step",
		Type:    "llm_call",
		Content: "Generate a summary",
	}

	_, err := e.executeStep(context.Background(), step, sCtx)
	require.NoError(t, err)

	// EventEmitter.Emit must be called once for the llm_call step
	require.Len(t, emitter.emitted, 1)
	assert.Equal(t, "ticket-5", emitter.emitted[0].ticketID)
	assert.Equal(t, "task-2", emitter.emitted[0].taskID)
	assert.Equal(t, "skill_llm_call_completed", emitter.emitted[0].eventType)
	assert.Equal(t, "info", emitter.emitted[0].severity)
	assert.Contains(t, emitter.emitted[0].message, "llm-step")
}

// TestExecuteAgentSDK_NoEmitWithoutEmitter verifies backward compatibility:
// no panic when EventEmitter is nil.
func TestExecuteAgentSDK_NoEmitWithoutEmitter(t *testing.T) {
	mockAgent := &mockAgentRunner{output: "ok"}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mockAgent)

	sCtx := NewSkillContext()
	// No EventEmitter set

	step := SkillStep{
		ID:      "step1",
		Type:    "agentsdk",
		Content: "Do work",
	}

	result, err := e.executeStep(context.Background(), step, sCtx)
	require.NoError(t, err)
	assert.Equal(t, "ok", result.Output)
}

// TestSubSkill_PropagatesPipelineCtx verifies that subskill execution
// propagates PipelineCtx, HandoffDB, ProgressDB, and EventEmitter from parent context.
func TestSubSkill_PropagatesPipelineCtx(t *testing.T) {
	mockAgent := &mockAgentRunner{output: "child result"}
	e := NewEngine(nil, nil, t.TempDir(), "main")
	e.SetAgentRunner(mockAgent)

	handoffDB := &mockHandoffAccessor{
		handoffs: []models.HandoffRecord{
			{TicketID: "t1", Key: "key1", Value: "val1"},
		},
	}
	progressDB := &mockProgressAccessor{}
	eventEmitter := &mockSkillEventEmitter{}
	pCtx := &telemetry.PipelineContext{
		TicketID: "t1",
		Stage:    "post_lint",
	}

	sub := &Skill{
		ID:      "child",
		Trigger: "post_lint",
		Steps:   []SkillStep{{ID: "s1", Type: "agentsdk", Content: "child task"}},
	}
	parent := &Skill{
		ID:      "parent",
		Trigger: "post_lint",
		Steps:   []SkillStep{{ID: "call-child", Type: "subskill", SkillRef: "child"}},
	}
	e.RegisterSkills([]*Skill{sub, parent})

	sCtx := NewSkillContext()
	sCtx.PipelineCtx = pCtx
	sCtx.HandoffDB = handoffDB
	sCtx.ProgressDB = progressDB
	sCtx.EventEmitter = eventEmitter

	err := e.Execute(context.Background(), parent, sCtx)
	require.NoError(t, err)

	// The subskill ran an agentsdk step — system prompt must contain handoff data
	assert.Contains(t, mockAgent.lastReq.SystemPrompt, "key1")
	assert.Contains(t, mockAgent.lastReq.SystemPrompt, "val1")

	// Progress was saved (from sub-skill's agentsdk step)
	require.Len(t, progressDB.saved, 1)
	assert.Equal(t, "t1", progressDB.saved[0].TicketID)
}
