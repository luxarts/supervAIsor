package ingest

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/luxarts/supervaisor/internal/broadcast"
	"github.com/luxarts/supervaisor/internal/events"
	"github.com/luxarts/supervaisor/internal/state"
	"github.com/luxarts/supervaisor/internal/store"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler is the HTTP handler for the /ws/ingest WebSocket endpoint.
// It processes ingest envelopes from host pollers, applying state derivation,
// persisting, and broadcasting updates. Multiple concurrent pollers are
// supported; sessions are keyed by (hostname, session_id).
type Handler struct {
	Store *store.SQLite
	Hub   *broadcast.Hub
}

type updateFrame struct {
	Kind    string         `json:"kind"`
	Session *state.Session `json:"session"`
}

// Serve handles a single /ws/ingest WebSocket connection.
func (h *Handler) Serve(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ingest upgrade: %v", err)
		return
	}
	defer conn.Close()

	ctx := r.Context()
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var env events.IngestEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			log.Printf("ingest: malformed envelope: %v", err)
			continue
		}

		if env.Hostname == "" {
			log.Printf("ingest: missing hostname for session_id=%s; dropping", env.SessionID)
			continue
		}

		prev, _ := h.Store.GetSession(ctx, env.Hostname, env.SessionID)
		next, err := state.Apply(prev, env)
		if err != nil {
			log.Printf("ingest: apply: %v", err)
			continue
		}

		// Recompute time-based status transitions immediately.
		state.RecomputeStatus(next, time.Now().UTC())

		if err := h.Store.UpsertSession(ctx, next); err != nil {
			log.Printf("ingest: upsert: %v", err)
			continue
		}
		if err := h.Store.AppendEvent(ctx, env.Hostname, env.SessionID, next.LastEventAt, peekType(env.Raw), env.Raw); err != nil {
			log.Printf("ingest: append event: %v", err)
		}

		frame := updateFrame{Kind: "update", Session: next}
		if b, err := json.Marshal(frame); err == nil {
			h.Hub.Broadcast(b)
		}
	}
}

// peekType extracts the "type" field from a raw JSONL line without full decode.
func peekType(raw json.RawMessage) string {
	var head struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &head)
	return head.Type
}
