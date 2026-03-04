package hooks

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/luxarts/supervaisor/internal/agent"
	"github.com/luxarts/supervaisor/internal/state"
	"github.com/luxarts/supervaisor/internal/ws"
)

// Payload mirrors the JSON that Claude Code sends via its hook commands.
// Fields are populated depending on the event type.
type Payload struct {
	ToolName     string          `json:"tool_name"`
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolResponse json.RawMessage `json:"tool_response"`
	Message      string          `json:"message"` // Notification events
}

type Handler struct {
	store *state.Store
	hub   *ws.Hub
}

func NewHandler(store *state.Store, hub *ws.Hub) *Handler {
	return &Handler{store: store, hub: hub}
}

// Handle processes POST /hooks?agent_id=<id>&event=<type>
func (h *Handler) Handle(c *gin.Context) {
	agentID := c.Query("agent_id")
	event := c.Query("event")

	if agentID == "" || event == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id and event are required"})
		return
	}

	var payload Payload
	if err := c.ShouldBindJSON(&payload); err != nil {
		log.Printf("hooks: decode payload for agent %s event %s: %v", agentID, event, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	a, err := h.store.Get(agentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	wsEventType := "AGENT_UPDATED"

	switch event {
	case "PreToolUse":
		a.Status = agent.StatusWorking
		a.CurrentTool = payload.ToolName
		a.LastEvent = event
	case "PostToolUse":
		a.CurrentTool = payload.ToolName
		a.LastEvent = event
	case "Notification":
		a.LastEvent = payload.Message
	case "Stop":
		a.Status = agent.StatusStopped
		a.CurrentTool = ""
		a.LastEvent = event
		wsEventType = "AGENT_STOPPED"
	default:
		log.Printf("hooks: unknown event type %q for agent %s", event, agentID)
	}

	h.store.Set(a)
	h.hub.Broadcast(ws.Event{Type: wsEventType, Payload: a})

	c.Status(http.StatusOK)
}
