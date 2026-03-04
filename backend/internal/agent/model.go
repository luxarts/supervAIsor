package agent

import "time"

type Status string
type Type string

const (
	StatusIdle    Status = "idle"
	StatusWorking Status = "working"
	StatusStopped Status = "stopped"
	StatusError   Status = "error"
)

const (
	TypeDeveloper Type = "developer"
	TypeReviewer  Type = "reviewer"
	TypeTester    Type = "tester"
	TypeDesigner  Type = "designer"
)

type Agent struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        Type      `json:"type"`
	Status      Status    `json:"status"`
	CurrentTool string    `json:"current_tool,omitempty"`
	LastEvent   string    `json:"last_event,omitempty"`
	WorkDir     string    `json:"work_dir"`
	Pid         int       `json:"pid,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}
