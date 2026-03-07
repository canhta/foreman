package models

import "sync"

// DiscoveryBoard accumulates discoveries (patterns and file relevance scores)
// shared across parallel DAG tasks within a single ticket. It is safe for
// concurrent use.
//
//nolint:govet // fieldalignment: both field orderings produce 40 bytes; linter false positive
type DiscoveryBoard struct {
	mu       sync.RWMutex
	patterns map[string]string
	files    map[string]float64
}

// NewDiscoveryBoard constructs an empty DiscoveryBoard.
func NewDiscoveryBoard() *DiscoveryBoard {
	return &DiscoveryBoard{
		patterns: make(map[string]string),
		files:    make(map[string]float64),
	}
}

// AddPattern records a discovered pattern under the given key. If the key
// already exists it is overwritten with the new value.
func (b *DiscoveryBoard) AddPattern(key, value string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.patterns[key] = value
}

// AddFile records a relevance score for the given file path. The entry is
// updated only when score is strictly greater than the current value.
func (b *DiscoveryBoard) AddFile(path string, score float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if score > b.files[path] {
		b.files[path] = score
	}
}

// GetPatterns returns a copy of the current pattern map. Mutations to the
// returned map do not affect the board.
func (b *DiscoveryBoard) GetPatterns() map[string]string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	cp := make(map[string]string, len(b.patterns))
	for k, v := range b.patterns {
		cp[k] = v
	}
	return cp
}

// GetFiles returns a copy of the current file-score map. Mutations to the
// returned map do not affect the board.
//
// Reserved for future extensions that consume per-file relevance scores
// discovered by parallel tasks (e.g. boosting file candidates beyond what
// DB progress patterns provide). Call AddFile to populate it.
func (b *DiscoveryBoard) GetFiles() map[string]float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	cp := make(map[string]float64, len(b.files))
	for k, v := range b.files {
		cp[k] = v
	}
	return cp
}

// Patterns converts the board's pattern map into a []ProgressPattern for use
// with file-selector scoring. The supplied ticketID is embedded in each entry.
func (b *DiscoveryBoard) Patterns(ticketID string) []ProgressPattern {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]ProgressPattern, 0, len(b.patterns))
	for k, v := range b.patterns {
		out = append(out, ProgressPattern{
			TicketID:     ticketID,
			PatternKey:   k,
			PatternValue: v,
		})
	}
	return out
}
