package models

// PlanOutput is the structured output from the planning stage.
// It represents the typed contract between the planner LLM call and the
// plan validator / pipeline orchestrator.
//
// To use with LlmRequest.OutputSchema:
//
//	schema, _ := json.Marshal(map[string]any{
//	    "type": "object",
//	    "properties": map[string]any{
//	        "tasks":     map[string]any{"type": "array"},
//	        "rationale": map[string]any{"type": "string"},
//	    },
//	    "required": []string{"tasks", "rationale"},
//	})
//	raw := json.RawMessage(schema)
//	req := models.LlmRequest{OutputSchema: &raw, ...}
type PlanOutput struct {
	Rationale string     `json:"rationale"`
	Tasks     []PlanTask `json:"tasks"`
}

// PlanTask is a single task within a PlanOutput.
type PlanTask struct {
	Description   string   `json:"description"`
	ID            string   `json:"id"`
	FilesToModify []string `json:"files_to_modify,omitempty"`
	FilesToRead   []string `json:"files_to_read,omitempty"`
	Dependencies  []string `json:"dependencies,omitempty"`
}

// ImplementedChange is a single file modification produced by the implementation stage.
// It mirrors agent.FileChange but lives in models to avoid an import cycle:
// internal/pipeline → internal/models is valid; the reverse is not.
type ImplementedChange struct {
	// OldContent is the original content targeted by a SEARCH block, if any.
	OldContent string `json:"old_content,omitempty"`
	// NewContent is the replacement content or unified diff.
	NewContent string `json:"new_content"`
	// Path is the file path relative to the repo root.
	Path string `json:"path"`
	// IsNew is true when the file is newly created rather than modified.
	IsNew bool `json:"is_new"`
}

// ImplementOutput is the structured output from the implementation stage.
// It represents the typed contract between the implementer and the TDD verifier /
// lint runner / spec reviewer.
type ImplementOutput struct {
	Summary string              `json:"summary"`
	Changes []ImplementedChange `json:"changes"`
}

// ReviewIssue is a single issue found during any review stage.
type ReviewIssue struct {
	// Description is the human-readable issue text.
	Description string `json:"description"`
	// Severity is one of "minor", "major", or "critical".
	Severity string `json:"severity"`
	// File is the file where the issue was found, if known.
	File string `json:"file,omitempty"`
}

// ReviewOutput is the structured output from any review stage (spec, quality, final).
// It represents the typed contract between a reviewer and the pipeline feedback loop.
//
// To use with LlmRequest.OutputSchema, generate a JSON Schema from this type and
// assign it to LlmRequest.OutputSchema before calling LlmProvider.Complete.
type ReviewOutput struct {
	Summary     string        `json:"summary"`
	ReviewNotes string        `json:"review_notes,omitempty"`
	Severity    string        `json:"severity"`
	Issues      []ReviewIssue `json:"issues"`
	Approved    bool          `json:"approved"`
}
