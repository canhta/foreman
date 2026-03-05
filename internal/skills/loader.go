// internal/skills/loader.go
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill is a YAML workflow definition.
type Skill struct {
	ID          string      `yaml:"id"`
	Description string      `yaml:"description"`
	Trigger     string      `yaml:"trigger"`
	Steps       []SkillStep `yaml:"steps"`
}

// SkillStep is one step within a skill.
type SkillStep struct {
	ID             string            `yaml:"id"`
	Type           string            `yaml:"type"`
	PromptTemplate string            `yaml:"prompt_template,omitempty"`
	Model          string            `yaml:"model,omitempty"`
	Context        map[string]string `yaml:"context,omitempty"`
	Command        string            `yaml:"command,omitempty"`
	Args           []string          `yaml:"args,omitempty"`
	AllowFailure   bool              `yaml:"allow_failure,omitempty"`
	Path           string            `yaml:"path,omitempty"`
	Content        string            `yaml:"content,omitempty"`
	Mode           string            `yaml:"mode,omitempty"`
}

// StepResult holds the output of an executed skill step.
type StepResult struct {
	Output   string
	Stderr   string
	ExitCode int
	Error    string
}

var validTriggers = map[string]bool{
	"post_lint": true,
	"pre_pr":    true,
	"post_pr":   true,
}

var validStepTypes = map[string]bool{
	"llm_call":    true,
	"run_command": true,
	"file_write":  true,
	"git_diff":    true,
}

// LoadSkill loads and validates a single skill file.
func LoadSkill(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading skill file %s: %w", path, err)
	}

	var skill Skill
	if err := yaml.Unmarshal(data, &skill); err != nil {
		return nil, fmt.Errorf("parsing skill file %s: %w", path, err)
	}

	// Validate trigger
	if !validTriggers[skill.Trigger] {
		return nil, fmt.Errorf("invalid trigger '%s' in skill '%s' (valid: post_lint, pre_pr, post_pr)", skill.Trigger, skill.ID)
	}

	// Validate step types
	for _, step := range skill.Steps {
		if !validStepTypes[step.Type] {
			return nil, fmt.Errorf("unknown step type '%s' in skill '%s' step '%s'", step.Type, skill.ID, step.ID)
		}
	}

	return &skill, nil
}

// LoadSkillsDir loads all .yml/.yaml files from a directory.
func LoadSkillsDir(dir string) ([]*Skill, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading skills directory %s: %w", dir, err)
	}

	var skills []*Skill
	for _, entry := range entries {
		if entry.IsDir() {
			// Recurse into subdirectories (e.g., community/)
			subSkills, err := LoadSkillsDir(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, err
			}
			skills = append(skills, subSkills...)
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yml" && ext != ".yaml" {
			continue
		}
		skill, err := LoadSkill(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		skills = append(skills, skill)
	}

	return skills, nil
}
