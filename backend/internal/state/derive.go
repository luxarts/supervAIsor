package state

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/luxarts/supervaisor/internal/events"
)

const (
	workingDebounce = 2 * time.Second
	staleAfter      = time.Hour
)

// Apply takes the previous session state (or nil for the first event)
// and a new event, and returns the next session state. Pure function.
func Apply(prev *Session, env events.IngestEnvelope) (*Session, error) {
	var line events.RawLine
	if err := json.Unmarshal(env.Raw, &line); err != nil {
		return nil, fmt.Errorf("decode raw: %w", err)
	}

	next := Session{}
	if prev != nil {
		next = *prev
	}
	if next.PendingToolUseIDs == nil {
		next.PendingToolUseIDs = map[string]struct{}{}
	}

	if next.ID == "" {
		next.ID = env.SessionID
	}
	if next.Hostname == "" {
		next.Hostname = env.Hostname
	}
	if p := DecodeProjectDir(env.ProjectDir); p != "" {
		next.Project = p
	}
	// ProjectDirEncoded must hold the on-disk directory name (the
	// "-Users-x-Projects-foo" form), which is what the poller needs to
	// construct the JSONL path for delete. Prefer the explicit Raw field
	// from new pollers; fall back to ProjectDir for backward compat when
	// it doesn't look like a resolved absolute path.
	switch {
	case env.ProjectDirRaw != "":
		next.ProjectDirEncoded = env.ProjectDirRaw
	case env.ProjectDir != "" && !strings.HasPrefix(env.ProjectDir, "/"):
		next.ProjectDirEncoded = env.ProjectDir
	}

	ts := env.FileMTime
	if line.Timestamp != nil {
		ts = *line.Timestamp
	}

	if prev == nil {
		next.StartedAt = ts
		next.Name = shortID(env.SessionID)
	}
	next.LastEventAt = ts

	switch line.Type {
	case "custom-title":
		if line.CustomTitle != "" {
			next.Name = line.CustomTitle
		}
	case "agent-name":
		if line.AgentName != "" && (prev == nil || prev.Name == shortID(env.SessionID)) {
			next.Name = line.AgentName
		}
	case "assistant":
		applyAssistant(&next, line)
	case "user":
		applyUser(&next, line, ts)
	}

	RecomputeStatus(&next, ts)
	return &next, nil
}

func applyAssistant(s *Session, line events.RawLine) {
	if line.Message == nil {
		return
	}
	var lastTextAction, lastToolAction string
	for _, b := range line.Message.Content {
		switch b.Type {
		case "tool_use":
			s.PendingToolUseIDs[b.ID] = struct{}{}
			lastToolAction = describeToolUse(b)
		case "text":
			if t := strings.TrimSpace(b.Text); t != "" {
				lastTextAction = truncate(t, 80)
			}
		}
	}
	if lastToolAction != "" {
		s.CurrentAction = lastToolAction
	} else if lastTextAction != "" {
		s.CurrentAction = lastTextAction
	}
}

func applyUser(s *Session, line events.RawLine, ts time.Time) {
	if line.Message == nil {
		return
	}
	sawToolResult := false
	for _, b := range line.Message.Content {
		if b.Type == "tool_result" {
			sawToolResult = true
			delete(s.PendingToolUseIDs, b.ToolUseID)
			if b.IsError {
				s.LastErrorAt = ts
			}
		}
	}
	if !sawToolResult {
		// Real user prompt.
		for _, b := range line.Message.Content {
			if b.Type == "text" {
				s.CurrentAction = truncate("user: "+strings.TrimSpace(b.Text), 80)
				break
			}
		}
	}
}

func describeToolUse(b events.ContentBlock) string {
	var input map[string]any
	_ = json.Unmarshal(b.Input, &input)
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := input[k]; ok {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
			}
		}
		return ""
	}
	detail := ""
	switch b.Name {
	case "Write", "Edit", "Read", "NotebookEdit":
		detail = pick("file_path", "notebook_path")
	case "Bash":
		detail = truncate(pick("command"), 60)
	case "Task":
		detail = pick("description", "subagent_type")
	case "WebFetch", "WebSearch":
		detail = pick("url", "query")
	case "Grep", "Glob":
		detail = pick("pattern")
	}
	if detail == "" {
		return b.Name
	}
	return b.Name + ": " + detail
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func shortID(id string) string {
	if len(id) < 8 {
		return id
	}
	return id[:8]
}

// RecomputeStatus mutates s.Status based on pending tool_use and elapsed
// time since the last event. WORKING when a tool is pending OR the last
// event was within the debounce window (smooths intra-turn streaming).
// STALE when older than 1h. DONE otherwise.
func RecomputeStatus(s *Session, now time.Time) {
	if len(s.PendingToolUseIDs) > 0 {
		s.Status = StatusWorking
		return
	}
	age := now.Sub(s.LastEventAt)
	switch {
	case age >= staleAfter:
		s.Status = StatusStale
	case age < workingDebounce:
		s.Status = StatusWorking
	default:
		s.Status = StatusDone
	}
}
