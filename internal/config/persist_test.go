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

	if !strings.Contains(content, "+84111111111") {
		t.Error("original number missing")
	}
	if !strings.Contains(content, "+84222222222") {
		t.Error("new number missing")
	}
}

func TestAddAllowedNumber_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "foreman.toml")

	initial := `[channel.whatsapp]
allowed_numbers = ["+84111111111"]
`
	os.WriteFile(path, []byte(initial), 0o644)

	err := AddAllowedNumber(path, "+84111111111")
	if err != nil {
		t.Fatalf("AddAllowedNumber: %v", err)
	}

	data, _ := os.ReadFile(path)
	count := strings.Count(string(data), "+84111111111")
	if count != 1 {
		t.Errorf("expected 1 occurrence, got %d", count)
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
