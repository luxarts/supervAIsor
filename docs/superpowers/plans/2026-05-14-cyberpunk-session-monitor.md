# Cyberpunk Session Monitor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mobile-first Cyberpunk 2077-themed dashboard that monitors all local Claude Code sessions live via a three-process system (host poller → containerized Go backend → containerized React frontend).

**Architecture:** A host-native Go poller tails `~/.claude/projects/**/*.jsonl`, ships raw events over WebSocket to a Dockerized Go backend that derives session state, persists to SQLite, and broadcasts updates to a Dockerized React frontend.

**Tech Stack:** Go 1.26 (Gin, gorilla/websocket, fsnotify, modernc.org/sqlite), React 19 + Vite + TypeScript + Tailwind CSS, Docker Compose.

**Spec:** `docs/superpowers/specs/2026-05-14-cyberpunk-session-monitor-design.md`

---

## File Structure

### Backend (`backend/`) — refactor; old code is deleted

**Deleted:**
- `internal/agent/` (whole package)
- `internal/hooks/` (whole package)
- `internal/state/store.go` (old shape, replaced)

**Created / replaced:**
- `cmd/server/main.go` — wires new modules (replaced)
- `internal/events/types.go` — wire types for ingest WS + raw JSONL line model
- `internal/state/session.go` — `Session` struct + status / action constants
- `internal/state/derive.go` — pure state derivation from a raw JSONL line
- `internal/state/derive_test.go` — table-driven derivation tests
- `internal/state/projectdir.go` — decode Claude project dir name → real path
- `internal/store/sqlite.go` — SQLite repository (sessions + events)
- `internal/store/sqlite_test.go`
- `internal/ingest/handler.go` — `/ws/ingest` WS handler
- `internal/ingest/handler_test.go`
- `internal/broadcast/hub.go` — frontend WS hub (refactored from `internal/ws/hub.go`)
- `internal/broadcast/client.go`
- `internal/broadcast/handler.go` — `/ws/clients` WS handler
- `internal/api/sessions.go` — `GET /sessions`, `GET /healthz`
- `internal/api/sessions_test.go`

### Poller (`poller/`) — new top-level

- `cmd/poller/main.go` — entry point, config, signal handling
- `internal/scanner/scanner.go` — finds session files; reconciles state on startup
- `internal/tailer/tailer.go` — tails a single file, emits new lines on a channel
- `internal/tailer/tailer_test.go`
- `internal/wsclient/client.go` — WS connection to backend with reconnect
- `internal/wsclient/client_test.go`
- `internal/offsets/offsets.go` — persists per-file read offsets
- `internal/offsets/offsets_test.go`
- `go.mod`, `go.sum`

### Frontend (`frontend/`) — new top-level

- `package.json`, `vite.config.ts`, `tsconfig.json`, `index.html`
- `tailwind.config.js`, `postcss.config.js`, `src/index.css`
- `src/main.tsx`
- `src/App.tsx`
- `src/types.ts` — `Session` type matching backend JSON
- `src/api.ts` — REST helpers
- `src/useSessionsSocket.ts` — WS hook
- `src/components/SessionCard.tsx`
- `src/components/StatusBadge.tsx`
- `src/components/ScanlineOverlay.tsx`
- `src/components/SessionDetail.tsx`
- `src/lib/time.ts` — duration formatters
- `src/lib/time.test.ts`
- `Dockerfile`
- `.dockerignore`

### Infrastructure (`infrastructure/`) — new top-level

- `docker-compose.yml`
- `backend.Dockerfile`
- `Makefile` (root-level convenience targets, optional)

---

## Phase 1 — Backend cleanup and skeleton

### Task 1.1: Delete legacy backend code

**Files:**
- Delete: `backend/internal/agent/`
- Delete: `backend/internal/hooks/`
- Delete: `backend/internal/state/store.go`
- Delete: `backend/internal/api/handler.go`
- Delete: `backend/internal/ws/hub.go`
- Delete: `backend/internal/ws/client.go`
- Modify: `backend/cmd/server/main.go` — strip references

- [ ] **Step 1: Delete the obsolete packages and files**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor/backend
rm -rf internal/agent internal/hooks internal/ws
rm -f internal/state/store.go internal/api/handler.go
```

- [ ] **Step 2: Reduce `cmd/server/main.go` to a build-passing stub**

Replace `backend/cmd/server/main.go` with:

```go
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	r := gin.Default()
	r.Use(corsMiddleware())
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	log.Printf("supervAIsor backend listening on :%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
```

- [ ] **Step 3: Verify it still builds**

Run: `cd backend && go mod tidy && go build ./...`
Expected: build succeeds with no errors. `go.sum` may shrink.

- [ ] **Step 4: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add -A backend/
git commit -m "refactor(backend): remove legacy agent/hooks/ws packages"
```

---

### Task 1.2: Add dependencies for new backend

**Files:**
- Modify: `backend/go.mod` (via `go get`)

- [ ] **Step 1: Add SQLite (pure Go) + fsnotify-free deps**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor/backend
go get modernc.org/sqlite@latest
go get github.com/stretchr/testify@latest
go mod tidy
```

Expected: `go.mod` lists `modernc.org/sqlite` and `github.com/stretchr/testify`.

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add backend/go.mod backend/go.sum
git commit -m "chore(backend): add sqlite (modernc) and testify deps"
```

---

## Phase 2 — Session state derivation (pure, TDD)

### Task 2.1: Define `Session`, status enum, and event wire types

**Files:**
- Create: `backend/internal/state/session.go`
- Create: `backend/internal/events/types.go`

- [ ] **Step 1: Write `backend/internal/state/session.go`**

```go
package state

import "time"

type Status string

const (
	StatusWorking      Status = "working"
	StatusWaitingInput Status = "waiting_input"
	StatusIdle         Status = "idle"
	StatusStale        Status = "stale"
)

// Session is the derived view of a Claude Code session.
type Session struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Project       string     `json:"project"`
	Status        Status     `json:"status"`
	StartedAt     time.Time  `json:"started_at"`
	LastPromptAt  *time.Time `json:"last_prompt_at,omitempty"`
	CurrentAction string     `json:"current_action"`
	LastEventAt   time.Time  `json:"last_event_at"`

	// Internal book-keeping not exposed in JSON.
	PendingToolUseIDs map[string]struct{} `json:"-"`
}
```

- [ ] **Step 2: Write `backend/internal/events/types.go`**

```go
package events

import (
	"encoding/json"
	"time"
)

// IngestEnvelope is one message the poller sends over /ws/ingest.
type IngestEnvelope struct {
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
type MessageContent struct {
	Role    string         `json:"role,omitempty"`
	Content []ContentBlock `json:"content,omitempty"`
}

type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Name      string          `json:"name,omitempty"`       // tool_use
	Input     json.RawMessage `json:"input,omitempty"`      // tool_use
	ID        string          `json:"id,omitempty"`         // tool_use id
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result
}
```

- [ ] **Step 3: Verify build**

Run: `cd backend && go build ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/state/session.go backend/internal/events/types.go
git commit -m "feat(backend): add Session model and ingest event types"
```

---

### Task 2.2: Project directory decoder

**Files:**
- Create: `backend/internal/state/projectdir.go`
- Create: `backend/internal/state/projectdir_test.go`

- [ ] **Step 1: Write the failing test `backend/internal/state/projectdir_test.go`**

```go
package state

import "testing"

func TestDecodeProjectDir(t *testing.T) {
	cases := map[string]string{
		"-Users-lucasbacelo-Projects-supervAIsor": "/Users/lucasbacelo/Projects/supervAIsor",
		"-tmp-foo":                                "/tmp/foo",
		"":                                        "",
	}
	for in, want := range cases {
		got := DecodeProjectDir(in)
		if got != want {
			t.Fatalf("DecodeProjectDir(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the test — it should fail to compile**

Run: `cd backend && go test ./internal/state/...`
Expected: failure (`undefined: DecodeProjectDir`).

- [ ] **Step 3: Implement `backend/internal/state/projectdir.go`**

```go
package state

import "strings"

// DecodeProjectDir converts Claude's encoded project dir name back into
// a real filesystem path. Claude replaces every "/" in the absolute path
// with "-". e.g. "-Users-lucasbacelo-foo" -> "/Users/lucasbacelo/foo".
func DecodeProjectDir(s string) string {
	if s == "" {
		return ""
	}
	return strings.ReplaceAll(s, "-", "/")
}
```

- [ ] **Step 4: Run the test — it should pass**

Run: `go test ./internal/state/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/state/projectdir.go backend/internal/state/projectdir_test.go
git commit -m "feat(backend): decode Claude project dir names"
```

---

### Task 2.3: Pure derivation — basic apply + name resolution

**Files:**
- Create: `backend/internal/state/derive.go`
- Create: `backend/internal/state/derive_test.go`

- [ ] **Step 1: Write failing test for the simplest case**

`backend/internal/state/derive_test.go`:

```go
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
			"type":         "custom-title",
			"customTitle":  "my-session",
			"timestamp":    now,
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
```

- [ ] **Step 2: Run the test — should fail to compile**

Run: `go test ./internal/state/...`
Expected: failure.

- [ ] **Step 3: Implement minimal `backend/internal/state/derive.go`**

```go
package state

import (
	"encoding/json"
	"fmt"

	"github.com/luxarts/supervaisor/internal/events"
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
	if next.Project == "" {
		next.Project = DecodeProjectDir(env.ProjectDir)
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
	}

	return &next, nil
}

func shortID(id string) string {
	if len(id) < 8 {
		return id
	}
	return id[:8]
}
```

- [ ] **Step 4: Run the test — should pass**

Run: `go test ./internal/state/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/state/derive.go backend/internal/state/derive_test.go
git commit -m "feat(backend): pure Apply() for session derivation — name + timestamps"
```

---

### Task 2.4: Derivation — `working` status from tool_use without matching tool_result

**Files:**
- Modify: `backend/internal/state/derive.go`
- Modify: `backend/internal/state/derive_test.go`

- [ ] **Step 1: Add failing test**

Append to `derive_test.go`:

```go
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
						"type":         "tool_result",
						"tool_use_id":  "tool_1",
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
```

- [ ] **Step 2: Run tests — should fail**

Run: `go test ./internal/state/...`
Expected: FAIL (status not set, action empty).

- [ ] **Step 3: Update `derive.go`**

Replace the function body of `Apply` after the `case` block with the following extended logic. Final file:

```go
package state

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/luxarts/supervaisor/internal/events"
)

const idleAfter = 30 // seconds
const staleAfter = 3600

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
	if next.Project == "" {
		next.Project = DecodeProjectDir(env.ProjectDir)
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
		applyAssistant(&next, line, ts)
	case "user":
		applyUser(&next, line, ts)
	}

	// Status derivation: working trumps all if any pending tool_use.
	if len(next.PendingToolUseIDs) > 0 {
		next.Status = StatusWorking
	} else {
		next.Status = StatusWaitingInput
	}
	return &next, nil
}

func applyAssistant(s *Session, line events.RawLine, ts interface{}) {
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

func applyUser(s *Session, line events.RawLine, ts interface{}) {
	if line.Message == nil {
		// Some user entries are flat text — handled later if needed.
		return
	}
	sawToolResult := false
	for _, b := range line.Message.Content {
		if b.Type == "tool_result" {
			sawToolResult = true
			delete(s.PendingToolUseIDs, b.ToolUseID)
		}
	}
	if !sawToolResult {
		// Real user prompt.
		if t, ok := ts.(interface{}); ok {
			_ = t
		}
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
```

Note: We use `ts interface{}` in the helpers as a placeholder; actually we don't need it inside `applyAssistant`/`applyUser`. Remove the parameter — replace each helper signature with `(s *Session, line events.RawLine)` and remove the unused-arg dance. Cleaner version:

```go
func applyAssistant(s *Session, line events.RawLine) { /* ... unchanged body, drop ts ... */ }
func applyUser(s *Session, line events.RawLine)      { /* ... unchanged body, drop ts ... */ }
```

And update call sites in `Apply` to match.

- [ ] **Step 4: Run tests — should pass**

Run: `go test ./internal/state/...`
Expected: PASS for the two new tests; the first test (`TestApply_FirstEvent_SetsStartedAt`) still passes — the assertion didn't check Status. Note Status will now be `waiting_input` after the first event since no pending tool_use; the original test doesn't assert Status so it passes.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/state/derive.go backend/internal/state/derive_test.go
git commit -m "feat(backend): derive working status + current_action from tool_use"
```

---

### Task 2.5: Derivation — idle / stale time-based recompute

**Files:**
- Modify: `backend/internal/state/derive.go`
- Modify: `backend/internal/state/derive_test.go`

- [ ] **Step 1: Add failing test**

Append:

```go
func TestRecomputeStatus_IdleAfter30s(t *testing.T) {
	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	s := &Session{
		Status:            StatusWaitingInput,
		LastEventAt:       now.Add(-31 * time.Second),
		PendingToolUseIDs: map[string]struct{}{},
	}
	RecomputeStatus(s, now)
	if s.Status != StatusIdle {
		t.Errorf("Status = %q, want idle", s.Status)
	}
}

func TestRecomputeStatus_StaleAfter1h(t *testing.T) {
	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	s := &Session{
		Status:            StatusIdle,
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

- [ ] **Step 2: Run — fail to compile**

Run: `go test ./internal/state/...`
Expected: FAIL.

- [ ] **Step 3: Add `RecomputeStatus` to `derive.go`**

Append:

```go
import "time"

// RecomputeStatus mutates s.Status based on elapsed time since last event.
// Pending tool_use always wins. Otherwise transitions: waiting_input -> idle
// after 30s, idle -> stale after 1h.
func RecomputeStatus(s *Session, now time.Time) {
	if len(s.PendingToolUseIDs) > 0 {
		s.Status = StatusWorking
		return
	}
	age := now.Sub(s.LastEventAt)
	switch {
	case age >= time.Hour:
		s.Status = StatusStale
	case age >= 30*time.Second:
		s.Status = StatusIdle
	default:
		s.Status = StatusWaitingInput
	}
}
```

(Combine the `time` import with the existing import block; do not add a duplicate.)

- [ ] **Step 4: Run tests — pass**

Run: `go test ./internal/state/...`
Expected: PASS all.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/state/derive.go backend/internal/state/derive_test.go
git commit -m "feat(backend): time-based idle/stale status recompute"
```

---

## Phase 3 — SQLite persistence

### Task 3.1: SQLite store — upsert and list

**Files:**
- Create: `backend/internal/store/sqlite.go`
- Create: `backend/internal/store/sqlite_test.go`

- [ ] **Step 1: Write failing test `backend/internal/store/sqlite_test.go`**

```go
package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/luxarts/supervaisor/internal/state"
)

func newTestStore(t *testing.T) *SQLite {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpsertAndList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	sess := &state.Session{
		ID:            "s1",
		Name:          "n1",
		Project:       "/tmp",
		Status:        state.StatusWorking,
		StartedAt:     now,
		LastEventAt:   now,
		CurrentAction: "Write: x.go",
	}
	if err := s.UpsertSession(ctx, sess); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "s1" || got[0].Name != "n1" {
		t.Fatalf("unexpected list: %#v", got)
	}

	// Update.
	sess.Name = "renamed"
	if err := s.UpsertSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ListSessions(ctx)
	if got[0].Name != "renamed" {
		t.Errorf("Name = %q", got[0].Name)
	}
}
```

- [ ] **Step 2: Run — fail to compile**

Run: `cd backend && go test ./internal/store/...`
Expected: FAIL.

- [ ] **Step 3: Implement `backend/internal/store/sqlite.go`**

```go
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/luxarts/supervaisor/internal/state"
)

type SQLite struct {
	db *sql.DB
}

func Open(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &SQLite{db: db}, nil
}

func (s *SQLite) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
  id              TEXT PRIMARY KEY,
  name            TEXT NOT NULL,
  project         TEXT NOT NULL,
  status          TEXT NOT NULL,
  started_at      TIMESTAMP NOT NULL,
  last_prompt_at  TIMESTAMP,
  current_action  TEXT,
  last_event_at   TIMESTAMP NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id  TEXT NOT NULL,
  ts          TIMESTAMP NOT NULL,
  type        TEXT NOT NULL,
  payload     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_session_ts ON events(session_id, ts);
`

func (s *SQLite) UpsertSession(ctx context.Context, sess *state.Session) error {
	var lastPrompt any
	if sess.LastPromptAt != nil {
		lastPrompt = *sess.LastPromptAt
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sessions (id, name, project, status, started_at, last_prompt_at, current_action, last_event_at)
VALUES (?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  name=excluded.name,
  project=excluded.project,
  status=excluded.status,
  started_at=excluded.started_at,
  last_prompt_at=excluded.last_prompt_at,
  current_action=excluded.current_action,
  last_event_at=excluded.last_event_at
`,
		sess.ID, sess.Name, sess.Project, string(sess.Status),
		sess.StartedAt, lastPrompt, sess.CurrentAction, sess.LastEventAt,
	)
	return err
}

func (s *SQLite) ListSessions(ctx context.Context) ([]*state.Session, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, project, status, started_at, last_prompt_at, current_action, last_event_at
FROM sessions ORDER BY last_event_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*state.Session
	for rows.Next() {
		var (
			sess   state.Session
			status string
			lp     sql.NullTime
		)
		if err := rows.Scan(
			&sess.ID, &sess.Name, &sess.Project, &status,
			&sess.StartedAt, &lp, &sess.CurrentAction, &sess.LastEventAt,
		); err != nil {
			return nil, err
		}
		sess.Status = state.Status(status)
		if lp.Valid {
			t := lp.Time
			sess.LastPromptAt = &t
		}
		out = append(out, &sess)
	}
	return out, rows.Err()
}

// AppendEvent stores the raw JSONL line for history.
func (s *SQLite) AppendEvent(ctx context.Context, sessionID string, ts time.Time, eventType string, payload []byte) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (session_id, ts, type, payload) VALUES (?,?,?,?)`,
		sessionID, ts, eventType, string(payload),
	)
	return err
}

// GetSession returns one session by id, or nil if not found.
func (s *SQLite) GetSession(ctx context.Context, id string) (*state.Session, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, project, status, started_at, last_prompt_at, current_action, last_event_at
FROM sessions WHERE id = ?`, id)
	var (
		sess   state.Session
		status string
		lp     sql.NullTime
	)
	err := row.Scan(&sess.ID, &sess.Name, &sess.Project, &status,
		&sess.StartedAt, &lp, &sess.CurrentAction, &sess.LastEventAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sess.Status = state.Status(status)
	if lp.Valid {
		t := lp.Time
		sess.LastPromptAt = &t
	}
	return &sess, nil
}
```

- [ ] **Step 4: Run tests — pass**

Run: `go test ./internal/store/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/store/
git commit -m "feat(backend): sqlite store with upsert/list/append-event"
```

---

## Phase 4 — Backend WS hubs and HTTP wiring

### Task 4.1: Frontend broadcast hub

**Files:**
- Create: `backend/internal/broadcast/hub.go`
- Create: `backend/internal/broadcast/client.go`
- Create: `backend/internal/broadcast/hub_test.go`

- [ ] **Step 1: Write failing test `backend/internal/broadcast/hub_test.go`**

```go
package broadcast

import (
	"testing"
	"time"
)

func TestHub_BroadcastReachesSubscribers(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	c1 := h.Subscribe()
	c2 := h.Subscribe()

	h.Broadcast([]byte(`{"ok":1}`))

	for _, c := range []chan []byte{c1, c2} {
		select {
		case msg := <-c:
			if string(msg) != `{"ok":1}` {
				t.Errorf("got %s", msg)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for broadcast")
		}
	}
}
```

- [ ] **Step 2: Run — fail**

Run: `go test ./internal/broadcast/...`
Expected: FAIL (package not found).

- [ ] **Step 3: Implement `backend/internal/broadcast/hub.go`**

```go
package broadcast

import "sync"

type Hub struct {
	mu      sync.Mutex
	subs    map[chan []byte]struct{}
	in      chan []byte
	done    chan struct{}
	stopped bool
}

func NewHub() *Hub {
	return &Hub{
		subs: map[chan []byte]struct{}{},
		in:   make(chan []byte, 64),
		done: make(chan struct{}),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case <-h.done:
			return
		case msg := <-h.in:
			h.mu.Lock()
			for c := range h.subs {
				select {
				case c <- msg:
				default: // drop if subscriber is slow
				}
			}
			h.mu.Unlock()
		}
	}
}

func (h *Hub) Stop() {
	h.mu.Lock()
	if !h.stopped {
		h.stopped = true
		close(h.done)
	}
	h.mu.Unlock()
}

func (h *Hub) Subscribe() chan []byte {
	c := make(chan []byte, 32)
	h.mu.Lock()
	h.subs[c] = struct{}{}
	h.mu.Unlock()
	return c
}

func (h *Hub) Unsubscribe(c chan []byte) {
	h.mu.Lock()
	delete(h.subs, c)
	h.mu.Unlock()
	close(c)
}

func (h *Hub) Broadcast(msg []byte) {
	select {
	case h.in <- msg:
	default:
	}
}
```

- [ ] **Step 4: Run tests — pass**

Run: `go test ./internal/broadcast/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/broadcast/
git commit -m "feat(backend): broadcast hub for frontend fan-out"
```

---

### Task 4.2: WS client handler for `/ws/clients`

**Files:**
- Create: `backend/internal/broadcast/handler.go`

- [ ] **Step 1: Implement `backend/internal/broadcast/handler.go`**

```go
package broadcast

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/luxarts/supervaisor/internal/state"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// SnapshotProvider supplies the current session list for hydration.
type SnapshotProvider interface {
	Snapshot() []*state.Session
}

type Handler struct {
	Hub      *Hub
	Snapshot SnapshotProvider
}

type frame struct {
	Kind      string         `json:"kind"`
	Sessions  []*state.Session `json:"sessions,omitempty"`
	Session   *state.Session `json:"session,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
}

func (h *Handler) Serve(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("clients upgrade: %v", err)
		return
	}
	defer conn.Close()

	// Send initial snapshot.
	snap := frame{Kind: "snapshot", Sessions: h.Snapshot.Snapshot()}
	if b, err := json.Marshal(snap); err == nil {
		_ = conn.WriteMessage(websocket.TextMessage, b)
	}

	sub := h.Hub.Subscribe()
	defer h.Hub.Unsubscribe(sub)

	// Reader goroutine drops incoming messages (clients are read-only) and
	// surfaces close events.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := conn.NextReader(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-closed:
			return
		case msg, ok := <-sub:
			if !ok {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}
}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/broadcast/handler.go
git commit -m "feat(backend): /ws/clients handler with snapshot + live updates"
```

---

### Task 4.3: Ingest WS handler

**Files:**
- Create: `backend/internal/ingest/handler.go`
- Create: `backend/internal/ingest/handler_test.go`

- [ ] **Step 1: Write failing test `backend/internal/ingest/handler_test.go`**

```go
package ingest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/luxarts/supervaisor/internal/broadcast"
	"github.com/luxarts/supervaisor/internal/events"
	"github.com/luxarts/supervaisor/internal/store"
)

func wsURL(s string) string { return "ws" + strings.TrimPrefix(s, "http") }

func TestIngest_PersistsAndBroadcasts(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hub := broadcast.NewHub()
	go hub.Run()
	defer hub.Stop()
	sub := hub.Subscribe()

	h := &Handler{Store: db, Hub: hub}

	srv := httptest.NewServer(http.HandlerFunc(h.Serve))
	defer srv.Close()

	c, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	now := time.Now().UTC().Truncate(time.Second)
	env := events.IngestEnvelope{
		SessionID: "s1", ProjectDir: "-tmp", FileMTime: now, LineIndex: 0,
		Raw: json.RawMessage(`{"type":"custom-title","customTitle":"hi","timestamp":"` + now.Format(time.RFC3339) + `"}`),
	}
	if err := c.WriteJSON(env); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-sub:
		if !strings.Contains(string(msg), `"kind":"update"`) {
			t.Errorf("expected update frame, got: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no broadcast")
	}

	got, _ := db.ListSessions(context.Background())
	if len(got) != 1 || got[0].Name != "hi" {
		t.Errorf("db: %#v", got)
	}
}
```

- [ ] **Step 2: Run — fail to compile**

Run: `go test ./internal/ingest/...`
Expected: FAIL.

- [ ] **Step 3: Implement `backend/internal/ingest/handler.go`**

```go
package ingest

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync/atomic"

	"github.com/gorilla/websocket"

	"github.com/luxarts/supervaisor/internal/broadcast"
	"github.com/luxarts/supervaisor/internal/events"
	"github.com/luxarts/supervaisor/internal/state"
	"github.com/luxarts/supervaisor/internal/store"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Handler struct {
	Store *store.SQLite
	Hub   *broadcast.Hub

	connected atomic.Bool
}

type updateFrame struct {
	Kind    string         `json:"kind"`
	Session *state.Session `json:"session"`
}

func (h *Handler) Serve(w http.ResponseWriter, r *http.Request) {
	// Single-writer guard.
	if !h.connected.CompareAndSwap(false, true) {
		http.Error(w, "ingest already connected", http.StatusConflict)
		return
	}
	defer h.connected.Store(false)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ingest upgrade: %v", err)
		return
	}
	defer conn.Close()

	ctx := r.Context()
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var env events.IngestEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			log.Printf("ingest: malformed envelope: %v", err)
			continue
		}

		prev, _ := h.Store.GetSession(ctx, env.SessionID)
		next, err := state.Apply(prev, env)
		if err != nil {
			log.Printf("ingest: apply: %v", err)
			continue
		}
		if err := h.Store.UpsertSession(ctx, next); err != nil {
			log.Printf("ingest: upsert: %v", err)
			continue
		}
		_ = h.Store.AppendEvent(ctx, env.SessionID, next.LastEventAt, peekType(env.Raw), env.Raw)

		frame := updateFrame{Kind: "update", Session: next}
		if b, err := json.Marshal(frame); err == nil {
			h.Hub.Broadcast(b)
		}
		_ = ctx // ctx referenced
	}
}

func peekType(raw json.RawMessage) string {
	var head struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &head)
	return head.Type
}
```

- [ ] **Step 4: Run tests — pass**

Run: `go test ./internal/ingest/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/ingest/
git commit -m "feat(backend): /ws/ingest handler — apply, persist, broadcast"
```

---

### Task 4.4: REST `GET /sessions` + `GET /healthz`

**Files:**
- Create: `backend/internal/api/sessions.go`
- Create: `backend/internal/api/sessions_test.go`

- [ ] **Step 1: Write failing test `backend/internal/api/sessions_test.go`**

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/luxarts/supervaisor/internal/state"
	"github.com/luxarts/supervaisor/internal/store"
)

func TestGetSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _ := store.Open(filepath.Join(t.TempDir(), "a.db"))
	defer db.Close()

	now := time.Now().UTC().Truncate(time.Second)
	_ = db.UpsertSession(context.Background(), &state.Session{
		ID: "s1", Name: "n", Project: "/tmp",
		Status: state.StatusIdle, StartedAt: now, LastEventAt: now,
	})

	h := &Handler{Store: db}
	r := gin.New()
	h.Register(r)

	req := httptest.NewRequest("GET", "/sessions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	var out []state.Session
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "s1" {
		t.Errorf("got %#v", out)
	}
}
```

- [ ] **Step 2: Run — fail to compile**

Run: `go test ./internal/api/...`
Expected: FAIL.

- [ ] **Step 3: Implement `backend/internal/api/sessions.go`**

```go
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luxarts/supervaisor/internal/store"
)

type Handler struct {
	Store *store.SQLite
}

func (h *Handler) Register(r *gin.Engine) {
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.GET("/sessions", h.listSessions)
}

func (h *Handler) listSessions(c *gin.Context) {
	sessions, err := h.Store.ListSessions(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if sessions == nil {
		sessions = []*state.Session{}
	}
	c.JSON(http.StatusOK, sessions)
}
```

Note: that final `state.Session{}` reference requires `import "github.com/luxarts/supervaisor/internal/state"`. Use:

```go
import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luxarts/supervaisor/internal/state"
	"github.com/luxarts/supervaisor/internal/store"
)
```

And declare the empty slice as `sessions = []*state.Session{}`.

- [ ] **Step 4: Run tests — pass**

Run: `go test ./internal/api/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/
git commit -m "feat(backend): GET /sessions + /healthz handlers"
```

---

### Task 4.5: Wire it all in `cmd/server/main.go`

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Replace `main.go`**

```go
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/luxarts/supervaisor/internal/api"
	"github.com/luxarts/supervaisor/internal/broadcast"
	"github.com/luxarts/supervaisor/internal/ingest"
	"github.com/luxarts/supervaisor/internal/state"
	"github.com/luxarts/supervaisor/internal/store"
)

func main() {
	port := envDefault("PORT", "8080")
	dbPath := envDefault("DB_PATH", "/var/lib/supervaisor/data.db")

	if err := os.MkdirAll(dirOf(dbPath), 0o755); err != nil {
		log.Fatalf("mkdir db dir: %v", err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	hub := broadcast.NewHub()
	go hub.Run()
	defer hub.Stop()

	ingestH := &ingest.Handler{Store: db, Hub: hub}
	clientsH := &broadcast.Handler{Hub: hub, Snapshot: snapshotProvider{db}}
	apiH := &api.Handler{Store: db}

	r := gin.Default()
	r.Use(cors())
	apiH.Register(r)
	r.GET("/ws/ingest", gin.WrapF(ingestH.Serve))
	r.GET("/ws/clients", gin.WrapF(clientsH.Serve))

	// Periodic status recompute (idle/stale).
	go runStatusTicker(db, hub)

	log.Printf("supervAIsor backend on :%s, db=%s", port, dbPath)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}

type snapshotProvider struct{ db *store.SQLite }

func (s snapshotProvider) Snapshot() []*state.Session {
	sess, _ := s.db.ListSessions(context.Background())
	return sess
}

func runStatusTicker(db *store.SQLite, hub *broadcast.Hub) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for now := range tick.C {
		sessions, err := db.ListSessions(context.Background())
		if err != nil {
			log.Printf("status tick list: %v", err)
			continue
		}
		for _, s := range sessions {
			prev := s.Status
			state.RecomputeStatus(s, now.UTC())
			if s.Status != prev {
				_ = db.UpsertSession(context.Background(), s)
				if b, err := json.Marshal(map[string]any{"kind": "update", "session": s}); err == nil {
					hub.Broadcast(b)
				}
			}
		}
	}
}

func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func envDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}
```

Add the missing imports `"context"` and `"encoding/json"` (they are used in `snapshotProvider.Snapshot` and `runStatusTicker`):

```go
import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
	// ... rest
)
```

- [ ] **Step 2: Build and run the test suite**

Run: `cd backend && go build ./... && go test ./...`
Expected: build success, all tests pass.

- [ ] **Step 3: Smoke-test locally**

```bash
DB_PATH=/tmp/supervaisor.db go run ./cmd/server &
sleep 1
curl -s localhost:8080/healthz
curl -s localhost:8080/sessions
kill %1
```
Expected: `{"ok":true}` and `[]`.

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "feat(backend): wire ingest/clients WS, REST, status ticker in main"
```

---

## Phase 5 — Poller (host-native Go binary)

### Task 5.1: Bootstrap the `poller/` module

**Files:**
- Create: `poller/go.mod`
- Create: `poller/cmd/poller/main.go`

- [ ] **Step 1: Initialize module**

```bash
mkdir -p /Users/lucasbacelo/Projects/supervAIsor/poller/cmd/poller
cd /Users/lucasbacelo/Projects/supervAIsor/poller
go mod init github.com/luxarts/supervaisor-poller
go get github.com/gorilla/websocket@latest
go get github.com/fsnotify/fsnotify@latest
go get github.com/stretchr/testify@latest
```

- [ ] **Step 2: Write a minimal `cmd/poller/main.go` that just prints config**

```go
package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
)

type Config struct {
	ProjectsDir string
	StateFile   string
	BackendURL  string
}

func loadConfig() Config {
	home, _ := os.UserHomeDir()
	c := Config{
		ProjectsDir: filepath.Join(home, ".claude", "projects"),
		StateFile:   filepath.Join(home, ".supervAIsor", "poller-state.json"),
		BackendURL:  "ws://localhost:8080/ws/ingest",
	}
	flag.StringVar(&c.ProjectsDir, "projects-dir", c.ProjectsDir, "Claude projects dir")
	flag.StringVar(&c.StateFile, "state-file", c.StateFile, "Offset state file")
	flag.StringVar(&c.BackendURL, "backend", c.BackendURL, "Backend WS URL")
	flag.Parse()
	return c
}

func main() {
	cfg := loadConfig()
	log.Printf("poller config: %+v", cfg)
}
```

- [ ] **Step 3: Build**

Run: `cd poller && go build ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add poller/
git commit -m "feat(poller): bootstrap module with config"
```

---

### Task 5.2: Offset persistence

**Files:**
- Create: `poller/internal/offsets/offsets.go`
- Create: `poller/internal/offsets/offsets_test.go`

- [ ] **Step 1: Write failing test**

`poller/internal/offsets/offsets_test.go`:

```go
package offsets

import (
	"path/filepath"
	"testing"
)

func TestLoadSaveRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	s, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	s.Set("file-a", 1234, 100)
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}

	s2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	off, ino, ok := s2.Get("file-a")
	if !ok || off != 100 || ino != 1234 {
		t.Errorf("got off=%d ino=%d ok=%v", off, ino, ok)
	}
}
```

- [ ] **Step 2: Run — fail**

Run: `cd poller && go test ./internal/offsets/...`
Expected: FAIL.

- [ ] **Step 3: Implement `poller/internal/offsets/offsets.go`**

```go
package offsets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type fileOffset struct {
	Inode  uint64 `json:"inode"`
	Offset int64  `json:"offset"`
}

type Store struct {
	mu sync.Mutex
	m  map[string]fileOffset
}

func Load(path string) (*Store, error) {
	s := &Store{m: map[string]fileOffset{}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, &s.m); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Save(path string) error {
	s.mu.Lock()
	b, err := json.MarshalIndent(s.m, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) Get(file string) (offset int64, inode uint64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[file]
	return v.Offset, v.Inode, ok
}

func (s *Store) Set(file string, inode uint64, offset int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[file] = fileOffset{Inode: inode, Offset: offset}
}
```

- [ ] **Step 4: Run — pass**

Run: `go test ./internal/offsets/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add poller/internal/offsets/
git commit -m "feat(poller): persistent per-file offset store"
```

---

### Task 5.3: Tailer — read new lines from a single file

**Files:**
- Create: `poller/internal/tailer/tailer.go`
- Create: `poller/internal/tailer/tailer_test.go`

- [ ] **Step 1: Write failing test**

`poller/internal/tailer/tailer_test.go`:

```go
package tailer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRead_OnlyNewLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.jsonl")
	if err := os.WriteFile(p, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := &Tailer{Path: p, Offset: 0}
	lines, newOff, err := tr.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "line1" || lines[1] != "line2" {
		t.Fatalf("first read got %v", lines)
	}
	if newOff == 0 {
		t.Errorf("offset still 0")
	}

	// Append more.
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("line3\n")
	f.Close()

	tr2 := &Tailer{Path: p, Offset: newOff}
	lines2, _, err := tr2.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines2) != 1 || lines2[0] != "line3" {
		t.Errorf("delta read got %v", lines2)
	}

	_ = time.Now()
}
```

- [ ] **Step 2: Run — fail**

Run: `cd poller && go test ./internal/tailer/...`
Expected: FAIL.

- [ ] **Step 3: Implement `poller/internal/tailer/tailer.go`**

```go
package tailer

import (
	"bufio"
	"errors"
	"io"
	"os"
	"syscall"
)

type Tailer struct {
	Path   string
	Offset int64
}

// Inode returns the current inode of the file, or 0 if unknown.
func (t *Tailer) Inode() (uint64, error) {
	fi, err := os.Stat(t.Path)
	if err != nil {
		return 0, err
	}
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("stat: not a unix file")
	}
	return uint64(sys.Ino), nil
}

// Read returns the lines added since t.Offset and the new offset.
// If the file is smaller than t.Offset (rotation), it reads from the start.
func (t *Tailer) Read() ([]string, int64, error) {
	f, err := os.Open(t.Path)
	if err != nil {
		return nil, t.Offset, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, t.Offset, err
	}
	size := fi.Size()

	start := t.Offset
	if start > size {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, t.Offset, err
	}

	var out []string
	br := bufio.NewReader(f)
	pos := start
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			trimmed := line
			if line[len(line)-1] == '\n' {
				trimmed = line[:len(line)-1]
				out = append(out, trimmed)
				pos += int64(len(line))
			} else {
				// Partial line — stop, leave it for next read.
				break
			}
		}
		if err != nil {
			break
		}
	}
	return out, pos, nil
}
```

- [ ] **Step 4: Run — pass**

Run: `go test ./internal/tailer/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add poller/internal/tailer/
git commit -m "feat(poller): tailer reads only newly appended lines"
```

---

### Task 5.4: WS client to backend

**Files:**
- Create: `poller/internal/wsclient/client.go`
- Create: `poller/internal/wsclient/client_test.go`

- [ ] **Step 1: Write failing test**

`poller/internal/wsclient/client_test.go`:

```go
package wsclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestSend_DeliversEnvelope(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var (
		mu       sync.Mutex
		received []map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			var m map[string]any
			_ = json.Unmarshal(data, &m)
			mu.Lock()
			received = append(received, m)
			mu.Unlock()
		}
	}))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	cli := New(url)
	if err := cli.Connect(); err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	if err := cli.Send(map[string]any{"hello": "world"}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 || received[0]["hello"] != "world" {
		t.Errorf("got %#v", received)
	}
}
```

- [ ] **Step 2: Run — fail**

Run: `cd poller && go test ./internal/wsclient/...`
Expected: FAIL.

- [ ] **Step 3: Implement `poller/internal/wsclient/client.go`**

```go
package wsclient

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	url  string
	mu   sync.Mutex
	conn *websocket.Conn
}

func New(url string) *Client { return &Client{url: url} }

func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return nil
	}
	conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) Send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return websocket.ErrCloseSent
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.conn.WriteMessage(websocket.TextMessage, b)
}

// EnsureConnected retries connect with exponential backoff capped at 30s.
// It returns once connected or ctx-cancelled (caller passes its own cancel).
func (c *Client) EnsureConnected(stop <-chan struct{}) {
	backoff := time.Second
	for {
		err := c.Connect()
		if err == nil {
			return
		}
		select {
		case <-stop:
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}
```

- [ ] **Step 4: Run — pass**

Run: `go test ./internal/wsclient/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add poller/internal/wsclient/
git commit -m "feat(poller): WS client with reconnect helper"
```

---

### Task 5.5: Scanner — discover files and orchestrate the poll loop

**Files:**
- Create: `poller/internal/scanner/scanner.go`
- Modify: `poller/cmd/poller/main.go`

- [ ] **Step 1: Implement `poller/internal/scanner/scanner.go`**

```go
package scanner

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luxarts/supervaisor-poller/internal/offsets"
	"github.com/luxarts/supervaisor-poller/internal/tailer"
	"github.com/luxarts/supervaisor-poller/internal/wsclient"
)

type Envelope struct {
	SessionID  string          `json:"session_id"`
	ProjectDir string          `json:"project_dir"`
	FileMTime  time.Time       `json:"file_mtime"`
	LineIndex  int             `json:"line_index"`
	Raw        json.RawMessage `json:"raw"`
}

type Scanner struct {
	ProjectsDir string
	Offsets     *offsets.Store
	OffsetsPath string
	Client      *wsclient.Client
	Interval    time.Duration
}

func (s *Scanner) RunOnce() error {
	entries, err := os.ReadDir(s.ProjectsDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirPath := filepath.Join(s.ProjectsDir, e.Name())
		files, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(dirPath, f.Name())
			if err := s.processFile(path, e.Name(), strings.TrimSuffix(f.Name(), ".jsonl")); err != nil {
				log.Printf("process %s: %v", path, err)
			}
		}
	}
	return s.Offsets.Save(s.OffsetsPath)
}

func (s *Scanner) processFile(path, projectDir, sessionID string) error {
	prevOff, prevIno, _ := s.Offsets.Get(path)

	t := &tailer.Tailer{Path: path, Offset: prevOff}
	curIno, err := t.Inode()
	if err != nil {
		return err
	}
	if prevIno != 0 && curIno != prevIno {
		// File rotated/replaced — restart from 0.
		t.Offset = 0
	}

	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	lines, newOff, err := t.Read()
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return nil
	}

	for i, line := range lines {
		env := Envelope{
			SessionID:  sessionID,
			ProjectDir: projectDir,
			FileMTime:  fi.ModTime().UTC(),
			LineIndex:  int(prevOff) + i, // approximate
			Raw:        json.RawMessage(line),
		}
		if !json.Valid(env.Raw) {
			continue
		}
		if err := s.Client.Send(env); err != nil {
			return err
		}
	}
	s.Offsets.Set(path, curIno, newOff)
	return nil
}

func (s *Scanner) Run(stop <-chan struct{}) {
	if s.Interval == 0 {
		s.Interval = time.Second
	}
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	for {
		s.Client.EnsureConnected(stop)
		if err := s.RunOnce(); err != nil {
			log.Printf("scan: %v", err)
			s.Client.Close() // force reconnect on next loop
		}
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}
```

- [ ] **Step 2: Rewrite `poller/cmd/poller/main.go`**

```go
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/luxarts/supervaisor-poller/internal/offsets"
	"github.com/luxarts/supervaisor-poller/internal/scanner"
	"github.com/luxarts/supervaisor-poller/internal/wsclient"
)

type Config struct {
	ProjectsDir string
	StateFile   string
	BackendURL  string
	Interval    time.Duration
}

func loadConfig() Config {
	home, _ := os.UserHomeDir()
	c := Config{
		ProjectsDir: filepath.Join(home, ".claude", "projects"),
		StateFile:   filepath.Join(home, ".supervAIsor", "poller-state.json"),
		BackendURL:  "ws://localhost:8080/ws/ingest",
		Interval:    time.Second,
	}
	flag.StringVar(&c.ProjectsDir, "projects-dir", c.ProjectsDir, "Claude projects dir")
	flag.StringVar(&c.StateFile, "state-file", c.StateFile, "Offset state file")
	flag.StringVar(&c.BackendURL, "backend", c.BackendURL, "Backend WS URL")
	flag.DurationVar(&c.Interval, "interval", c.Interval, "Poll interval")
	flag.Parse()
	return c
}

func main() {
	cfg := loadConfig()
	log.Printf("poller: %+v", cfg)

	off, err := offsets.Load(cfg.StateFile)
	if err != nil {
		log.Fatalf("load offsets: %v", err)
	}
	cli := wsclient.New(cfg.BackendURL)

	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		log.Println("shutting down")
		close(stop)
		cli.Close()
	}()

	s := &scanner.Scanner{
		ProjectsDir: cfg.ProjectsDir,
		Offsets:     off,
		OffsetsPath: cfg.StateFile,
		Client:      cli,
		Interval:    cfg.Interval,
	}
	s.Run(stop)
}
```

- [ ] **Step 3: Build and smoke test**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor/poller
go build ./...
# Run against a running backend:
# 1. In one terminal: cd backend && go run ./cmd/server
# 2. In another:      cd poller  && go run ./cmd/poller
# 3. In a third:      curl localhost:8080/sessions
```
Expected: backend's `/sessions` lists at least one real session derived from the JSONL on disk.

- [ ] **Step 4: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add poller/
git commit -m "feat(poller): scanner orchestrates tail+send on poll interval"
```

---

## Phase 6 — Frontend (React + Vite + Tailwind, Cyberpunk theme)

### Task 6.1: Scaffold the Vite + TS + Tailwind project

**Files:**
- Create: `frontend/` tree

- [ ] **Step 1: Bootstrap Vite project**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
npm create vite@latest frontend -- --template react-ts -y
cd frontend
npm install
npm install -D tailwindcss@^3 postcss autoprefixer vitest @testing-library/react @testing-library/dom jsdom
npx tailwindcss init -p
```

- [ ] **Step 2: Configure Tailwind in `frontend/tailwind.config.js`**

```js
/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        bg: {
          base:  "#0a0a0f",
          panel: "#11111a",
        },
        cy:  "#00f0ff",
        yl:  "#fcee0a",
        rd:  "#ff003c",
        dim: "#6b7280",
        txt: "#e6f9ff",
      },
      fontFamily: {
        hud:  ["'Share Tech Mono'", "monospace"],
        mono: ["'JetBrains Mono'", "monospace"],
      },
      keyframes: {
        pulseDot: {
          "0%,100%": { opacity: 1 },
          "50%":     { opacity: 0.3 },
        },
        glitch: {
          "0%,100%": { transform: "translate(0,0)" },
          "20%":     { transform: "translate(-1px,1px)" },
          "40%":     { transform: "translate(1px,-1px)" },
        },
      },
      animation: {
        pulseDot: "pulseDot 1.2s ease-in-out infinite",
        glitch:   "glitch 0.4s steps(2,end) 1",
      },
    },
  },
  plugins: [],
};
```

- [ ] **Step 3: Replace `frontend/src/index.css`**

```css
@tailwind base;
@tailwind components;
@tailwind utilities;

@import url("https://fonts.googleapis.com/css2?family=Share+Tech+Mono&family=JetBrains+Mono:wght@400;700&display=swap");

html, body, #root {
  height: 100%;
  background: #0a0a0f;
  color: #e6f9ff;
  font-family: "JetBrains Mono", monospace;
}
```

- [ ] **Step 4: Update `frontend/vite.config.ts` for Vitest + jsdom**

```ts
/// <reference types="vitest" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: { host: true, port: 5173 },
  test: { environment: "jsdom", globals: true },
});
```

- [ ] **Step 5: Verify dev server starts**

```bash
npm run dev -- --host 127.0.0.1 &
sleep 3
curl -sI http://127.0.0.1:5173 | head -1
kill %1
```
Expected: `HTTP/1.1 200 OK`.

- [ ] **Step 6: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add frontend/
git commit -m "feat(frontend): scaffold Vite+TS+Tailwind with cyberpunk theme tokens"
```

---

### Task 6.2: Types + time formatters

**Files:**
- Create: `frontend/src/types.ts`
- Create: `frontend/src/lib/time.ts`
- Create: `frontend/src/lib/time.test.ts`

- [ ] **Step 1: Write `frontend/src/types.ts`**

```ts
export type Status = "working" | "waiting_input" | "idle" | "stale";

export interface Session {
  id: string;
  name: string;
  project: string;
  status: Status;
  started_at: string;       // ISO-8601
  last_prompt_at?: string;
  current_action: string;
  last_event_at: string;
}

export type Frame =
  | { kind: "snapshot"; sessions: Session[] }
  | { kind: "update"; session: Session }
  | { kind: "delete"; session_id: string };
```

- [ ] **Step 2: Write failing test `frontend/src/lib/time.test.ts`**

```ts
import { describe, it, expect } from "vitest";
import { formatDuration } from "./time";

describe("formatDuration", () => {
  it("formats seconds", () => {
    expect(formatDuration(45_000)).toBe("00:45");
  });
  it("formats minutes:seconds", () => {
    expect(formatDuration(125_000)).toBe("02:05");
  });
  it("formats hours:minutes:seconds when >= 1h", () => {
    expect(formatDuration(3_725_000)).toBe("01:02:05");
  });
});
```

- [ ] **Step 3: Run — fail**

Run: `cd frontend && npx vitest run src/lib/time.test.ts`
Expected: FAIL.

- [ ] **Step 4: Implement `frontend/src/lib/time.ts`**

```ts
export function formatDuration(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const s = total % 60;
  const m = Math.floor(total / 60) % 60;
  const h = Math.floor(total / 3600);
  const pad = (n: number) => n.toString().padStart(2, "0");
  if (h > 0) return `${pad(h)}:${pad(m)}:${pad(s)}`;
  return `${pad(m)}:${pad(s)}`;
}
```

- [ ] **Step 5: Run — pass**

Run: `npx vitest run src/lib/time.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/types.ts frontend/src/lib/
git commit -m "feat(frontend): types and time formatting helpers"
```

---

### Task 6.3: Session WS hook

**Files:**
- Create: `frontend/src/useSessionsSocket.ts`

- [ ] **Step 1: Implement `frontend/src/useSessionsSocket.ts`**

```ts
import { useEffect, useRef, useState } from "react";
import type { Session, Frame } from "./types";

export interface SocketState {
  sessions: Session[];
  connected: boolean;
}

export function useSessionsSocket(url: string): SocketState {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [connected, setConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);
  const retryRef = useRef<number>(0);

  useEffect(() => {
    let stopped = false;

    const connect = () => {
      if (stopped) return;
      const ws = new WebSocket(url);
      wsRef.current = ws;

      ws.onopen = () => {
        setConnected(true);
        retryRef.current = 0;
      };

      ws.onmessage = (ev) => {
        try {
          const frame = JSON.parse(ev.data) as Frame;
          if (frame.kind === "snapshot") {
            setSessions(frame.sessions);
          } else if (frame.kind === "update") {
            setSessions((prev) => {
              const idx = prev.findIndex((s) => s.id === frame.session.id);
              if (idx < 0) return [frame.session, ...prev];
              const copy = prev.slice();
              copy[idx] = frame.session;
              return copy;
            });
          } else if (frame.kind === "delete") {
            setSessions((prev) => prev.filter((s) => s.id !== frame.session_id));
          }
        } catch {
          // ignore malformed frames
        }
      };

      ws.onclose = () => {
        setConnected(false);
        if (stopped) return;
        const delay = Math.min(15000, 1000 * Math.pow(2, retryRef.current++));
        setTimeout(connect, delay);
      };

      ws.onerror = () => {
        ws.close();
      };
    };

    connect();
    return () => {
      stopped = true;
      wsRef.current?.close();
    };
  }, [url]);

  return { sessions, connected };
}
```

- [ ] **Step 2: Build**

Run: `cd frontend && npx tsc --noEmit`
Expected: no type errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/useSessionsSocket.ts
git commit -m "feat(frontend): useSessionsSocket hook with reconnect"
```

---

### Task 6.4: Cyberpunk UI components

**Files:**
- Create: `frontend/src/components/StatusBadge.tsx`
- Create: `frontend/src/components/ScanlineOverlay.tsx`
- Create: `frontend/src/components/SessionCard.tsx`

- [ ] **Step 1: Write `frontend/src/components/StatusBadge.tsx`**

```tsx
import type { Status } from "../types";

const STYLES: Record<Status, { label: string; color: string; bg: string }> = {
  working:       { label: "WORKING", color: "text-cy", bg: "border-cy/60" },
  waiting_input: { label: "WAIT",    color: "text-yl", bg: "border-yl/60" },
  idle:          { label: "IDLE",    color: "text-dim", bg: "border-dim/60" },
  stale:         { label: "STALE",   color: "text-rd", bg: "border-rd/60" },
};

export function StatusBadge({ status }: { status: Status }) {
  const s = STYLES[status];
  return (
    <span className={`inline-flex items-center gap-2 border ${s.bg} ${s.color} px-2 py-1 text-xs font-hud tracking-widest`}>
      <span
        aria-hidden
        className={`h-2 w-2 rounded-full bg-current ${status === "working" ? "animate-pulseDot" : ""}`}
      />
      {s.label}
    </span>
  );
}
```

- [ ] **Step 2: Write `frontend/src/components/ScanlineOverlay.tsx`**

```tsx
export function ScanlineOverlay() {
  return (
    <div
      aria-hidden
      className="pointer-events-none fixed inset-0 z-50 opacity-[0.06]"
      style={{
        background:
          "repeating-linear-gradient(to bottom, transparent 0px, transparent 2px, #00f0ff 2px, #00f0ff 3px)",
      }}
    />
  );
}
```

- [ ] **Step 3: Write `frontend/src/components/SessionCard.tsx`**

```tsx
import { useEffect, useState } from "react";
import type { Session } from "../types";
import { StatusBadge } from "./StatusBadge";
import { formatDuration } from "../lib/time";

export function SessionCard({ session }: { session: Session }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  const total = now - new Date(session.started_at).getTime();
  const prompt = session.last_prompt_at
    ? now - new Date(session.last_prompt_at).getTime()
    : null;

  return (
    <article
      key={session.id}
      className={`relative min-h-[160px] border bg-bg-panel p-4 transition-colors
                  border-cy/30 hover:border-cy active:scale-[0.99]
                  ${session.status === "working" ? "shadow-[0_0_18px_rgba(0,240,255,0.25)]" : ""}`}
    >
      <header className="flex items-start justify-between gap-2">
        <StatusBadge status={session.status} />
        <div className="font-hud text-[10px] text-dim truncate max-w-[55%]">{session.project}</div>
      </header>

      <h2 className="mt-3 font-hud text-xl uppercase tracking-wider text-txt truncate">
        {session.name}
      </h2>

      <p className="mt-2 line-clamp-2 font-mono text-xs text-cy/80">
        {session.current_action || "—"}
      </p>

      <footer className="mt-auto flex items-end justify-between pt-4 font-hud text-xs">
        <div>
          <div className="text-dim">TOTAL</div>
          <div className="text-cy text-base">{formatDuration(total)}</div>
        </div>
        {prompt !== null && session.status === "working" && (
          <div className="text-right">
            <div className="text-dim">PROMPT</div>
            <div className="text-yl text-base">{formatDuration(prompt)}</div>
          </div>
        )}
      </footer>
    </article>
  );
}
```

- [ ] **Step 4: Verify TS**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/
git commit -m "feat(frontend): StatusBadge, ScanlineOverlay, SessionCard"
```

---

### Task 6.5: App shell

**Files:**
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/main.tsx`

- [ ] **Step 1: Replace `frontend/src/App.tsx`**

```tsx
import { useSessionsSocket } from "./useSessionsSocket";
import { SessionCard } from "./components/SessionCard";
import { ScanlineOverlay } from "./components/ScanlineOverlay";

const WS_URL =
  (import.meta.env.VITE_BACKEND_WS as string | undefined) ??
  "ws://localhost:8080/ws/clients";

export default function App() {
  const { sessions, connected } = useSessionsSocket(WS_URL);

  return (
    <div className="min-h-full p-4">
      <ScanlineOverlay />
      <header className="mb-4 flex items-center justify-between">
        <h1 className="font-hud text-2xl tracking-widest text-cy">
          SUPERV<span className="text-yl">AI</span>SOR
        </h1>
        <span className={`font-hud text-xs ${connected ? "text-cy" : "text-rd animate-glitch"}`}>
          {connected ? "// LINK OK" : "// DISCONNECTED"}
        </span>
      </header>

      {sessions.length === 0 ? (
        <div className="mt-12 text-center font-hud text-dim">NO SESSIONS DETECTED</div>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {sessions.map((s) => (
            <SessionCard key={s.id} session={s} />
          ))}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Update `frontend/src/main.tsx`**

```tsx
import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import "./index.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
```

- [ ] **Step 3: Verify TS and build**

Run: `cd frontend && npx tsc --noEmit && npm run build`
Expected: build succeeds.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/App.tsx frontend/src/main.tsx
git commit -m "feat(frontend): app shell with cyberpunk header and session grid"
```

---

## Phase 7 — Infrastructure

### Task 7.1: Backend Dockerfile

**Files:**
- Create: `infrastructure/backend.Dockerfile`

- [ ] **Step 1: Write `infrastructure/backend.Dockerfile`**

```dockerfile
# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
ENV DB_PATH=/var/lib/supervaisor/data.db
USER nonroot
ENTRYPOINT ["/server"]
```

- [ ] **Step 2: Build it locally**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
docker build -f infrastructure/backend.Dockerfile -t supervaisor-backend:dev .
```
Expected: image builds successfully.

- [ ] **Step 3: Commit**

```bash
git add infrastructure/backend.Dockerfile
git commit -m "feat(infra): backend Dockerfile (distroless, static binary)"
```

---

### Task 7.2: Frontend Dockerfile

**Files:**
- Create: `frontend/Dockerfile`
- Create: `frontend/.dockerignore`

- [ ] **Step 1: Write `frontend/.dockerignore`**

```
node_modules
dist
.git
```

- [ ] **Step 2: Write `frontend/Dockerfile`**

```dockerfile
# syntax=docker/dockerfile:1.7

FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY <<'EOF' /etc/nginx/conf.d/default.conf
server {
  listen 5173;
  root /usr/share/nginx/html;
  location / {
    try_files $uri /index.html;
  }
}
EOF
EXPOSE 5173
```

- [ ] **Step 3: Build it**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor/frontend
docker build -t supervaisor-frontend:dev .
```
Expected: image builds successfully.

- [ ] **Step 4: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add frontend/Dockerfile frontend/.dockerignore
git commit -m "feat(infra): frontend Dockerfile (nginx serves static build)"
```

---

### Task 7.3: docker-compose and root Makefile

**Files:**
- Create: `infrastructure/docker-compose.yml`
- Create: `Makefile`

- [ ] **Step 1: Write `infrastructure/docker-compose.yml`**

```yaml
services:
  backend:
    build:
      context: ..
      dockerfile: infrastructure/backend.Dockerfile
    ports:
      - "8080:8080"
    volumes:
      - supervaisor-data:/var/lib/supervaisor
    restart: unless-stopped

  frontend:
    build:
      context: ../frontend
    ports:
      - "5173:5173"
    depends_on:
      - backend
    restart: unless-stopped

volumes:
  supervaisor-data:
```

- [ ] **Step 2: Write root `Makefile`**

```makefile
.PHONY: up down poller poller-install logs test

up:
	docker compose -f infrastructure/docker-compose.yml up --build -d

down:
	docker compose -f infrastructure/docker-compose.yml down

logs:
	docker compose -f infrastructure/docker-compose.yml logs -f

poller:
	cd poller && go run ./cmd/poller

poller-install:
	cd poller && go build -o $(HOME)/.local/bin/supervaisor-poller ./cmd/poller
	@echo "Installed to $(HOME)/.local/bin/supervaisor-poller"
	@echo "Run: supervaisor-poller"

test:
	cd backend && go test ./...
	cd poller  && go test ./...
	cd frontend && npx vitest run
```

- [ ] **Step 3: End-to-end smoke test**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
make up
sleep 5
curl -s localhost:8080/healthz   # {"ok":true}
curl -sI localhost:5173 | head -1 # HTTP/1.1 200 OK
make poller &
sleep 5
curl -s localhost:8080/sessions | head -c 400
kill %1
make down
```
Expected: backend and frontend reachable, `/sessions` returns at least one session derived from the local JSONL files.

- [ ] **Step 4: Commit**

```bash
git add infrastructure/docker-compose.yml Makefile
git commit -m "feat(infra): docker compose + Makefile (up/down/poller/test)"
```

---

### Task 7.4: README updates

**Files:**
- Modify: `README.md`
- Modify: `backend/README.md`

- [ ] **Step 1: Replace `README.md` with the new architecture overview**

```markdown
# supervAIsor

Mobile-first Cyberpunk 2077-themed dashboard for monitoring local Claude Code sessions.

## Architecture

```
host poller  ──ws──►  backend (Docker)  ◄──ws──  frontend (Docker)
                    └─ SQLite (volume)
```

- **`poller/`** — host-native Go binary; tails `~/.claude/projects/**/*.jsonl`.
- **`backend/`** — Go (Gin + WS), derives session state, persists to SQLite, broadcasts to clients.
- **`frontend/`** — React + Vite, Cyberpunk 2077 theme, mobile-first.
- **`infrastructure/`** — Docker Compose.

## Quickstart

```bash
make up              # starts backend + frontend
make poller-install  # installs poller into ~/.local/bin
supervaisor-poller   # run it
open http://localhost:5173
```
```

- [ ] **Step 2: Trim `backend/README.md`**

Replace the old agent/hooks content with a short description of the new endpoints:

```markdown
# supervAIsor — Backend

Go server: SQLite-backed session state, WS ingest from the host poller, WS broadcast to frontends.

## Endpoints

- `GET  /healthz`          — liveness
- `GET  /sessions`         — snapshot of derived sessions
- `WS   /ws/ingest`        — single-writer; receives raw JSONL events from the poller
- `WS   /ws/clients`       — fan-out; sends snapshot + update + delete frames to clients

## Env vars

| Var       | Default                          |
|-----------|----------------------------------|
| `PORT`    | `8080`                           |
| `DB_PATH` | `/var/lib/supervaisor/data.db`   |

## Tests

```bash
go test ./...
```
```

- [ ] **Step 3: Commit**

```bash
git add README.md backend/README.md
git commit -m "docs: update READMEs for new three-process architecture"
```

---

## Verification Checklist (run after Phase 7)

- [ ] `cd backend && go test ./...` — all green
- [ ] `cd poller && go test ./...` — all green
- [ ] `cd frontend && npx vitest run` — all green
- [ ] `make up` → `http://localhost:5173` opens the dashboard
- [ ] `make poller` → within ~2 s, the dashboard shows at least one session card
- [ ] Open Claude Code in any project, send a message, watch the card flip to `WORKING` then `WAIT`
- [ ] Open the dashboard on a phone via LAN → cards are tappable, ≥44 px hit targets, status colors readable
- [ ] Kill the backend container; the dashboard switches to `// DISCONNECTED`; restart, dashboard reconnects with a fresh snapshot

---

## Notes for the executor

- **Module paths.** Backend uses `github.com/luxarts/supervaisor`. Poller uses `github.com/luxarts/supervaisor-poller`. Do not cross-import.
- **Pure derivation.** All session-state logic lives in `internal/state/`. The ingest handler is the only place that calls `Apply` and `RecomputeStatus`.
- **Single-writer ingest.** `internal/ingest/handler.go` enforces one connection at a time via `atomic.Bool`. The second poller gets `409 Conflict`.
- **No CGO.** Use `modernc.org/sqlite`; do not switch to `mattn/go-sqlite3`. Distroless image will not have a C runtime.
- **Frontend env var.** `VITE_BACKEND_WS` defaults to `ws://localhost:8080/ws/clients`. Override at build time or via docker-compose env.
- **The poller's `LineIndex` is approximate** — it's `prevOffset + i`. The backend doesn't depend on it being monotonic; it's purely informational.
