package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/canhta/foreman/internal/models"
)

// complexityTier defines token and call budgets for a single task complexity level.
type complexityTier struct {
	llmCalls        int
	avgInputTokens  int
	avgOutputTokens int
}

// complexityTiers defines token and call budgets for each task complexity tier.
// These are calibrated estimates based on typical Foreman pipeline usage:
//   - simple:  2 LLM calls, ~20k input tokens, ~4k output tokens
//   - medium:  4 LLM calls, ~40k input tokens, ~8k output tokens
//   - complex: 6 LLM calls, ~60k input tokens, ~12k output tokens
var complexityTiers = map[string]complexityTier{
	"simple":  {llmCalls: 2, avgInputTokens: 20000, avgOutputTokens: 4000},
	"medium":  {llmCalls: 4, avgInputTokens: 40000, avgOutputTokens: 8000},
	"complex": {llmCalls: 6, avgInputTokens: 60000, avgOutputTokens: 12000},
}

// PlanValidation holds the result of validating a planner's output.
type PlanValidation struct {
	Valid    bool
	Errors   []string
	Warnings []string
}

func (v *PlanValidation) addError(format string, args ...interface{}) {
	v.Errors = append(v.Errors, fmt.Sprintf(format, args...))
	v.Valid = false
}

func (v *PlanValidation) addWarning(format string, args ...interface{}) {
	v.Warnings = append(v.Warnings, fmt.Sprintf(format, args...))
}

// isNewFile reports whether a file path carries the "(new)" marker.
func isNewFile(path string) bool {
	return strings.HasSuffix(path, " (new)") || strings.HasSuffix(path, "(new)")
}

// stripNewSuffix removes the "(new)" marker from a file path.
func stripNewSuffix(path string) string {
	path = strings.TrimSuffix(path, " (new)")
	return strings.TrimSuffix(path, "(new)")
}

// ValidatePlan checks a planner result for issues before execution.
func ValidatePlan(plan *PlannerResult, workDir string, config *models.LimitsConfig) *PlanValidation {
	v := &PlanValidation{Valid: true}
	if config == nil {
		v.addError("config must not be nil")
		return v
	}

	// 1. Check task count limit
	if len(plan.Tasks) > config.MaxTasksPerTicket {
		v.addError("Plan has %d tasks, exceeding limit of %d", len(plan.Tasks), config.MaxTasksPerTicket)
	}

	// 2. Check file paths exist
	for _, task := range plan.Tasks {
		for _, path := range task.FilesToRead {
			if !fileExistsAt(workDir, path) {
				v.addError("Task '%s' references non-existent file: %s", task.Title, path)
			}
		}
		for _, path := range task.FilesToModify {
			if isNewFile(path) {
				continue // New files don't need to exist
			}
			if !fileExistsAt(workDir, path) {
				v.addError("Task '%s' modifies non-existent file: %s", task.Title, path)
			}
		}
	}

	// 3. Check for dependency cycles and unknown dependency references
	if _, err := TopologicalSort(plan.Tasks); err != nil {
		v.addError("%s", err.Error())
	}

	// 4. Warn about shared files without explicit ordering
	fileOwners := map[string][]string{}
	for _, task := range plan.Tasks {
		for _, path := range task.FilesToModify {
			cleanPath := stripNewSuffix(path)
			fileOwners[cleanPath] = append(fileOwners[cleanPath], task.Title)
		}
	}
	for path, owners := range fileOwners {
		if len(owners) > 1 {
			if !hasOrderingBetween(plan.Tasks, owners) {
				v.addWarning("Multiple tasks modify '%s' without explicit ordering: %v", path, owners)
			}
		}
	}

	return v
}

func fileExistsAt(workDir, path string) bool {
	_, err := os.Stat(filepath.Join(workDir, path))
	return err == nil
}

func hasOrderingBetween(tasks []PlannedTask, titles []string) bool {
	titleSet := map[string]bool{}
	for _, t := range titles {
		titleSet[t] = true
	}
	for _, task := range tasks {
		if !titleSet[task.Title] {
			continue
		}
		for _, dep := range task.DependsOn {
			if titleSet[dep] {
				return true
			}
		}
	}
	return false
}

// EstimateTicketCost estimates the total cost for a set of planned tasks
// based on complexity tiers and model pricing.
//
// It returns the total estimated cost and a sorted, deduplicated list of model
// keys not found in the pricing map. Callers should treat a non-empty list as
// a misconfiguration warning — cost for those models is counted as 0.
func EstimateTicketCost(tasks []PlannedTask, pricing map[string]models.PricingConfig, implModel, reviewModel string) (float64, []string) {
	missingModels := map[string]bool{}

	var totalCost float64
	for _, task := range tasks {
		t, ok := complexityTiers[task.EstimatedComplexity]
		if !ok {
			t = complexityTiers["medium"]
		}
		implCalls := t.llmCalls / 2
		if implCalls < 1 {
			implCalls = 1
		}

		if _, found := pricing[implModel]; !found {
			missingModels[implModel] = true
		}
		totalCost += float64(implCalls) * estimateCallCost(implModel, t.avgInputTokens, t.avgOutputTokens, pricing)

		reviewCalls := t.llmCalls - implCalls
		if _, found := pricing[reviewModel]; !found {
			missingModels[reviewModel] = true
		}
		totalCost += float64(reviewCalls) * estimateCallCost(reviewModel, t.avgInputTokens/2, t.avgOutputTokens/4, pricing)
	}

	missing := make([]string, 0, len(missingModels))
	for k := range missingModels {
		missing = append(missing, k)
	}
	sort.Strings(missing)

	return totalCost, missing
}

func estimateCallCost(model string, inputTokens, outputTokens int, pricing map[string]models.PricingConfig) float64 {
	p, ok := pricing[model]
	if !ok {
		return 0
	}
	return (float64(inputTokens)/1_000_000)*p.Input + (float64(outputTokens)/1_000_000)*p.Output
}
