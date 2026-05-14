package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/luxarts/supervaisor/internal/state"
)

func newTestStore(t *testing.T) *SQLite {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpsertAndList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	sess := &state.Session{
		ID:            "s1",
		Name:          "n1",
		Project:       "/tmp",
		Status:        state.StatusWorking,
		StartedAt:     now,
		LastEventAt:   now,
		CurrentAction: "Write: x.go",
	}
	if err := s.UpsertSession(ctx, sess); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "s1" || got[0].Name != "n1" {
		t.Fatalf("unexpected list: %#v", got)
	}

	// Update.
	sess.Name = "renamed"
	if err := s.UpsertSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ListSessions(ctx)
	if got[0].Name != "renamed" {
		t.Errorf("Name = %q", got[0].Name)
	}
}

func TestGetSession_NotFound_ReturnsNil(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetSession(ctx, "nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %#v", got)
	}
}

func TestAppendEvent_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	// Insert a session so foreign key relationship is logically satisfied
	// (SQLite doesn't enforce FK by default without PRAGMA).
	sess := &state.Session{
		ID:          "sess-1",
		Name:        "test",
		Project:     "/tmp",
		Status:      state.StatusWorking,
		StartedAt:   now,
		LastEventAt: now,
	}
	if err := s.UpsertSession(ctx, sess); err != nil {
		t.Fatal(err)
	}

	payload := []byte(`{"type":"assistant","timestamp":"2026-05-14T12:00:00Z"}`)
	if err := s.AppendEvent(ctx, "sess-1", now, "assistant", payload); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	n, err := s.CountEvents(ctx)
	if err != nil {
		t.Fatalf("CountEvents failed: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 event, got %d", n)
	}
}
