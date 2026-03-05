package db

import (
	"testing"
)

func TestNewPostgresDB_InvalidURL(t *testing.T) {
	_, err := NewPostgresDB("invalid://url", 5)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}
