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
