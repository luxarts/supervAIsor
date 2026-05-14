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
//
// Claude Code's JSONL uses two shapes for message.content:
//   - a plain string (real user prompts, system reminders)
//   - an array of typed blocks (assistant replies, tool_use, tool_result)
//
// UnmarshalJSON normalizes the string form to a single synthetic text block
// so callers can treat Content uniformly.
type MessageContent struct {
	Role    string         `json:"role,omitempty"`
	Content []ContentBlock `json:"content,omitempty"`
}

// UnmarshalJSON accepts either []ContentBlock or string for the content field.
func (m *MessageContent) UnmarshalJSON(data []byte) error {
	var aux struct {
		Role    string          `json:"role,omitempty"`
		Content json.RawMessage `json:"content,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	m.Role = aux.Role
	if len(aux.Content) == 0 || string(aux.Content) == "null" {
		m.Content = nil
		return nil
	}
	// Try array first.
	if err := json.Unmarshal(aux.Content, &m.Content); err == nil {
		return nil
	}
	// Fall back to string.
	var s string
	if err := json.Unmarshal(aux.Content, &s); err != nil {
		return err
	}
	m.Content = []ContentBlock{{Type: "text", Text: s}}
	return nil
}

type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Name      string          `json:"name,omitempty"`        // tool_use
	Input     json.RawMessage `json:"input,omitempty"`       // tool_use
	ID        string          `json:"id,omitempty"`          // tool_use id
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result
}
