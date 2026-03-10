package pipeline

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/canhta/foreman/internal/agent"
	"github.com/canhta/foreman/internal/models"
)

// AgentPlanner implements planning by delegating to an AgentRunner
// that can explore the codebase and return structured output.
//
// Note: AgentPlanner requires an AgentRunner that populates AgentResult.Structured
// (e.g., the claudecode runner which extracts JSON from the agent's structured output).
// With the builtin runner, Structured will be nil and planning will fail with
// "no structured output returned". Use claudecode or a compatible runner.
type AgentPlanner struct {
	runner agent.AgentRunner
	limits *models.LimitsConfig
}

// NewAgentPlanner creates a planner that delegates to an AgentRunner.
func NewAgentPlanner(runner agent.AgentRunner, limits *models.LimitsConfig) *AgentPlanner {
	return &AgentPlanner{runner: runner, limits: limits}
}

// Plan generates a task plan by having the agent explore the codebase.
func (ap *AgentPlanner) Plan(ctx context.Context, workDir string, ticket *models.Ticket) (*PlannerResult, error) {
	prompt := ap.buildPlanningPrompt(ticket)
	schema := ap.plannerOutputSchema()

	result, err := ap.runner.Run(ctx, agent.AgentRequest{
		Prompt:       prompt,
		WorkDir:      workDir,
		OutputSchema: schema,
		MaxTurns:     30,
		TimeoutSecs:  300,
	})
	if err != nil {
		return nil, fmt.Errorf("agent planner: %w", err)
	}

	// Parse structured output
	planResult, err := ap.parseStructured(result.Structured)
	if err != nil {
		return nil, fmt.Errorf("agent planner: parse structured output: %w", err)
	}

	if planResult.Status != "OK" {
		return planResult, nil
	}

	// Validate task count limit
	if ap.limits != nil && len(planResult.Tasks) > ap.limits.MaxTasksPerTicket {
		return nil, fmt.Errorf("agent plan validation failed: plan has %d tasks, exceeding limit of %d",
			len(planResult.Tasks), ap.limits.MaxTasksPerTicket)
	}

	// Normalize numeric depends_on entries (LLM may use 0-based index strings instead of titles)
	normalizeNumericDepsOn(planResult)

	// Topological sort (also validates dependency graph for cycles/unknown deps)
	sorted, err := TopologicalSort(planResult.Tasks)
	if err != nil {
		return nil, fmt.Errorf("agent plan sort: %w", err)
	}
	planResult.Tasks = sorted

	return planResult, nil
}

func (ap *AgentPlanner) buildPlanningPrompt(ticket *models.Ticket) string {
	prompt := fmt.Sprintf(`You are a software architect planning the implementation of a ticket.

## Ticket
**Title:** %s
**Description:** %s
`, ticket.Title, ticket.Description)

	if ticket.AcceptanceCriteria != "" {
		prompt += "\n**Acceptance Criteria:**\n" + ticket.AcceptanceCriteria + "\n"
	}

	prompt += `
## Instructions
1. Explore the codebase to understand the architecture, conventions, and relevant files.
2. Decompose this ticket into ordered, independent implementation tasks.
3. For each task, specify: title, description, acceptance_criteria, files_to_read, files_to_modify, estimated_complexity, depends_on.
4. Detect codebase patterns: language, framework, test_runner, style_notes.
5. Return your plan as structured output matching the schema.

Keep tasks small and focused. Each task should be independently implementable and testable.

Set status to exactly "OK" when you have produced a valid plan. Use "CLARIFICATION_NEEDED" only if the ticket is too ambiguous to plan, and "TICKET_TOO_LARGE" only if the scope is too broad to decompose into tasks.
`
	return prompt
}

func (ap *AgentPlanner) plannerOutputSchema() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"status": map[string]interface{}{
				"type": "string",
				"enum": []string{"OK", "CLARIFICATION_NEEDED", "TICKET_TOO_LARGE"},
			},
			"message": map[string]string{"type": "string"},
			"codebase_patterns": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"language":    map[string]string{"type": "string"},
					"framework":   map[string]string{"type": "string"},
					"test_runner": map[string]string{"type": "string"},
					"style_notes": map[string]string{"type": "string"},
				},
			},
			"tasks": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title":                map[string]string{"type": "string"},
						"description":          map[string]string{"type": "string"},
						"acceptance_criteria":  map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
						"test_assertions":      map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
						"files_to_read":        map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
						"files_to_modify":      map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
						"estimated_complexity": map[string]string{"type": "string"},
						"depends_on":           map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
					},
					"required": []string{"title", "description"},
				},
			},
		},
		"required": []string{"status", "tasks"},
	}
	data, _ := json.Marshal(schema)
	return data
}

func (ap *AgentPlanner) parseStructured(structured json.RawMessage) (*PlannerResult, error) {
	if len(structured) == 0 {
		return nil, fmt.Errorf("no structured output returned")
	}

	var result PlannerResult
	if err := json.Unmarshal(structured, &result); err != nil {
		return nil, fmt.Errorf("unmarshal plan: %w", err)
	}
	return &result, nil
}
