package state

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/luxarts/supervaisor/internal/events"
)

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestApply_FirstEvent_SetsStartedAt(t *testing.T) {
	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	env := events.IngestEnvelope{
		SessionID:  "abc",
		ProjectDir: "-tmp-proj",
		FileMTime:  now,
		LineIndex:  0,
		Raw: mustRaw(t, map[string]any{
			"type":        "custom-title",
			"customTitle": "my-session",
			"timestamp":   now,
		}),
	}

	got, err := Apply(nil, env)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "abc" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.Project != "/tmp/proj" {
		t.Errorf("Project = %q", got.Project)
	}
	if got.Name != "my-session" {
		t.Errorf("Name = %q", got.Name)
	}
	if !got.StartedAt.Equal(now) {
		t.Errorf("StartedAt = %v", got.StartedAt)
	}
	if !got.LastEventAt.Equal(now) {
		t.Errorf("LastEventAt = %v", got.LastEventAt)
	}
}

func TestApply_AssistantToolUse_MarksWorking(t *testing.T) {
	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	env := events.IngestEnvelope{
		SessionID: "abc", ProjectDir: "-tmp",
		FileMTime: now, LineIndex: 1,
		Raw: mustRaw(t, map[string]any{
			"type":      "assistant",
			"timestamp": now,
			"message": map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "tool_use",
						"id":   "tool_1",
						"name": "Write",
						"input": map[string]any{
							"file_path": "/tmp/x.go",
						},
					},
				},
			},
		}),
	}
	prev := &Session{ID: "abc", StartedAt: now.Add(-1 * time.Minute), LastEventAt: now.Add(-30 * time.Second)}
	got, err := Apply(prev, env)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusWorking {
		t.Errorf("Status = %q, want working", got.Status)
	}
	if got.CurrentAction != "Write: /tmp/x.go" {
		t.Errorf("CurrentAction = %q", got.CurrentAction)
	}
	if _, ok := got.PendingToolUseIDs["tool_1"]; !ok {
		t.Errorf("tool_1 not pending")
	}
}

func TestApply_ToolResult_ClearsPending(t *testing.T) {
	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	prev := &Session{
		ID:                "abc",
		StartedAt:         now.Add(-1 * time.Minute),
		LastEventAt:       now.Add(-10 * time.Second),
		Status:            StatusWorking,
		PendingToolUseIDs: map[string]struct{}{"tool_1": {}},
	}
	eventTime := now.Add(-3 * time.Second)
	env := events.IngestEnvelope{
		SessionID: "abc", ProjectDir: "-tmp",
		FileMTime: eventTime, LineIndex: 2,
		Raw: mustRaw(t, map[string]any{
			"type":      "user",
			"timestamp": eventTime,
			"message": map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "tool_1",
					},
				},
			},
		}),
	}
	got, err := Apply(prev, env)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.PendingToolUseIDs["tool_1"]; ok {
		t.Errorf("tool_1 should have been cleared")
	}
	// Apply calls RecomputeStatus(&next, ts) where ts is the event timestamp,
	// and LastEventAt was just set to that same ts — so age=0 falls inside the
	// 2-s WORKING debounce. The runStatusTicker (wall-clock-driven) is what
	// later flips the session to DONE once 2s have elapsed.
	if got.Status != StatusWorking {
		t.Errorf("Status = %q, want working (debounce window)", got.Status)
	}
}

func TestRecomputeStatus_DoneAfter2sNoPending(t *testing.T) {
	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	s := &Session{
		LastEventAt:       now.Add(-3 * time.Second),
		PendingToolUseIDs: map[string]struct{}{},
	}
	RecomputeStatus(s, now)
	if s.Status != StatusDone {
		t.Errorf("Status = %q, want done", s.Status)
	}
}

func TestRecomputeStatus_WorkingDebounceUnder2s(t *testing.T) {
	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	s := &Session{
		LastEventAt:       now.Add(-1 * time.Second),
		PendingToolUseIDs: map[string]struct{}{},
	}
	RecomputeStatus(s, now)
	if s.Status != StatusWorking {
		t.Errorf("Status = %q, want working (within 2-s debounce)", s.Status)
	}
}

func TestRecomputeStatus_StaleAfter1h(t *testing.T) {
	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	s := &Session{
		LastEventAt:       now.Add(-2 * time.Hour),
		PendingToolUseIDs: map[string]struct{}{},
	}
	RecomputeStatus(s, now)
	if s.Status != StatusStale {
		t.Errorf("Status = %q, want stale", s.Status)
	}
}

func TestRecomputeStatus_WorkingNotDowngraded(t *testing.T) {
	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	s := &Session{
		LastEventAt:       now.Add(-2 * time.Hour),
		PendingToolUseIDs: map[string]struct{}{"x": {}},
	}
	RecomputeStatus(s, now)
	if s.Status != StatusWorking {
		t.Errorf("Status = %q, want working (pending tool_use overrides time)", s.Status)
	}
}

func TestApply_ToolResultError_SetsLastErrorAt(t *testing.T) {
	now := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	prev := &Session{
		ID:                "abc",
		StartedAt:         now.Add(-1 * time.Minute),
		LastEventAt:       now.Add(-10 * time.Second),
		Status:            StatusWorking,
		PendingToolUseIDs: map[string]struct{}{"tool_1": {}},
	}
	env := events.IngestEnvelope{
		SessionID: "abc", ProjectDir: "-tmp",
		FileMTime: now, LineIndex: 2,
		Raw: mustRaw(t, map[string]any{
			"type":      "user",
			"timestamp": now,
			"message": map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "tool_1",
						"is_error":    true,
					},
				},
			},
		}),
	}
	got, err := Apply(prev, env)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastErrorAt.Equal(now) {
		t.Errorf("LastErrorAt = %v, want %v", got.LastErrorAt, now)
	}
}

func TestApply_ToolResultSuccess_PreservesLastErrorAt(t *testing.T) {
	earlier := time.Date(2026, 5, 15, 9, 59, 0, 0, time.UTC)
	now := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	prev := &Session{
		ID:                "abc",
		StartedAt:         earlier.Add(-1 * time.Minute),
		LastEventAt:       earlier,
		LastErrorAt:       earlier,
		Status:            StatusWorking,
		PendingToolUseIDs: map[string]struct{}{"tool_2": {}},
	}
	env := events.IngestEnvelope{
		SessionID: "abc", ProjectDir: "-tmp",
		FileMTime: now, LineIndex: 3,
		Raw: mustRaw(t, map[string]any{
			"type":      "user",
			"timestamp": now,
			"message": map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "tool_2",
					},
				},
			},
		}),
	}
	got, err := Apply(prev, env)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastErrorAt.Equal(earlier) {
		t.Errorf("LastErrorAt = %v, want %v (success result must not clear)", got.LastErrorAt, earlier)
	}
}

func TestApply_SetsHostnameOnFirstEvent(t *testing.T) {
	now := time.Now().UTC()
	env := events.IngestEnvelope{
		Hostname:  "mac-A",
		SessionID: "s1",
		FileMTime: now,
		Raw:       json.RawMessage(`{"type":"user","timestamp":"` + now.Format(time.RFC3339) + `"}`),
	}
	got, err := Apply(nil, env)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hostname != "mac-A" {
		t.Errorf("Hostname = %q, want mac-A", got.Hostname)
	}
}

func TestApply_PropagatesProjectDirEncoded(t *testing.T) {
	now := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	env := events.IngestEnvelope{
		Hostname: "h", SessionID: "abc",
		ProjectDir: "-Users-x-Projects-foo",
		FileMTime:  now,
		Raw: mustRaw(t, map[string]any{
			"type":      "user",
			"timestamp": now,
		}),
	}
	got, err := Apply(nil, env)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectDirEncoded != "-Users-x-Projects-foo" {
		t.Errorf("ProjectDirEncoded = %q, want -Users-x-Projects-foo", got.ProjectDirEncoded)
	}
}
