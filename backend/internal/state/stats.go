package state

import (
	"encoding/json"
	"time"

	"github.com/luxarts/supervaisor/internal/events"
)

// Stats is the on-demand aggregate view of a session's stored events.
type Stats struct {
	Hostname         string         `json:"hostname"`
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Project          string         `json:"project"`
	Model            string         `json:"model,omitempty"`
	StartedAt        time.Time      `json:"started_at"`
	LastEventAt      time.Time      `json:"last_event_at"`
	WallClockSeconds int64          `json:"wall_clock_seconds"`
	Tokens           TokenTotals    `json:"tokens"`
	Counts           SessionCounts  `json:"counts"`
	ToolBreakdown    map[string]int `json:"tool_breakdown"`
}

type TokenTotals struct {
	Input         int `json:"input"`
	Output        int `json:"output"`
	CacheCreation int `json:"cache_creation"`
	CacheRead     int `json:"cache_read"`
}

type SessionCounts struct {
	UserPrompts    int `json:"user_prompts"`
	AssistantTurns int `json:"assistant_turns"`
	ToolCalls      int `json:"tool_calls"`
	Errors         int `json:"errors"`
}

// ComputeStats aggregates over a session's stored events. Pure function:
// no I/O, tolerant of malformed payloads (silently skipped). The returned
// ToolBreakdown is always non-nil so callers can range over it safely.
func ComputeStats(evs []events.Event, sess *Session) Stats {
	out := Stats{
		Hostname:      sess.Hostname,
		ID:            sess.ID,
		Name:          sess.Name,
		Project:       sess.Project,
		StartedAt:     sess.StartedAt,
		LastEventAt:   sess.LastEventAt,
		ToolBreakdown: map[string]int{},
	}
	if !sess.LastEventAt.IsZero() && !sess.StartedAt.IsZero() {
		d := sess.LastEventAt.Sub(sess.StartedAt)
		if d > 0 {
			out.WallClockSeconds = int64(d.Seconds())
		}
	}

	for _, ev := range evs {
		var line events.RawLine
		if err := json.Unmarshal(ev.Payload, &line); err != nil {
			continue
		}
		switch line.Type {
		case "assistant":
			out.Counts.AssistantTurns++
			if line.Message == nil {
				continue
			}
			if line.Message.Model != "" {
				out.Model = line.Message.Model
			}
			if u := line.Message.Usage; u != nil {
				out.Tokens.Input += u.InputTokens
				out.Tokens.Output += u.OutputTokens
				out.Tokens.CacheCreation += u.CacheCreationInputTokens
				out.Tokens.CacheRead += u.CacheReadInputTokens
			}
			for _, b := range line.Message.Content {
				if b.Type == "tool_use" {
					out.Counts.ToolCalls++
					if b.Name != "" {
						out.ToolBreakdown[b.Name]++
					}
				}
			}
		case "user":
			if line.Message == nil {
				continue
			}
			sawToolResult := false
			sawText := false
			for _, b := range line.Message.Content {
				switch b.Type {
				case "tool_result":
					sawToolResult = true
					if b.IsError {
						out.Counts.Errors++
					}
				case "text":
					if b.Text != "" {
						sawText = true
					}
				}
			}
			if !sawToolResult && sawText {
				out.Counts.UserPrompts++
			}
		}
	}
	return out
}
