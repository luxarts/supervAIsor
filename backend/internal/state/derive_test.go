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
	// Status would be re-derived to working only if there are still pending IDs.
	if got.Status == StatusWorking {
		t.Errorf("Status should not still be working after last tool_result cleared")
	}
}
