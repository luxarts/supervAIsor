package ingest

import (
	"errors"
	"sync"
)

// Registry tracks active ingest connections per hostname. Each connection
// is represented by its outbound writer channel; the ingest handler is
// responsible for owning the channel lifecycle.
type Registry struct {
	mu    sync.RWMutex
	conns map[string]map[chan []byte]struct{}
	subs  map[chan map[string]bool]struct{}
}

// ErrNoPoller is returned by Send when no connection is registered for the
// requested hostname.
var ErrNoPoller = errors.New("no poller registered for hostname")

func NewRegistry() *Registry {
	return &Registry{
		conns: map[string]map[chan []byte]struct{}{},
		subs:  map[chan map[string]bool]struct{}{},
	}
}

// Add registers a writer channel for the given hostname.
func (r *Registry) Add(hostname string, ch chan []byte) {
	r.mu.Lock()
	if r.conns[hostname] == nil {
		r.conns[hostname] = map[chan []byte]struct{}{}
	}
	r.conns[hostname][ch] = struct{}{}
	snap := r.snapshotLocked()
	r.mu.Unlock()
	r.notify(snap)
}

// Remove deregisters a writer channel; if it was the last one for the
// hostname the host is reported as offline by IsOnline.
func (r *Registry) Remove(hostname string, ch chan []byte) {
	r.mu.Lock()
	if set, ok := r.conns[hostname]; ok {
		delete(set, ch)
		if len(set) == 0 {
			delete(r.conns, hostname)
		}
	}
	snap := r.snapshotLocked()
	r.mu.Unlock()
	r.notify(snap)
}

func (r *Registry) IsOnline(hostname string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.conns[hostname]) > 0
}

// Online returns a snapshot of the current online state.
func (r *Registry) Online() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshotLocked()
}

// Send delivers payload to one of the registered connections for hostname.
// Returns ErrNoPoller when no connection exists. The pick is arbitrary
// (Go map iteration order); callers should not assume affinity.
func (r *Registry) Send(hostname string, payload []byte) error {
	r.mu.RLock()
	set := r.conns[hostname]
	if len(set) == 0 {
		r.mu.RUnlock()
		return ErrNoPoller
	}
	var ch chan []byte
	for c := range set {
		ch = c
		break
	}
	r.mu.RUnlock()
	ch <- payload
	return nil
}

// Subscribe returns a channel that emits a snapshot of the online map on
// every Add/Remove transition. The returned func unsubscribes.
func (r *Registry) Subscribe() (<-chan map[string]bool, func()) {
	ch := make(chan map[string]bool, 8)
	r.mu.Lock()
	r.subs[ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		if _, ok := r.subs[ch]; ok {
			delete(r.subs, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
}

func (r *Registry) snapshotLocked() map[string]bool {
	out := make(map[string]bool, len(r.conns))
	for h := range r.conns {
		out[h] = true
	}
	return out
}

func (r *Registry) notify(snap map[string]bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for ch := range r.subs {
		select {
		case ch <- snap:
		default:
		}
	}
}
