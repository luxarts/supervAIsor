package events

import (
	"encoding/json"
	"time"
)

// IngestEnvelope is one message the poller sends over /ws/ingest.
type IngestEnvelope struct {
	SessionID  string          `json:"session_id"`
	ProjectDir string          `json:"project_dir"`
	FileMTime  time.Time       `json:"file_mtime"`
	LineIndex  int             `json:"line_index"`
	Raw        json.RawMessage `json:"raw"`
}

// RawLine is the parsed shape of a JSONL line; we only model the fields
// we actually read. Anything else stays in the original RawMessage.
type RawLine struct {
	Type        string          `json:"type"`
	Timestamp   *time.Time      `json:"timestamp,omitempty"`
	CustomTitle string          `json:"customTitle,omitempty"`
	AgentName   string          `json:"agentName,omitempty"`
	SessionID   string          `json:"sessionId,omitempty"`
	Message     *MessageContent `json:"message,omitempty"`
}

// MessageContent maps the relevant Claude message envelope.
type MessageContent struct {
	Role    string         `json:"role,omitempty"`
	Content []ContentBlock `json:"content,omitempty"`
}

type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Name      string          `json:"name,omitempty"`        // tool_use
	Input     json.RawMessage `json:"input,omitempty"`       // tool_use
	ID        string          `json:"id,omitempty"`          // tool_use id
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result
}
