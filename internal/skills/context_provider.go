package skills

import (
	"context"
	"path/filepath"

	fmtctx "github.com/canhta/foreman/internal/context"
	"github.com/canhta/foreman/internal/models"
)

// SkillsContextProvider implements agent.ContextProvider.
// It queries progress patterns reactively as the agent accesses files, injecting
// only patterns relevant to the directories touched. Patterns are never injected twice.
// Injection stops when the token budget is consumed.
type SkillsContextProvider struct {
	db             fmtctx.ProgressStore
	ticketID       string
	injected       map[string]bool
	tokensBudget   int // 0 = unlimited; rough estimate: len(text)/4
	tokensInjected int
}

// NewSkillsContextProvider creates a provider with an 8000-token default budget.
func NewSkillsContextProvider(db fmtctx.ProgressStore, ticketID string) *SkillsContextProvider {
	return &SkillsContextProvider{
		db:           db,
		ticketID:     ticketID,
		injected:     make(map[string]bool),
		tokensBudget: 8000,
	}
}

// WithTokenBudget sets a custom token budget (0 = unlimited).
func (p *SkillsContextProvider) WithTokenBudget(n int) *SkillsContextProvider {
	p.tokensBudget = n
	return p
}

// OnFilesAccessed implements agent.ContextProvider.
// Called once per turn after all parallel tool calls complete.
func (p *SkillsContextProvider) OnFilesAccessed(ctx context.Context, paths []string) (string, error) {
	if p.tokensBudget > 0 && p.tokensInjected >= p.tokensBudget {
		return "", nil
	}
	dirs := uniqueDirs(paths)
	all, err := fmtctx.GetPrunedPatterns(ctx, p.db, p.ticketID, dirs)
	if err != nil {
		return "", err
	}
	var fresh []models.ProgressPattern
	for _, pat := range all {
		if !p.injected[pat.PatternKey] {
			p.injected[pat.PatternKey] = true
			fresh = append(fresh, pat)
		}
	}
	if len(fresh) == 0 {
		return "", nil
	}
	text := fmtctx.FormatPatternsForPrompt(fresh)
	p.tokensInjected += len(text) / 4
	return text, nil
}

// uniqueDirs extracts unique directory paths from a list of file paths.
func uniqueDirs(paths []string) []string {
	seen := make(map[string]bool)
	var dirs []string
	for _, p := range paths {
		d := filepath.Dir(p)
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	return dirs
}
