package api

import (
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/luxarts/supervaisor/internal/agent"
	"github.com/luxarts/supervaisor/internal/state"
	wsinternal "github.com/luxarts/supervaisor/internal/ws"
)

var upgrader = websocket.Upgrader{
	// Allow all origins during development. Restrict in production.
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Handler struct {
	store   *state.Store
	manager *agent.Manager
	hub     *wsinternal.Hub
	port    string
}

func NewHandler(store *state.Store, manager *agent.Manager, hub *wsinternal.Hub, port string) *Handler {
	return &Handler{store: store, manager: manager, hub: hub, port: port}
}

// ListAgents godoc
// GET /agents
func (h *Handler) ListAgents(c *gin.Context) {
	c.JSON(http.StatusOK, h.store.List())
}

type createRequest struct {
	Name    string     `json:"name"     binding:"required"`
	Type    agent.Type `json:"type"     binding:"required"`
	WorkDir string     `json:"work_dir" binding:"required"`
	Prompt  string     `json:"prompt"   binding:"required"`
}

// CreateAgent godoc
// POST /agents
func (h *Handler) CreateAgent(c *gin.Context) {
	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if _, err := os.Stat(req.WorkDir); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "work_dir does not exist"})
		return
	}

	a := &agent.Agent{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Type:      req.Type,
		Status:    agent.StatusIdle,
		WorkDir:   req.WorkDir,
		CreatedAt: time.Now(),
	}

	cmd, err := h.manager.Spawn(agent.SpawnConfig{
		AgentID:  a.ID,
		WorkDir:  req.WorkDir,
		Prompt:   req.Prompt,
		HookBase: "http://localhost:" + h.port,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	a.Pid = cmd.Process.Pid
	a.Status = agent.StatusWorking

	h.store.Set(a)
	h.hub.Broadcast(wsinternal.Event{Type: "AGENT_CREATED", Payload: a})

	c.JSON(http.StatusCreated, a)
}

// DeleteAgent godoc
// DELETE /agents/:id
func (h *Handler) DeleteAgent(c *gin.Context) {
	id := c.Param("id")

	if err := h.manager.Kill(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	h.store.Delete(id)
	h.hub.Broadcast(wsinternal.Event{Type: "AGENT_STOPPED", Payload: gin.H{"id": id}})

	c.Status(http.StatusNoContent)
}

// WebSocket godoc
// GET /ws
func (h *Handler) WebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	client := wsinternal.NewClient(h.hub, conn, nil)
	h.hub.Register(client)

	go client.WritePump()
	go client.ReadPump()
}
