package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/luxarts/supervaisor/internal/broadcast"
	"github.com/luxarts/supervaisor/internal/events"
	"github.com/luxarts/supervaisor/internal/ingest"
	"github.com/luxarts/supervaisor/internal/state"
	"github.com/luxarts/supervaisor/internal/store"
)

// PollerSender is the subset of *ingest.Registry the API needs. Fakeable
// in tests.
type PollerSender interface {
	IsOnline(hostname string) bool
	Send(hostname string, payload []byte) error
}

// Handler holds dependencies for the REST API handlers.
type Handler struct {
	Store       *store.SQLite
	Sender      PollerSender
	Coordinator *ingest.DeleteCoordinator
	Hub         *broadcast.Hub
}

// Register mounts all REST routes onto the given Gin engine.
func (h *Handler) Register(r *gin.Engine) {
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.GET("/sessions", h.listSessions)
	r.GET("/sessions/:hostname/:id/events", h.getSessionEvents)
	r.GET("/sessions/:hostname/:id/stats", h.getSessionStats)
	r.DELETE("/sessions/:hostname/:id", h.deleteSession)
}

func (h *Handler) listSessions(c *gin.Context) {
	sessions, err := h.Store.ListSessions(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if sessions == nil {
		sessions = []*state.Session{}
	}
	c.JSON(http.StatusOK, sessions)
}

func (h *Handler) getSessionEvents(c *gin.Context) {
	hostname := c.Param("hostname")
	id := c.Param("id")
	ctx := c.Request.Context()

	sess, err := h.Store.GetSession(ctx, hostname, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	limit := 500
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 5000 {
			limit = n
		}
	}

	evs, err := h.Store.ListEvents(ctx, hostname, id, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if evs == nil {
		evs = []events.Event{}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, evs)
}

func (h *Handler) getSessionStats(c *gin.Context) {
	hostname := c.Param("hostname")
	id := c.Param("id")
	ctx := c.Request.Context()

	sess, err := h.Store.GetSession(ctx, hostname, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	// Pass -1 for unlimited so aggregates include the full event log.
	evs, err := h.Store.ListEvents(ctx, hostname, id, -1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	stats := state.ComputeStats(evs, sess)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, stats)
}

type deleteCommand struct {
	Type       string `json:"type"`
	RequestID  string `json:"request_id"`
	SessionID  string `json:"session_id"`
	ProjectDir string `json:"project_dir"`
}

type sessionRemovedFrame struct {
	Kind     string `json:"kind"`
	Hostname string `json:"hostname"`
	ID       string `json:"id"`
}

func newRequestID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (h *Handler) deleteSession(c *gin.Context) {
	hostname := c.Param("hostname")
	id := c.Param("id")
	ctx := c.Request.Context()

	sess, err := h.Store.GetSession(ctx, hostname, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	if h.Sender == nil || !h.Sender.IsOnline(hostname) {
		c.JSON(http.StatusConflict, gin.H{"error": "poller offline"})
		return
	}

	reqID := newRequestID()
	respCh, err := h.Coordinator.Begin(reqID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	cmd := deleteCommand{
		Type:       "delete",
		RequestID:  reqID,
		SessionID:  id,
		ProjectDir: sess.ProjectDirEncoded,
	}
	body, _ := json.Marshal(cmd)
	if err := h.Sender.Send(hostname, body); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	select {
	case r := <-respCh:
		if r.Err != nil {
			if errors.Is(r.Err, ingest.ErrTimeout) {
				c.JSON(http.StatusGatewayTimeout, gin.H{"error": r.Err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": r.Err.Error()})
			return
		}
	case <-ctx.Done():
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "client cancelled"})
		return
	}

	if err := h.Store.DeleteSession(ctx, hostname, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.Hub != nil {
		removed := sessionRemovedFrame{Kind: "session_removed", Hostname: hostname, ID: id}
		if b, err := json.Marshal(removed); err == nil {
			h.Hub.Broadcast(b)
		}
	}
	c.Status(http.StatusNoContent)
}
