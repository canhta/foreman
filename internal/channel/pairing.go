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
	for n := range a.numbers {
		// Strip the leading "+" for comparison
		// whatsmeow JIDs look like "84123456789@s.whatsapp.net"
		if len(n) > 1 {
			digits := n[1:] // "+84123" -> "84123"
			if len(senderID) >= len(digits) && senderID[:len(digits)] == digits {
				return true
			}
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
		_ = pm.db.DeletePairing(ctx, code)
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
