package dashboard

import (
	"context"
	"fmt"
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

// mockErrorAuthDB returns an error from ValidateAuthToken to simulate DB failure.
type mockErrorAuthDB struct{}

func (m *mockErrorAuthDB) ValidateAuthToken(_ context.Context, _ string) (bool, error) {
	return false, fmt.Errorf("db connection refused")
}

func TestAuthMiddleware_DBError_Returns401(t *testing.T) {
	handler := authMiddleware(&mockErrorAuthDB{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/status", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// A DB error must not let the request through — still 401.
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when DB returns error, got %d", rec.Code)
	}
}

// --- extractBearerToken unit tests ---

func TestExtractBearerToken_Valid(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
	req.Header.Set("Authorization", "Bearer my-secret-token")
	got := extractBearerToken(req)
	if got != "my-secret-token" {
		t.Errorf("expected my-secret-token, got %q", got)
	}
}

func TestExtractBearerToken_Missing(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
	got := extractBearerToken(req)
	if got != "" {
		t.Errorf("expected empty string for missing header, got %q", got)
	}
}

func TestExtractBearerToken_WrongScheme(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	got := extractBearerToken(req)
	if got != "" {
		t.Errorf("expected empty string for Basic scheme, got %q", got)
	}
}

// --- hashToken unit tests ---

func TestHashToken_Deterministic(t *testing.T) {
	h1 := hashToken("my-token")
	h2 := hashToken("my-token")
	if h1 != h2 {
		t.Errorf("hashToken is not deterministic: %q vs %q", h1, h2)
	}
}

func TestHashToken_DifferentInputs_DifferentOutput(t *testing.T) {
	h1 := hashToken("token-a")
	h2 := hashToken("token-b")
	if h1 == h2 {
		t.Error("different tokens should produce different hashes")
	}
}

func TestHashToken_NonEmpty(t *testing.T) {
	h := hashToken("any-token")
	if h == "" {
		t.Error("hashToken should never return an empty string")
	}
}
