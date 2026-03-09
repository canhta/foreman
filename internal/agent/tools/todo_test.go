package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTodoStore_AddAndList(t *testing.T) {
	store := NewTodoStore()
	store.Add(Todo{ID: "1", Content: "Write tests", Status: "pending"})
	store.Add(Todo{ID: "2", Content: "Implement feature", Status: "pending"})
	todos := store.List()
	assert.Len(t, todos, 2)
}

func TestTodoStore_Update(t *testing.T) {
	store := NewTodoStore()
	store.Add(Todo{ID: "1", Content: "Write tests", Status: "pending"})
	err := store.Update("1", "completed")
	require.NoError(t, err)
	todos := store.List()
	assert.Equal(t, "completed", todos[0].Status)
}

func TestTodoStore_Replace(t *testing.T) {
	store := NewTodoStore()
	store.Add(Todo{ID: "1", Content: "Old", Status: "pending"})
	store.Replace([]Todo{
		{ID: "1", Content: "Updated", Status: "in_progress"},
		{ID: "2", Content: "New", Status: "pending"},
	})
	todos := store.List()
	assert.Len(t, todos, 2)
	assert.Equal(t, "Updated", todos[0].Content)
}
