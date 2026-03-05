package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// localTicket is the JSON shape of a local ticket file.
type localTicket struct {
	ExternalID         string   `json:"external_id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria string   `json:"acceptance_criteria"`
	Labels             []string `json:"labels"`
	Priority           string   `json:"priority"`
	Status             string   `json:"status"`
}

// LocalFileTracker reads tickets from JSON files in a directory.
// Used for local development and testing.
type LocalFileTracker struct {
	dir         string
	pickupLabel string
}

// NewLocalFileTracker creates a local file tracker.
func NewLocalFileTracker(dir, pickupLabel string) *LocalFileTracker {
	return &LocalFileTracker{dir: dir, pickupLabel: pickupLabel}
}

func (t *LocalFileTracker) ticketsDir() string {
	return filepath.Join(t.dir, "tickets")
}

func (t *LocalFileTracker) FetchReadyTickets(ctx context.Context) ([]Ticket, error) {
	entries, err := os.ReadDir(t.ticketsDir())
	if err != nil {
		return nil, fmt.Errorf("reading tickets dir: %w", err)
	}

	var tickets []Ticket
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		// Skip comment files
		if strings.HasSuffix(entry.Name(), ".comments.json") {
			continue
		}

		lt, err := t.readTicketFile(filepath.Join(t.ticketsDir(), entry.Name()))
		if err != nil {
			log.Warn().Str("file", entry.Name()).Err(err).Msg("skipping unreadable ticket file")
			continue
		}
		if !containsLabel(lt.Labels, t.pickupLabel) {
			continue
		}
		tickets = append(tickets, toTicket(lt))
	}
	return tickets, nil
}

func (t *LocalFileTracker) GetTicket(ctx context.Context, externalID string) (*Ticket, error) {
	path := filepath.Join(t.ticketsDir(), externalID+".json")
	lt, err := t.readTicketFile(path)
	if err != nil {
		return nil, fmt.Errorf("ticket %s not found: %w", externalID, err)
	}
	ticket := toTicket(lt)
	return &ticket, nil
}

func (t *LocalFileTracker) UpdateStatus(ctx context.Context, externalID, status string) error {
	return t.updateField(externalID, func(lt *localTicket) { lt.Status = status })
}

func (t *LocalFileTracker) AddComment(ctx context.Context, externalID, comment string) error {
	commentsFile := filepath.Join(t.ticketsDir(), externalID+".comments.json")
	var comments []map[string]string

	if data, err := os.ReadFile(commentsFile); err == nil {
		json.Unmarshal(data, &comments) //nolint:errcheck
	}

	comments = append(comments, map[string]string{
		"author":     "foreman",
		"body":       comment,
		"created_at": time.Now().Format(time.RFC3339),
	})

	data, err := json.MarshalIndent(comments, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling comments: %w", err)
	}
	return os.WriteFile(commentsFile, data, 0o644)
}

func (t *LocalFileTracker) AttachPR(ctx context.Context, externalID, prURL string) error {
	return t.AddComment(ctx, externalID, fmt.Sprintf("PR created: %s", prURL))
}

func (t *LocalFileTracker) AddLabel(ctx context.Context, externalID, label string) error {
	return t.updateField(externalID, func(lt *localTicket) {
		if !containsLabel(lt.Labels, label) {
			lt.Labels = append(lt.Labels, label)
		}
	})
}

func (t *LocalFileTracker) RemoveLabel(ctx context.Context, externalID, label string) error {
	return t.updateField(externalID, func(lt *localTicket) {
		filtered := make([]string, 0, len(lt.Labels))
		for _, l := range lt.Labels {
			if l != label {
				filtered = append(filtered, l)
			}
		}
		lt.Labels = filtered
	})
}

func (t *LocalFileTracker) HasLabel(ctx context.Context, externalID, label string) (bool, error) {
	lt, err := t.readTicketFile(filepath.Join(t.ticketsDir(), externalID+".json"))
	if err != nil {
		return false, err
	}
	return containsLabel(lt.Labels, label), nil
}

func (t *LocalFileTracker) ProviderName() string { return "local_file" }

func (t *LocalFileTracker) readTicketFile(path string) (*localTicket, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lt localTicket
	if err := json.Unmarshal(data, &lt); err != nil {
		return nil, err
	}
	return &lt, nil
}

func (t *LocalFileTracker) updateField(externalID string, fn func(*localTicket)) error {
	path := filepath.Join(t.ticketsDir(), externalID+".json")
	lt, err := t.readTicketFile(path)
	if err != nil {
		return err
	}
	fn(lt)
	data, err := json.MarshalIndent(lt, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling ticket: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

func toTicket(lt *localTicket) Ticket {
	return Ticket{
		ExternalID:         lt.ExternalID,
		Title:              lt.Title,
		Description:        lt.Description,
		AcceptanceCriteria: lt.AcceptanceCriteria,
		Labels:             lt.Labels,
		Priority:           lt.Priority,
	}
}

func containsLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

var _ IssueTracker = (*LocalFileTracker)(nil)
