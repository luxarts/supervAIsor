package ingest

import (
	"sync"
	"testing"
	"time"
)

func TestRegistry_AddRemoveOnline(t *testing.T) {
	r := NewRegistry()
	if r.IsOnline("h1") {
		t.Fatal("h1 should be offline before Add")
	}
	chA := make(chan []byte, 1)
	r.Add("h1", chA)
	if !r.IsOnline("h1") {
		t.Fatal("h1 should be online after Add")
	}
	if got := r.Online()["h1"]; !got {
		t.Fatalf("Online()['h1'] = %v, want true", got)
	}
	r.Remove("h1", chA)
	if r.IsOnline("h1") {
		t.Fatal("h1 should be offline after Remove")
	}
}

func TestRegistry_MultipleConnsForSameHost(t *testing.T) {
	r := NewRegistry()
	a := make(chan []byte, 1)
	b := make(chan []byte, 1)
	r.Add("h", a)
	r.Add("h", b)
	r.Remove("h", a)
	if !r.IsOnline("h") {
		t.Fatal("host should still be online with one remaining conn")
	}
	r.Remove("h", b)
	if r.IsOnline("h") {
		t.Fatal("host should be offline after all conns removed")
	}
}

func TestRegistry_SendDeliversAndErrorsWhenOffline(t *testing.T) {
	r := NewRegistry()
	if err := r.Send("missing", []byte("x")); err == nil {
		t.Fatal("Send to offline host should return error")
	}
	ch := make(chan []byte, 1)
	r.Add("h", ch)
	if err := r.Send("h", []byte("hello")); err != nil {
		t.Fatalf("Send to online host: %v", err)
	}
	select {
	case msg := <-ch:
		if string(msg) != "hello" {
			t.Errorf("got %q, want hello", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestRegistry_SubscribeReceivesTransitions(t *testing.T) {
	r := NewRegistry()
	sub, unsub := r.Subscribe()
	defer unsub()

	a := make(chan []byte, 1)

	var (
		mu        sync.Mutex
		snapshots []map[string]bool
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for snap := range sub {
			mu.Lock()
			snapshots = append(snapshots, snap)
			if len(snapshots) >= 2 {
				mu.Unlock()
				return
			}
			mu.Unlock()
		}
	}()

	r.Add("h1", a)
	r.Remove("h1", a)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("did not receive both transitions")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(snapshots) < 2 {
		t.Fatalf("got %d snapshots", len(snapshots))
	}
	if !snapshots[0]["h1"] {
		t.Errorf("first snapshot should show h1 online: %+v", snapshots[0])
	}
	if snapshots[1]["h1"] {
		t.Errorf("second snapshot should show h1 offline: %+v", snapshots[1])
	}
}
