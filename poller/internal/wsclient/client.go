package wsclient

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Command is the parsed shape of a backend→poller command frame.
type Command struct {
	Type       string `json:"type"`
	RequestID  string `json:"request_id"`
	SessionID  string `json:"session_id"`
	ProjectDir string `json:"project_dir"`
}

// Ack is the poller→backend response to a Command.
type Ack struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	SessionID string `json:"session_id"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
}

type Client struct {
	url  string
	mu   sync.Mutex
	conn *websocket.Conn

	// OnCommand is invoked from Run for every received command frame.
	// Implementations should be quick and either return nil (successful
	// ack will be sent automatically) or an error (sent as the ack error).
	OnCommand func(cmd Command) error

	// OnConnect fires once per successful (re)connect, after the conn is
	// installed and the lock released. Runs in its own goroutine so the
	// callback may call Send safely. Use this to push a "hello" frame so
	// the backend can mark the host online before the first event flows.
	OnConnect func()
}

func New(url string) *Client { return &Client{url: url} }

func (c *Client) Connect() error {
	c.mu.Lock()
	if c.conn != nil {
		c.mu.Unlock()
		return nil
	}
	conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		c.mu.Unlock()
		return err
	}
	c.conn = conn
	onConnect := c.OnConnect
	c.mu.Unlock()
	log.Printf("wsclient: connected to %s", c.url)
	if onConnect != nil {
		// Run async so the callback can call Send without deadlocking on mu.
		go onConnect()
	}
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

// Run reads frames off the WebSocket and dispatches command frames to
// OnCommand. Each command produces an automatic ack on the same socket.
// Exits when stop is closed or the connection drops.
func (c *Client) Run(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}

		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			log.Printf("wsclient: read loop exiting: %v", err)
			// Make sure the broken conn doesn't get reused by the next
			// iteration of the relaunch loop — Close clears c.conn so
			// EnsureConnected reconnects on the scanner's next pass.
			c.Close()
			return
		}
		log.Printf("wsclient: received frame (%d bytes)", len(data))
		var cmd Command
		if err := json.Unmarshal(data, &cmd); err != nil {
			log.Printf("wsclient: malformed frame, ignoring: %v", err)
			continue
		}
		if cmd.Type == "" {
			log.Printf("wsclient: frame missing 'type', ignoring (preview=%.120s)", string(data))
			continue
		}
		log.Printf("wsclient: dispatch %s req=%s session=%s project_dir=%s", cmd.Type, cmd.RequestID, cmd.SessionID, cmd.ProjectDir)
		ackErr := ""
		ok := true
		if c.OnCommand != nil {
			if e := c.OnCommand(cmd); e != nil {
				ok = false
				ackErr = e.Error()
				log.Printf("wsclient: OnCommand error: %v", e)
			}
		} else {
			log.Printf("wsclient: WARNING no OnCommand handler set; acking ok with no action")
		}
		ackType := cmd.Type + "_ack"
		log.Printf("wsclient: send %s req=%s ok=%v err=%q", ackType, cmd.RequestID, ok, ackErr)
		if err := c.Send(Ack{Type: ackType, RequestID: cmd.RequestID, SessionID: cmd.SessionID, OK: ok, Error: ackErr}); err != nil {
			log.Printf("wsclient: ack send: %v", err)
			return
		}
	}
}
