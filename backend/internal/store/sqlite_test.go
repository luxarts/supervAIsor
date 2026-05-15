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
		Hostname:      "host-test",
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

	got, err := s.GetSession(ctx, "host-test", "nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %#v", got)
	}
}

func TestListEvents(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	mustAppend := func(sid string, offset time.Duration, typ, payload string) {
		if err := s.AppendEvent(ctx, "host-test", sid, now.Add(offset), typ, []byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	mustAppend("sess-A", 0, "user", `{"n":1}`)
	mustAppend("sess-A", 2*time.Second, "assistant", `{"n":2}`)
	mustAppend("sess-A", time.Second, "user", `{"n":3}`)
	mustAppend("sess-B", 0, "user", `{"n":99}`)

	evs, err := s.ListEvents(ctx, "host-test", "sess-A", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Fatalf("got %d events, want 3", len(evs))
	}
	if string(evs[0].Payload) != `{"n":1}` ||
		string(evs[1].Payload) != `{"n":3}` ||
		string(evs[2].Payload) != `{"n":2}` {
		t.Fatalf("events not ordered by ts ASC: %+v", evs)
	}

	// Limit returns the TAIL (most recent) in ASC order.
	evs2, err := s.ListEvents(ctx, "host-test", "sess-A", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs2) != 2 {
		t.Fatalf("limit not honored, got %d", len(evs2))
	}
	if string(evs2[0].Payload) != `{"n":3}` || string(evs2[1].Payload) != `{"n":2}` {
		t.Fatalf("expected last 2 events (n=3 then n=2), got %+v", evs2)
	}
}

func TestAppendEvent_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	// Insert a session so foreign key relationship is logically satisfied
	// (SQLite doesn't enforce FK by default without PRAGMA).
	sess := &state.Session{
		Hostname:    "host-test",
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
	if err := s.AppendEvent(ctx, "host-test", "sess-1", now, "assistant", payload); err != nil {
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

func TestListEvents_NegativeLimitReturnsAll(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	mustAppend := func(offset time.Duration, typ, payload string) {
		t.Helper()
		if err := s.AppendEvent(ctx, "host-test", "sess-neg", now.Add(offset), typ, []byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	mustAppend(0, "user", `{"n":1}`)
	mustAppend(time.Second, "assistant", `{"n":2}`)
	mustAppend(2*time.Second, "user", `{"n":3}`)

	evs, err := s.ListEvents(ctx, "host-test", "sess-neg", -1)
	if err != nil {
		t.Fatalf("ListEvents with -1 failed: %v", err)
	}
	if len(evs) != 3 {
		t.Errorf("got %d events with limit=-1, want 3 (all rows)", len(evs))
	}
}

func TestUpsertSession_RoundTripsLastErrorAt(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "err.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	in := &state.Session{
		ID: "abc", Hostname: "h", Name: "n", Project: "/p",
		Status:      state.StatusDone,
		StartedAt:   t0,
		LastEventAt: t0.Add(time.Minute),
		LastErrorAt: t0.Add(30 * time.Second),
	}
	if err := st.UpsertSession(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSession(ctx, "h", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("got nil session")
	}
	if !got.LastErrorAt.Equal(in.LastErrorAt) {
		t.Errorf("LastErrorAt = %v, want %v", got.LastErrorAt, in.LastErrorAt)
	}
}

func TestUpsertSession_ZeroLastErrorAtRoundTrips(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "noerr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	in := &state.Session{
		ID: "abc", Hostname: "h", Name: "n", Project: "/p",
		Status:      state.StatusDone,
		StartedAt:   t0,
		LastEventAt: t0,
	}
	if err := st.UpsertSession(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSession(ctx, "h", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastErrorAt.IsZero() {
		t.Errorf("LastErrorAt = %v, want zero", got.LastErrorAt)
	}
}

func TestUpsertSession_SameIDDifferentHosts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	a := &state.Session{Hostname: "mac-A", ID: "uuid-1", Name: "A", Project: "/p",
		Status: state.StatusWorking, StartedAt: now, LastEventAt: now}
	b := &state.Session{Hostname: "mac-B", ID: "uuid-1", Name: "B", Project: "/p",
		Status: state.StatusWorking, StartedAt: now, LastEventAt: now}

	if err := s.UpsertSession(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSession(ctx, b); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d", len(got))
	}

	ga, _ := s.GetSession(ctx, "mac-A", "uuid-1")
	gb, _ := s.GetSession(ctx, "mac-B", "uuid-1")
	if ga == nil || ga.Name != "A" || gb == nil || gb.Name != "B" {
		t.Errorf("rows did not isolate by hostname: A=%v B=%v", ga, gb)
	}
}

func TestDeleteSession_RemovesSessionAndEvents(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "del.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	in := &state.Session{
		ID: "abc", Hostname: "h", Name: "n", Project: "/p",
		Status:      state.StatusDone,
		StartedAt:   t0,
		LastEventAt: t0,
	}
	if err := st.UpsertSession(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendEvent(ctx, "h", "abc", t0, "user", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendEvent(ctx, "h", "abc", t0, "assistant", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteSession(ctx, "h", "abc"); err != nil {
		t.Fatal(err)
	}

	got, _ := st.GetSession(ctx, "h", "abc")
	if got != nil {
		t.Errorf("session still present after delete: %+v", got)
	}
	evs, _ := st.ListEvents(ctx, "h", "abc", -1)
	if len(evs) != 0 {
		t.Errorf("events not purged: %d remain", len(evs))
	}
}

func TestDeleteSession_NoOpOnMissing(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "del2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.DeleteSession(context.Background(), "h", "missing"); err != nil {
		t.Errorf("DeleteSession on missing should be no-op, got %v", err)
	}
}

func TestUpsertSession_RoundTripsProjectDirEncoded(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "pde.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	in := &state.Session{
		ID: "abc", Hostname: "h", Name: "n", Project: "/p",
		Status:            state.StatusDone,
		StartedAt:         t0,
		LastEventAt:       t0,
		ProjectDirEncoded: "-Users-x-Projects-foo",
	}
	if err := st.UpsertSession(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSession(ctx, "h", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectDirEncoded != "-Users-x-Projects-foo" {
		t.Errorf("ProjectDirEncoded = %q", got.ProjectDirEncoded)
	}
}
