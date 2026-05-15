package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/luxarts/supervaisor/internal/events"
	"github.com/luxarts/supervaisor/internal/state"
	"github.com/luxarts/supervaisor/internal/store"
)

// Handler holds dependencies for the REST API handlers.
type Handler struct {
	Store *store.SQLite
}

// Register mounts all REST routes onto the given Gin engine.
func (h *Handler) Register(r *gin.Engine) {
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.GET("/sessions", h.listSessions)
	r.GET("/sessions/:hostname/:id/events", h.getSessionEvents)
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
