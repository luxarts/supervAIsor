package broadcast

import "sync"

// Hub fans out messages to all subscribed WebSocket clients.
// Run must be started as a goroutine before calling Subscribe/Broadcast.
type Hub struct {
	mu      sync.Mutex
	subs    map[chan []byte]struct{}
	in      chan []byte
	done    chan struct{}
	stopped bool
}

// NewHub constructs a Hub ready to run.
func NewHub() *Hub {
	return &Hub{
		subs: map[chan []byte]struct{}{},
		in:   make(chan []byte, 64),
		done: make(chan struct{}),
	}
}

// Run processes incoming messages and fans them out to all subscribers.
// Must be run as a goroutine. Exits when Stop is called.
func (h *Hub) Run() {
	for {
		select {
		case <-h.done:
			return
		case msg := <-h.in:
			h.mu.Lock()
			for c := range h.subs {
				select {
				case c <- msg:
				default: // drop if subscriber is slow
				}
			}
			h.mu.Unlock()
		}
	}
}

// Stop shuts down the hub. Safe to call multiple times.
func (h *Hub) Stop() {
	h.mu.Lock()
	if !h.stopped {
		h.stopped = true
		close(h.done)
	}
	h.mu.Unlock()
}

// Subscribe creates and registers a new subscriber channel (buffer 32).
func (h *Hub) Subscribe() chan []byte {
	c := make(chan []byte, 32)
	h.mu.Lock()
	h.subs[c] = struct{}{}
	h.mu.Unlock()
	return c
}

// Unsubscribe removes the subscriber and closes its channel.
func (h *Hub) Unsubscribe(c chan []byte) {
	h.mu.Lock()
	delete(h.subs, c)
	h.mu.Unlock()
	close(c)
}

// Broadcast sends msg to the hub's input channel (non-blocking, drops if full).
func (h *Hub) Broadcast(msg []byte) {
	select {
	case h.in <- msg:
	default:
	}
}
