package channel

import (
	"context"
	"testing"
	"time"

	"github.com/canhta/foreman/internal/models"
)

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
