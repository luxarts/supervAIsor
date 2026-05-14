package wsclient

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	url  string
	mu   sync.Mutex
	conn *websocket.Conn
}

func New(url string) *Client { return &Client{url: url} }

func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return nil
	}
	conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) Send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return websocket.ErrCloseSent
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.conn.WriteMessage(websocket.TextMessage, b)
}

// EnsureConnected retries connect with exponential backoff capped at 30s.
// It returns once connected or stop is closed (caller passes its own stop channel).
func (c *Client) EnsureConnected(stop <-chan struct{}) {
	backoff := time.Second
	for {
		err := c.Connect()
		if err == nil {
			return
		}
		select {
		case <-stop:
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}
