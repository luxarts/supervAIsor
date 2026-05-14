package broadcast

import (
	"testing"
	"time"
)

func TestHub_BroadcastReachesSubscribers(t *testing.T) {
	h := NewHub()
	go h.Run()
	t.Cleanup(h.Stop)

	c1 := h.Subscribe()
	c2 := h.Subscribe()

	h.Broadcast([]byte(`{"ok":1}`))

	for _, c := range []chan []byte{c1, c2} {
		select {
		case msg := <-c:
			if string(msg) != `{"ok":1}` {
				t.Errorf("got %s", msg)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for broadcast")
		}
	}
}

func TestHub_Stop_Idempotent(t *testing.T) {
	h := NewHub()
	go h.Run()
	h.Stop()
	h.Stop() // must not panic
}

func TestHub_Unsubscribe(t *testing.T) {
	h := NewHub()
	go h.Run()
	t.Cleanup(h.Stop)

	c := h.Subscribe()
	h.Unsubscribe(c)

	// After unsubscribe the channel is closed; broadcast must not deadlock.
	h.Broadcast([]byte(`{"test":1}`))
}
