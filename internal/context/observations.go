package context

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Observation records a single event observed during development.
type Observation struct {
	Type    string            `json:"type"`
	Details map[string]string `json:"details,omitempty"`
	File    string            `json:"file,omitempty"`
	Time    time.Time         `json:"ts"`
}

// ObservationLog manages a JSONL file of observations.
type ObservationLog struct {
	workDir string
}

// NewObservationLog creates an ObservationLog for the given working directory.
func NewObservationLog(workDir string) *ObservationLog {
	return &ObservationLog{workDir: workDir}
}

func (o *ObservationLog) filePath() string {
	return filepath.Join(o.workDir, ".foreman", "observations.jsonl")
}

// Append writes a single observation to the JSONL file.
func (o *ObservationLog) Append(obs Observation) error {
	dir := filepath.Join(o.workDir, ".foreman")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create .foreman dir: %w", err)
	}

	f, err := os.OpenFile(o.filePath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open observations file: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(obs)
	if err != nil {
		return fmt.Errorf("marshal observation: %w", err)
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write observation: %w", err)
	}

	return nil
}

// ReadFrom reads observations starting from the given byte offset.
// Returns the observations and the new cursor position.
func (o *ObservationLog) ReadFrom(cursor int64) ([]Observation, int64, error) {
	f, err := os.Open(o.filePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, cursor, fmt.Errorf("open observations file: %w", err)
	}
	defer f.Close()

	if _, err := f.Seek(cursor, io.SeekStart); err != nil {
		return nil, cursor, fmt.Errorf("seek: %w", err)
	}

	var observations []Observation
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var obs Observation
		if err := json.Unmarshal(line, &obs); err != nil {
			continue // skip malformed lines
		}
		observations = append(observations, obs)
	}

	if err := scanner.Err(); err != nil {
		return observations, cursor, fmt.Errorf("scan: %w", err)
	}

	// Calculate new cursor position
	pos, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		// Fallback: estimate from what we read
		return observations, cursor, nil
	}

	return observations, pos, nil
}
