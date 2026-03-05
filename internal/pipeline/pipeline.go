// internal/pipeline/pipeline.go
package pipeline

import (
	"fmt"

	"github.com/canhta/foreman/internal/models"
)

// minDescriptionLength is the minimum character count for a ticket description
// to be considered adequately detailed without acceptance criteria.
const minDescriptionLength = 50

// PipelineConfig holds pipeline-specific configuration.
type PipelineConfig struct {
	MaxImplementationRetries int
	MaxSpecReviewCycles      int
	MaxQualityReviewCycles   int
	MaxLlmCallsPerTask       int
	EnableTDDVerification    bool
	EnableClarification      bool
	EnablePartialPR          bool
}

// Pipeline orchestrates the execution of a ticket through the full pipeline.
type Pipeline struct {
	config PipelineConfig
}

// NewPipeline creates a new pipeline orchestrator.
func NewPipeline(config PipelineConfig) *Pipeline {
	return &Pipeline{config: config}
}

// CheckTicketClarity determines if a ticket has enough detail to plan.
func (p *Pipeline) CheckTicketClarity(ticket *models.Ticket) (bool, error) {
	if !p.config.EnableClarification {
		return true, nil
	}

	// Heuristic checks (no LLM needed)
	if len(ticket.Description) < minDescriptionLength && ticket.AcceptanceCriteria == "" {
		return false, nil
	}

	return true, nil
}

// TopologicalSort orders tasks by their dependency graph.
func TopologicalSort(tasks []PlannedTask) ([]PlannedTask, error) {
	// Build index by title
	taskMap := map[string]*PlannedTask{}
	for i := range tasks {
		taskMap[tasks[i].Title] = &tasks[i]
	}

	// Validate all dependency references exist
	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			if _, ok := taskMap[dep]; !ok {
				return nil, fmt.Errorf("task %q depends on unknown task %q", t.Title, dep)
			}
		}
	}

	// Kahn's algorithm
	inDegree := map[string]int{}
	graph := map[string][]string{} // task → dependents

	for _, t := range tasks {
		if _, ok := inDegree[t.Title]; !ok {
			inDegree[t.Title] = 0
		}
		for _, dep := range t.DependsOn {
			graph[dep] = append(graph[dep], t.Title)
			inDegree[t.Title]++
		}
	}

	// Find all tasks with no dependencies
	var queue []string
	for _, t := range tasks {
		if inDegree[t.Title] == 0 {
			queue = append(queue, t.Title)
		}
	}

	var sorted []PlannedTask
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		sorted = append(sorted, *taskMap[current])

		for _, dependent := range graph[current] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(sorted) != len(tasks) {
		return nil, fmt.Errorf("task dependencies contain a cycle")
	}

	return sorted, nil
}
