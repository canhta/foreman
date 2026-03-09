// internal/prompts/frontmatter.go
package prompts

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseFrontmatter splits a Markdown document into YAML frontmatter and body.
// If no frontmatter delimiter is found, returns empty map and full content as body.
func ParseFrontmatter(content string) (map[string]any, string, error) {
	if !strings.HasPrefix(content, "---\n") {
		return map[string]any{}, content, nil
	}

	// Search for closing delimiter: "\n---" starting from position 3 (the \n of the opening line).
	// This handles both "---\n---\n..." (empty) and "---\nyaml\n---\n..." (normal).
	closeIdx := strings.Index(content[3:], "\n---")
	if closeIdx < 0 {
		return map[string]any{}, content, nil
	}
	// closeIdx is relative to content[3:], so absolute position of '\n' before closing --- is 3+closeIdx.
	// YAML content lives between position 4 (after opening "---\n") and 3+closeIdx (the \n before closing ---).
	yamlStart := 4
	yamlEnd := 3 + closeIdx // position of '\n' before closing ---
	var yamlStr string
	if yamlEnd > yamlStart {
		yamlStr = content[yamlStart:yamlEnd]
	}
	// Body starts after the closing "---" line; skip the \n + --- + any trailing \n
	bodyStart := 3 + closeIdx + 4 // skip "\n---"
	body := strings.TrimLeft(content[bodyStart:], "\n")

	if strings.TrimSpace(yamlStr) == "" {
		return map[string]any{}, body, nil
	}

	var fm map[string]any
	if err := yaml.Unmarshal([]byte(yamlStr), &fm); err != nil {
		return nil, "", fmt.Errorf("parse frontmatter YAML: %w", err)
	}

	return fm, body, nil
}
