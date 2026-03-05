package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/canhta/foreman/internal/models"
)

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
	return err == nil || !os.IsNotExist(err)
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
