package ws

import (
	"encoding/json"
	"log"
	"sync"
)

// Event is the envelope sent to every connected frontend client.
type Event struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// Hub maintains the set of active WebSocket clients and broadcasts events to them.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*Client]struct{})}
}

func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
}

// Broadcast encodes event as JSON and sends it to all connected clients.
// Slow clients that cannot keep up are dropped.
func (h *Hub) Broadcast(event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("ws hub: marshal: %v", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for c := range h.clients {
		select {
		case c.send <- data:
		default:
			// client buffer full — drop message rather than block
		}
	}
}
