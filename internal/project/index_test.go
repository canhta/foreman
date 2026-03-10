package project

import (
	"path/filepath"
	"testing"
	"time"
)

func TestProjectIndex_AddAndList(t *testing.T) {
	dir := t.TempDir()
	idx := NewIndex(filepath.Join(dir, "projects.json"))

	entry := IndexEntry{
		ID:        "test-uuid",
		Name:      "TestProject",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		Active:    true,
	}

	if err := idx.Add(entry); err != nil {
		t.Fatalf("Add: %v", err)
	}

	entries, err := idx.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("List returned %d entries, want 1", len(entries))
	}
	if entries[0].ID != "test-uuid" {
		t.Errorf("ID = %q, want test-uuid", entries[0].ID)
	}
	if entries[0].Name != "TestProject" {
		t.Errorf("Name = %q, want TestProject", entries[0].Name)
	}
}

func TestProjectIndex_Remove(t *testing.T) {
	dir := t.TempDir()
	idx := NewIndex(filepath.Join(dir, "projects.json"))

	_ = idx.Add(IndexEntry{ID: "a", Name: "A", Active: true})
	_ = idx.Add(IndexEntry{ID: "b", Name: "B", Active: true})

	if err := idx.Remove("a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	entries, _ := idx.List()
	if len(entries) != 1 {
		t.Fatalf("List returned %d entries after remove, want 1", len(entries))
	}
	if entries[0].ID != "b" {
		t.Errorf("remaining entry ID = %q, want b", entries[0].ID)
	}
}
