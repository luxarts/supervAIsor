# Frontend Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add hide-stale toggle, fix project path display (including dashed names), add a conversation modal with markdown rendering, and add a search bar — all touch-friendly.

**Architecture:** Resolve project paths against the host filesystem in the poller (the only process with FS access). Expose a new backend endpoint to read raw events for one session. Frontend gets a header (toggle + search), clickable cards, and a swipe-to-close modal that renders messages with `react-markdown`.

**Tech Stack:** Go 1.x (poller, backend Gin + modernc sqlite), React 19 + Vite + TypeScript + Tailwind, `react-markdown` + `remark-gfm`.

**Spec:** `docs/superpowers/specs/2026-05-14-frontend-improvements-design.md`

---

## File map

**Create**
- `poller/internal/scanner/projectpath.go`
- `poller/internal/scanner/projectpath_test.go`
- `frontend/src/lib/path.ts`
- `frontend/src/lib/path.test.ts`
- `frontend/src/lib/backendUrl.ts`
- `frontend/src/components/ConversationModal.tsx`
- `frontend/src/components/ConversationModal.test.tsx`
- `frontend/src/components/HeaderControls.tsx`

**Modify**
- `poller/internal/scanner/scanner.go` — call `ResolveProjectPath` before sending envelope.
- `backend/internal/state/projectdir.go` — passthrough when path already starts with `/`.
- `backend/internal/store/sqlite.go` — add `ListEvents`.
- `backend/internal/api/sessions.go` — add `GET /sessions/:id/events`.
- `backend/internal/api/sessions_test.go` — cover new endpoint.
- `backend/internal/store/sqlite_test.go` — cover `ListEvents`.
- `frontend/package.json` — add `react-markdown`, `remark-gfm`.
- `frontend/src/App.tsx` — header, filtering, modal state.
- `frontend/src/components/SessionCard.tsx` — clickable, uses `shortProject`.
- `frontend/src/index.css` or wherever pertinent — no changes expected.

---

## Task 1: Poller — `ResolveProjectPath` greedy FS resolver

**Files:**
- Create: `poller/internal/scanner/projectpath.go`
- Test: `poller/internal/scanner/projectpath_test.go`

- [ ] **Step 1: Write the failing test**

Create `poller/internal/scanner/projectpath_test.go`:

```go
package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProjectPath_DashedName(t *testing.T) {
	root := t.TempDir()
	// Simulate /<root>/Projects/ai-dream-team existing on disk.
	target := filepath.Join(root, "Projects", "ai-dream-team")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	// Encoded form Claude would produce: replace "/" with "-".
	encoded := encodePath(target) // helper defined below
	got := ResolveProjectPath(encoded)
	if got != target {
		t.Fatalf("ResolveProjectPath(%q) = %q, want %q", encoded, got, target)
	}
}

func TestResolveProjectPath_NoDashes(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "plainproj")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	encoded := encodePath(target)
	got := ResolveProjectPath(encoded)
	if got != target {
		t.Fatalf("got %q want %q", got, target)
	}
}

func TestResolveProjectPath_FallbackWhenNothingExists(t *testing.T) {
	// "-nonexistent-path" cannot be resolved; naive decode kicks in.
	got := ResolveProjectPath("-nonexistent-path")
	want := "/nonexistent/path"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveProjectPath_Empty(t *testing.T) {
	if got := ResolveProjectPath(""); got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

// encodePath mimics Claude's encoding: every "/" becomes "-".
func encodePath(p string) string {
	out := make([]byte, 0, len(p))
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			out = append(out, '-')
		} else {
			out = append(out, p[i])
		}
	}
	return string(out)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd poller && go test ./internal/scanner/ -run ResolveProjectPath -v`
Expected: FAIL (`undefined: ResolveProjectPath`).

- [ ] **Step 3: Implement `ResolveProjectPath`**

Create `poller/internal/scanner/projectpath.go`:

```go
package scanner

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveProjectPath converts Claude's encoded project dir name (where every
// "/" in the absolute path has been replaced with "-") back to the real
// filesystem path. Because project names may themselves contain "-", the
// encoded form is ambiguous; we resolve it greedily against the actual
// filesystem, preferring the longest segment that exists at each step.
//
// If resolution fails (filesystem path absent), falls back to naive
// "-"->"/" replacement so the caller still gets a usable string.
func ResolveProjectPath(encoded string) string {
	if encoded == "" {
		return ""
	}
	if !strings.HasPrefix(encoded, "-") {
		return encoded
	}

	parts := strings.Split(encoded, "-") // first element is "" (leading "-")
	if len(parts) < 2 {
		return strings.ReplaceAll(encoded, "-", "/")
	}

	current := "/"
	i := 1
	for i < len(parts) {
		bestJ := -1
		// Find the longest j such that joining parts[i..j] (with "-") names
		// a directory inside current.
		for j := i; j < len(parts); j++ {
			name := strings.Join(parts[i:j+1], "-")
			candidate := filepath.Join(current, name)
			info, err := os.Stat(candidate)
			if err == nil && info.IsDir() {
				bestJ = j
			}
		}
		if bestJ < 0 {
			// Resolution stuck — fall back to naive decode.
			return strings.ReplaceAll(encoded, "-", "/")
		}
		current = filepath.Join(current, strings.Join(parts[i:bestJ+1], "-"))
		i = bestJ + 1
	}
	return current
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd poller && go test ./internal/scanner/ -run ResolveProjectPath -v`
Expected: PASS (all four cases).

- [ ] **Step 5: Commit**

```bash
git add poller/internal/scanner/projectpath.go poller/internal/scanner/projectpath_test.go
git commit -m "feat(poller): resolve encoded project paths against filesystem"
```

---

## Task 2: Poller — call `ResolveProjectPath` in `scanner.go`

**Files:**
- Modify: `poller/internal/scanner/scanner.go` (around line 56–60, where `processFile` is invoked)

- [ ] **Step 1: Edit `scanner.go`**

Change the `processFile` invocation inside `RunOnce` to pass the *resolved* project path instead of `e.Name()`:

Find:
```go
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
			sessionID := strings.TrimSuffix(f.Name(), ".jsonl")
			if err := s.processFile(path, e.Name(), sessionID); err != nil {
```

Replace with:
```go
		dirPath := filepath.Join(s.ProjectsDir, e.Name())
		files, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		resolvedProject := ResolveProjectPath(e.Name())
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(dirPath, f.Name())
			sessionID := strings.TrimSuffix(f.Name(), ".jsonl")
			if err := s.processFile(path, resolvedProject, sessionID); err != nil {
```

- [ ] **Step 2: Run the full poller test suite**

Run: `cd poller && go test ./...`
Expected: PASS (no existing test should break — `processFile` still gets a string, just resolved).

- [ ] **Step 3: Commit**

```bash
git add poller/internal/scanner/scanner.go
git commit -m "feat(poller): send resolved real project path in envelopes"
```

---

## Task 3: Backend — `DecodeProjectDir` passthrough for absolute paths

**Files:**
- Modify: `backend/internal/state/projectdir.go`
- Modify: `backend/internal/state/projectdir_test.go`

- [ ] **Step 1: Write the new failing test cases**

Append to `backend/internal/state/projectdir_test.go` inside the existing test table (or as a new test):

```go
func TestDecodeProjectDir_AbsolutePathPassthrough(t *testing.T) {
	cases := map[string]string{
		"/Users/lucasbacelo/Projects/ai-dream-team": "/Users/lucasbacelo/Projects/ai-dream-team",
		"/Users/lucasbacelo/Projects":               "/Users/lucasbacelo/Projects",
	}
	for in, want := range cases {
		got := DecodeProjectDir(in)
		if got != want {
			t.Fatalf("DecodeProjectDir(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/state/ -run DecodeProjectDir_AbsolutePathPassthrough -v`
Expected: FAIL — naive decode turns `/Users/.../ai-dream-team` into `/Users/.../ai/dream/team`.

- [ ] **Step 3: Update `projectdir.go` to passthrough absolute paths**

Replace contents of `backend/internal/state/projectdir.go`:

```go
package state

import "strings"

// DecodeProjectDir converts Claude's encoded project dir name back into
// a real filesystem path. Claude encodes paths by replacing every "/"
// with "-" (e.g. "-Users-lucasbacelo-foo" -> "/Users/lucasbacelo/foo").
//
// If the input already starts with "/" the poller has already resolved
// the real path against the host filesystem (the only place that can
// disambiguate project names containing "-"), so we return it as-is.
func DecodeProjectDir(s string) string {
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "/") {
		return s
	}
	return strings.ReplaceAll(s, "-", "/")
}
```

- [ ] **Step 4: Run state tests**

Run: `cd backend && go test ./internal/state/ -v`
Expected: PASS (legacy `-tmp-proj` style cases still work; new absolute-path cases pass).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/state/projectdir.go backend/internal/state/projectdir_test.go
git commit -m "feat(backend): passthrough already-resolved project paths"
```

---

## Task 4: Backend — `ListEvents` store method

**Files:**
- Modify: `backend/internal/store/sqlite.go`
- Modify: `backend/internal/store/sqlite_test.go`

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/store/sqlite_test.go`:

```go
func TestListEvents(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Insert three events for "sess-A" and one for "sess-B".
	mustAppend := func(sid string, offset time.Duration, typ, payload string) {
		if err := s.AppendEvent(ctx, sid, now.Add(offset), typ, []byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	mustAppend("sess-A", 0, "user", `{"n":1}`)
	mustAppend("sess-A", 2*time.Second, "assistant", `{"n":2}`)
	mustAppend("sess-A", time.Second, "user", `{"n":3}`)
	mustAppend("sess-B", 0, "user", `{"n":99}`)

	evs, err := s.ListEvents(ctx, "sess-A", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Fatalf("got %d events, want 3", len(evs))
	}
	// Ordered ASC by ts.
	if string(evs[0].Payload) != `{"n":1}` ||
		string(evs[1].Payload) != `{"n":3}` ||
		string(evs[2].Payload) != `{"n":2}` {
		t.Fatalf("events not ordered by ts ASC: %+v", evs)
	}

	// Limit honored.
	evs2, err := s.ListEvents(ctx, "sess-A", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs2) != 2 {
		t.Fatalf("limit not honored, got %d", len(evs2))
	}
}
```

If `filepath` import is missing in the test file, add it.

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/store/ -run TestListEvents -v`
Expected: FAIL (`s.ListEvents undefined`).

- [ ] **Step 3: Implement `ListEvents` + `Event` type**

Append to `backend/internal/store/sqlite.go` (top-level, after existing methods):

```go
// Event is one row from the events table, returned by ListEvents.
type Event struct {
	TS      time.Time       `json:"ts"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ListEvents returns events for a session ordered by ts ASC, capped at limit.
func (s *SQLite) ListEvents(ctx context.Context, sessionID string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT ts, type, payload FROM events WHERE session_id = ? ORDER BY ts ASC LIMIT ?`,
		sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var (
			ts      time.Time
			typ     string
			payload string
		)
		if err := rows.Scan(&ts, &typ, &payload); err != nil {
			return nil, err
		}
		out = append(out, Event{TS: ts, Type: typ, Payload: json.RawMessage(payload)})
	}
	return out, rows.Err()
}
```

Add `"encoding/json"` to imports at the top if not present.

- [ ] **Step 4: Run to verify it passes**

Run: `cd backend && go test ./internal/store/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/store/sqlite.go backend/internal/store/sqlite_test.go
git commit -m "feat(backend): add ListEvents store method"
```

---

## Task 5: Backend — `GET /sessions/:id/events` handler

**Files:**
- Modify: `backend/internal/api/sessions.go`
- Modify: `backend/internal/api/sessions_test.go`

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/api/sessions_test.go` (mirror the style of existing tests there — they spin up a `Handler` with a real `*store.SQLite`):

```go
func TestGetSessionEvents(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	// Seed a session row and two events.
	sess := &state.Session{
		ID:           "abc",
		Name:         "s",
		Project:      "/tmp/p",
		Status:       "idle",
		StartedAt:    now,
		LastEventAt:  now,
		CurrentAction: "",
	}
	if err := st.UpsertSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendEvent(ctx, "abc", now, "user", []byte(`{"text":"hi"}`)); err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	(&Handler{Store: st}).Register(r)

	req := httptest.NewRequest("GET", "/sessions/abc/events", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"hi"`) {
		t.Fatalf("body missing event payload: %s", w.Body.String())
	}

	// 404 for unknown session.
	req2 := httptest.NewRequest("GET", "/sessions/does-not-exist/events", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w2.Code)
	}
}
```

Add any missing imports (`net/http/httptest`, `strings`, `path/filepath`, `time`, `context`, `github.com/gin-gonic/gin`, `github.com/luxarts/supervaisor/internal/state`, `github.com/luxarts/supervaisor/internal/store`).

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/api/ -run TestGetSessionEvents -v`
Expected: FAIL (route returns 404 for both).

- [ ] **Step 3: Implement route**

Edit `backend/internal/api/sessions.go`:

```go
package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/luxarts/supervaisor/internal/state"
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
	r.GET("/sessions/:id/events", h.getSessionEvents)
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

func (h *Handler) getSessionEvents(c *gin.Context) {
	id := c.Param("id")
	ctx := c.Request.Context()

	sess, err := h.Store.GetSession(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	limit := 500
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 5000 {
			limit = n
		}
	}

	evs, err := h.Store.ListEvents(ctx, id, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if evs == nil {
		evs = []store.Event{}
	}
	c.JSON(http.StatusOK, evs)
}
```

If `GetSession` returns `(nil, nil)` on missing row, the 404 branch fires. If it returns an error like `sql.ErrNoRows`, adjust to `errors.Is(err, sql.ErrNoRows)`. Check `backend/internal/store/sqlite.go:121` to confirm; the existing `GetSession` returns `(nil, nil)` for missing rows in this codebase — if not, switch to `errors.Is`.

- [ ] **Step 4: Run to verify it passes**

Run: `cd backend && go test ./internal/api/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/sessions.go backend/internal/api/sessions_test.go
git commit -m "feat(backend): add GET /sessions/:id/events endpoint"
```

---

## Task 6: Frontend — install `react-markdown` + `remark-gfm`

**Files:**
- Modify: `frontend/package.json`, `frontend/package-lock.json`

- [ ] **Step 1: Install**

Run:
```bash
cd frontend && npm install react-markdown remark-gfm
```

- [ ] **Step 2: Verify it builds**

Run: `cd frontend && npm run build`
Expected: build succeeds.

- [ ] **Step 3: Commit**

```bash
git add frontend/package.json frontend/package-lock.json
git commit -m "chore(frontend): add react-markdown + remark-gfm"
```

---

## Task 7: Frontend — `shortProject` helper

**Files:**
- Create: `frontend/src/lib/path.ts`
- Create: `frontend/src/lib/path.test.ts`

- [ ] **Step 1: Write the failing test**

Create `frontend/src/lib/path.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { shortProject } from "./path";

describe("shortProject", () => {
  it("returns <NONE> for empty input", () => {
    expect(shortProject("")).toBe("<NONE>");
  });

  it("returns basename of a normal path", () => {
    expect(shortProject("/Users/u/Projects/supervAIsor")).toBe("supervAIsor");
  });

  it("preserves dashed project names", () => {
    expect(shortProject("/Users/u/Projects/ai-dream-team")).toBe("ai-dream-team");
  });

  it("returns <NONE> when the path ends at the Projects dir", () => {
    expect(shortProject("/Users/u/Projects")).toBe("<NONE>");
  });

  it("ignores trailing slash", () => {
    expect(shortProject("/Users/u/Projects/foo/")).toBe("foo");
  });

  it("handles single-segment paths", () => {
    expect(shortProject("/tmp")).toBe("tmp");
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `cd frontend && npx vitest run src/lib/path.test.ts`
Expected: FAIL (no module).

- [ ] **Step 3: Implement**

Create `frontend/src/lib/path.ts`:

```ts
// Returns a short, human-friendly label for a project absolute path.
// - Empty input → "<NONE>".
// - If the basename is "Projects" (e.g. "/Users/x/Projects"), we treat it
//   as no-project-context and return "<NONE>". Edge case: a project literally
//   named "Projects" would collide; acceptable for v1.
// - Otherwise: basename.
export function shortProject(path: string): string {
  if (!path) return "<NONE>";
  const trimmed = path.replace(/\/+$/, "");
  if (!trimmed) return "<NONE>";
  const idx = trimmed.lastIndexOf("/");
  const base = idx === -1 ? trimmed : trimmed.slice(idx + 1);
  if (!base) return "<NONE>";
  if (base === "Projects") return "<NONE>";
  return base;
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd frontend && npx vitest run src/lib/path.test.ts`
Expected: PASS (6/6).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/path.ts frontend/src/lib/path.test.ts
git commit -m "feat(frontend): add shortProject helper"
```

---

## Task 8: Frontend — `backendUrl` helper

**Files:**
- Create: `frontend/src/lib/backendUrl.ts`

This derives the HTTP base URL from `VITE_BACKEND_WS` so the modal fetch and the WS share configuration.

- [ ] **Step 1: Create the helper**

Create `frontend/src/lib/backendUrl.ts`:

```ts
// Derives the HTTP base URL of the backend from the WS URL the app uses.
// E.g. "ws://localhost:8080/ws/clients" -> "http://localhost:8080".
export function backendHttpBase(wsUrl: string): string {
  let s = wsUrl;
  if (s.startsWith("wss://")) s = "https://" + s.slice("wss://".length);
  else if (s.startsWith("ws://")) s = "http://" + s.slice("ws://".length);
  // Strip path: keep scheme + host + port.
  try {
    const u = new URL(s);
    return `${u.protocol}//${u.host}`;
  } catch {
    return s;
  }
}
```

- [ ] **Step 2: Smoke check via existing build**

Run: `cd frontend && npm run build`
Expected: PASS (no usage yet, but file must compile under `tsc -b`).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/backendUrl.ts
git commit -m "feat(frontend): add backendHttpBase helper"
```

---

## Task 9: Frontend — make `SessionCard` clickable + use `shortProject`

**Files:**
- Modify: `frontend/src/components/SessionCard.tsx`

- [ ] **Step 1: Update the component**

Replace contents of `frontend/src/components/SessionCard.tsx`:

```tsx
import { useEffect, useState } from "react";
import type { Session } from "../types";
import { StatusBadge } from "./StatusBadge";
import { formatDuration } from "../lib/time";
import { shortProject } from "../lib/path";

interface Props {
  session: Session;
  onOpen: (id: string) => void;
}

export function SessionCard({ session, onOpen }: Props) {
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
    <button
      type="button"
      onClick={() => onOpen(session.id)}
      className={`relative min-h-[160px] w-full border bg-bg-panel p-4 text-left transition-colors
                  border-cy/30 hover:border-cy active:scale-[0.99] touch-manipulation
                  focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cy
                  ${session.status === "working" ? "shadow-[0_0_18px_rgba(0,240,255,0.25)]" : ""}`}
    >
      <header className="flex items-start justify-between gap-2">
        <StatusBadge status={session.status} />
        <div className="font-hud text-[10px] text-dim truncate max-w-[55%]">
          {shortProject(session.project)}
        </div>
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
    </button>
  );
}
```

- [ ] **Step 2: Build to catch type errors (App.tsx will now break — that's expected)**

Run: `cd frontend && npx tsc -b`
Expected: error about missing `onOpen` prop in `App.tsx`. We'll fix that in Task 11.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/SessionCard.tsx
git commit -m "feat(frontend): make session card clickable, show short project"
```

---

## Task 10: Frontend — `HeaderControls` component (toggle + search)

**Files:**
- Create: `frontend/src/components/HeaderControls.tsx`

- [ ] **Step 1: Create the component**

```tsx
interface Props {
  hideStale: boolean;
  onToggleHideStale: () => void;
  query: string;
  onQueryChange: (q: string) => void;
}

export function HeaderControls({ hideStale, onToggleHideStale, query, onQueryChange }: Props) {
  return (
    <div className="mt-3 flex flex-col gap-2 sm:flex-row sm:items-center">
      <div className="relative flex-1">
        <input
          type="search"
          inputMode="search"
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
          placeholder="SEARCH SESSIONS…"
          className="h-12 w-full bg-bg-panel pl-3 pr-12 font-hud text-base text-txt
                     border border-cy/30 focus:border-cy focus:outline-none
                     placeholder:text-dim"
        />
        {query.length > 0 && (
          <button
            type="button"
            aria-label="Clear search"
            onClick={() => onQueryChange("")}
            className="absolute right-0 top-0 grid h-12 w-11 place-items-center
                       font-hud text-lg text-dim hover:text-cy touch-manipulation"
          >
            ×
          </button>
        )}
      </div>

      <button
        type="button"
        onClick={onToggleHideStale}
        aria-pressed={hideStale}
        className={`h-11 px-4 font-hud text-xs uppercase tracking-widest border
                    touch-manipulation
                    ${hideStale
                      ? "border-cy text-cy bg-cy/10"
                      : "border-cy/30 text-dim hover:border-cy/60"}`}
      >
        {hideStale ? "HIDE STALE" : "SHOW ALL"}
      </button>
    </div>
  );
}
```

- [ ] **Step 2: Build check**

Run: `cd frontend && npx tsc -b`
Expected: still errors only from `App.tsx` (Task 11 fixes them).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/HeaderControls.tsx
git commit -m "feat(frontend): add header controls (toggle + search)"
```

---

## Task 11: Frontend — `ConversationModal` component

**Files:**
- Create: `frontend/src/components/ConversationModal.tsx`
- Create: `frontend/src/components/ConversationModal.test.tsx`

- [ ] **Step 1: Write the failing test**

Create `frontend/src/components/ConversationModal.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { ConversationModal } from "./ConversationModal";

const sample = [
  {
    ts: "2026-05-14T12:00:00Z",
    type: "user",
    payload: {
      type: "user",
      message: { role: "user", content: [{ type: "text", text: "hello" }] },
    },
  },
  {
    ts: "2026-05-14T12:00:01Z",
    type: "assistant",
    payload: {
      type: "assistant",
      message: {
        role: "assistant",
        content: [
          { type: "text", text: "this is **bold**" },
          { type: "tool_use", name: "Bash", id: "x" },
        ],
      },
    },
  },
  {
    ts: "2026-05-14T12:00:02Z",
    type: "user",
    payload: {
      type: "user",
      message: {
        role: "user",
        content: [{ type: "tool_result", tool_use_id: "x", content: "should be hidden" }],
      },
    },
  },
];

describe("ConversationModal", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        status: 200,
        json: async () => sample,
      })),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders messages with markdown and omits tool_result", async () => {
    render(
      <ConversationModal
        sessionId="abc"
        sessionName="my-sess"
        project="/Users/u/Projects/foo"
        backendHttpBase="http://localhost:8080"
        onClose={() => {}}
      />,
    );

    await waitFor(() => expect(screen.getByText("hello")).toBeTruthy());

    // Markdown bold renders as <strong>.
    const bold = await screen.findByText("bold");
    expect(bold.tagName).toBe("STRONG");

    // Tool use compact label is shown.
    expect(screen.getByText(/▶\s*Bash/)).toBeTruthy();

    // Tool result content is NOT in the DOM.
    expect(screen.queryByText(/should be hidden/)).toBeNull();
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `cd frontend && npx vitest run src/components/ConversationModal.test.tsx`
Expected: FAIL (module missing).

- [ ] **Step 3: Implement the modal**

Create `frontend/src/components/ConversationModal.tsx`:

```tsx
import { useEffect, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { shortProject } from "../lib/path";

interface ContentBlock {
  type: string;
  text?: string;
  name?: string;
  id?: string;
  tool_use_id?: string;
}

interface RawLine {
  type?: string;
  message?: {
    role?: string;
    content?: ContentBlock[];
  };
}

interface Event {
  ts: string;
  type: string;
  payload: RawLine;
}

interface Props {
  sessionId: string;
  sessionName: string;
  project: string;
  backendHttpBase: string;
  onClose: () => void;
}

export function ConversationModal({
  sessionId,
  sessionName,
  project,
  backendHttpBase,
  onClose,
}: Props) {
  const [events, setEvents] = useState<Event[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const rootRef = useRef<HTMLDivElement>(null);

  // --- Fetch events ---
  useEffect(() => {
    let cancelled = false;
    setEvents(null);
    setError(null);
    fetch(`${backendHttpBase}/sessions/${encodeURIComponent(sessionId)}/events?limit=500`)
      .then(async (r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return (await r.json()) as Event[];
      })
      .then((data) => {
        if (!cancelled) setEvents(data);
      })
      .catch((e) => {
        if (!cancelled) setError(String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [sessionId, backendHttpBase]);

  // --- Auto-scroll to bottom on load ---
  useEffect(() => {
    if (events && bodyRef.current) {
      bodyRef.current.scrollTop = bodyRef.current.scrollHeight;
    }
  }, [events]);

  // --- Esc to close ---
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [onClose]);

  // --- Swipe-down to close (mobile only) ---
  useEffect(() => {
    if (typeof window === "undefined") return;
    if (!window.matchMedia("(pointer: coarse)").matches) return;
    const root = rootRef.current;
    const body = bodyRef.current;
    if (!root || !body) return;

    let startY = 0;
    let active = false;

    const onStart = (e: TouchEvent) => {
      if (e.touches.length !== 1) return;
      if (body.scrollTop > 0) return;
      active = true;
      startY = e.touches[0].clientY;
    };
    const onMove = (e: TouchEvent) => {
      if (!active) return;
      const dy = e.touches[0].clientY - startY;
      if (dy > 0) {
        root.style.transform = `translateY(${dy}px)`;
        root.style.transition = "none";
      }
    };
    const onEnd = (e: TouchEvent) => {
      if (!active) return;
      active = false;
      const dy = (e.changedTouches[0]?.clientY ?? startY) - startY;
      root.style.transition = "transform 150ms ease-out";
      if (dy > 80) {
        root.style.transform = `translateY(100vh)`;
        setTimeout(onClose, 150);
      } else {
        root.style.transform = "translateY(0)";
      }
    };

    root.addEventListener("touchstart", onStart, { passive: true });
    root.addEventListener("touchmove", onMove, { passive: true });
    root.addEventListener("touchend", onEnd);
    return () => {
      root.removeEventListener("touchstart", onStart);
      root.removeEventListener("touchmove", onMove);
      root.removeEventListener("touchend", onEnd);
    };
  }, [onClose, events]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-stretch sm:items-center sm:justify-center"
      onClick={onClose}
    >
      <div className="absolute inset-0 bg-black/70" />
      <div
        ref={rootRef}
        onClick={(e) => e.stopPropagation()}
        className="relative z-10 flex h-full w-full flex-col bg-bg-panel
                   sm:h-auto sm:max-h-[90vh] sm:w-full sm:max-w-3xl
                   border border-cy/40 shadow-[0_0_30px_rgba(0,240,255,0.2)]"
      >
        {/* Drag handle (mobile) */}
        <div className="grid place-items-center pt-2 sm:hidden">
          <div className="h-1 w-12 rounded bg-cy/40" aria-hidden />
        </div>

        {/* Header */}
        <header className="flex items-start justify-between gap-3 border-b border-cy/20 p-3">
          <div className="min-w-0">
            <div className="truncate font-hud text-base uppercase tracking-wider text-txt">
              {sessionName}
            </div>
            <div className="truncate font-hud text-[10px] text-dim">
              {shortProject(project)}
            </div>
          </div>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className="grid h-12 w-12 flex-shrink-0 place-items-center
                       font-hud text-2xl text-cy hover:bg-cy/10 touch-manipulation"
          >
            ×
          </button>
        </header>

        {/* Body */}
        <div
          ref={bodyRef}
          className="flex-1 overflow-y-auto overscroll-contain p-3 space-y-3"
        >
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
        </div>
      </div>
    </div>
  );
}

function MessageView({ ev }: { ev: Event }) {
  const role = ev.payload?.message?.role ?? ev.payload?.type ?? ev.type;
  const blocks = ev.payload?.message?.content ?? [];

  // Skip events without renderable content (e.g. summaries).
  if (blocks.length === 0) return null;

  // Skip events that only carry tool_result blocks (v1: omit).
  const renderable = blocks.filter((b) => b.type !== "tool_result");
  if (renderable.length === 0) return null;

  const isAssistant = role === "assistant";
  const alignment = isAssistant ? "ml-auto" : "mr-auto";
  const border = isAssistant ? "border-yl/40" : "border-cy/40";
  const text = isAssistant ? "text-yl/90" : "text-cy/90";

  return (
    <div className={`max-w-[90%] border ${border} ${alignment} bg-bg-panel/60 p-2`}>
      <div className={`mb-1 font-hud text-[10px] uppercase ${text}`}>{role}</div>
      <div className="space-y-2">
        {renderable.map((b, i) => {
          if (b.type === "text") {
            return (
              <div key={i} className="prose prose-invert max-w-none text-sm
                                       prose-p:my-1 prose-pre:my-1">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>
                  {b.text ?? ""}
                </ReactMarkdown>
              </div>
            );
          }
          if (b.type === "tool_use") {
            return (
              <div key={i} className="font-mono text-xs text-dim">
                ▶ {b.name ?? "tool"}
              </div>
            );
          }
          return null;
        })}
      </div>
    </div>
  );
}
```

Tailwind `prose` classes require the `@tailwindcss/typography` plugin; this codebase doesn't appear to use it. Drop the `prose` classes if they cause warnings — `react-markdown` still renders correctly without them. If you want bold/code styling, add this minimal CSS to `frontend/src/index.css` instead:

```css
.md strong { color: #fcee0a; font-weight: 700; }
.md code { background: rgba(0, 240, 255, 0.1); padding: 0 4px; border-radius: 2px; }
.md a { color: #00f0ff; text-decoration: underline; }
.md ul { list-style: disc; padding-left: 1.25rem; }
.md ol { list-style: decimal; padding-left: 1.25rem; }
.md p { margin: 0.25rem 0; }
.md pre { background: rgba(0,0,0,0.4); padding: 0.5rem; overflow-x: auto; }
```

Then change the `<div className="prose ...">` to `<div className="md text-sm">`.

- [ ] **Step 2: Implement the CSS adjustment if you took the alternative path**

Edit `frontend/src/index.css` and append the `.md ...` rules above. Then update `ConversationModal.tsx` to use `className="md text-sm"` instead of the `prose` classes.

- [ ] **Step 3: Run the modal test**

Run: `cd frontend && npx vitest run src/components/ConversationModal.test.tsx`
Expected: PASS (renders "hello", `<strong>bold</strong>`, `▶ Bash`, omits tool_result).

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/ConversationModal.tsx frontend/src/components/ConversationModal.test.tsx frontend/src/index.css
git commit -m "feat(frontend): add conversation modal with markdown + swipe-to-close"
```

---

## Task 12: Frontend — wire `App.tsx` (header + filter + modal)

**Files:**
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Replace `App.tsx`**

```tsx
import { useEffect, useMemo, useState } from "react";
import { useSessionsSocket } from "./useSessionsSocket";
import { SessionCard } from "./components/SessionCard";
import { ScanlineOverlay } from "./components/ScanlineOverlay";
import { HeaderControls } from "./components/HeaderControls";
import { ConversationModal } from "./components/ConversationModal";
import { shortProject } from "./lib/path";
import { backendHttpBase } from "./lib/backendUrl";

const WS_URL =
  (import.meta.env.VITE_BACKEND_WS as string | undefined) ??
  "ws://localhost:8080/ws/clients";

const HTTP_BASE = backendHttpBase(WS_URL);
const HIDE_STALE_KEY = "sv:hideStale";

export default function App() {
  const { sessions, connected } = useSessionsSocket(WS_URL);

  const [hideStale, setHideStale] = useState<boolean>(() => {
    const v = localStorage.getItem(HIDE_STALE_KEY);
    return v === null ? true : v === "1";
  });
  const [query, setQuery] = useState("");
  const [openId, setOpenId] = useState<string | null>(null);

  useEffect(() => {
    localStorage.setItem(HIDE_STALE_KEY, hideStale ? "1" : "0");
  }, [hideStale]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return sessions.filter((s) => {
      if (hideStale && s.status === "stale") return false;
      if (!q) return true;
      const project = shortProject(s.project).toLowerCase();
      const name = s.name.toLowerCase();
      return name.includes(q) || project.includes(q);
    });
  }, [sessions, hideStale, query]);

  const openSession = openId
    ? sessions.find((s) => s.id === openId) ?? null
    : null;

  return (
    <div className="min-h-full p-4">
      <ScanlineOverlay />
      <header className="mb-4">
        <div className="flex items-center justify-between">
          <h1 className="font-hud text-2xl tracking-widest text-cy">
            SUPERV<span className="text-yl">AI</span>SOR
          </h1>
          <span
            className={`font-hud text-xs ${connected ? "text-cy" : "text-rd animate-glitch"}`}
          >
            {connected ? "// LINK OK" : "// DISCONNECTED"}
          </span>
        </div>
        <HeaderControls
          hideStale={hideStale}
          onToggleHideStale={() => setHideStale((v) => !v)}
          query={query}
          onQueryChange={setQuery}
        />
      </header>

      {filtered.length === 0 ? (
        <div className="mt-12 text-center font-hud text-dim">
          {sessions.length === 0 ? "NO SESSIONS DETECTED" : "NO MATCHES"}
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {filtered.map((s) => (
            <SessionCard key={s.id} session={s} onOpen={setOpenId} />
          ))}
        </div>
      )}

      {openSession && (
        <ConversationModal
          sessionId={openSession.id}
          sessionName={openSession.name}
          project={openSession.project}
          backendHttpBase={HTTP_BASE}
          onClose={() => setOpenId(null)}
        />
      )}
    </div>
  );
}
```

- [ ] **Step 2: Run the full frontend test suite + build**

Run:
```bash
cd frontend && npx vitest run && npm run build
```
Expected: all tests pass, build succeeds.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/App.tsx
git commit -m "feat(frontend): wire header controls, search filter, and modal"
```

---

## Task 13: Manual smoke test

- [ ] **Step 1: Bring the stack up**

Run:
```bash
make up
make poller   # in a separate shell
```

- [ ] **Step 2: Open the UI**

Open `http://localhost:5173` (Vite dev) or whatever port the frontend container exposes.

- [ ] **Step 3: Verify each acceptance criterion**

- Stale sessions are hidden by default. Click `HIDE STALE` → it flips to `SHOW ALL` and stale cards reappear. Reload the page → state persists.
- Project labels show `supervAIsor`, not `/Users/.../Projects/supervAIsor`. A project named `ai-dream-team` renders as `ai-dream-team`, not `ai/dream/team`. A session run from `/Users/.../Projects` itself shows `<NONE>`.
- Search box filters by session name and short project name (case-insensitive). Clear button (×) resets.
- Clicking any card opens the modal. The conversation is scrollable; bold (`**text**`) renders bold; tool_use shows as `▶ name`; tool_result is hidden.
- On a touchscreen (or DevTools mobile emulation with touch events): swiping the modal down past ~80 px closes it. Close button (×) is ≥44 px. Search input does not zoom on iOS focus.
- `Esc` closes the modal on desktop. Backdrop click closes the modal.

- [ ] **Step 4: If everything passes, commit a tiny note (optional)**

If you needed to tweak styles during smoke test, commit those tweaks now.

---

## Final verification

- [ ] **Step 1: Full repo test suite**

Run:
```bash
make test
```

Expected: backend + poller + frontend all green.

- [ ] **Step 2: Stop services**

Run: `make down`
