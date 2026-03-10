package agent

import "sync"

type ChangeType string

const (
	ChangeCreated  ChangeType = "created"
	ChangeModified ChangeType = "modified"
	ChangeDeleted  ChangeType = "deleted"
)

// TrackedFileChange records a single file modification tracked by DiffTracker.
type TrackedFileChange struct {
	Path      string
	Type      ChangeType
	Additions int
	Deletions int
}

type DiffSummary struct {
	Files          []TrackedFileChange
	FilesChanged   int
	TotalAdditions int
	TotalDeletions int
}

type DiffTracker struct {
	changes map[string]*TrackedFileChange
	mu      sync.Mutex
}

func NewDiffTracker() *DiffTracker {
	return &DiffTracker{changes: make(map[string]*TrackedFileChange)}
}

func (dt *DiffTracker) RecordChange(path string, changeType ChangeType, additions, deletions int) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	if existing, ok := dt.changes[path]; ok {
		existing.Additions += additions
		existing.Deletions += deletions
		if changeType == ChangeDeleted {
			existing.Type = ChangeDeleted
		}
	} else {
		dt.changes[path] = &TrackedFileChange{Path: path, Type: changeType, Additions: additions, Deletions: deletions}
	}
}

func (dt *DiffTracker) Summary() DiffSummary {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	s := DiffSummary{FilesChanged: len(dt.changes)}
	for _, c := range dt.changes {
		s.TotalAdditions += c.Additions
		s.TotalDeletions += c.Deletions
		s.Files = append(s.Files, *c)
	}
	return s
}

func (dt *DiffTracker) ChangedFiles() []string {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	files := make([]string, 0, len(dt.changes))
	for path := range dt.changes {
		files = append(files, path)
	}
	return files
}
