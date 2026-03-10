package project

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// IndexEntry represents a project in the index.
//
//nolint:govet // fieldalignment: struct field order prioritises readability over padding
type IndexEntry struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Active    bool      `json:"active"`
}

type indexFile struct {
	Projects []IndexEntry `json:"projects"`
}

// Index manages the projects.json file.
// All writes are serialized through a mutex.
type Index struct {
	path string
	mu   sync.Mutex
}

// NewIndex creates an Index for the given file path.
func NewIndex(path string) *Index {
	return &Index{path: path}
}

// List returns all project entries.
func (idx *Index) List() ([]IndexEntry, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.load()
}

// Add adds a project entry to the index.
func (idx *Index) Add(entry IndexEntry) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	entries, err := idx.load()
	if err != nil {
		return err
	}

	entries = append(entries, entry)
	return idx.save(entries)
}

// UpdateName updates the name of an existing project entry.
func (idx *Index) UpdateName(id, name string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	entries, err := idx.load()
	if err != nil {
		return err
	}

	for i, e := range entries {
		if e.ID == id {
			entries[i].Name = name
			return idx.save(entries)
		}
	}
	return fmt.Errorf("project %s not found in index", id)
}

// Remove removes a project entry by ID.
func (idx *Index) Remove(id string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	entries, err := idx.load()
	if err != nil {
		return err
	}

	filtered := make([]IndexEntry, 0, len(entries))
	for _, e := range entries {
		if e.ID != id {
			filtered = append(filtered, e)
		}
	}
	return idx.save(filtered)
}

func (idx *Index) load() ([]IndexEntry, error) {
	data, err := os.ReadFile(idx.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}

	var f indexFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}
	return f.Projects, nil
}

func (idx *Index) save(entries []IndexEntry) error {
	data, err := json.MarshalIndent(indexFile{Projects: entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}
	return os.WriteFile(idx.path, data, 0644)
}
