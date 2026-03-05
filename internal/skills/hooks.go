// internal/skills/hooks.go
package skills

import (
	"context"
)

// HookResult holds the outcome of running a single skill at a hook point.
type HookResult struct {
	Error   error
	SkillID string
}

// HookRunner executes skills at pipeline hook points.
type HookRunner struct {
	engine *Engine
	skills []*Skill
}

// NewHookRunner creates a hook runner with all loaded skills.
func NewHookRunner(engine *Engine, skills []*Skill) *HookRunner {
	return &HookRunner{engine: engine, skills: skills}
}

// RunHook executes all skills matching the given trigger.
// Failures are recorded but do not block execution of subsequent skills.
func (h *HookRunner) RunHook(ctx context.Context, trigger string, sCtx *SkillContext) []HookResult {
	var results []HookResult
	for _, skill := range h.skills {
		if skill.Trigger != trigger {
			continue
		}
		err := h.engine.Execute(ctx, skill, sCtx)
		results = append(results, HookResult{
			SkillID: skill.ID,
			Error:   err,
		})
	}
	return results
}

// RunHookByNames executes only the named skills matching the trigger.
// Used when the pipeline config specifies which skills to run at a hook point.
func (h *HookRunner) RunHookByNames(ctx context.Context, trigger string, names []string, sCtx *SkillContext) []HookResult {
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	var results []HookResult
	for _, skill := range h.skills {
		if skill.Trigger != trigger || !nameSet[skill.ID] {
			continue
		}
		err := h.engine.Execute(ctx, skill, sCtx)
		results = append(results, HookResult{
			SkillID: skill.ID,
			Error:   err,
		})
	}
	return results
}
