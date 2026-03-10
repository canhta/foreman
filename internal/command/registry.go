package command

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Command is a user-invokable action with a template.
type Command struct {
	Name        string
	Description string
	Template    string
	Agent       string
	Model       string
	Source      string
	Subtask     bool
}

// Registry holds all available commands. It is safe for concurrent use.
type Registry struct {
	commands map[string]Command
	mu       sync.RWMutex
}

// NewRegistry creates an empty command registry.
func NewRegistry() *Registry {
	return &Registry{commands: make(map[string]Command)}
}

// Register adds or replaces a command.
func (r *Registry) Register(cmd Command) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands[cmd.Name] = cmd
}

// Get retrieves a command by name.
func (r *Registry) Get(name string) (Command, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cmd, ok := r.commands[name]
	if !ok {
		return Command{}, fmt.Errorf("command %q not found", name)
	}
	return cmd, nil
}

// List returns all commands sorted by name.
func (r *Registry) List() []Command {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		result = append(result, cmd)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Render substitutes arguments into a command template.
// $ARGUMENTS is replaced with all args joined by space.
// $1, $2, etc. are replaced with positional args.
func (r *Registry) Render(name string, args ...string) (string, error) {
	cmd, err := r.Get(name)
	if err != nil {
		return "", err
	}

	result := cmd.Template

	// Replace positional args
	for i, arg := range args {
		result = strings.ReplaceAll(result, fmt.Sprintf("$%d", i+1), arg)
	}

	// Replace $ARGUMENTS with all args
	result = strings.ReplaceAll(result, "$ARGUMENTS", strings.Join(args, " "))

	return result, nil
}
