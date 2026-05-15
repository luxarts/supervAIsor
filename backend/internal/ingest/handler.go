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
	Store       *store.SQLite
	Hub         *broadcast.Hub
	Registry    *Registry
	Coordinator *DeleteCoordinator
}

type updateFrame struct {
	Kind    string         `json:"kind"`
	Session *state.Session `json:"session"`
}

// envelopeWithType is used to peek at the discriminator before full decode.
type envelopeWithType struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	OK        bool   `json:"ok,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Serve handles a single /ws/ingest WebSocket connection. Splits into a
// reader goroutine (existing event-processing path) and a writer goroutine
// (drains the registry's writer channel for this connection).
func (h *Handler) Serve(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ingest upgrade: %v", err)
		return
	}
	defer conn.Close()

	writeCh := make(chan []byte, 16)
	done := make(chan struct{})

	// Writer goroutine.
	go func() {
		for {
			select {
			case <-done:
				return
			case msg, ok := <-writeCh:
				if !ok {
					return
				}
				_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					return
				}
			}
		}
	}()
	defer close(done)

	var registeredHost string
	defer func() {
		if registeredHost != "" && h.Registry != nil {
			h.Registry.Remove(registeredHost, writeCh)
			log.Printf("ingest: unregistered poller hostname=%q", registeredHost)
		}
	}()

	ctx := r.Context()
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		// Peek at discriminator.
		var head envelopeWithType
		_ = json.Unmarshal(data, &head)

		if head.Type == "delete_ack" {
			log.Printf("ingest: delete_ack req=%s ok=%v err=%q", head.RequestID, head.OK, head.Error)
			if h.Coordinator != nil {
				h.Coordinator.Resolve(head.RequestID, head.OK, head.Error)
			}
			continue
		}

		// Default path: treat as event envelope.
		var env events.IngestEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			log.Printf("ingest: malformed envelope: %v", err)
			continue
		}

		if env.Hostname == "" {
			log.Printf("ingest: missing hostname for session_id=%s; dropping", env.SessionID)
			continue
		}

		if registeredHost == "" && h.Registry != nil {
			h.Registry.Add(env.Hostname, writeCh)
			registeredHost = env.Hostname
			log.Printf("ingest: registered poller hostname=%q", env.Hostname)
		}

		prev, _ := h.Store.GetSession(ctx, env.Hostname, env.SessionID)
		next, err := state.Apply(prev, env)
		if err != nil {
			log.Printf("ingest: apply: %v", err)
			continue
		}

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
