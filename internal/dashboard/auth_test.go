package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockAuthDB struct {
	validHashes map[string]bool
}

func (m *mockAuthDB) ValidateAuthToken(_ context.Context, hash string) (bool, error) {
	return m.validHashes[hash], nil
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	db := &mockAuthDB{validHashes: make(map[string]bool)}
	// Pre-compute SHA256 of "test-token"
	db.validHashes[hashToken("test-token")] = true

	handler := authMiddleware(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/status", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	db := &mockAuthDB{validHashes: make(map[string]bool)}

	handler := authMiddleware(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/status", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_MetricsEndpointRequiresAuth(t *testing.T) {
	db := &mockAuthDB{validHashes: make(map[string]bool)}

	// Simulate the metrics endpoint wrapped with auth middleware
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("metrics data"))
	})
	handler := authMiddleware(db)(inner)

	// Request without auth should get 401
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated metrics request, got %d", rec.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	db := &mockAuthDB{validHashes: make(map[string]bool)}

	handler := authMiddleware(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/status", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
