# Status Semantics & Session Stats Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Collapse session status to three states (`WORKING` / `DONE` / `STALE`), surface aggregate stats (tokens, model, durations, tool breakdown) via a new `/stats` endpoint, and add an in-place DETAILS tab in the conversation modal.

**Architecture:** Backend status derivation (pure function `RecomputeStatus`) gains a 2-second debounce on `WORKING` and drops `waiting_input`/`idle` in favor of `done`. A new pure function `ComputeStats` aggregates over stored events on demand — no schema migration. `MessageContent.UnmarshalJSON` is extended to parse `model` and `usage`. The frontend modal grows a tab strip; the DETAILS tab renders the stats payload.

**Tech Stack:** Go 1.x (Gin, gorilla/websocket, modernc.org/sqlite), React 19 + Vite + TypeScript + Tailwind, Vitest, React Testing Library.

**Spec:** `docs/superpowers/specs/2026-05-14-status-semantics-and-stats-design.md`

---

## File Structure

**Modified:**
- `backend/internal/state/session.go` — replace `StatusWaitingInput`/`StatusIdle` with `StatusDone`
- `backend/internal/state/derive.go` — three-state `RecomputeStatus`, 2-s `WORKING` debounce, `Apply` defers status to `RecomputeStatus`
- `backend/internal/state/derive_test.go` — rename + new boundary cases
- `backend/internal/events/types.go` — add `Model`, `Usage`, `IsError`; extend `MessageContent.UnmarshalJSON`
- `backend/internal/events/types_test.go` — Usage/Model/IsError parsing
- `backend/internal/api/sessions.go` — register `GET /sessions/:hostname/:id/stats`
- `backend/internal/api/sessions_test.go` — `/stats` 200 + 404
- `frontend/src/types.ts` — `Status = "working" | "done" | "stale"`
- `frontend/src/components/StatusBadge.tsx` — three variants
- `frontend/src/components/SessionCard.tsx` — `STATUS · <age>` subtitle on the existing 1-s tick
- `frontend/src/components/SessionCard.test.tsx` — updated fixtures + age subtitle
- `frontend/src/components/ConversationModal.tsx` — tab strip (`CONVERSATION` | `DETAILS`); details fetch + render
- `frontend/src/components/ConversationModal.test.tsx` — tab switching + stats fetch behavior

**New:**
- `backend/internal/state/stats.go` — `Stats` struct + `ComputeStats(events, sess) Stats`
- `backend/internal/state/stats_test.go` — table-driven coverage
- `frontend/src/components/SessionDetails.tsx` — DETAILS tab body component
- `frontend/src/components/SessionDetails.test.tsx`
- `frontend/src/lib/relativeTime.ts` — `formatRelative(ms)` returns `"just now" | "12s" | "4m" | "2h"`
- `frontend/src/lib/relativeTime.test.ts`

---

## Task 1: Backend — Collapse Status Enum

**Files:**
- Modify: `backend/internal/state/session.go`
- Test: `backend/internal/state/derive_test.go`

- [ ] **Step 1.1: Update existing failing test expectations**

Open `backend/internal/state/derive_test.go`. Replace the two `RecomputeStatus` tests with the new three-state expectations and a new debounce case. Replace the existing `TestRecomputeStatus_IdleAfter30s` and `TestRecomputeStatus_StaleAfter1h` blocks (lines 133–157) with:

```go
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
```

Also, in `TestApply_ToolResult_ClearsPending` (line 94 area), update the trailing assertion:
```go
	if got.Status == StatusWorking {
		t.Errorf("Status should not still be working after last tool_result cleared")
	}
```
Add right after it:
```go
	if got.Status != StatusDone {
		t.Errorf("Status = %q, want done after pending cleared", got.Status)
	}
```

- [ ] **Step 1.2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/state/ -run 'TestRecomputeStatus|TestApply_ToolResult_ClearsPending' -v`
Expected: compile error — `StatusDone` undefined; `StatusIdle`/`StatusWaitingInput` still present in the test file references for `TestRecomputeStatus_IdleAfter30s` won't be removed yet if you skipped that. Make sure those two tests are deleted, not just modified.

- [ ] **Step 1.3: Update the Status enum**

Replace the `const ( … )` block in `backend/internal/state/session.go` (lines 7–12) with:

```go
const (
	StatusWorking Status = "working"
	StatusDone    Status = "done"
	StatusStale   Status = "stale"
)
```

- [ ] **Step 1.4: Run tests to confirm only RecomputeStatus is now broken**

Run: `cd backend && go test ./internal/state/ -v`
Expected: `RecomputeStatus_DoneAfter2sNoPending` and `RecomputeStatus_WorkingDebounceUnder2s` FAIL (still old logic). Other tests pass.

- [ ] **Step 1.5: Rewrite RecomputeStatus**

Open `backend/internal/state/derive.go`. Replace the `idleAfter`/`staleAfter` constants (lines 12–13) with:

```go
const (
	workingDebounce = 2 * time.Second
	staleAfter      = time.Hour
)
```

Replace `RecomputeStatus` (lines 167–184) with:

```go
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
```

- [ ] **Step 1.6: Update Apply to defer status to RecomputeStatus**

In `backend/internal/state/derive.go`, replace the trailing block of `Apply` (the comment + `if/else` setting status, lines 67–73) with a single call:

```go
	RecomputeStatus(&next, ts)
	return &next, nil
```

- [ ] **Step 1.7: Run all state tests**

Run: `cd backend && go test ./internal/state/ -v`
Expected: PASS.

- [ ] **Step 1.8: Run the whole backend suite to catch downstream type errors**

Run: `cd backend && go test ./...`
Expected: PASS. The ingest handler already calls `state.RecomputeStatus` and uses `state.Apply`; no symbol changes break it. The api test seeds `state.StatusIdle` (`sessions_test.go:27`) — change that one occurrence to `state.StatusDone`.

- [ ] **Step 1.9: Commit**

```bash
git add backend/internal/state/session.go backend/internal/state/derive.go backend/internal/state/derive_test.go backend/internal/api/sessions_test.go
git commit -m "refactor(state): collapse session status to working/done/stale with 2s debounce"
```

---

## Task 2: Backend — Extend Event Types for Stats

**Files:**
- Modify: `backend/internal/events/types.go`
- Test: `backend/internal/events/types_test.go`

- [ ] **Step 2.1: Write failing tests for new fields**

Open `backend/internal/events/types_test.go` and append:

```go
func TestMessageContent_UnmarshalJSON_ExtractsModelAndUsage(t *testing.T) {
	raw := []byte(`{
		"role": "assistant",
		"model": "claude-opus-4-7",
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"cache_creation_input_tokens": 20,
			"cache_read_input_tokens": 1000
		},
		"content": [{"type": "text", "text": "hi"}]
	}`)
	var m MessageContent
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Model != "claude-opus-4-7" {
		t.Errorf("Model = %q, want claude-opus-4-7", m.Model)
	}
	if m.Usage == nil {
		t.Fatal("Usage = nil")
	}
	if m.Usage.InputTokens != 100 || m.Usage.OutputTokens != 50 ||
		m.Usage.CacheCreationInputTokens != 20 || m.Usage.CacheReadInputTokens != 1000 {
		t.Errorf("Usage = %+v", *m.Usage)
	}
	if len(m.Content) != 1 || m.Content[0].Text != "hi" {
		t.Errorf("Content = %+v", m.Content)
	}
}

func TestContentBlock_UnmarshalJSON_ParsesIsError(t *testing.T) {
	raw := []byte(`{"type":"tool_result","tool_use_id":"x","is_error":true}`)
	var b ContentBlock
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	if !b.IsError {
		t.Error("IsError = false, want true")
	}
}
```

If the test file doesn't yet import `encoding/json`, add it.

- [ ] **Step 2.2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/events/ -run 'TestMessageContent_UnmarshalJSON_ExtractsModelAndUsage|TestContentBlock_UnmarshalJSON_ParsesIsError' -v`
Expected: compile error — `Model`, `Usage`, `IsError` undefined.

- [ ] **Step 2.3: Add the new fields and Usage type**

In `backend/internal/events/types.go`, replace the `MessageContent` struct + its `UnmarshalJSON` (lines 37–67) with:

```go
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
	if err := json.Unmarshal(aux.Content, &m.Content); err == nil {
		return nil
	}
	var s string
	if err := json.Unmarshal(aux.Content, &s); err != nil {
		return err
	}
	m.Content = []ContentBlock{{Type: "text", Text: s}}
	return nil
}
```

Then extend `ContentBlock` (lines 69–76) to add the `IsError` field:

```go
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Name      string          `json:"name,omitempty"`        // tool_use
	Input     json.RawMessage `json:"input,omitempty"`       // tool_use
	ID        string          `json:"id,omitempty"`          // tool_use id
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result
	IsError   bool            `json:"is_error,omitempty"`    // tool_result
}
```

- [ ] **Step 2.4: Run tests to verify pass**

Run: `cd backend && go test ./internal/events/ -v`
Expected: PASS, including the existing tests (additive change).

- [ ] **Step 2.5: Commit**

```bash
git add backend/internal/events/types.go backend/internal/events/types_test.go
git commit -m "feat(events): parse model, usage, and tool_result.is_error from JSONL"
```

---

## Task 3: Backend — `ComputeStats` Pure Function

**Files:**
- Create: `backend/internal/state/stats.go`
- Test: `backend/internal/state/stats_test.go`

- [ ] **Step 3.1: Write the failing test**

Create `backend/internal/state/stats_test.go`:

```go
package state

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/luxarts/supervaisor/internal/events"
	"github.com/luxarts/supervaisor/internal/store"
)

func mustEvent(t *testing.T, ts time.Time, typ string, payload any) store.Event {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return store.Event{TS: ts, Type: typ, Payload: b}
}

func TestComputeStats_AggregatesTokensModelAndCounts(t *testing.T) {
	t0 := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	sess := &Session{
		Hostname: "mac-A", ID: "s1", Name: "n", Project: "/tmp/p",
		StartedAt: t0, LastEventAt: t0.Add(10 * time.Minute),
	}

	evs := []store.Event{
		// User prompt.
		mustEvent(t, t0, "user", map[string]any{
			"type": "user",
			"message": map[string]any{
				"role":    "user",
				"content": []any{map[string]any{"type": "text", "text": "do thing"}},
			},
		}),
		// Assistant w/ tool_use + usage + model.
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
		// Tool_result with error.
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
		// Second assistant turn — different model is ignored; latest wins.
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
	evs := []store.Event{
		{TS: t0, Type: "assistant", Payload: json.RawMessage(`not json`)},
	}
	got := ComputeStats(evs, sess) // must not panic
	if got.Counts.AssistantTurns != 0 {
		t.Errorf("bad payload should not be counted; got %+v", got)
	}
}

// Used by string-content normalization path: a user prompt where content
// is a plain string (not an array) still counts as a UserPrompt.
func TestComputeStats_UserPromptStringContent(t *testing.T) {
	t0 := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	sess := &Session{StartedAt: t0, LastEventAt: t0}
	evs := []store.Event{
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

// Tool-result-only "user" events (which Claude emits to deliver tool output)
// must NOT be counted as user prompts.
func TestComputeStats_ToolResultsAreNotUserPrompts(t *testing.T) {
	t0 := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	sess := &Session{StartedAt: t0, LastEventAt: t0}
	evs := []store.Event{
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
```

- [ ] **Step 3.2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/state/ -run TestComputeStats -v`
Expected: compile error — `ComputeStats`, `Stats`, etc. undefined.

- [ ] **Step 3.3: Implement ComputeStats**

Create `backend/internal/state/stats.go`:

```go
package state

import (
	"encoding/json"
	"time"

	"github.com/luxarts/supervaisor/internal/events"
	"github.com/luxarts/supervaisor/internal/store"
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
func ComputeStats(evs []store.Event, sess *Session) Stats {
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
				out.Model = line.Message.Model // latest wins
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
```

- [ ] **Step 3.4: Run tests to verify pass**

Run: `cd backend && go test ./internal/state/ -run TestComputeStats -v`
Expected: PASS for all 5 cases.

- [ ] **Step 3.5: Run full backend suite**

Run: `cd backend && go test ./...`
Expected: PASS.

- [ ] **Step 3.6: Commit**

```bash
git add backend/internal/state/stats.go backend/internal/state/stats_test.go
git commit -m "feat(state): ComputeStats aggregates tokens, counts, and tool breakdown"
```

---

## Task 4: Backend — Wire `/stats` Endpoint

**Files:**
- Modify: `backend/internal/api/sessions.go`
- Test: `backend/internal/api/sessions_test.go`

- [ ] **Step 4.1: Write failing tests**

Append to `backend/internal/api/sessions_test.go`:

```go
func TestGetSessionStats(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	sess := &state.Session{
		ID:          "abc",
		Hostname:    "host-test",
		Name:        "n",
		Project:     "/tmp/p",
		Status:      state.StatusDone,
		StartedAt:   t0,
		LastEventAt: t0.Add(60 * time.Second),
	}
	if err := st.UpsertSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	assistantPayload := []byte(`{
		"type": "assistant",
		"message": {
			"role": "assistant",
			"model": "claude-opus-4-7",
			"usage": {"input_tokens": 5, "output_tokens": 7},
			"content": [{"type": "tool_use", "id": "x", "name": "Bash"}]
		}
	}`)
	if err := st.AppendEvent(ctx, "host-test", "abc", t0, "assistant", assistantPayload); err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	(&Handler{Store: st}).Register(r)

	req := httptest.NewRequest("GET", "/sessions/host-test/abc/stats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var out state.Stats
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Model != "claude-opus-4-7" {
		t.Errorf("Model = %q", out.Model)
	}
	if out.Tokens.Input != 5 || out.Tokens.Output != 7 {
		t.Errorf("Tokens = %+v", out.Tokens)
	}
	if out.Counts.AssistantTurns != 1 || out.Counts.ToolCalls != 1 {
		t.Errorf("Counts = %+v", out.Counts)
	}
	if out.ToolBreakdown["Bash"] != 1 {
		t.Errorf("ToolBreakdown = %+v", out.ToolBreakdown)
	}
	if out.WallClockSeconds != 60 {
		t.Errorf("WallClockSeconds = %d, want 60", out.WallClockSeconds)
	}
}

func TestGetSessionStats_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, _ := store.Open(filepath.Join(t.TempDir(), "x.db"))
	defer st.Close()
	r := gin.New()
	(&Handler{Store: st}).Register(r)

	req := httptest.NewRequest("GET", "/sessions/h/missing/stats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
```

- [ ] **Step 4.2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/api/ -run TestGetSessionStats -v`
Expected: 404 returned for both (no such route yet).

- [ ] **Step 4.3: Register the route + handler**

In `backend/internal/api/sessions.go`, inside `Register`, add the new route after the existing events route:

```go
	r.GET("/sessions/:hostname/:id/events", h.getSessionEvents)
	r.GET("/sessions/:hostname/:id/stats", h.getSessionStats)
```

Then append the handler function below `getSessionEvents`:

```go
func (h *Handler) getSessionStats(c *gin.Context) {
	hostname := c.Param("hostname")
	id := c.Param("id")
	ctx := c.Request.Context()

	sess, err := h.Store.GetSession(ctx, hostname, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	// 0 means "no limit" upstream; we want the full event log here.
	evs, err := h.Store.ListEvents(ctx, hostname, id, 100000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	stats := state.ComputeStats(evs, sess)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, stats)
}
```

- [ ] **Step 4.4: Run tests to verify pass**

Run: `cd backend && go test ./internal/api/ -v`
Expected: PASS, including the two new `TestGetSessionStats` cases.

- [ ] **Step 4.5: Run full backend suite**

Run: `cd backend && go test ./...`
Expected: PASS.

- [ ] **Step 4.6: Commit**

```bash
git add backend/internal/api/sessions.go backend/internal/api/sessions_test.go
git commit -m "feat(api): GET /sessions/:hostname/:id/stats returns aggregate stats"
```

---

## Task 5: Frontend — Status Type Update + StatusBadge

**Files:**
- Modify: `frontend/src/types.ts`
- Modify: `frontend/src/components/StatusBadge.tsx`

- [ ] **Step 5.1: Update Status union**

Replace `frontend/src/types.ts` line 1 with:

```ts
export type Status = "working" | "done" | "stale";
```

- [ ] **Step 5.2: Update StatusBadge variants**

Replace the `STYLES` map in `frontend/src/components/StatusBadge.tsx` (lines 3–8) with:

```ts
const STYLES: Record<Status, { label: string; color: string; bg: string }> = {
  working: { label: "WORKING", color: "text-cy",  bg: "border-cy/60" },
  done:    { label: "DONE",    color: "text-yl",  bg: "border-yl/60" },
  stale:   { label: "STALE",   color: "text-rd",  bg: "border-rd/60" },
};
```

- [ ] **Step 5.3: Run frontend tests to surface broken expectations**

Run: `cd frontend && npx vitest run`
Expected: any test fixture that referenced `waiting_input` or `idle` will fail to type-check via the union. The only such reference is in `SessionCard.test.tsx` indirectly via the `Session` type — verify no compilation errors first:

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS. (`SessionCard.test.tsx` uses `status: "working"` only.)

- [ ] **Step 5.4: Commit**

```bash
git add frontend/src/types.ts frontend/src/components/StatusBadge.tsx
git commit -m "refactor(ui): collapse status union to working/done/stale"
```

---

## Task 6: Frontend — `formatRelative` Helper

**Files:**
- Create: `frontend/src/lib/relativeTime.ts`
- Test: `frontend/src/lib/relativeTime.test.ts`

- [ ] **Step 6.1: Write the failing test**

Create `frontend/src/lib/relativeTime.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { formatRelative } from "./relativeTime";

describe("formatRelative", () => {
  it("returns 'just now' under 5 seconds", () => {
    expect(formatRelative(0)).toBe("just now");
    expect(formatRelative(4_999)).toBe("just now");
  });
  it("returns seconds when under a minute", () => {
    expect(formatRelative(12_000)).toBe("12s");
    expect(formatRelative(59_999)).toBe("59s");
  });
  it("returns minutes when under an hour", () => {
    expect(formatRelative(60_000)).toBe("1m");
    expect(formatRelative(4 * 60_000 + 30_000)).toBe("4m");
    expect(formatRelative(59 * 60_000)).toBe("59m");
  });
  it("returns hours when over an hour", () => {
    expect(formatRelative(60 * 60_000)).toBe("1h");
    expect(formatRelative(2 * 60 * 60_000 + 12 * 60_000)).toBe("2h");
  });
  it("clamps negative inputs to 'just now'", () => {
    expect(formatRelative(-50_000)).toBe("just now");
  });
});
```

- [ ] **Step 6.2: Run tests to verify they fail**

Run: `cd frontend && npx vitest run src/lib/relativeTime.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 6.3: Implement formatRelative**

Create `frontend/src/lib/relativeTime.ts`:

```ts
export function formatRelative(ms: number): string {
  if (ms < 5_000) return "just now";
  if (ms < 60_000) return `${Math.floor(ms / 1000)}s`;
  if (ms < 3_600_000) return `${Math.floor(ms / 60_000)}m`;
  return `${Math.floor(ms / 3_600_000)}h`;
}
```

- [ ] **Step 6.4: Run tests to verify pass**

Run: `cd frontend && npx vitest run src/lib/relativeTime.test.ts`
Expected: PASS.

- [ ] **Step 6.5: Commit**

```bash
git add frontend/src/lib/relativeTime.ts frontend/src/lib/relativeTime.test.ts
git commit -m "feat(ui): formatRelative helper for status age subtitles"
```

---

## Task 7: Frontend — Card Status Subtitle

**Files:**
- Modify: `frontend/src/components/SessionCard.tsx`
- Test: `frontend/src/components/SessionCard.test.tsx`

- [ ] **Step 7.1: Write failing test**

Append to `frontend/src/components/SessionCard.test.tsx`:

```ts
it("renders status badge with relative-age subtitle", () => {
  const sess2 = {
    ...sess,
    status: "done" as const,
    last_event_at: new Date(Date.now() - 12_000).toISOString(),
  };
  const { container } = render(<SessionCard session={sess2} onOpen={() => {}} />);
  expect(container.textContent).toContain("DONE");
  expect(container.textContent).toMatch(/·\s*1[12]s/); // tolerate small drift
});
```

- [ ] **Step 7.2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/components/SessionCard.test.tsx`
Expected: FAIL — no "·" subtitle present.

- [ ] **Step 7.3: Render the age subtitle**

In `frontend/src/components/SessionCard.tsx`, import the helper at the top:

```ts
import { formatRelative } from "../lib/relativeTime";
```

Compute the age from `last_event_at` inside the component, alongside the existing `total`/`prompt`:

```ts
const age = now - new Date(session.last_event_at).getTime();
```

Replace the `<header>` block (lines 33–38) with:

```tsx
      <header className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2">
          <StatusBadge status={session.status} />
          <span className="font-hud text-[10px] text-dim">
            · {formatRelative(age)}
          </span>
        </div>
        <div className="font-hud text-[10px] text-dim truncate max-w-[55%]">
          {shortProject(session.project)}
        </div>
      </header>
```

- [ ] **Step 7.4: Run tests to verify pass**

Run: `cd frontend && npx vitest run src/components/SessionCard.test.tsx`
Expected: PASS, including the existing "renders name@hostname" test.

- [ ] **Step 7.5: Commit**

```bash
git add frontend/src/components/SessionCard.tsx frontend/src/components/SessionCard.test.tsx
git commit -m "feat(ui): show DONE/STALE age subtitle on session card"
```

---

## Task 8: Frontend — `SessionDetails` Component

**Files:**
- Create: `frontend/src/components/SessionDetails.tsx`
- Test: `frontend/src/components/SessionDetails.test.tsx`

- [ ] **Step 8.1: Write failing test**

Create `frontend/src/components/SessionDetails.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { SessionDetails } from "./SessionDetails";

const stats = {
  hostname: "mac-A",
  id: "abc",
  name: "feature-x",
  project: "/Users/u/Projects/foo",
  model: "claude-opus-4-7",
  started_at: "2026-05-14T09:00:00Z",
  last_event_at: "2026-05-14T10:00:00Z",
  wall_clock_seconds: 3600,
  tokens: { input: 100, output: 50, cache_creation: 10, cache_read: 200 },
  counts: { user_prompts: 3, assistant_turns: 7, tool_calls: 12, errors: 1 },
  tool_breakdown: { Read: 8, Bash: 4 },
};

describe("SessionDetails", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({ ok: true, status: 200, json: async () => stats })),
    );
  });
  afterEach(() => vi.unstubAllGlobals());

  it("fetches and renders the stats payload", async () => {
    render(
      <SessionDetails
        sessionId="abc"
        hostname="mac-A"
        lastEventAt="2026-05-14T10:00:00Z"
        backendHttpBase="http://localhost:8080"
      />,
    );
    await waitFor(() => expect(screen.getByText("claude-opus-4-7")).toBeTruthy());
    expect(screen.getByText("/Users/u/Projects/foo")).toBeTruthy();
    expect(screen.getByText("mac-A")).toBeTruthy();
    expect(screen.getByText("100")).toBeTruthy(); // input tokens
    expect(screen.getByText("Read")).toBeTruthy();
    expect(screen.getByText("Bash")).toBeTruthy();

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining("/sessions/mac-A/abc/stats"),
      expect.anything(),
    );
  });

  it("refetches when lastEventAt changes", async () => {
    const { rerender } = render(
      <SessionDetails
        sessionId="abc"
        hostname="mac-A"
        lastEventAt="2026-05-14T10:00:00Z"
        backendHttpBase="http://localhost:8080"
      />,
    );
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    rerender(
      <SessionDetails
        sessionId="abc"
        hostname="mac-A"
        lastEventAt="2026-05-14T10:00:05Z"
        backendHttpBase="http://localhost:8080"
      />,
    );
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
  });
});
```

- [ ] **Step 8.2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/components/SessionDetails.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 8.3: Implement SessionDetails**

Create `frontend/src/components/SessionDetails.tsx`:

```tsx
import { useEffect, useState } from "react";

interface Tokens {
  input: number;
  output: number;
  cache_creation: number;
  cache_read: number;
}
interface Counts {
  user_prompts: number;
  assistant_turns: number;
  tool_calls: number;
  errors: number;
}
interface Stats {
  hostname: string;
  id: string;
  name: string;
  project: string;
  model?: string;
  started_at: string;
  last_event_at: string;
  wall_clock_seconds: number;
  tokens: Tokens;
  counts: Counts;
  tool_breakdown: Record<string, number>;
}

interface Props {
  sessionId: string;
  hostname: string;
  lastEventAt: string;
  backendHttpBase: string;
}

export function SessionDetails({ sessionId, hostname, lastEventAt, backendHttpBase }: Props) {
  const [stats, setStats] = useState<Stats | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetch(
      `${backendHttpBase}/sessions/${encodeURIComponent(hostname)}/${encodeURIComponent(sessionId)}/stats?_=${Date.now()}`,
      { cache: "no-store" },
    )
      .then(async (r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return (await r.json()) as Stats;
      })
      .then((d) => {
        if (!cancelled) setStats(d);
      })
      .catch((e) => {
        if (!cancelled) setError(String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [sessionId, hostname, lastEventAt, backendHttpBase]);

  if (error) {
    return <div className="font-hud text-rd p-3">// STATS LOST: {error}</div>;
  }
  if (!stats) {
    return <div className="font-hud text-dim p-3">// LOADING…</div>;
  }

  const fmtDur = (s: number) => {
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const sec = s % 60;
    if (h > 0) return `${h}h ${m}m ${sec}s`;
    if (m > 0) return `${m}m ${sec}s`;
    return `${sec}s`;
  };
  const fmtNum = (n: number) => n.toLocaleString();
  const breakdown = Object.entries(stats.tool_breakdown).sort((a, b) => b[1] - a[1]);
  const maxTool = breakdown.reduce((m, [, v]) => Math.max(m, v), 0) || 1;

  return (
    <div className="space-y-4 font-hud text-sm">
      <Section title="OVERVIEW">
        <Row k="PROJECT" v={stats.project} />
        <Row k="HOST" v={stats.hostname} />
        {stats.model && <Row k="MODEL" v={stats.model} />}
        <Row k="STARTED" v={new Date(stats.started_at).toLocaleString()} />
        <Row k="LAST EVENT" v={new Date(stats.last_event_at).toLocaleString()} />
        <Row k="DURATION" v={fmtDur(stats.wall_clock_seconds)} />
      </Section>

      <Section title="TOKENS">
        <Row k="Input" v={fmtNum(stats.tokens.input)} />
        <Row k="Output" v={fmtNum(stats.tokens.output)} />
        <Row k="Cache create" v={fmtNum(stats.tokens.cache_creation)} />
        <Row k="Cache read" v={fmtNum(stats.tokens.cache_read)} />
      </Section>

      <Section title="COUNTS">
        <Row k="User prompts" v={fmtNum(stats.counts.user_prompts)} />
        <Row k="Assistant turns" v={fmtNum(stats.counts.assistant_turns)} />
        <Row k="Tool calls" v={fmtNum(stats.counts.tool_calls)} />
        <Row k="Errors" v={fmtNum(stats.counts.errors)} />
      </Section>

      <Section title="TOOL BREAKDOWN">
        {breakdown.length === 0 && <div className="text-dim">// NONE</div>}
        {breakdown.map(([name, count]) => (
          <div key={name} className="flex items-center gap-2">
            <span className="w-24 text-cy">{name}</span>
            <div className="relative h-3 flex-1 bg-cy/10">
              <div
                className="absolute inset-y-0 left-0 bg-cy/60"
                style={{ width: `${(count / maxTool) * 100}%` }}
              />
            </div>
            <span className="w-10 text-right text-yl">{count}</span>
          </div>
        ))}
      </Section>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section>
      <div className="mb-2 border-b border-cy/20 pb-1 text-[11px] uppercase tracking-widest text-cy">
        {title}
      </div>
      <div className="space-y-1">{children}</div>
    </section>
  );
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-dim">{k}</span>
      <span className="text-txt break-all text-right">{v}</span>
    </div>
  );
}
```

- [ ] **Step 8.4: Run tests to verify pass**

Run: `cd frontend && npx vitest run src/components/SessionDetails.test.tsx`
Expected: PASS for both cases.

- [ ] **Step 8.5: Commit**

```bash
git add frontend/src/components/SessionDetails.tsx frontend/src/components/SessionDetails.test.tsx
git commit -m "feat(ui): SessionDetails fetches /stats and renders metric layout"
```

---

## Task 9: Frontend — Tabbed Conversation Modal

**Files:**
- Modify: `frontend/src/components/ConversationModal.tsx`
- Test: `frontend/src/components/ConversationModal.test.tsx`

- [ ] **Step 9.1: Write failing test**

Append to `frontend/src/components/ConversationModal.test.tsx`:

```tsx
import { fireEvent } from "@testing-library/react";

it("switches to DETAILS tab and fetches /stats", async () => {
  // The default beforeEach stubs fetch to return the conversation sample.
  // Re-stub so /stats and /events both succeed.
  const fetchMock = vi.fn(async (url: string) => {
    if (url.includes("/stats")) {
      return {
        ok: true,
        status: 200,
        json: async () => ({
          hostname: "mac-A",
          id: "abc",
          name: "my-sess",
          project: "/Users/u/Projects/foo",
          model: "claude-opus-4-7",
          started_at: "2026-05-14T11:00:00Z",
          last_event_at: "2026-05-14T12:00:02Z",
          wall_clock_seconds: 3722,
          tokens: { input: 0, output: 0, cache_creation: 0, cache_read: 0 },
          counts: { user_prompts: 0, assistant_turns: 0, tool_calls: 0, errors: 0 },
          tool_breakdown: {},
        }),
      };
    }
    return { ok: true, status: 200, json: async () => sample };
  });
  vi.stubGlobal("fetch", fetchMock);

  render(
    <ConversationModal
      sessionId="abc"
      hostname="mac-A"
      sessionName="my-sess"
      project="/Users/u/Projects/foo"
      lastEventAt="2026-05-14T12:00:02Z"
      backendHttpBase="http://localhost:8080"
      onClose={() => {}}
    />,
  );

  await waitFor(() => expect(screen.getByText("hello")).toBeTruthy());

  fireEvent.click(screen.getByRole("tab", { name: /details/i }));

  await waitFor(() =>
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining("/sessions/mac-A/abc/stats"),
      expect.anything(),
    ),
  );
  await waitFor(() => expect(screen.getByText("claude-opus-4-7")).toBeTruthy());
});
```

- [ ] **Step 9.2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/components/ConversationModal.test.tsx`
Expected: FAIL — no `tab` role found.

- [ ] **Step 9.3: Add the tab strip and details body**

In `frontend/src/components/ConversationModal.tsx`:

Add an import at the top:

```ts
import { SessionDetails } from "./SessionDetails";
```

Add a tab state next to `events`/`error` (around line 47):

```ts
  const [tab, setTab] = useState<"conversation" | "details">("conversation");
```

In the JSX, replace the `<header>` block (lines 164–182) and the body wrapper that follows by inserting a tab strip below the header and gating the existing body on `tab === "conversation"`. Concretely, after the existing `<header>` closing tag, insert:

```tsx
        <div role="tablist" className="flex border-b border-cy/20">
          <button
            type="button"
            role="tab"
            aria-selected={tab === "conversation"}
            onClick={() => setTab("conversation")}
            className={`flex-1 py-2 font-hud text-xs uppercase tracking-widest min-h-[44px] touch-manipulation
                        ${tab === "conversation" ? "text-cy border-b-2 border-cy" : "text-dim hover:text-cy"}`}
          >
            CONVERSATION
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "details"}
            onClick={() => setTab("details")}
            className={`flex-1 py-2 font-hud text-xs uppercase tracking-widest min-h-[44px] touch-manipulation
                        ${tab === "details" ? "text-cy border-b-2 border-cy" : "text-dim hover:text-cy"}`}
          >
            DETAILS
          </button>
        </div>
```

Then wrap the existing scrollable body so DETAILS shows the new component instead. Replace the existing `<div ref={bodyRef} …>{ … }</div>` block (lines 184–200) with:

```tsx
        <div
          ref={bodyRef}
          className="flex-1 overflow-y-auto overflow-x-hidden overscroll-contain p-3 space-y-3"
        >
          {tab === "conversation" ? (
            <>
              {error && (
                <div className="font-hud text-rd">// EVENT FEED LOST: {error}</div>
              )}
              {!error && events === null && (
                <div className="font-hud text-dim">// LOADING…</div>
              )}
              {!error && events && events.length === 0 && (
                <div className="font-hud text-dim">// NO MESSAGES</div>
              )}
              {!error && events && events.map((ev, i) => (
                <MessageView key={i} ev={ev} />
              ))}
            </>
          ) : (
            <SessionDetails
              sessionId={sessionId}
              hostname={hostname}
              lastEventAt={lastEventAt}
              backendHttpBase={backendHttpBase}
            />
          )}
        </div>
```

(Note the existing conversation `useEffect` that fetches `/events` keeps running regardless of tab — that is acceptable. Switching back to CONVERSATION shows already-loaded messages. The DETAILS fetch lives in `SessionDetails` and only fires while that component is mounted.)

- [ ] **Step 9.4: Run tests to verify pass**

Run: `cd frontend && npx vitest run src/components/ConversationModal.test.tsx`
Expected: PASS, including the existing "renders text messages with markdown" test.

- [ ] **Step 9.5: Run the full frontend suite**

Run: `cd frontend && npx vitest run`
Expected: PASS.

- [ ] **Step 9.6: Type check**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS.

- [ ] **Step 9.7: Commit**

```bash
git add frontend/src/components/ConversationModal.tsx frontend/src/components/ConversationModal.test.tsx
git commit -m "feat(ui): tabbed modal with CONVERSATION and DETAILS views"
```

---

## Task 10: End-to-End Smoke

**Files:**
- (None — manual verification with `make up` and the host poller.)

- [ ] **Step 10.1: Build everything**

Run: `make test`
Expected: backend, poller, and frontend tests all PASS.

- [ ] **Step 10.2: Bring up the stack**

Run: `make up`
Wait for containers to be healthy: `docker compose -f infrastructure/docker-compose.yml ps`.

- [ ] **Step 10.3: Start a host poller in another terminal**

Run: `make poller`
Expected: poller connects (`backend ws connected`-style log), starts emitting envelopes for any active Claude sessions.

- [ ] **Step 10.4: Verify in browser**

Open `http://localhost:5173`.
Manual checks:
- (a) Active session shows `WORKING`. Within 2 s of the assistant finishing a turn it flips to `DONE`.
- (b) An untouched session ages from `DONE · 12s` to `DONE · 4m` to `STALE` (>1h).
- (c) Click any card → modal opens on the CONVERSATION tab. Click `DETAILS` → numbers render. Tokens, counts, model, and tool breakdown look reasonable.
- (d) Trigger a new message in the underlying session → DETAILS numbers update on the live tick.

- [ ] **Step 10.5: Tear down**

Run: `make down`

- [ ] **Step 10.6: No commit needed.**

---

## Self-Review Notes

1. **Spec coverage:** every section maps to at least one task — status model (T1), event extensions (T2), ComputeStats (T3), `/stats` endpoint (T4), Status frontend type + badge (T5), age helper (T6), card subtitle (T7), DETAILS view (T8), tabs in modal (T9). Smoke test (T10) covers the migration concern noted in the spec's Migration & Rollout section.
2. **Pin/notify/delete reservations:** the spec reserves a `settings`/`danger` slot at the bottom of DETAILS for siblings. Task 8's component leaves the natural insertion point at the end of its render — siblings will append `<Section>` blocks without restructuring.
3. **Backwards compat:** stored sessions with `"waiting_input"` or `"idle"` strings get normalized within 5 s by the existing `runStatusTicker` in `cmd/server/main.go` (calls `RecomputeStatus` over all sessions on a tick) — no migration task needed.
4. **No placeholders.** Every code step shows the literal code; every command has an expected outcome.

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-05-14-status-semantics-and-stats.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**
