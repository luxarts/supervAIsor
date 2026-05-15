package state

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/luxarts/supervaisor/internal/events"
)

func mustEvent(t *testing.T, ts time.Time, typ string, payload any) events.Event {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return events.Event{TS: ts, Type: typ, Payload: b}
}

func TestComputeStats_AggregatesTokensModelAndCounts(t *testing.T) {
	t0 := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	sess := &Session{
		Hostname: "mac-A", ID: "s1", Name: "n", Project: "/tmp/p",
		StartedAt: t0, LastEventAt: t0.Add(10 * time.Minute),
	}

	evs := []events.Event{
		mustEvent(t, t0, "user", map[string]any{
			"type": "user",
			"message": map[string]any{
				"role":    "user",
				"content": []any{map[string]any{"type": "text", "text": "do thing"}},
			},
		}),
		mustEvent(t, t0.Add(time.Second), "assistant", map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"role":  "assistant",
				"model": "claude-opus-4-7",
				"usage": map[string]any{
					"input_tokens":                100,
					"output_tokens":               50,
					"cache_creation_input_tokens": 10,
					"cache_read_input_tokens":     200,
				},
				"content": []any{
					map[string]any{"type": "tool_use", "id": "t1", "name": "Bash"},
					map[string]any{"type": "tool_use", "id": "t2", "name": "Read"},
				},
			},
		}),
		mustEvent(t, t0.Add(2*time.Second), "user", map[string]any{
			"type": "user",
			"message": map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "tool_result", "tool_use_id": "t1", "is_error": true},
					map[string]any{"type": "tool_result", "tool_use_id": "t2"},
				},
			},
		}),
		mustEvent(t, t0.Add(3*time.Second), "assistant", map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"role":  "assistant",
				"model": "claude-sonnet-4-6",
				"usage": map[string]any{
					"input_tokens":  10,
					"output_tokens": 20,
				},
				"content": []any{map[string]any{"type": "text", "text": "done"}},
			},
		}),
	}

	got := ComputeStats(evs, sess)

	if got.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q, want latest assistant model", got.Model)
	}
	if got.Tokens.Input != 110 || got.Tokens.Output != 70 ||
		got.Tokens.CacheCreation != 10 || got.Tokens.CacheRead != 200 {
		t.Errorf("Tokens = %+v", got.Tokens)
	}
	if got.Counts.UserPrompts != 1 {
		t.Errorf("UserPrompts = %d, want 1", got.Counts.UserPrompts)
	}
	if got.Counts.AssistantTurns != 2 {
		t.Errorf("AssistantTurns = %d, want 2", got.Counts.AssistantTurns)
	}
	if got.Counts.ToolCalls != 2 {
		t.Errorf("ToolCalls = %d, want 2", got.Counts.ToolCalls)
	}
	if got.Counts.Errors != 1 {
		t.Errorf("Errors = %d, want 1", got.Counts.Errors)
	}
	if got.ToolBreakdown["Bash"] != 1 || got.ToolBreakdown["Read"] != 1 {
		t.Errorf("ToolBreakdown = %+v", got.ToolBreakdown)
	}
	if got.WallClockSeconds != 600 {
		t.Errorf("WallClockSeconds = %d, want 600", got.WallClockSeconds)
	}
}

func TestComputeStats_EmptyEvents(t *testing.T) {
	t0 := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	sess := &Session{
		Hostname: "h", ID: "id", StartedAt: t0, LastEventAt: t0,
	}
	got := ComputeStats(nil, sess)
	if got.Counts.AssistantTurns != 0 || got.Tokens.Input != 0 {
		t.Errorf("expected zero stats, got %+v", got)
	}
	if got.ToolBreakdown == nil {
		t.Error("ToolBreakdown should be non-nil empty map")
	}
}

func TestComputeStats_TolerantToBadPayloads(t *testing.T) {
	t0 := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	sess := &Session{StartedAt: t0, LastEventAt: t0}
	evs := []events.Event{
		{TS: t0, Type: "assistant", Payload: json.RawMessage(`not json`)},
	}
	got := ComputeStats(evs, sess)
	if got.Counts.AssistantTurns != 0 {
		t.Errorf("bad payload should not be counted; got %+v", got)
	}
}

func TestComputeStats_UserPromptStringContent(t *testing.T) {
	t0 := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	sess := &Session{StartedAt: t0, LastEventAt: t0}
	evs := []events.Event{
		mustEvent(t, t0, "user", map[string]any{
			"type":    "user",
			"message": map[string]any{"role": "user", "content": "hello"},
		}),
	}
	got := ComputeStats(evs, sess)
	if got.Counts.UserPrompts != 1 {
		t.Errorf("UserPrompts = %d, want 1", got.Counts.UserPrompts)
	}
}

func TestComputeStats_MixedTextAndToolResultCountsAsUserPrompt(t *testing.T) {
	t0 := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	sess := &Session{StartedAt: t0, LastEventAt: t0}
	evs := []events.Event{
		mustEvent(t, t0, "user", map[string]any{
			"type": "user",
			"message": map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "tool_result", "tool_use_id": "x"},
					map[string]any{"type": "text", "text": "follow-up question"},
				},
			},
		}),
	}
	got := ComputeStats(evs, sess)
	if got.Counts.UserPrompts != 1 {
		t.Errorf("UserPrompts = %d, want 1 (mixed text+tool_result counts)", got.Counts.UserPrompts)
	}
}

func TestComputeStats_ToolResultsAreNotUserPrompts(t *testing.T) {
	t0 := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	sess := &Session{StartedAt: t0, LastEventAt: t0}
	evs := []events.Event{
		mustEvent(t, t0, "user", map[string]any{
			"type": "user",
			"message": map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "tool_result", "tool_use_id": "x"},
				},
			},
		}),
	}
	got := ComputeStats(evs, sess)
	if got.Counts.UserPrompts != 0 {
		t.Errorf("UserPrompts = %d, want 0 (tool_result only)", got.Counts.UserPrompts)
	}
}

func TestComputeStats_IncludesProjectDirEncoded(t *testing.T) {
	t0 := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	sess := &Session{
		Hostname: "h", ID: "abc", Name: "n", Project: "/p",
		StartedAt: t0, LastEventAt: t0,
		ProjectDirEncoded: "-Users-x-Projects-foo",
	}
	got := ComputeStats(nil, sess)
	if got.ProjectDirEncoded != "-Users-x-Projects-foo" {
		t.Errorf("ProjectDirEncoded = %q", got.ProjectDirEncoded)
	}
}
