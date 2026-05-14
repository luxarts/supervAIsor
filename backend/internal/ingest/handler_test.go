package ingest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/luxarts/supervaisor/internal/broadcast"
	"github.com/luxarts/supervaisor/internal/events"
	"github.com/luxarts/supervaisor/internal/store"
)

func wsURL(s string) string { return "ws" + strings.TrimPrefix(s, "http") }

func TestIngest_PersistsAndBroadcasts(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hub := broadcast.NewHub()
	go hub.Run()
	defer hub.Stop()
	sub := hub.Subscribe()

	h := &Handler{Store: db, Hub: hub}

	srv := httptest.NewServer(http.HandlerFunc(h.Serve))
	defer srv.Close()

	c, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	now := time.Now().UTC().Truncate(time.Second)
	env := events.IngestEnvelope{
		SessionID: "s1", ProjectDir: "-tmp", FileMTime: now, LineIndex: 0,
		Raw: json.RawMessage(`{"type":"custom-title","customTitle":"hi","timestamp":"` + now.Format(time.RFC3339) + `"}`),
	}
	if err := c.WriteJSON(env); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-sub:
		if !strings.Contains(string(msg), `"kind":"update"`) {
			t.Errorf("expected update frame, got: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no broadcast")
	}

	got, _ := db.ListSessions(context.Background())
	if len(got) != 1 || got[0].Name != "hi" {
		t.Errorf("db: %#v", got)
	}
}
