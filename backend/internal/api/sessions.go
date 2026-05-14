package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

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
