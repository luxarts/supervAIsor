package events

import (
	"encoding/json"
	"time"
)

// IngestEnvelope is one message the poller sends over /ws/ingest.
type IngestEnvelope struct {
	Hostname   string          `json:"hostname"`
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
	Model   string         `json:"model,omitempty"`
	Usage   *Usage         `json:"usage,omitempty"`
	Content []ContentBlock `json:"content,omitempty"`
}

// Usage carries the token accounting fields Anthropic returns on
// assistant messages. Any field may be zero/missing.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// UnmarshalJSON accepts either []ContentBlock or string for the content field
// and additionally captures model + usage when present.
func (m *MessageContent) UnmarshalJSON(data []byte) error {
	var aux struct {
		Role    string          `json:"role,omitempty"`
		Model   string          `json:"model,omitempty"`
		Usage   *Usage          `json:"usage,omitempty"`
		Content json.RawMessage `json:"content,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	m.Role = aux.Role
	m.Model = aux.Model
	m.Usage = aux.Usage
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
	IsError   bool            `json:"is_error,omitempty"`    // tool_result
}

// Event is one row from the events table, as returned by store.ListEvents.
// Defined here (not in store) to avoid an import cycle: store imports state,
// and state needs this type for ComputeStats.
type Event struct {
	TS      time.Time       `json:"ts"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}
