package ingest

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrTimeout is the error sent on a pending request when no ack arrives
// within the coordinator's configured timeout.
var ErrTimeout = errors.New("poller did not ack within timeout")

// DeleteResult is the outcome of a delete request awaited by an HTTP handler.
type DeleteResult struct {
	Err error
}

type pending struct {
	resp  chan DeleteResult
	timer *time.Timer
}

// DeleteCoordinator correlates outbound delete commands with delete_ack
// envelopes by request ID. Use Begin to obtain the response channel before
// sending; Resolve when an ack arrives. Pending requests time out after
// the configured duration.
type DeleteCoordinator struct {
	timeout time.Duration

	mu      sync.Mutex
	pending map[string]*pending
	closed  bool
}

func NewDeleteCoordinator(timeout time.Duration) *DeleteCoordinator {
	return &DeleteCoordinator{
		timeout: timeout,
		pending: map[string]*pending{},
	}
}

// Begin registers a pending request. The returned channel receives exactly
// one DeleteResult, either from Resolve or from the timeout.
func (c *DeleteCoordinator) Begin(reqID string) (<-chan DeleteResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("coordinator closed")
	}
	if _, exists := c.pending[reqID]; exists {
		return nil, fmt.Errorf("request id %q already pending", reqID)
	}
	resp := make(chan DeleteResult, 1)
	p := &pending{resp: resp}
	p.timer = time.AfterFunc(c.timeout, func() {
		c.fire(reqID, DeleteResult{Err: ErrTimeout})
	})
	c.pending[reqID] = p
	return resp, nil
}

// Resolve completes a pending request with success (ok=true) or an error
// (ok=false, msg becomes the error string). Unknown reqIDs are ignored.
func (c *DeleteCoordinator) Resolve(reqID string, ok bool, msg string) {
	if ok {
		c.fire(reqID, DeleteResult{Err: nil})
		return
	}
	if msg == "" {
		msg = "unknown error"
	}
	c.fire(reqID, DeleteResult{Err: errors.New(msg)})
}

func (c *DeleteCoordinator) fire(reqID string, r DeleteResult) {
	c.mu.Lock()
	p, ok := c.pending[reqID]
	if !ok {
		c.mu.Unlock()
		return
	}
	delete(c.pending, reqID)
	c.mu.Unlock()
	p.timer.Stop()
	p.resp <- r
}

// Close stops accepting new requests. Outstanding requests still resolve
// via Resolve or timer.
func (c *DeleteCoordinator) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
}
