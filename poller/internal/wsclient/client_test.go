package wsclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestSend_DeliversEnvelope(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var (
		mu       sync.Mutex
		received []map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			var m map[string]any
			_ = json.Unmarshal(data, &m)
			mu.Lock()
			received = append(received, m)
			mu.Unlock()
		}
	}))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	cli := New(url)
	if err := cli.Connect(); err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	if err := cli.Send(map[string]any{"hello": "world"}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 || received[0]["hello"] != "world" {
		t.Errorf("got %#v", received)
	}
}

func TestRun_DispatchesDeleteCommand(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.WriteMessage(websocket.TextMessage, []byte(
			`{"type":"delete","request_id":"r1","session_id":"abc","project_dir":"-x"}`,
		))
		time.Sleep(150 * time.Millisecond)
	}))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	cli := New(url)
	if err := cli.Connect(); err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	type call struct{ id, dir string }
	got := make(chan call, 1)
	cli.OnCommand = func(cmd Command) error {
		got <- call{id: cmd.SessionID, dir: cmd.ProjectDir}
		return nil
	}

	stop := make(chan struct{})
	defer close(stop)
	go cli.Run(stop)

	select {
	case c := <-got:
		if c.id != "abc" || c.dir != "-x" {
			t.Errorf("got %+v", c)
		}
	case <-time.After(time.Second):
		t.Fatal("OnCommand was never invoked")
	}
}
