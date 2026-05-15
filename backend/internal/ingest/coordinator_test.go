package ingest

import (
	"errors"
	"testing"
	"time"
)

func TestCoordinator_BeginAndResolveOK(t *testing.T) {
	c := NewDeleteCoordinator(time.Second)
	defer c.Close()

	id := "req-1"
	resp, err := c.Begin(id)
	if err != nil {
		t.Fatal(err)
	}
	go c.Resolve(id, true, "")
	r := <-resp
	if r.Err != nil {
		t.Errorf("Err = %v, want nil", r.Err)
	}
}

func TestCoordinator_ResolveError(t *testing.T) {
	c := NewDeleteCoordinator(time.Second)
	defer c.Close()

	id := "req-2"
	resp, _ := c.Begin(id)
	go c.Resolve(id, false, "boom")
	r := <-resp
	if r.Err == nil || r.Err.Error() != "boom" {
		t.Errorf("Err = %v, want 'boom'", r.Err)
	}
}

func TestCoordinator_Timeout(t *testing.T) {
	c := NewDeleteCoordinator(50 * time.Millisecond)
	defer c.Close()

	id := "req-3"
	resp, _ := c.Begin(id)
	r := <-resp
	if !errors.Is(r.Err, ErrTimeout) {
		t.Errorf("Err = %v, want ErrTimeout", r.Err)
	}
}

func TestCoordinator_DuplicateBeginRejected(t *testing.T) {
	c := NewDeleteCoordinator(time.Second)
	defer c.Close()
	if _, err := c.Begin("dup"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Begin("dup"); err == nil {
		t.Fatal("duplicate Begin should return error")
	}
}

func TestCoordinator_ResolveUnknownIsNoop(t *testing.T) {
	c := NewDeleteCoordinator(time.Second)
	defer c.Close()
	c.Resolve("nope", true, "")
}
