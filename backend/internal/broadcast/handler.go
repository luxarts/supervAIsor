package broadcast

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/luxarts/supervaisor/internal/state"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// SnapshotProvider supplies the current session list and per-host poller
// liveness for initial hydration.
type SnapshotProvider interface {
	Snapshot() []*state.Session
	PollersOnline() map[string]bool
}

// Handler upgrades HTTP connections to WebSocket, sends a snapshot frame,
// then forwards Hub broadcasts to the connection.
type Handler struct {
	Hub      *Hub
	Snapshot SnapshotProvider
}

type frame struct {
	Kind string `json:"kind"`
	// Sessions and Online intentionally do NOT use omitempty: encoding/json
	// omits empty slices and maps, which would make a fresh-install
	// snapshot/pollers frame arrive with no "sessions"/"online" key at all,
	// crashing the frontend's setSessions/setPollersOnline.
	Sessions []*state.Session `json:"sessions"`
	Session  *state.Session   `json:"session,omitempty"`
	Online   map[string]bool  `json:"online"`
}

// Serve handles the /ws/clients WebSocket endpoint.
func (h *Handler) Serve(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("clients upgrade: %v", err)
		return
	}
	defer conn.Close()

	// Send initial snapshot so the client can hydrate without a REST call.
	snap := frame{Kind: "snapshot", Sessions: h.Snapshot.Snapshot()}
	if b, err := json.Marshal(snap); err == nil {
		_ = conn.WriteMessage(websocket.TextMessage, b)
	}

	pollersFrame := frame{Kind: "pollers", Online: h.Snapshot.PollersOnline()}
	if b, err := json.Marshal(pollersFrame); err == nil {
		_ = conn.WriteMessage(websocket.TextMessage, b)
	}

	sub := h.Hub.Subscribe()
	defer h.Hub.Unsubscribe(sub)

	// Reader goroutine: drop all incoming messages (clients are read-only)
	// and signal when the connection is closed.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := conn.NextReader(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-closed:
			return
		case msg, ok := <-sub:
			if !ok {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}
}
