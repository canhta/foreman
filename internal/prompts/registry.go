// internal/prompts/registry.go
package prompts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/flosch/pongo2/v6"
)

// EntryKind distinguishes the type of prompt entry.
type EntryKind string

const (
	KindRole     EntryKind = "role"
	KindAgent    EntryKind = "agent"
	KindSkill    EntryKind = "skill"
	KindCommand  EntryKind = "command"
	KindFragment EntryKind = "fragment"
)

// Entry is a single loaded prompt/agent/skill/command/fragment.
type Entry struct {
	Name        string
	Kind        EntryKind
	Description string
	Metadata    map[string]any
	RawContent  string
	Path        string
	Includes    []string
}

// Registry holds all loaded prompt entries indexed by kind and name.
type Registry struct {
	entries map[EntryKind]map[string]*Entry
	baseDir string
}

// dirForKind maps each kind to its subdirectory name and file convention.
var dirForKind = map[EntryKind]struct {
	dir      string
	filename string
}{
	KindRole:    {dir: "roles", filename: "ROLE.md"},
	KindAgent:   {dir: "agents", filename: "AGENT.md"},
	KindSkill:   {dir: "skills", filename: "SKILL.md"},
	KindCommand: {dir: "commands", filename: "COMMAND.md"},
}

// Load scans baseDir for all prompt entries and returns a populated Registry.
func Load(baseDir string) (*Registry, error) {
	r := &Registry{
		entries: make(map[EntryKind]map[string]*Entry),
		baseDir: baseDir,
	}
	for kind := range dirForKind {
		r.entries[kind] = make(map[string]*Entry)
	}
	r.entries[KindFragment] = make(map[string]*Entry)

	// Load structured entries (roles, agents, skills, commands)
	for kind, info := range dirForKind {
		dir := filepath.Join(baseDir, info.dir)
		if err := r.scanDir(dir, kind, info.filename); err != nil {
			// Directory might not exist — not an error
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("scan %s: %w", info.dir, err)
			}
		}
	}

	// Load fragments
	fragDir := filepath.Join(baseDir, "fragments")
	if err := r.scanFragments(fragDir); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("scan fragments: %w", err)
	}

	return r, nil
}

func (r *Registry) scanDir(dir string, kind EntryKind, filename string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), filename)
		if _, statErr := os.Stat(path); statErr != nil {
			// Recurse into subdirectories (e.g., skills/community/)
			subDir := filepath.Join(dir, e.Name())
			_ = r.scanDir(subDir, kind, filename)
			continue
		}
		if err := r.loadEntry(path, kind); err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
	}
	return nil
}

func (r *Registry) scanFragments(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		r.entries[KindFragment][name] = &Entry{
			Name:       name,
			Kind:       KindFragment,
			RawContent: string(data),
			Path:       path,
		}
	}
	return nil
}

func (r *Registry) loadEntry(path string, kind EntryKind) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	fm, body, err := ParseFrontmatter(string(data))
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	name, _ := fm["name"].(string)
	if name == "" {
		return fmt.Errorf("%s: missing 'name' in frontmatter", path)
	}

	desc, _ := fm["description"].(string)

	var includes []string
	if inc, ok := fm["includes"]; ok {
		if list, ok := inc.([]any); ok {
			for _, item := range list {
				if s, ok := item.(string); ok {
					includes = append(includes, s)
				}
			}
		}
	}

	r.entries[kind][name] = &Entry{
		Name:        name,
		Kind:        kind,
		Description: desc,
		Metadata:    fm,
		RawContent:  body,
		Path:        path,
		Includes:    includes,
	}
	return nil
}

// Get retrieves a single entry by kind and name.
func (r *Registry) Get(kind EntryKind, name string) (*Entry, error) {
	m, ok := r.entries[kind]
	if !ok {
		return nil, fmt.Errorf("unknown entry kind: %s", kind)
	}
	entry, ok := m[name]
	if !ok {
		return nil, fmt.Errorf("%s %q not found", kind, name)
	}
	return entry, nil
}

// List returns all entries of a given kind, sorted by name.
func (r *Registry) List(kind EntryKind) []*Entry {
	m := r.entries[kind]
	result := make([]*Entry, 0, len(m))
	for _, e := range m {
		result = append(result, e)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Render resolves includes and renders template variables for an entry.
func (r *Registry) Render(kind EntryKind, name string, vars map[string]any) (string, error) {
	entry, err := r.Get(kind, name)
	if err != nil {
		return "", err
	}
	return r.RenderEntry(entry, vars)
}

// ForClaude writes .claude/ directory structure into workDir for Claude Code runner.
// Renders all agents as .claude/agents/*.md, commands as .claude/commands/*.md,
// and generates settings.json with permissions.
func (r *Registry) ForClaude(workDir string, vars map[string]any) error {
	claudeDir := filepath.Join(workDir, ".claude")

	// Write agents
	agentsDir := filepath.Join(claudeDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir agents: %w", err)
	}
	for _, agent := range r.List(KindAgent) {
		rendered, err := r.RenderEntry(agent, vars)
		if err != nil {
			return fmt.Errorf("render agent %s: %w", agent.Name, err)
		}
		path := filepath.Join(agentsDir, agent.Name+".md")
		if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
			return fmt.Errorf("write agent %s: %w", agent.Name, err)
		}
	}

	// Write commands
	cmdsDir := filepath.Join(claudeDir, "commands")
	if err := os.MkdirAll(cmdsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir commands: %w", err)
	}
	for _, cmd := range r.List(KindCommand) {
		rendered, err := r.RenderEntry(cmd, vars)
		if err != nil {
			return fmt.Errorf("render command %s: %w", cmd.Name, err)
		}
		path := filepath.Join(cmdsDir, cmd.Name+".md")
		if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
			return fmt.Errorf("write command %s: %w", cmd.Name, err)
		}
	}

	// Write settings.json
	settings := map[string]any{
		"permissions": map[string]any{
			"allow": []string{"Read", "Edit", "Write", "Glob", "Grep", "Bash"},
		},
	}
	settingsData, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(settingsPath, settingsData, 0o644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	return nil
}

// RenderEntry renders a single entry with the given variables.
func (r *Registry) RenderEntry(entry *Entry, vars map[string]any) (string, error) {
	// Build a pongo2 template set rooted at baseDir so {% include %} works
	loader, err := pongo2.NewLocalFileSystemLoader(r.baseDir)
	if err != nil {
		return "", fmt.Errorf("create template loader: %w", err)
	}
	tplSet := pongo2.NewSet("prompts", loader)

	tpl, err := tplSet.FromString(entry.RawContent)
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", entry.Name, err)
	}

	ctx := pongo2.Context{}
	for k, v := range vars {
		ctx[k] = v
	}

	result, err := tpl.Execute(ctx)
	if err != nil {
		return "", fmt.Errorf("render %s: %w", entry.Name, err)
	}
	return result, nil
}
