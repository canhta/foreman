package pipeline

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/rs/zerolog/log"
)

//go:embed assets/claude
var claudeAssets embed.FS

// SkillInjectorConfig holds template rendering values.
type SkillInjectorConfig struct {
	TestCommand string
	LintCommand string
	Language    string
}

// SkillInjector writes TDD skill templates into a working directory for Claude Code.
type SkillInjector struct {
	config SkillInjectorConfig
}

// NewSkillInjector creates a SkillInjector with the given config.
func NewSkillInjector(config SkillInjectorConfig) *SkillInjector {
	return &SkillInjector{config: config}
}

// Inject writes template files into workDir/.claude/. Merges settings.json
// if one already exists. All other files go under .claude/foreman/.
func (si *SkillInjector) Inject(workDir string) error {
	claudeDir := filepath.Join(workDir, ".claude")

	// Merge or create settings.json
	if err := si.mergeSettings(claudeDir); err != nil {
		return fmt.Errorf("merge settings: %w", err)
	}

	// Write template files under .claude/foreman/
	return si.writeTemplates(claudeDir)
}

// Cleanup removes .claude/foreman/ directory. Leaves settings.json intact.
func (si *SkillInjector) Cleanup(workDir string) {
	foremanDir := filepath.Join(workDir, ".claude", "foreman")
	if err := os.RemoveAll(foremanDir); err != nil {
		log.Warn().Err(err).Str("dir", foremanDir).Msg("skill_injector: cleanup failed")
	}
}

func (si *SkillInjector) mergeSettings(claudeDir string) error {
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return err
	}

	settingsPath := filepath.Join(claudeDir, "settings.json")

	// Load Foreman's settings template
	templateData, err := claudeAssets.ReadFile("assets/claude/settings.json")
	if err != nil {
		return fmt.Errorf("read embedded settings: %w", err)
	}
	var foremanSettings map[string]interface{}
	if err := json.Unmarshal(templateData, &foremanSettings); err != nil {
		return fmt.Errorf("parse embedded settings: %w", err)
	}

	// Load existing settings if present
	existing := make(map[string]interface{})
	if data, readErr := os.ReadFile(settingsPath); readErr == nil {
		if parseErr := json.Unmarshal(data, &existing); parseErr != nil {
			log.Warn().Err(parseErr).Msg("skill_injector: existing settings.json invalid, overwriting")
			existing = make(map[string]interface{})
		}
	}

	// Deep merge: Foreman settings into existing (existing keys preserved)
	merged := deepMerge(existing, foremanSettings)

	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal merged settings: %w", err)
	}
	return os.WriteFile(settingsPath, out, 0o644)
}

func (si *SkillInjector) writeTemplates(claudeDir string) error {
	entries, err := claudeAssets.ReadDir("assets/claude/foreman")
	if err != nil {
		return fmt.Errorf("read embedded foreman dir: %w", err)
	}
	return si.writeDir("assets/claude/foreman", filepath.Join(claudeDir, "foreman"), entries)
}

func (si *SkillInjector) writeDir(embedPath, diskPath string, entries []os.DirEntry) error {
	if err := os.MkdirAll(diskPath, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := embedPath + "/" + entry.Name()
		dstPath := filepath.Join(diskPath, entry.Name())

		if entry.IsDir() {
			subEntries, err := claudeAssets.ReadDir(srcPath)
			if err != nil {
				return err
			}
			if err := si.writeDir(srcPath, dstPath, subEntries); err != nil {
				return err
			}
			continue
		}

		data, err := claudeAssets.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", srcPath, err)
		}

		// Render templates
		rendered, err := si.renderTemplate(entry.Name(), string(data))
		if err != nil {
			return fmt.Errorf("render %s: %w", srcPath, err)
		}

		if err := os.WriteFile(dstPath, []byte(rendered), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dstPath, err)
		}
	}
	return nil
}

func (si *SkillInjector) renderTemplate(name, content string) (string, error) {
	tmpl, err := template.New(name).Parse(content)
	if err != nil {
		// Not a template — return raw content
		return content, nil
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, si.config); err != nil {
		return content, nil
	}
	return buf.String(), nil
}

// deepMerge merges src into dst. Existing keys in dst are NOT overwritten
// unless both values are maps (recursive merge).
func deepMerge(dst, src map[string]interface{}) map[string]interface{} {
	for k, srcVal := range src {
		if dstVal, exists := dst[k]; exists {
			// Both are maps: recursive merge
			dstMap, dstOk := dstVal.(map[string]interface{})
			srcMap, srcOk := srcVal.(map[string]interface{})
			if dstOk && srcOk {
				dst[k] = deepMerge(dstMap, srcMap)
				continue
			}
			// dst key exists and isn't a map merge — keep dst value
			continue
		}
		dst[k] = srcVal
	}
	return dst
}
