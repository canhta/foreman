package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

type Todo struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"` // pending, in_progress, completed
	Priority int    `json:"priority,omitempty"`
}

type TodoStore struct {
	mu    sync.RWMutex
	todos []Todo
}

func NewTodoStore() *TodoStore { return &TodoStore{} }

func (s *TodoStore) Add(t Todo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.todos = append(s.todos, t)
}

func (s *TodoStore) Update(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.todos {
		if s.todos[i].ID == id {
			s.todos[i].Status = status
			return nil
		}
	}
	return fmt.Errorf("todo %q not found", id)
}

func (s *TodoStore) Replace(todos []Todo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.todos = todos
}

func (s *TodoStore) List() []Todo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Todo, len(s.todos))
	copy(result, s.todos)
	return result
}

func (s *TodoStore) JSON() string {
	todos := s.List()
	data, _ := json.MarshalIndent(todos, "", "  ")
	return string(data)
}

// todoWriteTool replaces the session todo list.
type todoWriteTool struct {
	store *TodoStore
}

func (t *todoWriteTool) Name() string { return "TodoWrite" }
func (t *todoWriteTool) Description() string {
	return "Replace the session todo list with a new set of todos for tracking progress"
}
func (t *todoWriteTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"todos":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"content":{"type":"string"},"status":{"type":"string"},"priority":{"type":"integer"}},"required":["id","content","status"]}}},"required":["todos"]}`)
}
func (t *todoWriteTool) Execute(_ context.Context, _ string, input json.RawMessage) (string, error) {
	var args struct {
		Todos []Todo `json:"todos"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("todowrite: %w", err)
	}
	t.store.Replace(args.Todos)
	return fmt.Sprintf("Updated todo list with %d items", len(args.Todos)), nil
}

// todoReadTool returns the current session todo list.
type todoReadTool struct {
	store *TodoStore
}

func (t *todoReadTool) Name() string { return "TodoRead" }
func (t *todoReadTool) Description() string {
	return "Read the current session todo list"
}
func (t *todoReadTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *todoReadTool) Execute(_ context.Context, _ string, _ json.RawMessage) (string, error) {
	return t.store.JSON(), nil
}
