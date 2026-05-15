package broadcast

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/luxarts/supervaisor/internal/state"
)

// staticSnapshot is a simple SnapshotProvider for tests.
type staticSnapshot struct {
	sessions []*state.Session
}

func (s *staticSnapshot) Snapshot() []*state.Session     { return s.sessions }
func (s *staticSnapshot) PollersOnline() map[string]bool { return map[string]bool{} }

func wsURL(u string) string {
	return "ws" + strings.TrimPrefix(u, "http")
}

// TestHandler_SnapshotOnConnect verifies the first frame is a snapshot
// containing the seeded sessions.
func TestHandler_SnapshotOnConnect(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Stop)

	sessions := []*state.Session{
		{ID: "s1", Name: "alpha", Status: state.StatusWorking},
	}
	h := &Handler{
		Hub:      hub,
		Snapshot: &staticSnapshot{sessions: sessions},
	}

	srv := httptest.NewServer(http.HandlerFunc(h.Serve))
	t.Cleanup(srv.Close)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}

	var f frame
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.Kind != "snapshot" {
		t.Errorf("kind = %q, want snapshot", f.Kind)
	}
	if len(f.Sessions) != 1 || f.Sessions[0].ID != "s1" {
		t.Errorf("sessions = %v", f.Sessions)
	}
}

// TestHandler_LiveUpdate verifies that a Broadcast after connect is forwarded.
func TestHandler_LiveUpdate(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Stop)

	h := &Handler{
		Hub:      hub,
		Snapshot: &staticSnapshot{sessions: nil},
	}

	srv := httptest.NewServer(http.HandlerFunc(h.Serve))
	t.Cleanup(srv.Close)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Consume the snapshot and pollers frames.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read pollers: %v", err)
	}

	// Give the subscription a moment to register before broadcasting.
	time.Sleep(20 * time.Millisecond)

	hub.Broadcast([]byte(`{"kind":"update","session":{"id":"s2"}}`))

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read update: %v", err)
	}
	if !strings.Contains(string(raw), `"s2"`) {
		t.Errorf("expected s2 in update, got: %s", raw)
	}
}

func TestHandler_PollersFrameOnConnect(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Stop)

	provider := &staticSnapshotWithPollers{
		sessions: nil,
		pollers:  map[string]bool{"mac-A": true},
	}
	h := &Handler{Hub: hub, Snapshot: provider}
	srv := httptest.NewServer(http.HandlerFunc(h.Serve))
	t.Cleanup(srv.Close)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	for i := 0; i < 2; i++ {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var f map[string]any
		_ = json.Unmarshal(data, &f)
		if f["kind"] == "pollers" {
			online, _ := f["online"].(map[string]any)
			if online["mac-A"] != true {
				t.Errorf("expected mac-A online, got %+v", online)
			}
			return
		}
	}
	t.Fatal("never received pollers frame")
}

type staticSnapshotWithPollers struct {
	sessions []*state.Session
	pollers  map[string]bool
}

func (s *staticSnapshotWithPollers) Snapshot() []*state.Session     { return s.sessions }
func (s *staticSnapshotWithPollers) PollersOnline() map[string]bool { return s.pollers }
