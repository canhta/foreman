package agent

import (
	"encoding/json"
	"fmt"

	"github.com/canhta/foreman/internal/models"
)

// BuildStructuredOutputTool creates a tool definition that instructs the LLM
// to output structured JSON matching the given schema.
func BuildStructuredOutputTool(schema json.RawMessage) models.ToolDef {
	return models.ToolDef{
		Name:        "structured_output",
		Description: "You MUST use this tool to provide your response. Output your analysis as structured JSON matching the schema.",
		InputSchema: schema,
	}
}

// ValidateStructuredOutput checks if output is valid JSON.
// Schema validation is best-effort — we verify JSON validity.
func ValidateStructuredOutput(schema json.RawMessage, output string) error {
	if !json.Valid([]byte(output)) {
		return fmt.Errorf("output is not valid JSON")
	}
	return nil
}
