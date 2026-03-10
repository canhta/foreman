package agent

import (
	"fmt"
	"sync"
	"time"
)

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

type ManagedTask struct {
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ID          string
	Description string
	Prompt      string
	Result      string
	Error       string
	Mode        string
	Status      TaskStatus
}

type TaskManager struct {
	tasks map[string]*ManagedTask
	seq   int
	mu    sync.RWMutex
}

func NewTaskManager() *TaskManager {
	return &TaskManager{tasks: make(map[string]*ManagedTask)}
}

func (tm *TaskManager) Create(description, prompt string) string {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.seq++
	id := fmt.Sprintf("task-%d", tm.seq)
	tm.tasks[id] = &ManagedTask{
		ID:          id,
		Description: description,
		Prompt:      prompt,
		Status:      TaskStatusPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	return id
}

func (tm *TaskManager) Get(id string) (*ManagedTask, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	t, ok := tm.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task %q not found", id)
	}
	return t, nil
}

func (tm *TaskManager) SetRunning(id string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if t, ok := tm.tasks[id]; ok {
		t.Status = TaskStatusRunning
		t.UpdatedAt = time.Now()
	}
}

func (tm *TaskManager) Complete(id, result string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if t, ok := tm.tasks[id]; ok {
		t.Status = TaskStatusCompleted
		t.Result = result
		t.UpdatedAt = time.Now()
	}
}

func (tm *TaskManager) Fail(id, errMsg string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if t, ok := tm.tasks[id]; ok {
		t.Status = TaskStatusFailed
		t.Error = errMsg
		t.UpdatedAt = time.Now()
	}
}

func (tm *TaskManager) List() []*ManagedTask {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	result := make([]*ManagedTask, 0, len(tm.tasks))
	for _, t := range tm.tasks {
		result = append(result, t)
	}
	return result
}
