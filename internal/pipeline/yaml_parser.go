package pipeline

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// PlannerResult is the parsed output from the planner LLM call.
type PlannerResult struct {
	Status           string           `yaml:"status"`
	Message          string           `yaml:"message"`
	CodebasePatterns CodebasePatterns `yaml:"codebase_patterns"`
	Tasks            []PlannedTask    `yaml:"tasks"`
}

// CodebasePatterns holds detected patterns from the codebase.
type CodebasePatterns struct {
	Language   string `yaml:"language"`
	Framework  string `yaml:"framework"`
	TestRunner string `yaml:"test_runner"`
	StyleNotes string `yaml:"style_notes"`
}

// PlannedTask represents a single task decomposed from a ticket.
type PlannedTask struct {
	Title               string   `yaml:"title"`
	Description         string   `yaml:"description"`
	AcceptanceCriteria  []string `yaml:"acceptance_criteria"`
	TestAssertions      []string `yaml:"test_assertions"`
	FilesToRead         []string `yaml:"files_to_read"`
	FilesToModify       []string `yaml:"files_to_modify"`
	EstimatedComplexity string   `yaml:"estimated_complexity"`
	DependsOn           []string `yaml:"depends_on"`
}

// normalizeNumericDepsOn resolves numeric index strings in depends_on (e.g. "0", "1")
// to the corresponding task title. The LLM occasionally uses 0-based indices instead
// of task titles; this normalizes them so TopologicalSort can match by title.
func normalizeNumericDepsOn(result *PlannerResult) {
	if result == nil {
		return
	}
	for i, task := range result.Tasks {
		normalized := make([]string, 0, len(task.DependsOn))
		for _, dep := range task.DependsOn {
			if idx, err := strconv.Atoi(dep); err == nil && idx >= 0 && idx < len(result.Tasks) {
				normalized = append(normalized, result.Tasks[idx].Title)
			} else {
				normalized = append(normalized, dep)
			}
		}
		result.Tasks[i].DependsOn = normalized
	}
}

// ParsePlannerOutput parses LLM planner output using a strict to permissive to partial fallback chain.
func ParsePlannerOutput(raw string) (*PlannerResult, error) {
	// Strategy 1: Strict YAML parse
	result, err := parseStrictYAML(raw)
	if err == nil && result.Status != "" {
		normalizeNumericDepsOn(result)
		return result, nil
	}

	// Strategy 2: Extract content from markdown code fences, then parse
	if fenced := extractFencedYAML(raw); fenced != "" {
		result, err = parseStrictYAML(fenced)
		if err == nil && result.Status != "" {
			normalizeNumericDepsOn(result)
			return result, nil
		}
		// Strategy 3: Look for status field inside fenced content
		if idx := strings.Index(fenced, "status:"); idx != -1 {
			result, err = parseStrictYAML(fenced[idx:])
			if err == nil && result.Status != "" {
				normalizeNumericDepsOn(result)
				return result, nil
			}
		}
	}

	// Strategy 3 (fallback): Look for status field in fence-stripped text
	cleaned := stripMarkdownFences(raw)
	if idx := strings.Index(cleaned, "status:"); idx != -1 {
		result, err = parseStrictYAML(cleaned[idx:])
		if err == nil && result.Status != "" {
			normalizeNumericDepsOn(result)
			return result, nil
		}
	}

	// Strategy 4: Extract special statuses from prose
	if strings.Contains(raw, "CLARIFICATION_NEEDED") {
		question := extractAfterKey(raw, "CLARIFICATION_NEEDED")
		return &PlannerResult{
			Status:  "CLARIFICATION_NEEDED",
			Message: question,
		}, nil
	}
	if strings.Contains(raw, "TICKET_TOO_LARGE") {
		message := extractAfterKey(raw, "TICKET_TOO_LARGE")
		return &PlannerResult{
			Status:  "TICKET_TOO_LARGE",
			Message: message,
		}, nil
	}

	return nil, fmt.Errorf("failed to parse planner output (all strategies failed), raw length: %d", len(raw))
}

func parseStrictYAML(raw string) (*PlannerResult, error) {
	var result PlannerResult
	decoder := yaml.NewDecoder(strings.NewReader(raw))
	decoder.KnownFields(false)
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// extractFencedYAML extracts the content inside the first ```yaml or ``` code fence.
func extractFencedYAML(raw string) string {
	// Find opening fence
	for _, prefix := range []string{"```yaml\n", "```yml\n", "```\n"} {
		start := strings.Index(raw, prefix)
		if start == -1 {
			continue
		}
		content := raw[start+len(prefix):]
		end := strings.Index(content, "```")
		if end == -1 {
			continue
		}
		return strings.TrimSpace(content[:end])
	}
	return ""
}

func extractAfterKey(raw, key string) string {
	idx := strings.Index(raw, key)
	if idx == -1 {
		return ""
	}
	after := raw[idx+len(key):]
	// Skip colon and whitespace
	after = strings.TrimLeft(after, ": \t")
	// Take until end of line or end of string
	if nlIdx := strings.Index(after, "\n"); nlIdx != -1 {
		return strings.TrimSpace(after[:nlIdx])
	}
	return strings.TrimSpace(after)
}
