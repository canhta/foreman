// internal/skills/loader.go
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/canhta/foreman/internal/prompts"
)

// Skill is a YAML workflow definition.
type Skill struct {
	ID          string      `yaml:"id"`
	Description string      `yaml:"description"`
	Trigger     string      `yaml:"trigger"`
	Steps       []SkillStep `yaml:"steps"`
}

// SkillStepThinking holds extended thinking configuration in skill YAML.
// Use adaptive=true for Opus 4.6 / Sonnet 4.6 (no budget_tokens needed).
// Use enabled=true with budget_tokens for older models.
type SkillStepThinking struct {
	Enabled      bool `yaml:"enabled"`
	Adaptive     bool `yaml:"adaptive,omitempty"`
	BudgetTokens int  `yaml:"budget_tokens,omitempty"`
}

// SkillStep is one step within a skill.
type SkillStep struct {
	Context        map[string]string      `yaml:"context,omitempty"`
	Input          map[string]string      `yaml:"input,omitempty"`
	Thinking       *SkillStepThinking     `yaml:"thinking,omitempty"`
	OutputSchema   map[string]interface{} `yaml:"output_schema,omitempty"`
	Command        string                 `yaml:"command,omitempty"`
	OutputKey      string                 `yaml:"output_key,omitempty"`
	OutputFormat   string                 `yaml:"output_format,omitempty"`
	Type           string                 `yaml:"type"`
	Path           string                 `yaml:"path,omitempty"`
	Content        string                 `yaml:"content,omitempty"`
	Mode           string                 `yaml:"mode,omitempty"`
	SkillRef       string                 `yaml:"skill_ref,omitempty"`
	FallbackModel  string                 `yaml:"fallback_model,omitempty"`
	ID             string                 `yaml:"id"`
	PromptTemplate string                 `yaml:"prompt_template,omitempty"`
	Model          string                 `yaml:"model,omitempty"`
	AllowedTools   []string               `yaml:"allowed_tools,omitempty"`
	Args           []string               `yaml:"args,omitempty"`
	TimeoutSecs    int                    `yaml:"timeout_secs,omitempty"`
	MaxTurns       int                    `yaml:"max_turns,omitempty"`
	MaxTokens      int                    `yaml:"max_tokens,omitempty"`
	AllowFailure   bool                   `yaml:"allow_failure,omitempty"`
}

// StepResult holds the output of an executed skill step.
type StepResult struct {
	Output   string
	Stderr   string
	Error    string
	ExitCode int
}

var validTriggers = map[string]bool{
	"post_lint":  true,
	"pre_pr":     true,
	"post_pr":    true,
	"post_merge": true,
}

var validStepTypes = map[string]bool{
	"llm_call":    true,
	"run_command": true,
	"file_write":  true,
	"git_diff":    true,
	"agentsdk":    true,
	"subskill":    true,
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
		return nil, fmt.Errorf("invalid trigger '%s' in skill '%s' (valid: post_lint, pre_pr, post_pr, post_merge)", skill.Trigger, skill.ID)
	}

	// Validate step types
	for _, step := range skill.Steps {
		if !validStepTypes[step.Type] {
			return nil, fmt.Errorf("unknown step type '%s' in skill '%s' step '%s'", step.Type, skill.ID, step.ID)
		}
	}

	return &skill, nil
}

// LoadFromRegistry converts registry skill entries into Skill structs
// compatible with the existing engine.
func LoadFromRegistry(reg *prompts.Registry) ([]*Skill, error) {
	var skills []*Skill
	for _, entry := range reg.List(prompts.KindSkill) {
		steps, err := reg.SkillSteps(entry.Name)
		if err != nil {
			return nil, fmt.Errorf("load steps for %s: %w", entry.Name, err)
		}

		trigger, _ := entry.Metadata["trigger"].(string)

		skill := &Skill{
			ID:          entry.Name,
			Description: entry.Description,
			Trigger:     trigger,
		}
		for _, s := range steps {
			skill.Steps = append(skill.Steps, SkillStep{
				ID:            s.ID,
				Type:          s.Type,
				Content:       s.Prompt,
				Model:         s.Model,
				Command:       s.Command,
				AllowedTools:  s.AllowedTools,
				MaxTurns:      s.MaxTurns,
				TimeoutSecs:   s.TimeoutSecs,
				MaxTokens:     s.MaxTokens,
				OutputFormat:  s.OutputFormat,
				FallbackModel: s.FallbackModel,
				SkillRef:      s.SkillRef,
				AllowFailure:  s.AllowFailure,
			})
		}
		skills = append(skills, skill)
	}
	return skills, nil
}

// LoadFromPaths loads and validates skills from a list of file paths.
// Paths that fail to load are returned as errors; valid skills are collected.
// This is designed to be used with DiscoverSkillPaths.
func LoadFromPaths(paths []string) ([]*Skill, error) {
	var skills []*Skill
	for _, path := range paths {
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yml" && ext != ".yaml" {
			continue
		}
		skill, err := LoadSkill(path)
		if err != nil {
			return nil, fmt.Errorf("loading skill from path %s: %w", path, err)
		}
		skills = append(skills, skill)
	}
	return skills, nil
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
