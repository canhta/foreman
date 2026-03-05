package pipeline

import (
	"context"
	"testing"
)

type mockCapDB struct {
	counts map[string]int
}

func (m *mockCapDB) IncrementTaskLlmCalls(_ context.Context, id string) (int, error) {
	m.counts[id]++
	return m.counts[id], nil
}

func TestCheckTaskCallCap_UnderLimit(t *testing.T) {
	db := &mockCapDB{counts: map[string]int{"t1": 3}}
	err := CheckTaskCallCap(context.Background(), db, "t1", 8)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCheckTaskCallCap_AtLimit(t *testing.T) {
	db := &mockCapDB{counts: map[string]int{"t2": 7}}
	err := CheckTaskCallCap(context.Background(), db, "t2", 8)
	if err != nil {
		t.Errorf("expected no error at limit, got %v", err)
	}
}

func TestCheckTaskCallCap_OverLimit(t *testing.T) {
	db := &mockCapDB{counts: map[string]int{"t3": 8}}
	err := CheckTaskCallCap(context.Background(), db, "t3", 8)
	if err == nil {
		t.Error("expected error when over limit")
	}
}
