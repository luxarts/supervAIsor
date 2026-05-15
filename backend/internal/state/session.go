package state

import "time"

type Status string

const (
	StatusWorking Status = "working"
	StatusDone    Status = "done"
	StatusStale   Status = "stale"
)

// Session is the derived view of a Claude Code session.
type Session struct {
	Hostname      string     `json:"hostname"`
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Project       string     `json:"project"`
	Status        Status     `json:"status"`
	StartedAt     time.Time  `json:"started_at"`
	LastPromptAt  *time.Time `json:"last_prompt_at,omitempty"`
	CurrentAction string     `json:"current_action"`
	LastEventAt   time.Time  `json:"last_event_at"`
	LastErrorAt       time.Time  `json:"last_error_at,omitempty"`
	ProjectDirEncoded string     `json:"project_dir_encoded,omitempty"`

	// Internal book-keeping not exposed in JSON.
	PendingToolUseIDs map[string]struct{} `json:"-"`
}
