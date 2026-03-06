# WhatsApp Bidirectional Channel — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a WhatsApp bidirectional messaging channel that creates tickets in Foreman's DB, handles clarifications, sends lifecycle notifications, and supports daemon commands.

**Architecture:** New `Channel` interface (separate from `IssueTracker`) with a `ChannelRouter` for message classification/routing. WhatsApp implements `Channel` via `whatsmeow`. Orchestrator calls `channel.Send()` at lifecycle transitions. Daemon implements `CommandHandler` for status/pause/resume/cost.

**Tech Stack:** Go, whatsmeow (WhatsApp Web protocol), pelletier/go-toml v1 (config persistence), zerolog (logging)

**Design doc:** `docs/plans/2026-03-06-whatsapp-channel-design.md`

---

## Task 1: Models — ChannelConfig and Pairing

**Files:**
- Modify: `internal/models/config.go:20` (add Channel field to Config struct)
- Create: `internal/models/pairing.go`
- Modify: `internal/models/ticket.go:36` (add ChannelSenderID field)

**Step 1: Add ChannelConfig to Config struct**

In `internal/models/config.go`, add the `Channel` field inside the `Config` struct (after line 20, before the closing brace at line 21):

```go
Channel   ChannelConfig     `mapstructure:"channel"`
```

Then append these structs after the last struct in the file (after line 252):

```go
type ChannelConfig struct {
	Provider string                `mapstructure:"provider"` // "" (disabled) | "whatsapp"
	WhatsApp WhatsAppChannelConfig `mapstructure:"whatsapp"`
}

type WhatsAppChannelConfig struct {
	SessionDB      string   `mapstructure:"session_db"`
	PairingMode    string   `mapstructure:"pairing_mode"`
	DMPolicy       string   `mapstructure:"dm_policy"`
	AllowedNumbers []string `mapstructure:"allowed_numbers"`
}
```

**Step 2: Add ChannelSenderID to Ticket struct**

In `internal/models/ticket.go`, add inside the Ticket struct (after the `ParentTicketID` field, around line 26):

```go
ChannelSenderID          string
```

**Step 3: Create Pairing model**

Create `internal/models/pairing.go`:

```go
package models

import "time"

type Pairing struct {
	Code      string
	SenderID  string
	Channel   string
	ExpiresAt time.Time
	CreatedAt time.Time
}
```

**Step 4: Run tests to verify nothing breaks**

Run: `go build ./...`
Expected: BUILD SUCCESS (no compilation errors)

**Step 5: Commit**

```bash
git add internal/models/config.go internal/models/ticket.go internal/models/pairing.go
git commit -m "feat(models): add ChannelConfig, Pairing model, and ChannelSenderID on Ticket"
```

---

## Task 2: DB Schema — pending_pairings table and channel_sender_id column

**Files:**
- Modify: `internal/db/schema.go:132` (add new table and column)

**Step 1: Add channel_sender_id column to tickets table**

In `internal/db/schema.go`, find the tickets table CREATE statement. Add before the closing parenthesis of the tickets table (around line 31):

```sql
channel_sender_id TEXT DEFAULT '',
```

**Step 2: Add pending_pairings table**

After the last CREATE TABLE statement (around line 132, after auth_tokens table), add:

```sql
CREATE TABLE IF NOT EXISTS pending_pairings (
    code        TEXT PRIMARY KEY,
    sender_id   TEXT NOT NULL,
    channel     TEXT NOT NULL DEFAULT 'whatsapp',
    expires_at  DATETIME NOT NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

**Step 3: Run tests**

Run: `go build ./...`
Expected: BUILD SUCCESS

**Step 4: Commit**

```bash
git add internal/db/schema.go
git commit -m "feat(db): add pending_pairings table and channel_sender_id column"
```

---

## Task 3: DB Interface — Add pairing and channel query methods

**Files:**
- Modify: `internal/db/db.go:55` (add new methods before io.Closer)
- Modify: `internal/db/sqlite.go` (implement methods)
- Modify: `internal/db/postgres.go` (implement methods)
- Create: `internal/db/pairing_test.go`

**Step 1: Write the failing test**

Create `internal/db/pairing_test.go`:

```go
package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/db"
)

func TestPairingCRUD(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	ctx := context.Background()

	// Create pairing
	expiresAt := time.Now().Add(10 * time.Minute)
	err := database.CreatePairing(ctx, "XKCD-7291", "+84123456789", "whatsapp", expiresAt)
	if err != nil {
		t.Fatalf("CreatePairing: %v", err)
	}

	// Get pairing
	p, err := database.GetPairing(ctx, "XKCD-7291")
	if err != nil {
		t.Fatalf("GetPairing: %v", err)
	}
	if p.SenderID != "+84123456789" {
		t.Errorf("SenderID = %q, want %q", p.SenderID, "+84123456789")
	}
	if p.Channel != "whatsapp" {
		t.Errorf("Channel = %q, want %q", p.Channel, "whatsapp")
	}

	// List pairings
	pairings, err := database.ListPairings(ctx, "whatsapp")
	if err != nil {
		t.Fatalf("ListPairings: %v", err)
	}
	if len(pairings) != 1 {
		t.Fatalf("ListPairings len = %d, want 1", len(pairings))
	}

	// Delete pairing
	err = database.DeletePairing(ctx, "XKCD-7291")
	if err != nil {
		t.Fatalf("DeletePairing: %v", err)
	}
	p, err = database.GetPairing(ctx, "XKCD-7291")
	if err != nil {
		t.Fatalf("GetPairing after delete: %v", err)
	}
	if p != nil {
		t.Error("expected nil after delete")
	}
}

func TestDeleteExpiredPairings(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	ctx := context.Background()

	// Create one expired, one valid
	past := time.Now().Add(-1 * time.Minute)
	future := time.Now().Add(10 * time.Minute)
	database.CreatePairing(ctx, "EXPIRED1", "+84111", "whatsapp", past)
	database.CreatePairing(ctx, "VALID001", "+84222", "whatsapp", future)

	err := database.DeleteExpiredPairings(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredPairings: %v", err)
	}

	pairings, _ := database.ListPairings(ctx, "whatsapp")
	if len(pairings) != 1 {
		t.Fatalf("expected 1 pairing after cleanup, got %d", len(pairings))
	}
	if pairings[0].Code != "VALID001" {
		t.Errorf("expected VALID001, got %s", pairings[0].Code)
	}
}

func TestFindActiveClarification(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	ctx := context.Background()

	// Create a ticket with channel_sender_id and clarification_needed status
	err := database.CreateTicket(ctx, db.CreateTicketParams{
		ID:              "t-1",
		ExternalID:      "ext-1",
		Title:           "Test ticket",
		Description:     "desc",
		Status:          "clarification_needed",
		ChannelSenderID: "+84123456789",
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	// Find clarification for sender
	ticket, err := database.FindActiveClarification(ctx, "+84123456789")
	if err != nil {
		t.Fatalf("FindActiveClarification: %v", err)
	}
	if ticket == nil {
		t.Fatal("expected ticket, got nil")
	}
	if ticket.ID != "t-1" {
		t.Errorf("ticket ID = %q, want %q", ticket.ID, "t-1")
	}

	// No clarification for unknown sender
	ticket, err = database.FindActiveClarification(ctx, "+84999999999")
	if err != nil {
		t.Fatalf("FindActiveClarification unknown: %v", err)
	}
	if ticket != nil {
		t.Error("expected nil for unknown sender")
	}
}
```

Note: The test helper `setupTestDB` should already exist in the db_test package. If not, check existing test files for the pattern and use the same helper. The `CreateTicketParams` struct may need a `ChannelSenderID` field — adapt to the existing pattern used in `sqlite.go` for `CreateTicket`.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -run TestPairing -v`
Expected: FAIL — methods not defined on Database interface

**Step 3: Add methods to Database interface**

In `internal/db/db.go`, add before `io.Closer` (around line 56):

```go
	// Pairing
	CreatePairing(ctx context.Context, code, senderID, channel string, expiresAt time.Time) error
	GetPairing(ctx context.Context, code string) (*models.Pairing, error)
	DeletePairing(ctx context.Context, code string) error
	ListPairings(ctx context.Context, channel string) ([]models.Pairing, error)
	DeleteExpiredPairings(ctx context.Context) error

	// Channel queries
	FindActiveClarification(ctx context.Context, senderID string) (*models.Ticket, error)
```

Add the `models` import if not already present.

**Step 4: Implement in SQLite**

In `internal/db/sqlite.go`, add at the end of the file (before any closing test helpers):

```go
func (s *SQLiteDB) CreatePairing(ctx context.Context, code, senderID, channel string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pending_pairings (code, sender_id, channel, expires_at) VALUES (?, ?, ?, ?)`,
		code, senderID, channel, expiresAt)
	if err != nil {
		return fmt.Errorf("create pairing: %w", err)
	}
	return nil
}

func (s *SQLiteDB) GetPairing(ctx context.Context, code string) (*models.Pairing, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT code, sender_id, channel, expires_at, created_at FROM pending_pairings WHERE code = ?`, code)
	var p models.Pairing
	err := row.Scan(&p.Code, &p.SenderID, &p.Channel, &p.ExpiresAt, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pairing: %w", err)
	}
	return &p, nil
}

func (s *SQLiteDB) DeletePairing(ctx context.Context, code string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_pairings WHERE code = ?`, code)
	if err != nil {
		return fmt.Errorf("delete pairing: %w", err)
	}
	return nil
}

func (s *SQLiteDB) ListPairings(ctx context.Context, channel string) ([]models.Pairing, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT code, sender_id, channel, expires_at, created_at FROM pending_pairings WHERE channel = ? ORDER BY created_at`, channel)
	if err != nil {
		return nil, fmt.Errorf("list pairings: %w", err)
	}
	defer rows.Close()
	var result []models.Pairing
	for rows.Next() {
		var p models.Pairing
		if err := rows.Scan(&p.Code, &p.SenderID, &p.Channel, &p.ExpiresAt, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan pairing: %w", err)
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *SQLiteDB) DeleteExpiredPairings(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_pairings WHERE expires_at < datetime('now')`)
	if err != nil {
		return fmt.Errorf("delete expired pairings: %w", err)
	}
	return nil
}

func (s *SQLiteDB) FindActiveClarification(ctx context.Context, senderID string) (*models.Ticket, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, channel_sender_id FROM tickets WHERE channel_sender_id = ? AND status = 'clarification_needed' LIMIT 1`, senderID)
	var t models.Ticket
	err := row.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &t.Status, &t.ChannelSenderID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find active clarification: %w", err)
	}
	return &t, nil
}
```

**Step 5: Implement in Postgres**

In `internal/db/postgres.go`, add the same methods with `$1, $2, $3` placeholders instead of `?`, and use `NOW()` instead of `datetime('now')`:

```go
func (p *PostgresDB) CreatePairing(ctx context.Context, code, senderID, channel string, expiresAt time.Time) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO pending_pairings (code, sender_id, channel, expires_at) VALUES ($1, $2, $3, $4)`,
		code, senderID, channel, expiresAt)
	if err != nil {
		return fmt.Errorf("create pairing: %w", err)
	}
	return nil
}

func (p *PostgresDB) GetPairing(ctx context.Context, code string) (*models.Pairing, error) {
	row := p.db.QueryRowContext(ctx,
		`SELECT code, sender_id, channel, expires_at, created_at FROM pending_pairings WHERE code = $1`, code)
	var pr models.Pairing
	err := row.Scan(&pr.Code, &pr.SenderID, &pr.Channel, &pr.ExpiresAt, &pr.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pairing: %w", err)
	}
	return &pr, nil
}

func (p *PostgresDB) DeletePairing(ctx context.Context, code string) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM pending_pairings WHERE code = $1`, code)
	if err != nil {
		return fmt.Errorf("delete pairing: %w", err)
	}
	return nil
}

func (p *PostgresDB) ListPairings(ctx context.Context, channel string) ([]models.Pairing, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT code, sender_id, channel, expires_at, created_at FROM pending_pairings WHERE channel = $1 ORDER BY created_at`, channel)
	if err != nil {
		return nil, fmt.Errorf("list pairings: %w", err)
	}
	defer rows.Close()
	var result []models.Pairing
	for rows.Next() {
		var pr models.Pairing
		if err := rows.Scan(&pr.Code, &pr.SenderID, &pr.Channel, &pr.ExpiresAt, &pr.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan pairing: %w", err)
		}
		result = append(result, pr)
	}
	return result, rows.Err()
}

func (p *PostgresDB) DeleteExpiredPairings(ctx context.Context) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM pending_pairings WHERE expires_at < NOW()`)
	if err != nil {
		return fmt.Errorf("delete expired pairings: %w", err)
	}
	return nil
}

func (p *PostgresDB) FindActiveClarification(ctx context.Context, senderID string) (*models.Ticket, error) {
	row := p.db.QueryRowContext(ctx,
		`SELECT id, external_id, title, description, status, channel_sender_id FROM tickets WHERE channel_sender_id = $1 AND status = 'clarification_needed' LIMIT 1`, senderID)
	var t models.Ticket
	err := row.Scan(&t.ID, &t.ExternalID, &t.Title, &t.Description, &t.Status, &t.ChannelSenderID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find active clarification: %w", err)
	}
	return &t, nil
}
```

**Step 6: Update existing CreateTicket to handle ChannelSenderID**

Check the existing `CreateTicket` implementation in both sqlite.go and postgres.go. Add the `channel_sender_id` column to the INSERT statement and scan. The exact change depends on the current signature — adapt the params struct or add the field.

**Step 7: Run tests**

Run: `go test ./internal/db/ -run "TestPairing|TestFindActive" -v`
Expected: PASS

**Step 8: Commit**

```bash
git add internal/db/
git commit -m "feat(db): implement pairing CRUD and FindActiveClarification"
```

---

## Task 4: Channel Interface

**Files:**
- Create: `internal/channel/channel.go`

**Step 1: Create the Channel interface**

```go
package channel

import (
	"context"
	"time"
)

// Channel is a bidirectional messaging transport (e.g., WhatsApp).
// It receives inbound messages and sends outbound notifications.
// Completely separate from tracker.IssueTracker.
type Channel interface {
	// Start begins listening for inbound messages.
	// Blocks until ctx is cancelled or a fatal error occurs.
	// Must be called in a goroutine by the caller.
	Start(ctx context.Context, handler InboundHandler) error

	// Stop disconnects the channel transport. Called by daemon on shutdown
	// or when the channel reports an unrecoverable error (e.g., session revoked).
	Stop() error

	// Send sends a text message to a recipient.
	Send(ctx context.Context, recipientID string, message string) error

	// Name returns the channel name (e.g., "whatsapp").
	Name() string
}

// InboundMessage represents a message received from a channel.
type InboundMessage struct {
	SenderID  string
	Body      string
	Timestamp time.Time
}

// InboundHandler processes messages received by a Channel.
type InboundHandler interface {
	HandleMessage(ctx context.Context, msg InboundMessage) error
}

// CommandHandler provides daemon state to the channel router.
// Implemented by the daemon — the channel layer never imports daemon internals.
type CommandHandler interface {
	Status(ctx context.Context) (string, error)
	Pause(ctx context.Context) (string, error)
	Resume(ctx context.Context) (string, error)
	Cost(ctx context.Context) (string, error)
}
```

**Step 2: Run build**

Run: `go build ./internal/channel/...`
Expected: BUILD SUCCESS

**Step 3: Commit**

```bash
git add internal/channel/channel.go
git commit -m "feat(channel): define Channel, InboundHandler, and CommandHandler interfaces"
```

---

## Task 5: Classifier — Prefix Match + LLM Fallback

**Files:**
- Create: `internal/channel/classifier.go`
- Create: `internal/channel/classifier_test.go`

**Step 1: Write the failing test**

Create `internal/channel/classifier_test.go`:

```go
package channel

import (
	"context"
	"testing"
)

func TestClassifier_PrefixCommands(t *testing.T) {
	c := NewClassifier(nil) // no LLM needed for prefix tests

	tests := []struct {
		body    string
		kind    string
		command string
	}{
		{"/status", "command", "status"},
		{"/pause", "command", "pause"},
		{"/resume", "command", "resume"},
		{"/cost", "command", "cost"},
		{"/STATUS", "command", "status"},
		{"/pause please", "command", "pause"},
		{"Build a login page", "new_ticket", ""},
		{"", "new_ticket", ""},
	}

	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			result := c.Classify(context.Background(), tt.body)
			if result.Kind != tt.kind {
				t.Errorf("Classify(%q).Kind = %q, want %q", tt.body, result.Kind, tt.kind)
			}
			if result.Command != tt.command {
				t.Errorf("Classify(%q).Command = %q, want %q", tt.body, result.Command, tt.command)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/channel/ -run TestClassifier -v`
Expected: FAIL — NewClassifier not defined

**Step 3: Implement classifier**

Create `internal/channel/classifier.go`:

```go
package channel

import (
	"context"
	"strings"

	"github.com/canhta/foreman/internal/llm"
	"github.com/canhta/foreman/internal/models"
)

// MessageKind describes the classification of an inbound message.
type MessageKind struct {
	Kind    string // "command" | "new_ticket"
	Command string // e.g., "status", "pause" — only set when Kind == "command"
}

// Classifier determines the intent of an inbound channel message.
// Uses prefix matching first, then LLM fallback for ambiguous messages.
type Classifier struct {
	llm llm.LlmProvider // optional, nil disables LLM fallback
}

// NewClassifier creates a classifier. Pass nil for llm to disable LLM fallback.
func NewClassifier(llm llm.LlmProvider) *Classifier {
	return &Classifier{llm: llm}
}

var prefixCommands = map[string]string{
	"/status": "status",
	"/pause":  "pause",
	"/resume": "resume",
	"/cost":   "cost",
}

// Classify determines the intent of a message body.
// Does not access DB or sender context — pure text classification.
func (c *Classifier) Classify(ctx context.Context, body string) MessageKind {
	lower := strings.ToLower(strings.TrimSpace(body))

	// 1. Prefix match (deterministic, zero cost)
	for prefix, cmd := range prefixCommands {
		if lower == prefix || strings.HasPrefix(lower, prefix+" ") {
			return MessageKind{Kind: "command", Command: cmd}
		}
	}

	// 2. LLM fallback for ambiguous messages
	if c.llm != nil {
		if kind := c.classifyWithLLM(ctx, body); kind != nil {
			return *kind
		}
	}

	// 3. Default: new ticket
	return MessageKind{Kind: "new_ticket"}
}

func (c *Classifier) classifyWithLLM(ctx context.Context, body string) *MessageKind {
	resp, err := c.llm.Complete(ctx, models.LlmRequest{
		SystemPrompt: `You classify user messages into exactly one category.
Reply with ONLY one word: "status", "pause", "resume", "cost", or "ticket".
- "status" = user wants to know what's running or current state
- "pause" = user wants to stop/pause work
- "resume" = user wants to start/resume work
- "cost" = user wants to know spending or budget
- "ticket" = anything else (new task, question, request)`,
		UserPrompt: body,
	})
	if err != nil {
		return nil // fallback to default on LLM error
	}

	classification := strings.ToLower(strings.TrimSpace(resp.Content))
	switch classification {
	case "status", "pause", "resume", "cost":
		return &MessageKind{Kind: "command", Command: classification}
	default:
		return nil // "ticket" or unrecognized → default to new_ticket
	}
}
```

**Step 4: Run tests**

Run: `go test ./internal/channel/ -run TestClassifier -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/channel/classifier.go internal/channel/classifier_test.go
git commit -m "feat(channel): implement message classifier with prefix match and LLM fallback"
```

---

## Task 6: PairingManager

**Files:**
- Create: `internal/channel/pairing.go`
- Create: `internal/channel/pairing_test.go`

**Step 1: Write the failing test**

Create `internal/channel/pairing_test.go`:

```go
package channel

import (
	"context"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
)

// mockPairingDB implements only the pairing-related DB methods.
type mockPairingDB struct {
	pairings map[string]models.Pairing
}

func newMockPairingDB() *mockPairingDB {
	return &mockPairingDB{pairings: make(map[string]models.Pairing)}
}

func (m *mockPairingDB) CreatePairing(_ context.Context, code, senderID, channel string, expiresAt time.Time) error {
	m.pairings[code] = models.Pairing{Code: code, SenderID: senderID, Channel: channel, ExpiresAt: expiresAt, CreatedAt: time.Now()}
	return nil
}

func (m *mockPairingDB) GetPairing(_ context.Context, code string) (*models.Pairing, error) {
	p, ok := m.pairings[code]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

func (m *mockPairingDB) DeletePairing(_ context.Context, code string) error {
	delete(m.pairings, code)
	return nil
}

func TestPairingManager_GenerateCode(t *testing.T) {
	db := newMockPairingDB()
	pm := NewPairingManager(db, "whatsapp")

	code, err := pm.Challenge(context.Background(), "+84123456789")
	if err != nil {
		t.Fatalf("Challenge: %v", err)
	}
	if len(code) != 9 { // XXXX-XXXX format
		t.Errorf("code length = %d, want 9 (XXXX-XXXX)", len(code))
	}
	if code[4] != '-' {
		t.Errorf("code[4] = %c, want '-'", code[4])
	}

	// Verify stored in DB
	p, _ := db.GetPairing(context.Background(), code)
	if p == nil {
		t.Fatal("pairing not found in DB after Challenge")
	}
	if p.SenderID != "+84123456789" {
		t.Errorf("SenderID = %q, want %q", p.SenderID, "+84123456789")
	}
}

func TestPairingManager_Approve(t *testing.T) {
	db := newMockPairingDB()
	pm := NewPairingManager(db, "whatsapp")

	code, _ := pm.Challenge(context.Background(), "+84123456789")

	senderID, err := pm.Approve(context.Background(), code)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if senderID != "+84123456789" {
		t.Errorf("senderID = %q, want %q", senderID, "+84123456789")
	}

	// Verify deleted from DB
	p, _ := db.GetPairing(context.Background(), code)
	if p != nil {
		t.Error("pairing should be deleted after Approve")
	}
}

func TestPairingManager_ApproveInvalidCode(t *testing.T) {
	db := newMockPairingDB()
	pm := NewPairingManager(db, "whatsapp")

	_, err := pm.Approve(context.Background(), "INVALID")
	if err == nil {
		t.Fatal("expected error for invalid code")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/channel/ -run TestPairingManager -v`
Expected: FAIL — NewPairingManager not defined

**Step 3: Implement PairingManager**

Create `internal/channel/pairing.go`:

```go
package channel

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/canhta/foreman/internal/models"
)

// PairingDB is the subset of db.Database needed by PairingManager.
type PairingDB interface {
	CreatePairing(ctx context.Context, code, senderID, channel string, expiresAt time.Time) error
	GetPairing(ctx context.Context, code string) (*models.Pairing, error)
	DeletePairing(ctx context.Context, code string) error
}

// Allowlist checks whether a sender is authorized to submit tickets.
type Allowlist struct {
	numbers map[string]bool
}

// NewAllowlist creates an allowlist from a slice of E.164 phone numbers.
func NewAllowlist(numbers []string) *Allowlist {
	m := make(map[string]bool, len(numbers))
	for _, n := range numbers {
		m[n] = true
	}
	return &Allowlist{numbers: m}
}

// IsAllowed returns true if the senderID (JID string) contains an allowed number.
func (a *Allowlist) IsAllowed(senderID string) bool {
	// whatsmeow JIDs look like "84123456789@s.whatsapp.net"
	// We check if any allowed number's digits are a prefix of the JID
	for n := range a.numbers {
		// Strip the leading "+" for comparison
		digits := n[1:] // "+84123" -> "84123"
		if len(senderID) >= len(digits) && senderID[:len(digits)] == digits {
			return true
		}
	}
	return false
}

// Add adds a number to the allowlist.
func (a *Allowlist) Add(number string) {
	a.numbers[number] = true
}

// Remove removes a number from the allowlist.
func (a *Allowlist) Remove(number string) {
	delete(a.numbers, number)
}

// PairingManager handles unknown sender pairing flow.
type PairingManager struct {
	db      PairingDB
	channel string
	ttl     time.Duration
}

// NewPairingManager creates a new PairingManager.
func NewPairingManager(db PairingDB, channel string) *PairingManager {
	return &PairingManager{db: db, channel: channel, ttl: 10 * time.Minute}
}

// Challenge generates a pairing code for an unknown sender and stores it in DB.
// Returns the code in XXXX-XXXX format.
func (pm *PairingManager) Challenge(ctx context.Context, senderID string) (string, error) {
	code := generateCode()
	expiresAt := time.Now().Add(pm.ttl)
	if err := pm.db.CreatePairing(ctx, code, senderID, pm.channel, expiresAt); err != nil {
		return "", fmt.Errorf("create pairing: %w", err)
	}
	return code, nil
}

// Approve validates a pairing code, returns the sender ID, and deletes the pairing.
func (pm *PairingManager) Approve(ctx context.Context, code string) (string, error) {
	p, err := pm.db.GetPairing(ctx, code)
	if err != nil {
		return "", fmt.Errorf("get pairing: %w", err)
	}
	if p == nil {
		return "", fmt.Errorf("pairing code %q not found or expired", code)
	}
	if time.Now().After(p.ExpiresAt) {
		pm.db.DeletePairing(ctx, code)
		return "", fmt.Errorf("pairing code %q has expired", code)
	}
	senderID := p.SenderID
	if err := pm.db.DeletePairing(ctx, code); err != nil {
		return "", fmt.Errorf("delete pairing: %w", err)
	}
	return senderID, nil
}

const codeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no I/O/0/1 for readability

func generateCode() string {
	b := make([]byte, 8)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(codeChars))))
		b[i] = codeChars[n.Int64()]
	}
	return string(b[:4]) + "-" + string(b[4:])
}
```

**Step 4: Run tests**

Run: `go test ./internal/channel/ -run TestPairingManager -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/channel/pairing.go internal/channel/pairing_test.go
git commit -m "feat(channel): implement PairingManager with DB-backed code storage"
```

---

## Task 7: ChannelRouter

**Files:**
- Create: `internal/channel/router.go`
- Create: `internal/channel/router_test.go`

**Step 1: Write the failing test**

Create `internal/channel/router_test.go`:

```go
package channel

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog"
)

type mockChannel struct {
	mu   sync.Mutex
	sent []sentMessage
}

type sentMessage struct {
	recipientID string
	message     string
}

func (m *mockChannel) Start(ctx context.Context, handler InboundHandler) error {
	<-ctx.Done()
	return nil
}
func (m *mockChannel) Stop() error    { return nil }
func (m *mockChannel) Name() string   { return "mock" }
func (m *mockChannel) Send(_ context.Context, recipientID, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, sentMessage{recipientID, message})
	return nil
}
func (m *mockChannel) lastSent() *sentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sent) == 0 {
		return nil
	}
	return &m.sent[len(m.sent)-1]
}

type mockRouterDB struct {
	mockPairingDB
	tickets         []models.Ticket
	createdTickets  []models.Ticket
}

func (m *mockRouterDB) FindActiveClarification(_ context.Context, senderID string) (*models.Ticket, error) {
	for _, t := range m.tickets {
		if t.ChannelSenderID == senderID && string(t.Status) == "clarification_needed" {
			return &t, nil
		}
	}
	return nil, nil
}

type mockCommands struct {
	statusReply string
}

func (m *mockCommands) Status(_ context.Context) (string, error) { return m.statusReply, nil }
func (m *mockCommands) Pause(_ context.Context) (string, error)  { return "paused", nil }
func (m *mockCommands) Resume(_ context.Context) (string, error) { return "resumed", nil }
func (m *mockCommands) Cost(_ context.Context) (string, error)   { return "$42 today", nil }

func TestRouter_AllowlistReject(t *testing.T) {
	ch := &mockChannel{}
	allowlist := NewAllowlist([]string{"+84111111111"})
	router := NewRouter(ch, nil, NewClassifier(nil), allowlist, nil, nil, zerolog.Nop())

	err := router.HandleMessage(context.Background(), InboundMessage{
		SenderID: "84999999999@s.whatsapp.net",
		Body:     "hello",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	// Message from non-allowlisted sender without pairing manager should be silently dropped
}

func TestRouter_CommandRouting(t *testing.T) {
	ch := &mockChannel{}
	allowlist := NewAllowlist([]string{"+84111111111"})
	cmds := &mockCommands{statusReply: "3 tickets active"}
	db := &mockRouterDB{mockPairingDB: *newMockPairingDB()}
	router := NewRouter(ch, db, NewClassifier(nil), allowlist, nil, cmds, zerolog.Nop())

	err := router.HandleMessage(context.Background(), InboundMessage{
		SenderID:  "84111111111@s.whatsapp.net",
		Body:      "/status",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	msg := ch.lastSent()
	if msg == nil {
		t.Fatal("expected a sent message")
	}
	if msg.message != "3 tickets active" {
		t.Errorf("sent = %q, want %q", msg.message, "3 tickets active")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/channel/ -run TestRouter -v`
Expected: FAIL — NewRouter not defined

**Step 3: Implement router**

Create `internal/channel/router.go`:

```go
package channel

import (
	"context"
	"fmt"

	"github.com/canhta/foreman/internal/models"
	"github.com/rs/zerolog"
)

// RouterDB is the subset of db.Database needed by ChannelRouter.
type RouterDB interface {
	PairingDB
	FindActiveClarification(ctx context.Context, senderID string) (*models.Ticket, error)
	CreateTicket(ctx context.Context, params interface{}) error // adapt to actual CreateTicket signature
	UpdateTicketStatus(ctx context.Context, id string, status models.TicketStatus) error
}

// ChannelRouter implements InboundHandler and routes messages to the right action.
type ChannelRouter struct {
	channel    Channel
	db         RouterDB
	classifier *Classifier
	allowlist  *Allowlist
	pairing    *PairingManager
	commands   CommandHandler
	logger     zerolog.Logger
}

// NewRouter creates a ChannelRouter.
func NewRouter(
	channel Channel,
	db RouterDB,
	classifier *Classifier,
	allowlist *Allowlist,
	pairing *PairingManager,
	commands CommandHandler,
	logger zerolog.Logger,
) *ChannelRouter {
	return &ChannelRouter{
		channel:    channel,
		db:         db,
		classifier: classifier,
		allowlist:  allowlist,
		pairing:    pairing,
		commands:   commands,
		logger:     logger.With().Str("component", "channel-router").Logger(),
	}
}

// HandleMessage processes an inbound message from the channel.
func (r *ChannelRouter) HandleMessage(ctx context.Context, msg InboundMessage) error {
	// 1. Check allowlist
	if !r.allowlist.IsAllowed(msg.SenderID) {
		return r.handleUnknownSender(ctx, msg)
	}

	// 2. Check for active clarification (before classifier — context makes intent obvious)
	if r.db != nil {
		ticket, err := r.db.FindActiveClarification(ctx, msg.SenderID)
		if err != nil {
			r.logger.Error().Err(err).Msg("failed to check clarification")
		} else if ticket != nil {
			return r.handleClarificationReply(ctx, msg, ticket)
		}
	}

	// 3. Classify message
	kind := r.classifier.Classify(ctx, msg.Body)

	switch kind.Kind {
	case "command":
		return r.handleCommand(ctx, msg, kind.Command)
	default:
		return r.handleNewTicket(ctx, msg)
	}
}

func (r *ChannelRouter) handleUnknownSender(ctx context.Context, msg InboundMessage) error {
	if r.pairing == nil {
		r.logger.Warn().Str("sender", msg.SenderID).Msg("rejected message from unknown sender")
		return nil
	}

	code, err := r.pairing.Challenge(ctx, msg.SenderID)
	if err != nil {
		r.logger.Error().Err(err).Msg("failed to create pairing challenge")
		return nil
	}

	reply := fmt.Sprintf("Pairing code: %s\nRun: foreman pairing approve %s", code, code)
	if err := r.channel.Send(ctx, msg.SenderID, reply); err != nil {
		r.logger.Error().Err(err).Msg("failed to send pairing challenge")
	}
	return nil
}

func (r *ChannelRouter) handleCommand(ctx context.Context, msg InboundMessage, command string) error {
	if r.commands == nil {
		return nil
	}

	var reply string
	var err error
	switch command {
	case "status":
		reply, err = r.commands.Status(ctx)
	case "pause":
		reply, err = r.commands.Pause(ctx)
	case "resume":
		reply, err = r.commands.Resume(ctx)
	case "cost":
		reply, err = r.commands.Cost(ctx)
	default:
		reply = fmt.Sprintf("Unknown command: %s", command)
	}

	if err != nil {
		reply = fmt.Sprintf("Error: %v", err)
	}

	return r.channel.Send(ctx, msg.SenderID, reply)
}

func (r *ChannelRouter) handleClarificationReply(ctx context.Context, msg InboundMessage, ticket *models.Ticket) error {
	// Update ticket with clarification response and requeue
	if err := r.db.UpdateTicketStatus(ctx, ticket.ID, models.TicketStatusQueued); err != nil {
		r.logger.Error().Err(err).Str("ticket", ticket.ID).Msg("failed to requeue after clarification")
		return err
	}

	reply := fmt.Sprintf("Updated ticket #%s, resuming...", ticket.ID)
	if err := r.channel.Send(ctx, msg.SenderID, reply); err != nil {
		r.logger.Error().Err(err).Msg("failed to send clarification confirmation")
	}
	return nil
}

func (r *ChannelRouter) handleNewTicket(ctx context.Context, msg InboundMessage) error {
	r.logger.Info().Str("sender", msg.SenderID).Msg("new ticket from channel")
	// Ticket creation will be wired in Task 11 when we integrate with daemon.
	// For now, this is a placeholder that logs the intent.
	// The actual DB call depends on the exact CreateTicket signature.
	return nil
}
```

Note: The `RouterDB` interface and `handleNewTicket` will be refined in Task 11 (daemon integration) when we wire the actual `db.Database` and its `CreateTicket` signature. For now the router compiles and routes correctly.

**Step 4: Run tests**

Run: `go test ./internal/channel/ -run TestRouter -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/channel/router.go internal/channel/router_test.go
git commit -m "feat(channel): implement ChannelRouter with allowlist, pairing, and command routing"
```

---

## Task 8: Add whatsmeow dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add dependencies**

```bash
go get go.mau.fi/whatsmeow@latest
go get github.com/mdp/qrterminal/v3@latest
go get github.com/pelletier/go-toml@v1.9.5
```

**Step 2: Tidy**

```bash
go mod tidy
```

**Step 3: Verify build**

Run: `go build ./...`
Expected: BUILD SUCCESS

**Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add whatsmeow, qrterminal, and go-toml v1 dependencies"
```

---

## Task 9: WhatsApp Channel Implementation

**Files:**
- Create: `internal/channel/whatsapp/whatsapp.go`
- Create: `internal/channel/whatsapp/session.go`

**Step 1: Create WhatsApp channel**

Create `internal/channel/whatsapp/whatsapp.go`:

```go
package whatsapp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/canhta/foreman/internal/channel"
	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// WhatsAppChannel implements channel.Channel using whatsmeow.
type WhatsAppChannel struct {
	client    *whatsmeow.Client
	container *sqlstore.Container
	handler   channel.InboundHandler
	logger    zerolog.Logger
	limiter   *rateLimiter
	sessionDB string
	mu        sync.Mutex
	connected bool
}

// New creates a new WhatsAppChannel. Does not connect — call Start() to begin.
func New(sessionDB string, logger zerolog.Logger) *WhatsAppChannel {
	return &WhatsAppChannel{
		sessionDB: sessionDB,
		logger:    logger.With().Str("component", "whatsapp").Logger(),
		limiter:   newRateLimiter(10, time.Minute),
	}
}

func (w *WhatsAppChannel) Name() string { return "whatsapp" }

// Start connects to WhatsApp and begins listening for messages.
// Blocks until ctx is cancelled or a fatal error occurs.
func (w *WhatsAppChannel) Start(ctx context.Context, handler channel.InboundHandler) error {
	w.handler = handler

	container, err := sqlstore.New("sqlite3",
		fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", w.sessionDB),
		waLog.Noop)
	if err != nil {
		return fmt.Errorf("whatsapp session db: %w", err)
	}
	w.container = container

	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		return fmt.Errorf("whatsapp device store: %w", err)
	}

	w.client = whatsmeow.NewClient(deviceStore, waLog.Noop)
	w.client.AddEventHandler(func(evt interface{}) {
		w.handleEvent(ctx, evt)
	})

	if err := w.client.Connect(); err != nil {
		return fmt.Errorf("whatsapp connect: %w", err)
	}

	w.mu.Lock()
	w.connected = true
	w.mu.Unlock()

	w.logger.Info().Msg("WhatsApp connected")

	// Start rate limiter cleanup
	go w.limiter.cleanupLoop(ctx)

	// Block until context cancelled
	<-ctx.Done()
	w.logger.Info().Msg("WhatsApp shutting down")
	return nil
}

// Stop disconnects the WhatsApp client.
func (w *WhatsAppChannel) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.connected {
		w.connected = false
		w.client.Disconnect()
	}
	return nil
}

// Send sends a text message to a WhatsApp JID.
func (w *WhatsAppChannel) Send(ctx context.Context, recipientID string, message string) error {
	jid, err := types.ParseJID(recipientID)
	if err != nil {
		return fmt.Errorf("invalid JID %q: %w", recipientID, err)
	}
	_, err = w.client.SendMessage(ctx, jid, &waE2E.Message{
		Conversation: proto.String(message),
	})
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	return nil
}

func (w *WhatsAppChannel) handleEvent(ctx context.Context, evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		w.handleMessage(ctx, v)
	case *events.LoggedOut:
		w.logger.Warn().Msg("WhatsApp session logged out — manual re-login required")
		go w.Stop()
	case *events.Disconnected:
		w.logger.Warn().Msg("WhatsApp disconnected")
		go w.handleDisconnect(ctx)
	case *events.Connected:
		w.logger.Info().Msg("WhatsApp connected")
	}
}

func (w *WhatsAppChannel) handleMessage(ctx context.Context, evt *events.Message) {
	// 1. Ignore group messages
	if evt.Info.IsGroup {
		return
	}
	// 2. Ignore own messages (echo)
	if evt.Info.IsFromMe {
		return
	}
	// 3. Ignore media — reply with friendly message
	if isMediaMessage(evt.Message) {
		w.Send(ctx, evt.Info.Sender.String(), "Attachments not supported — please describe the task in text")
		return
	}
	// 4. Rate limit
	if !w.limiter.Allow(evt.Info.Sender.String()) {
		return
	}
	// 5. Extract and truncate body
	body := extractBody(evt.Message)
	if len(body) > 2000 {
		body = body[:2000]
	}
	if body == "" {
		return
	}

	w.handler.HandleMessage(ctx, channel.InboundMessage{
		SenderID:  evt.Info.Sender.String(),
		Body:      body,
		Timestamp: evt.Info.Timestamp,
	})
}

func (w *WhatsAppChannel) handleDisconnect(ctx context.Context) {
	backoff := []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second, 60 * time.Second}
	for attempt := 0; ; attempt++ {
		delay := backoff[min(attempt, len(backoff)-1)]
		w.logger.Warn().Dur("delay", delay).Int("attempt", attempt+1).Msg("reconnecting")
		select {
		case <-ctx.Done():
			w.logger.Info().Msg("reconnect cancelled — shutting down")
			return
		case <-time.After(delay):
		}
		if err := w.client.Connect(); err == nil {
			w.mu.Lock()
			w.connected = true
			w.mu.Unlock()
			w.logger.Info().Msg("reconnected")
			return
		}
	}
}

func isMediaMessage(msg *waE2E.Message) bool {
	return msg.GetImageMessage() != nil ||
		msg.GetVideoMessage() != nil ||
		msg.GetAudioMessage() != nil ||
		msg.GetDocumentMessage() != nil ||
		msg.GetStickerMessage() != nil
}

func extractBody(msg *waE2E.Message) string {
	if msg.GetConversation() != "" {
		return msg.GetConversation()
	}
	if msg.GetExtendedTextMessage() != nil {
		return msg.GetExtendedTextMessage().GetText()
	}
	return ""
}

// rateLimiter implements a simple per-JID windowed counter.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
	max     int
	window  time.Duration
}

type rateBucket struct {
	count     int
	windowEnd time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		buckets: make(map[string]*rateBucket),
		max:     max,
		window:  window,
	}
}

func (r *rateLimiter) Allow(jid string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	b, ok := r.buckets[jid]
	if !ok || now.After(b.windowEnd) {
		r.buckets[jid] = &rateBucket{count: 1, windowEnd: now.Add(r.window)}
		return true
	}
	if b.count >= r.max {
		return false
	}
	b.count++
	return true
}

func (r *rateLimiter) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.mu.Lock()
			now := time.Now()
			for jid, b := range r.buckets {
				if now.After(b.windowEnd) {
					delete(r.buckets, jid)
				}
			}
			r.mu.Unlock()
		}
	}
}
```

**Step 2: Create session login flows**

Create `internal/channel/whatsapp/session.go`:

```go
package whatsapp

import (
	"context"
	"fmt"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// LoginWithPairingCode links a WhatsApp account using a pairing code.
// Blocks until the device is linked or context is cancelled.
func LoginWithPairingCode(ctx context.Context, sessionDB, phone string) error {
	container, err := sqlstore.New("sqlite3",
		fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", sessionDB),
		waLog.Noop)
	if err != nil {
		return fmt.Errorf("session db: %w", err)
	}

	deviceStore := container.NewDevice()
	client := whatsmeow.NewClient(deviceStore, waLog.Noop)

	code, err := client.PairPhone(phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		return fmt.Errorf("pair phone: %w", err)
	}

	fmt.Printf("Pairing code: %s\n", code)
	fmt.Println("Open WhatsApp -> Linked Devices -> Link a Device -> Enter code")
	fmt.Println("Waiting for confirmation...")

	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// Wait for connection or context cancellation
	<-ctx.Done()
	client.Disconnect()
	fmt.Println("WhatsApp linked successfully. Session saved.")
	return nil
}

// LoginWithQR links a WhatsApp account using a QR code.
// Blocks until the device is linked or context is cancelled.
func LoginWithQR(ctx context.Context, sessionDB string) error {
	container, err := sqlstore.New("sqlite3",
		fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", sessionDB),
		waLog.Noop)
	if err != nil {
		return fmt.Errorf("session db: %w", err)
	}

	deviceStore := container.NewDevice()
	client := whatsmeow.NewClient(deviceStore, waLog.Noop)

	qrChan, _ := client.GetQRChannel(ctx)
	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	for evt := range qrChan {
		switch evt.Event {
		case "code":
			fmt.Println("Scan the QR code below with WhatsApp -> Linked Devices -> Link a Device")
			qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, qrterminal.DefaultConfig)
		case "login":
			fmt.Println("WhatsApp linked successfully. Session saved.")
			client.Disconnect()
			return nil
		case "timeout":
			client.Disconnect()
			return fmt.Errorf("QR code expired — please try again")
		}
	}

	client.Disconnect()
	return nil
}
```

**Step 3: Verify build**

Run: `go build ./internal/channel/whatsapp/...`
Expected: BUILD SUCCESS (may need to adjust imports based on exact whatsmeow API version)

Note: whatsmeow's API may differ slightly between versions. The implementer should check `go doc go.mau.fi/whatsmeow` and adapt if needed. The structure and intent are correct.

**Step 4: Commit**

```bash
git add internal/channel/whatsapp/
git commit -m "feat(whatsapp): implement WhatsApp channel with whatsmeow client and session login"
```

---

## Task 10: Config Persistence — Round-Trip TOML Editing

**Files:**
- Create: `internal/config/persist.go`
- Create: `internal/config/persist_test.go`

**Step 1: Write the failing test**

Create `internal/config/persist_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddAllowedNumber(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "foreman.toml")

	initial := `# Main config
[channel]
provider = "whatsapp"

[channel.whatsapp]
session_db = "~/.foreman/whatsapp.db"
dm_policy = "pairing"
allowed_numbers = ["+84111111111"]
`
	os.WriteFile(path, []byte(initial), 0o644)

	err := AddAllowedNumber(path, "+84222222222")
	if err != nil {
		t.Fatalf("AddAllowedNumber: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	// Should contain both numbers
	if !strings.Contains(content, "+84111111111") {
		t.Error("original number missing")
	}
	if !strings.Contains(content, "+84222222222") {
		t.Error("new number missing")
	}
	// Should preserve comment
	if !strings.Contains(content, "# Main config") {
		t.Error("comment was not preserved")
	}
}

func TestRemoveAllowedNumber(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "foreman.toml")

	initial := `[channel.whatsapp]
allowed_numbers = ["+84111111111", "+84222222222"]
`
	os.WriteFile(path, []byte(initial), 0o644)

	err := RemoveAllowedNumber(path, "+84111111111")
	if err != nil {
		t.Fatalf("RemoveAllowedNumber: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	if strings.Contains(content, "+84111111111") {
		t.Error("removed number still present")
	}
	if !strings.Contains(content, "+84222222222") {
		t.Error("kept number missing")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run "TestAddAllowed|TestRemoveAllowed" -v`
Expected: FAIL — functions not defined

**Step 3: Implement config persistence**

Create `internal/config/persist.go`:

```go
package config

import (
	"fmt"
	"os"

	toml "github.com/pelletier/go-toml"
)

// AddAllowedNumber appends a phone number to channel.whatsapp.allowed_numbers in a TOML config file.
// Preserves comments and formatting using go-toml v1 tree manipulation.
func AddAllowedNumber(configPath string, phone string) error {
	tree, err := loadTree(configPath)
	if err != nil {
		return err
	}

	numbers := getAllowedNumbers(tree)
	for _, n := range numbers {
		if n == phone {
			return nil // already present
		}
	}
	numbers = append(numbers, phone)
	tree.SetPath([]string{"channel", "whatsapp", "allowed_numbers"}, numbers)

	return writeTree(configPath, tree)
}

// RemoveAllowedNumber removes a phone number from channel.whatsapp.allowed_numbers in a TOML config file.
func RemoveAllowedNumber(configPath string, phone string) error {
	tree, err := loadTree(configPath)
	if err != nil {
		return err
	}

	numbers := getAllowedNumbers(tree)
	filtered := make([]string, 0, len(numbers))
	for _, n := range numbers {
		if n != phone {
			filtered = append(filtered, n)
		}
	}
	tree.SetPath([]string{"channel", "whatsapp", "allowed_numbers"}, filtered)

	return writeTree(configPath, tree)
}

func loadTree(path string) (*toml.Tree, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	tree, err := toml.LoadBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return tree, nil
}

func writeTree(path string, tree *toml.Tree) error {
	out, err := tree.Marshal()
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func getAllowedNumbers(tree *toml.Tree) []string {
	val := tree.GetPath([]string{"channel", "whatsapp", "allowed_numbers"})
	if val == nil {
		return nil
	}
	arr, ok := val.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
```

**Step 4: Run tests**

Run: `go test ./internal/config/ -run "TestAddAllowed|TestRemoveAllowed" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/persist.go internal/config/persist_test.go
git commit -m "feat(config): implement round-trip TOML editing for allowed_numbers"
```

---

## Task 11: Daemon Integration

**Files:**
- Modify: `internal/daemon/daemon.go` (add channel, SetChannelRouter, CommandHandler impl, poll loop cleanup)
- Modify: `internal/daemon/orchestrator.go` (add channel, notify method, notification calls)

**Step 1: Add channel fields to Daemon**

In `internal/daemon/daemon.go`, add to the Daemon struct (around line 63):

```go
channel       channel.Channel
channelRouter channel.InboundHandler
```

Add import for `"github.com/canhta/foreman/internal/channel"`.

**Step 2: Add SetChannelRouter method**

Add after the Daemon struct:

```go
// SetChannelRouter wires the channel router after construction (breaks circular dependency).
func (d *Daemon) SetChannelRouter(router channel.InboundHandler) {
	d.channelRouter = router
}
```

**Step 3: Start channel in Start()**

In the `Start()` method, after the merge checker goroutine launch (around line 167), add:

```go
if d.channel != nil && d.channelRouter != nil {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		if err := d.channel.Start(ctx, d.channelRouter); err != nil {
			d.log.Error().Err(err).Msg("channel stopped with error")
		}
	}()
}
```

**Step 4: Add expired pairings cleanup to poll loop**

In the poll loop's ticker case (around line 182-186, near `checkClarificationTimeouts`), add:

```go
if err := d.db.DeleteExpiredPairings(ctx); err != nil {
	d.log.Error().Err(err).Msg("failed to delete expired pairings")
}
```

**Step 5: Stop channel on shutdown**

Before the `return` at the end of `Start()` (or in the shutdown sequence), add:

```go
if d.channel != nil {
	if err := d.channel.Stop(); err != nil {
		d.log.Error().Err(err).Msg("channel stop error")
	}
}
```

**Step 6: Implement CommandHandler on Daemon**

Add at the end of daemon.go:

```go
func (d *Daemon) Status(ctx context.Context) (string, error) {
	tickets, err := d.db.ListTickets(ctx)
	if err != nil {
		return "", err
	}
	active := 0
	var summary []string
	for _, t := range tickets {
		if t.Status == models.TicketStatusImplementing || t.Status == models.TicketStatusPlanning {
			active++
			summary = append(summary, fmt.Sprintf("#%s %s (%s)", t.ID, t.Title, t.Status))
		}
	}
	if active == 0 {
		return "No active tickets.", nil
	}
	result := fmt.Sprintf("%d active ticket(s):\n", active)
	for _, s := range summary {
		result += "  " + s + "\n"
	}
	return result, nil
}

func (d *Daemon) Pause(ctx context.Context) (string, error) {
	d.paused.Store(true)
	return "Daemon paused. No new tickets will be picked up.", nil
}

func (d *Daemon) Resume(ctx context.Context) (string, error) {
	d.paused.Store(false)
	return "Daemon resumed. Picking up tickets again.", nil
}

func (d *Daemon) Cost(ctx context.Context) (string, error) {
	daily, err := d.db.GetDailyCost(ctx)
	if err != nil {
		return "", err
	}
	monthly, err := d.db.GetMonthlyCost(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Today: $%.2f\nThis month: $%.2f", daily, monthly), nil
}
```

Note: Adapt `ListTickets` and `GetDailyCost`/`GetMonthlyCost` calls to match the actual DB interface signatures. Check existing callers in daemon.go for patterns.

**Step 7: Add channel and notify to Orchestrator**

In `internal/daemon/orchestrator.go`, add to the Orchestrator struct (around line 117):

```go
channel channel.Channel
```

Add the notify method:

```go
func (o *Orchestrator) notify(ctx context.Context, ticket models.Ticket, msg string) {
	if o.channel == nil || ticket.ChannelSenderID == "" {
		return
	}
	if err := o.channel.Send(ctx, ticket.ChannelSenderID, msg); err != nil {
		o.log.Warn().Err(err).Str("ticket", ticket.ID).Msg("channel notify failed")
	}
}
```

**Step 8: Insert notify calls at status transitions**

In `ProcessTicket()`, add notify calls after each status transition:

After line 179 (queued -> planning):
```go
o.notify(ctx, ticket, fmt.Sprintf("Ticket #%s picked up — planning...", ticket.ID))
```

After line 208 (-> clarification_needed):
```go
o.notify(ctx, ticket, fmt.Sprintf("Question about ticket #%s:\n%s", ticket.ID, clarificationQuestion))
```
Note: Extract the clarification question from the clarity check result variable.

After line 287 (-> implementing):
```go
taskCount := len(tasks)
o.notify(ctx, ticket, fmt.Sprintf("Implementing %d tasks for ticket #%s...", taskCount, ticket.ID))
```

After line 405-409 (PR created / awaiting_merge):
```go
if ticket.PRURL != "" {
	o.notify(ctx, ticket, fmt.Sprintf("PR opened for ticket #%s: %s", ticket.ID, ticket.PRURL))
}
```

In the error/failure handler (deferred function around line 158-176):
```go
o.notify(ctx, ticket, fmt.Sprintf("Ticket #%s failed: %s", ticket.ID, err.Error()))
```

**Step 9: Verify build**

Run: `go build ./internal/daemon/...`
Expected: BUILD SUCCESS

**Step 10: Commit**

```bash
git add internal/daemon/daemon.go internal/daemon/orchestrator.go
git commit -m "feat(daemon): integrate channel for notifications, commands, and lifecycle events"
```

---

## Task 12: CLI Commands — channel and pairing

**Files:**
- Create: `cmd/channel.go`
- Create: `cmd/pairing.go`
- Modify: `cmd/root.go` (register new subcommands)

**Step 1: Create channel commands**

Create `cmd/channel.go`:

```go
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/canhta/foreman/internal/channel/whatsapp"
	"github.com/spf13/cobra"
)

func newChannelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channel",
		Short: "Manage messaging channels",
	}
	cmd.AddCommand(newChannelLoginCmd())
	cmd.AddCommand(newChannelStatusCmd())
	return cmd
}

func newChannelLoginCmd() *cobra.Command {
	var phone string
	var mode string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Link a WhatsApp account to Foreman",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigAndDB()
			if err != nil {
				return err
			}

			sessionDB := cfg.Channel.WhatsApp.SessionDB
			if sessionDB == "" {
				sessionDB = "~/.foreman/whatsapp.db"
			}
			// Expand ~ to home dir
			if sessionDB[:2] == "~/" {
				home, _ := os.UserHomeDir()
				sessionDB = home + sessionDB[1:]
			}

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			switch mode {
			case "qr":
				return whatsapp.LoginWithQR(ctx, sessionDB)
			default:
				if phone == "" {
					return fmt.Errorf("--phone is required for pairing code mode")
				}
				return whatsapp.LoginWithPairingCode(ctx, sessionDB, phone)
			}
		},
	}

	cmd.Flags().StringVar(&phone, "phone", "", "Phone number in E.164 format (e.g., +84123456789)")
	cmd.Flags().StringVar(&mode, "mode", "code", "Login mode: code or qr")
	return cmd
}

func newChannelStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show channel connection status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigAndDB()
			if err != nil {
				return err
			}

			if cfg.Channel.Provider == "" {
				fmt.Println("No channel configured.")
				return nil
			}

			sessionDB := cfg.Channel.WhatsApp.SessionDB
			if sessionDB == "" {
				sessionDB = "~/.foreman/whatsapp.db"
			}
			if sessionDB[:2] == "~/" {
				home, _ := os.UserHomeDir()
				sessionDB = home + sessionDB[1:]
			}

			if _, err := os.Stat(sessionDB); os.IsNotExist(err) {
				fmt.Println("whatsapp    ✗ not linked    Run: foreman channel login")
				return nil
			}

			fmt.Printf("whatsapp    ✓ session exists    %s\n", sessionDB)
			return nil
		},
	}
}
```

**Step 2: Create pairing commands**

Create `cmd/pairing.go`:

```go
package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/canhta/foreman/internal/config"
	"github.com/spf13/cobra"
)

func newPairingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pairing",
		Short: "Manage sender pairing for messaging channels",
	}
	cmd.AddCommand(newPairingListCmd())
	cmd.AddCommand(newPairingApproveCmd())
	cmd.AddCommand(newPairingRevokeCmd())
	return cmd
}

func newPairingListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List pending pairing requests",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, db, err := loadConfigAndDB()
			if err != nil {
				return err
			}
			defer db.Close()

			pairings, err := db.ListPairings(context.Background(), "whatsapp")
			if err != nil {
				return err
			}

			if len(pairings) == 0 {
				fmt.Println("No pending pairing requests.")
				return nil
			}

			fmt.Printf("%-12s %-20s %s\n", "CODE", "SENDER", "EXPIRES")
			for _, p := range pairings {
				remaining := time.Until(p.ExpiresAt).Round(time.Minute)
				fmt.Printf("%-12s %-20s in %s\n", p.Code, p.SenderID, remaining)
			}
			return nil
		},
	}
}

func newPairingApproveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve <CODE>",
		Short: "Approve a pending pairing request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, db, err := loadConfigAndDB()
			if err != nil {
				return err
			}
			defer db.Close()

			code := args[0]
			p, err := db.GetPairing(context.Background(), code)
			if err != nil {
				return err
			}
			if p == nil {
				return fmt.Errorf("pairing code %q not found", code)
			}
			if time.Now().After(p.ExpiresAt) {
				db.DeletePairing(context.Background(), code)
				return fmt.Errorf("pairing code %q has expired", code)
			}

			// Add to config file
			configPath := "foreman.toml" // adapt to actual config path resolution
			if err := config.AddAllowedNumber(configPath, p.SenderID); err != nil {
				return fmt.Errorf("update config: %w", err)
			}

			// Delete pairing
			if err := db.DeletePairing(context.Background(), code); err != nil {
				return fmt.Errorf("delete pairing: %w", err)
			}

			fmt.Printf("Approved %s — added to allowed_numbers.\n", p.SenderID)
			_ = cfg // suppress unused
			return nil
		},
	}
}

func newPairingRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <PHONE>",
		Short: "Remove a phone number from the allowlist",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			phone := args[0]
			configPath := "foreman.toml" // adapt to actual config path resolution
			if err := config.RemoveAllowedNumber(configPath, phone); err != nil {
				return fmt.Errorf("update config: %w", err)
			}
			fmt.Printf("Revoked %s — removed from allowed_numbers.\n", phone)
			return nil
		},
	}
}
```

**Step 3: Register subcommands in root.go**

In `cmd/root.go`, in the `init()` function (around line 305), add:

```go
rootCmd.AddCommand(newChannelCmd())
rootCmd.AddCommand(newPairingCmd())
```

**Step 4: Verify build**

Run: `go build ./...`
Expected: BUILD SUCCESS

**Step 5: Commit**

```bash
git add cmd/channel.go cmd/pairing.go cmd/root.go
git commit -m "feat(cmd): add channel login/status and pairing list/approve/revoke commands"
```

---

## Task 13: Wire Channel in cmd/start.go

**Files:**
- Modify: `cmd/start.go` (add channel initialization and wiring)

**Step 1: Add channel initialization**

In `cmd/start.go`, after the tracker initialization (around line 119) and before the orchestrator creation, add:

```go
// Initialize channel (optional).
var ch channel.Channel
if cfg.Channel.Provider == "whatsapp" {
	sessionDB := cfg.Channel.WhatsApp.SessionDB
	if sessionDB == "" {
		sessionDB = "~/.foreman/whatsapp.db"
	}
	if sessionDB[:2] == "~/" {
		home, _ := os.UserHomeDir()
		sessionDB = home + sessionDB[1:]
	}
	waCh := whatsapp.New(sessionDB, logger)
	ch = waCh
}
```

Add imports:
```go
"github.com/canhta/foreman/internal/channel"
"github.com/canhta/foreman/internal/channel/whatsapp"
```

**Step 2: Pass channel to orchestrator**

When creating the orchestrator (around line 141-179), pass `ch` as the channel:

```go
// Add ch to orchestrator constructor or use a setter:
orch.SetChannel(ch)
```

Add `SetChannel` method to Orchestrator if it doesn't exist (setter pattern like other components).

**Step 3: Create router and wire to daemon**

After daemon creation but before `d.Start()`:

```go
if ch != nil {
	classifier := channel.NewClassifier(llmProvider) // use existing llmProvider
	allowlist := channel.NewAllowlist(cfg.Channel.WhatsApp.AllowedNumbers)
	var pairingMgr *channel.PairingManager
	if cfg.Channel.WhatsApp.DMPolicy == "pairing" {
		pairingMgr = channel.NewPairingManager(database, "whatsapp")
	}
	router := channel.NewRouter(ch, database, classifier, allowlist, pairingMgr, d, logger)
	d.SetChannelRouter(router)
}
```

Note: `database` and `llmProvider` and `logger` are already in scope from the existing start command setup. Adapt variable names to match the actual locals in start.go.

**Step 4: Set channel on daemon**

The daemon constructor or a setter needs to receive `ch`:
```go
d.SetChannel(ch)
```

Add `SetChannel` to Daemon if not already present:
```go
func (d *Daemon) SetChannel(ch channel.Channel) {
	d.channel = ch
}
```

**Step 5: Verify build**

Run: `go build ./...`
Expected: BUILD SUCCESS

**Step 6: Run all tests**

Run: `go test ./...`
Expected: ALL PASS (existing tests unaffected, new tests pass)

**Step 7: Commit**

```bash
git add cmd/start.go internal/daemon/daemon.go
git commit -m "feat(cmd): wire WhatsApp channel into daemon startup"
```

---

## Task 14: Integration Test

**Files:**
- Create: `internal/channel/integration_test.go`

**Step 1: Write an integration test using mock channel**

```go
package channel_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/channel"
	"github.com/rs/zerolog"
)

// mockIntegrationChannel records sent messages.
type mockIntegrationChannel struct {
	mu   sync.Mutex
	sent []struct{ To, Body string }
}

func (m *mockIntegrationChannel) Start(ctx context.Context, h channel.InboundHandler) error {
	<-ctx.Done()
	return nil
}
func (m *mockIntegrationChannel) Stop() error  { return nil }
func (m *mockIntegrationChannel) Name() string { return "mock" }
func (m *mockIntegrationChannel) Send(_ context.Context, to, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, struct{ To, Body string }{to, body})
	return nil
}

func TestRouter_FullFlow(t *testing.T) {
	ch := &mockIntegrationChannel{}
	allowlist := channel.NewAllowlist([]string{"+84111111111"})
	classifier := channel.NewClassifier(nil)

	cmds := &staticCommands{status: "2 tickets active"}
	router := channel.NewRouter(ch, nil, classifier, allowlist, nil, cmds, zerolog.Nop())

	ctx := context.Background()

	// Test 1: Command routing
	router.HandleMessage(ctx, channel.InboundMessage{
		SenderID:  "84111111111@s.whatsapp.net",
		Body:      "/status",
		Timestamp: time.Now(),
	})

	ch.mu.Lock()
	if len(ch.sent) != 1 || ch.sent[0].Body != "2 tickets active" {
		t.Errorf("expected status reply, got %v", ch.sent)
	}
	ch.sent = nil
	ch.mu.Unlock()

	// Test 2: Unknown sender rejected silently (no pairing manager)
	router.HandleMessage(ctx, channel.InboundMessage{
		SenderID:  "84999999999@s.whatsapp.net",
		Body:      "hello",
		Timestamp: time.Now(),
	})

	ch.mu.Lock()
	if len(ch.sent) != 0 {
		t.Errorf("expected no reply for unknown sender, got %v", ch.sent)
	}
	ch.mu.Unlock()
}

type staticCommands struct{ status string }

func (s *staticCommands) Status(_ context.Context) (string, error) { return s.status, nil }
func (s *staticCommands) Pause(_ context.Context) (string, error)  { return "paused", nil }
func (s *staticCommands) Resume(_ context.Context) (string, error) { return "resumed", nil }
func (s *staticCommands) Cost(_ context.Context) (string, error)   { return "$0", nil }
```

**Step 2: Run test**

Run: `go test ./internal/channel/ -run TestRouter_FullFlow -v`
Expected: PASS

**Step 3: Run full test suite**

Run: `go test ./...`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add internal/channel/integration_test.go
git commit -m "test(channel): add integration test for router full message flow"
```

---

## Task 15: Final Verification

**Step 1: Run full build**

Run: `go build -o foreman ./main.go`
Expected: BUILD SUCCESS

**Step 2: Run all tests**

Run: `go test ./... -count=1`
Expected: ALL PASS

**Step 3: Run linter**

Run: `go vet ./...`
Expected: No issues

**Step 4: Verify CLI commands registered**

Run: `./foreman channel --help`
Expected: Shows login and status subcommands

Run: `./foreman pairing --help`
Expected: Shows list, approve, and revoke subcommands

**Step 5: Final commit if any cleanup needed**

```bash
git add -A
git commit -m "chore: final cleanup for WhatsApp channel integration"
```
