# Multi-machine pollers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow multiple pollers running on different machines to feed one backend; render each session card as `name@hostname`.

**Architecture:** Each poller stamps every ingest envelope with its `hostname`. Backend keys sessions by composite `(hostname, session_id)` so the same Claude UUID can coexist across machines. The single-writer guard on `/ws/ingest` is removed. Frontend gets a new `hostname` field and renders cards as `name@hostname`.

**Tech Stack:** Go 1.x (backend, poller), gorilla/websocket, modernc.org/sqlite, Gin; React 19 + TypeScript + Vite + Tailwind.

**Spec reference:** `docs/superpowers/specs/2026-05-14-multi-machine-pollers-design.md`

---

## File Structure

**Backend — modify:**
- `backend/internal/events/types.go` — add `Hostname` to `IngestEnvelope`
- `backend/internal/state/session.go` — add `Hostname` field
- `backend/internal/state/derive.go` — set `Hostname` from envelope
- `backend/internal/store/sqlite.go` — schema with composite PK, signature changes
- `backend/internal/store/sqlite_test.go` — update for composite key
- `backend/internal/ingest/handler.go` — drop single-writer guard, validate hostname, pass through
- `backend/internal/ingest/handler_test.go` — update for new envelope shape; add multi-connection test
- `backend/internal/api/handler.go` — new route `/sessions/:hostname/:id/events`
- `backend/internal/api/handler_test.go` — update for new route

**Poller — modify:**
- `poller/internal/scanner/scanner.go` — add `Hostname` to `Envelope`, stamp on each envelope, Scanner gets `Hostname` field
- `poller/cmd/poller/main.go` — new `-backend-host`, `-backend-port`, `-hostname` flags; URL derivation; hostname resolution with `.local` strip

**Poller — create:**
- `poller/internal/hostname/hostname.go` — resolve hostname, strip macOS `.local`
- `poller/internal/hostname/hostname_test.go`
- `poller/cmd/poller/config_test.go` — test URL derivation logic (after refactor)

**Frontend — modify:**
- `frontend/src/types.ts` — add `hostname`
- `frontend/src/components/SessionCard.tsx` — render `name@hostname`
- `frontend/src/components/SessionCard.test.tsx` (new) — render assertion
- `frontend/src/useSessionsSocket.ts` — composite-key dedup
- `frontend/src/App.tsx` — open modal with `(hostname, id)` tuple; key list by composite
- `frontend/src/components/ConversationModal.tsx` — accept `hostname`, use new events URL
- `frontend/src/components/ConversationModal.test.tsx` — update sample + assertion

---

## Task 1: Add Hostname to ingest envelope (backend)

**Files:**
- Modify: `backend/internal/events/types.go`

- [ ] **Step 1: Add `Hostname` field to `IngestEnvelope`**

Edit `backend/internal/events/types.go` — add `Hostname` as the first field:

```go
type IngestEnvelope struct {
    Hostname   string          `json:"hostname"`
    SessionID  string          `json:"session_id"`
    ProjectDir string          `json:"project_dir"`
    FileMTime  time.Time       `json:"file_mtime"`
    LineIndex  int             `json:"line_index"`
    Raw        json.RawMessage `json:"raw"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd backend && go build ./...`
Expected: build succeeds (tests still pass — they use struct literals that ignore unset fields).

- [ ] **Step 3: Commit**

```bash
git add backend/internal/events/types.go
git commit -m "feat(backend): add Hostname to IngestEnvelope"
```

---

## Task 2: Add Hostname to Session state

**Files:**
- Modify: `backend/internal/state/session.go`
- Modify: `backend/internal/state/derive.go`

- [ ] **Step 1: Add `Hostname` field to `Session`**

Edit `backend/internal/state/session.go` — add `Hostname` after `ID`:

```go
type Session struct {
    Hostname      string     `json:"hostname"`
    ID            string     `json:"id"`
    Name          string     `json:"name"`
    Project       string     `json:"project"`
    Status        Status     `json:"status"`
    StartedAt     time.Time  `json:"started_at"`
    LastPromptAt  *time.Time `json:"last_prompt_at,omitempty"`
    CurrentAction string     `json:"current_action"`
    LastEventAt   time.Time  `json:"last_event_at"`

    PendingToolUseIDs map[string]struct{} `json:"-"`
}
```

- [ ] **Step 2: Set Hostname in `state.Apply`**

In `backend/internal/state/derive.go`, locate the block right after the `ID` assignment:

```go
if next.ID == "" {
    next.ID = env.SessionID
}
```

Add immediately below:

```go
if next.Hostname == "" {
    next.Hostname = env.Hostname
}
```

- [ ] **Step 3: Add a derive test for hostname propagation**

Append to `backend/internal/state/derive_test.go` (create if absent — find existing test file with `ls backend/internal/state/*_test.go`):

```go
func TestApply_SetsHostnameOnFirstEvent(t *testing.T) {
    now := time.Now().UTC()
    env := events.IngestEnvelope{
        Hostname:  "mac-A",
        SessionID: "s1",
        FileMTime: now,
        Raw:       json.RawMessage(`{"type":"user","timestamp":"` + now.Format(time.RFC3339) + `"}`),
    }
    got, err := state.Apply(nil, env)
    if err != nil {
        t.Fatal(err)
    }
    if got.Hostname != "mac-A" {
        t.Errorf("Hostname = %q, want mac-A", got.Hostname)
    }
}
```

Imports needed: `encoding/json`, `testing`, `time`, the project's `events` and `state` packages.

- [ ] **Step 4: Run the test**

Run: `cd backend && go test ./internal/state/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/state/
git commit -m "feat(backend): set Session.Hostname from envelope"
```

---

## Task 3: Migrate SQLite schema to composite key

**Files:**
- Modify: `backend/internal/store/sqlite.go`
- Modify: `backend/internal/store/sqlite_test.go`

This task changes the public store API. We update tests in the same step so the package keeps compiling.

- [ ] **Step 1: Update schema and store methods**

Replace the `schema` constant and the affected methods in `backend/internal/store/sqlite.go`:

```go
const schema = `
CREATE TABLE IF NOT EXISTS sessions (
  hostname        TEXT NOT NULL,
  id              TEXT NOT NULL,
  name            TEXT NOT NULL,
  project         TEXT NOT NULL,
  status          TEXT NOT NULL,
  started_at      TIMESTAMP NOT NULL,
  last_prompt_at  TIMESTAMP,
  current_action  TEXT,
  last_event_at   TIMESTAMP NOT NULL,
  PRIMARY KEY (hostname, id)
);
CREATE TABLE IF NOT EXISTS events (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  hostname    TEXT NOT NULL DEFAULT '',
  session_id  TEXT NOT NULL,
  ts          TIMESTAMP NOT NULL,
  type        TEXT NOT NULL,
  payload     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_session_ts ON events(hostname, session_id, ts);
`
```

Replace `UpsertSession`:

```go
func (s *SQLite) UpsertSession(ctx context.Context, sess *state.Session) error {
    var lastPrompt any
    if sess.LastPromptAt != nil {
        lastPrompt = *sess.LastPromptAt
    }
    _, err := s.db.ExecContext(ctx, `
INSERT INTO sessions (hostname, id, name, project, status, started_at, last_prompt_at, current_action, last_event_at)
VALUES (?,?,?,?,?,?,?,?,?)
ON CONFLICT(hostname, id) DO UPDATE SET
  name=excluded.name,
  project=excluded.project,
  status=excluded.status,
  started_at=excluded.started_at,
  last_prompt_at=excluded.last_prompt_at,
  current_action=excluded.current_action,
  last_event_at=excluded.last_event_at
`,
        sess.Hostname, sess.ID, sess.Name, sess.Project, string(sess.Status),
        sess.StartedAt, lastPrompt, sess.CurrentAction, sess.LastEventAt,
    )
    return err
}
```

Replace `ListSessions` SELECT and Scan to include `hostname`:

```go
func (s *SQLite) ListSessions(ctx context.Context) ([]*state.Session, error) {
    rows, err := s.db.QueryContext(ctx, `
SELECT hostname, id, name, project, status, started_at, last_prompt_at, current_action, last_event_at
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
            &sess.Hostname, &sess.ID, &sess.Name, &sess.Project, &status,
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
```

Replace `GetSession` signature and body:

```go
func (s *SQLite) GetSession(ctx context.Context, hostname, id string) (*state.Session, error) {
    row := s.db.QueryRowContext(ctx, `
SELECT hostname, id, name, project, status, started_at, last_prompt_at, current_action, last_event_at
FROM sessions WHERE hostname = ? AND id = ?`, hostname, id)
    var (
        sess   state.Session
        status string
        lp     sql.NullTime
    )
    err := row.Scan(&sess.Hostname, &sess.ID, &sess.Name, &sess.Project, &status,
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

Replace `AppendEvent`:

```go
func (s *SQLite) AppendEvent(ctx context.Context, hostname, sessionID string, ts time.Time, eventType string, payload []byte) error {
    _, err := s.db.ExecContext(ctx,
        `INSERT INTO events (hostname, session_id, ts, type, payload) VALUES (?,?,?,?,?)`,
        hostname, sessionID, ts, eventType, string(payload),
    )
    return err
}
```

Replace `ListEvents` signature (look at the current code for the SELECT/Scan; update to accept `hostname` and filter `WHERE hostname=? AND session_id=?`):

```go
func (s *SQLite) ListEvents(ctx context.Context, hostname, sessionID string, limit int) ([]Event, error) {
    rows, err := s.db.QueryContext(ctx,
        `SELECT ts, type, payload FROM events
         WHERE hostname = ? AND session_id = ?
         ORDER BY ts ASC LIMIT ?`,
        hostname, sessionID, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var out []Event
    for rows.Next() {
        var ev Event
        var payload string
        if err := rows.Scan(&ev.Ts, &ev.Type, &payload); err != nil {
            return nil, err
        }
        ev.Payload = json.RawMessage(payload)
        out = append(out, ev)
    }
    return out, rows.Err()
}
```

(If the existing `ListEvents` differs in field details, preserve them — only the WHERE clause and signature change.)

- [ ] **Step 2: Update `sqlite_test.go` callsites**

In `backend/internal/store/sqlite_test.go`:

- Every `state.Session{...}` literal in tests must include `Hostname: "host-test"` (or any string — just non-empty).
- Every `GetSession(ctx, "s1")` becomes `GetSession(ctx, "host-test", "s1")`.
- Every `AppendEvent(ctx, "s1", ...)` becomes `AppendEvent(ctx, "host-test", "s1", ...)`.
- Every `ListEvents(ctx, "sess-A", 100)` becomes `ListEvents(ctx, "host-test", "sess-A", 100)`.

- [ ] **Step 3: Add a test asserting two hosts with the same UUID coexist**

Append to `backend/internal/store/sqlite_test.go`:

```go
func TestUpsertSession_SameIDDifferentHosts(t *testing.T) {
    s := newTestStore(t)
    ctx := context.Background()
    now := time.Now().UTC().Truncate(time.Second)

    a := &state.Session{Hostname: "mac-A", ID: "uuid-1", Name: "A", Project: "/p",
        Status: state.StatusWorking, StartedAt: now, LastEventAt: now}
    b := &state.Session{Hostname: "mac-B", ID: "uuid-1", Name: "B", Project: "/p",
        Status: state.StatusWorking, StartedAt: now, LastEventAt: now}

    if err := s.UpsertSession(ctx, a); err != nil {
        t.Fatal(err)
    }
    if err := s.UpsertSession(ctx, b); err != nil {
        t.Fatal(err)
    }

    got, err := s.ListSessions(ctx)
    if err != nil {
        t.Fatal(err)
    }
    if len(got) != 2 {
        t.Fatalf("want 2 rows, got %d", len(got))
    }

    ga, _ := s.GetSession(ctx, "mac-A", "uuid-1")
    gb, _ := s.GetSession(ctx, "mac-B", "uuid-1")
    if ga == nil || ga.Name != "A" || gb == nil || gb.Name != "B" {
        t.Errorf("rows did not isolate by hostname: A=%v B=%v", ga, gb)
    }
}
```

- [ ] **Step 4: Run store tests**

Run: `cd backend && go test ./internal/store/...`
Expected: PASS (including the new test).

If you see "no such column: hostname", you ran against a leftover DB file. The tests create their own temp DB, so this should not happen — but if it does, ensure no test reuses a path.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/store/
git commit -m "feat(backend): composite (hostname, id) primary key for sessions"
```

---

## Task 4: Update ingest handler — drop guard, validate hostname

**Files:**
- Modify: `backend/internal/ingest/handler.go`
- Modify: `backend/internal/ingest/handler_test.go`

- [ ] **Step 1: Remove single-writer guard and pass hostname through**

Edit `backend/internal/ingest/handler.go`. Replace the `Handler` struct and `Serve` method:

```go
type Handler struct {
    Store *store.SQLite
    Hub   *broadcast.Hub
}

func (h *Handler) Serve(w http.ResponseWriter, r *http.Request) {
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
        if env.Hostname == "" {
            log.Printf("ingest: dropping envelope with empty hostname (session_id=%s)", env.SessionID)
            continue
        }

        prev, _ := h.Store.GetSession(ctx, env.Hostname, env.SessionID)
        next, err := state.Apply(prev, env)
        if err != nil {
            log.Printf("ingest: apply: %v", err)
            continue
        }

        state.RecomputeStatus(next, time.Now().UTC())

        if err := h.Store.UpsertSession(ctx, next); err != nil {
            log.Printf("ingest: upsert: %v", err)
            continue
        }
        if err := h.Store.AppendEvent(ctx, env.Hostname, env.SessionID, next.LastEventAt, peekType(env.Raw), env.Raw); err != nil {
            log.Printf("ingest: append event: %v", err)
        }

        frame := updateFrame{Kind: "update", Session: next}
        if b, err := json.Marshal(frame); err == nil {
            h.Hub.Broadcast(b)
        }
    }
}
```

Remove the now-unused `sync/atomic` import.

- [ ] **Step 2: Update existing ingest test to include hostname**

In `backend/internal/ingest/handler_test.go`, find the envelope construction in `TestIngest_PersistsAndBroadcasts`:

```go
env := events.IngestEnvelope{
    SessionID: "s1", ProjectDir: "-tmp", FileMTime: now, LineIndex: 0,
    Raw: json.RawMessage(`{"type":"custom-title",...`),
}
```

Change to:

```go
env := events.IngestEnvelope{
    Hostname: "host-test", SessionID: "s1", ProjectDir: "-tmp", FileMTime: now, LineIndex: 0,
    Raw: json.RawMessage(`{"type":"custom-title","customTitle":"hi","timestamp":"` + now.Format(time.RFC3339) + `"}`),
}
```

- [ ] **Step 3: Add a multi-connection test**

Append to `backend/internal/ingest/handler_test.go`:

```go
func TestIngest_AcceptsConcurrentPollers(t *testing.T) {
    db, _ := store.Open(filepath.Join(t.TempDir(), "i.db"))
    defer db.Close()
    hub := broadcast.NewHub()
    go hub.Run()
    defer hub.Stop()
    h := &Handler{Store: db, Hub: hub}
    srv := httptest.NewServer(http.HandlerFunc(h.Serve))
    defer srv.Close()

    dial := func() *websocket.Conn {
        c, resp, err := websocket.DefaultDialer.Dial(wsURL(srv.URL), nil)
        if err != nil {
            t.Fatalf("dial failed (status=%v): %v", resp, err)
        }
        return c
    }
    a := dial()
    defer a.Close()
    b := dial()
    defer b.Close()

    now := time.Now().UTC().Truncate(time.Second)
    mkEnv := func(host string) events.IngestEnvelope {
        return events.IngestEnvelope{
            Hostname: host, SessionID: "uuid-shared", ProjectDir: "-tmp", FileMTime: now,
            Raw: json.RawMessage(`{"type":"custom-title","customTitle":"` + host + `","timestamp":"` + now.Format(time.RFC3339) + `"}`),
        }
    }
    if err := a.WriteJSON(mkEnv("mac-A")); err != nil {
        t.Fatal(err)
    }
    if err := b.WriteJSON(mkEnv("mac-B")); err != nil {
        t.Fatal(err)
    }

    // Give the handler a moment to process both.
    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        got, _ := db.ListSessions(context.Background())
        if len(got) == 2 {
            return
        }
        time.Sleep(20 * time.Millisecond)
    }
    got, _ := db.ListSessions(context.Background())
    t.Fatalf("expected 2 sessions from 2 pollers, got %d: %#v", len(got), got)
}
```

Imports to add if missing: `context`, `path/filepath`, `github.com/luxarts/supervaisor/internal/broadcast`, `github.com/luxarts/supervaisor/internal/store`.

- [ ] **Step 4: Run ingest tests**

Run: `cd backend && go test ./internal/ingest/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/ingest/
git commit -m "feat(backend): allow concurrent pollers, key ingest by hostname"
```

---

## Task 5: Update REST API for hostname

**Files:**
- Modify: `backend/internal/api/handler.go`
- Modify: `backend/internal/api/handler_test.go`

- [ ] **Step 1: Change the events route to include hostname**

In `backend/internal/api/handler.go`:

```go
func (h *Handler) Register(r *gin.Engine) {
    r.GET("/healthz", func(c *gin.Context) {
        c.JSON(http.StatusOK, gin.H{"ok": true})
    })
    r.GET("/sessions", h.listSessions)
    r.GET("/sessions/:hostname/:id/events", h.getSessionEvents)
}

func (h *Handler) getSessionEvents(c *gin.Context) {
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

    limit := 500
    if v := c.Query("limit"); v != "" {
        if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 5000 {
            limit = n
        }
    }

    evs, err := h.Store.ListEvents(ctx, hostname, id, limit)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    if evs == nil {
        evs = []store.Event{}
    }
    c.Header("Cache-Control", "no-store")
    c.JSON(http.StatusOK, evs)
}
```

- [ ] **Step 2: Update tests**

In `backend/internal/api/handler_test.go`:

- Add `Hostname: "host-test"` to every `state.Session{...}` literal.
- Change every `GET /sessions/<id>/events` URL in tests to `GET /sessions/host-test/<id>/events`.
- Any `db.UpsertSession` / `db.AppendEvent` calls already inherit the hostname from the session struct / extra arg from Task 3.

- [ ] **Step 3: Run api tests**

Run: `cd backend && go test ./internal/api/...`
Expected: PASS.

- [ ] **Step 4: Full backend test run**

Run: `cd backend && go test ./...`
Expected: PASS across all packages.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/
git commit -m "feat(backend): events route includes hostname segment"
```

---

## Task 6: Poller — hostname resolution package

**Files:**
- Create: `poller/internal/hostname/hostname.go`
- Create: `poller/internal/hostname/hostname_test.go`

- [ ] **Step 1: Write the failing test**

Create `poller/internal/hostname/hostname_test.go`:

```go
package hostname

import "testing"

func TestClean_StripsDotLocal(t *testing.T) {
    if got := Clean("lucas-mbp.local"); got != "lucas-mbp" {
        t.Errorf("Clean = %q, want lucas-mbp", got)
    }
}

func TestClean_LeavesOthersAlone(t *testing.T) {
    if got := Clean("lucas-mbp"); got != "lucas-mbp" {
        t.Errorf("Clean = %q, want lucas-mbp", got)
    }
    if got := Clean("server.example.com"); got != "server.example.com" {
        t.Errorf("Clean = %q, want server.example.com", got)
    }
}

func TestResolve_ReturnsNonEmpty(t *testing.T) {
    got, err := Resolve()
    if err != nil {
        t.Fatalf("Resolve error: %v", err)
    }
    if got == "" {
        t.Fatal("Resolve returned empty hostname")
    }
}
```

- [ ] **Step 2: Run to verify fail**

Run: `cd poller && go test ./internal/hostname/...`
Expected: FAIL (package doesn't exist).

- [ ] **Step 3: Implement**

Create `poller/internal/hostname/hostname.go`:

```go
// Package hostname resolves the local machine's hostname and applies macOS
// cleanup (strip trailing ".local") so cards read e.g. "lucas-mbp" not
// "lucas-mbp.local".
package hostname

import (
    "errors"
    "os"
    "strings"
)

// Resolve returns the cleaned hostname or an error if the OS call fails or
// returns an empty string.
func Resolve() (string, error) {
    h, err := os.Hostname()
    if err != nil {
        return "", err
    }
    h = Clean(h)
    if h == "" {
        return "", errors.New("hostname is empty")
    }
    return h, nil
}

// Clean strips a trailing ".local" suffix (macOS Bonjour adornment).
func Clean(h string) string {
    return strings.TrimSuffix(h, ".local")
}
```

- [ ] **Step 4: Verify tests pass**

Run: `cd poller && go test ./internal/hostname/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add poller/internal/hostname/
git commit -m "feat(poller): hostname resolution with macOS .local strip"
```

---

## Task 7: Poller — flags, URL derivation, Scanner gets hostname

**Files:**
- Modify: `poller/cmd/poller/main.go`
- Create: `poller/cmd/poller/config_test.go`
- Modify: `poller/internal/scanner/scanner.go`

- [ ] **Step 1: Refactor `loadConfig` to support new flags + extract URL builder**

Replace `poller/cmd/poller/main.go` with:

```go
package main

import (
    "errors"
    "flag"
    "fmt"
    "log"
    "os"
    "os/signal"
    "path/filepath"
    "syscall"
    "time"

    "github.com/luxarts/supervaisor-poller/internal/hostname"
    "github.com/luxarts/supervaisor-poller/internal/offsets"
    "github.com/luxarts/supervaisor-poller/internal/scanner"
    "github.com/luxarts/supervaisor-poller/internal/wsclient"
)

type Config struct {
    ProjectsDir  string
    StateFile    string
    BackendHost  string
    BackendPort  int
    BackendURL   string // explicit override; empty => derive from host+port
    Hostname     string // explicit override; empty => resolve from OS
    Interval     time.Duration
}

func loadConfig(args []string) (Config, error) {
    home, _ := os.UserHomeDir()
    c := Config{
        ProjectsDir: filepath.Join(home, ".claude", "projects"),
        StateFile:   filepath.Join(home, ".supervAIsor", "poller-state.json"),
        BackendHost: "localhost",
        BackendPort: 8080,
        Interval:    time.Second,
    }
    fs := flag.NewFlagSet("poller", flag.ContinueOnError)
    fs.StringVar(&c.ProjectsDir, "projects-dir", c.ProjectsDir, "Claude projects dir")
    fs.StringVar(&c.StateFile, "state-file", c.StateFile, "Offset state file")
    fs.StringVar(&c.BackendHost, "backend-host", c.BackendHost, "Backend host")
    fs.IntVar(&c.BackendPort, "backend-port", c.BackendPort, "Backend port")
    fs.StringVar(&c.BackendURL, "backend", "", "Backend WS URL (overrides host+port)")
    fs.StringVar(&c.Hostname, "hostname", "", "Hostname tag (default: OS hostname, .local stripped)")
    fs.DurationVar(&c.Interval, "interval", c.Interval, "Poll interval")
    if err := fs.Parse(args); err != nil {
        return c, err
    }
    return c, nil
}

func resolveBackendURL(c Config) string {
    if c.BackendURL != "" {
        return c.BackendURL
    }
    return fmt.Sprintf("ws://%s:%d/ws/ingest", c.BackendHost, c.BackendPort)
}

func resolveHostname(c Config) (string, error) {
    if c.Hostname != "" {
        return c.Hostname, nil
    }
    h, err := hostname.Resolve()
    if err != nil {
        return "", fmt.Errorf("resolve hostname: %w", err)
    }
    return h, nil
}

func main() {
    cfg, err := loadConfig(os.Args[1:])
    if err != nil {
        log.Fatalf("flag parse: %v", err)
    }
    host, err := resolveHostname(cfg)
    if err != nil {
        log.Fatalf("%v", err)
    }
    if host == "" {
        log.Fatal(errors.New("hostname is empty"))
    }
    url := resolveBackendURL(cfg)
    log.Printf("poller: host=%s backend=%s projects=%s", host, url, cfg.ProjectsDir)

    off, err := offsets.Load(cfg.StateFile)
    if err != nil {
        log.Fatalf("load offsets: %v", err)
    }
    cli := wsclient.New(url)

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
        Hostname:    host,
        ProjectsDir: cfg.ProjectsDir,
        Offsets:     off,
        OffsetsPath: cfg.StateFile,
        Client:      cli,
        Interval:    cfg.Interval,
    }
    s.Run(stop)
}
```

- [ ] **Step 2: Add config test**

Create `poller/cmd/poller/config_test.go`:

```go
package main

import "testing"

func TestResolveBackendURL_DerivesFromHostPort(t *testing.T) {
    got := resolveBackendURL(Config{BackendHost: "10.0.0.5", BackendPort: 9000})
    want := "ws://10.0.0.5:9000/ws/ingest"
    if got != want {
        t.Errorf("got %q want %q", got, want)
    }
}

func TestResolveBackendURL_RespectsOverride(t *testing.T) {
    got := resolveBackendURL(Config{BackendURL: "ws://custom/path", BackendHost: "x", BackendPort: 1})
    if got != "ws://custom/path" {
        t.Errorf("override ignored: got %q", got)
    }
}

func TestResolveHostname_ExplicitWins(t *testing.T) {
    got, err := resolveHostname(Config{Hostname: "explicit-name"})
    if err != nil {
        t.Fatal(err)
    }
    if got != "explicit-name" {
        t.Errorf("got %q", got)
    }
}

func TestLoadConfig_Defaults(t *testing.T) {
    c, err := loadConfig(nil)
    if err != nil {
        t.Fatal(err)
    }
    if c.BackendHost != "localhost" || c.BackendPort != 8080 {
        t.Errorf("bad defaults: %#v", c)
    }
}

func TestLoadConfig_FlagsApplied(t *testing.T) {
    c, err := loadConfig([]string{"-backend-host", "1.2.3.4", "-backend-port", "9999", "-hostname", "mac-A"})
    if err != nil {
        t.Fatal(err)
    }
    if c.BackendHost != "1.2.3.4" || c.BackendPort != 9999 || c.Hostname != "mac-A" {
        t.Errorf("flags not applied: %#v", c)
    }
}
```

- [ ] **Step 3: Add Hostname to Scanner and Envelope**

Edit `poller/internal/scanner/scanner.go`. Modify `Envelope`:

```go
type Envelope struct {
    Hostname   string          `json:"hostname"`
    SessionID  string          `json:"session_id"`
    ProjectDir string          `json:"project_dir"`
    FileMTime  time.Time       `json:"file_mtime"`
    LineIndex  int             `json:"line_index"`
    Raw        json.RawMessage `json:"raw"`
}
```

Modify `Scanner`:

```go
type Scanner struct {
    Hostname    string
    ProjectsDir string
    Offsets     *offsets.Store
    OffsetsPath string
    Client      *wsclient.Client
    Interval    time.Duration
}
```

In `processFile`, change the envelope construction inside the `for i, line := range lines` loop:

```go
env := Envelope{
    Hostname:   s.Hostname,
    SessionID:  sessionID,
    ProjectDir: projectDir,
    FileMTime:  fi.ModTime().UTC(),
    LineIndex:  int(prevOff) + i,
    Raw:        json.RawMessage(line),
}
```

- [ ] **Step 4: Run poller tests**

Run: `cd poller && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add poller/
git commit -m "feat(poller): host/port flags, hostname resolution, stamp envelopes"
```

---

## Task 8: Frontend — type and card display

**Files:**
- Modify: `frontend/src/types.ts`
- Modify: `frontend/src/components/SessionCard.tsx`
- Create: `frontend/src/components/SessionCard.test.tsx`

- [ ] **Step 1: Add `hostname` to the Session type**

Edit `frontend/src/types.ts`:

```ts
export type Status = "working" | "waiting_input" | "idle" | "stale";

export interface Session {
  id: string;
  hostname: string;
  name: string;
  project: string;
  status: Status;
  started_at: string;
  last_prompt_at?: string;
  current_action: string;
  last_event_at: string;
}

export type Frame =
  | { kind: "snapshot"; sessions: Session[] }
  | { kind: "update"; session: Session }
  | { kind: "delete"; session_id: string; hostname: string };
```

(The `delete` frame variant adds `hostname` defensively in case future backend code emits it; it is not produced today but typing it matches Task 9's dedup keying.)

- [ ] **Step 2: Write a failing card test**

Create `frontend/src/components/SessionCard.test.tsx`:

```tsx
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { SessionCard } from "./SessionCard";
import type { Session } from "../types";

const sess: Session = {
  id: "abc",
  hostname: "mac-A",
  name: "feature-x",
  project: "/Users/u/Projects/foo",
  status: "working",
  started_at: new Date().toISOString(),
  current_action: "Edit: x.ts",
  last_event_at: new Date().toISOString(),
};

describe("SessionCard", () => {
  it("renders name@hostname", () => {
    render(<SessionCard session={sess} onOpen={() => {}} />);
    expect(screen.getByText("feature-x@mac-A")).toBeTruthy();
  });
});
```

- [ ] **Step 3: Run test to verify fail**

Run: `cd frontend && npx vitest run src/components/SessionCard.test.tsx`
Expected: FAIL — text "feature-x@mac-A" not found.

- [ ] **Step 4: Update SessionCard to render `name@hostname`**

In `frontend/src/components/SessionCard.tsx`, replace the `<h2>` block:

```tsx
<h2 className="mt-3 font-hud text-xl uppercase tracking-wider text-txt truncate">
  {session.name}@{session.hostname}
</h2>
```

- [ ] **Step 5: Run test to verify pass**

Run: `cd frontend && npx vitest run src/components/SessionCard.test.tsx`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/types.ts frontend/src/components/SessionCard.tsx frontend/src/components/SessionCard.test.tsx
git commit -m "feat(frontend): render session card as name@hostname"
```

---

## Task 9: Frontend — composite-key dedup in socket and list

**Files:**
- Modify: `frontend/src/useSessionsSocket.ts`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Update socket reducer to key by `hostname:id`**

In `frontend/src/useSessionsSocket.ts`, replace the `update` branch:

```ts
} else if (frame.kind === "update") {
  setSessions((prev) => {
    const key = (s: Session) => `${s.hostname}:${s.id}`;
    const target = key(frame.session);
    const idx = prev.findIndex((s) => key(s) === target);
    if (idx < 0) return [frame.session, ...prev];
    const copy = prev.slice();
    copy[idx] = frame.session;
    return copy;
  });
} else if (frame.kind === "delete") {
  setSessions((prev) =>
    prev.filter((s) => !(s.id === frame.session_id && s.hostname === frame.hostname)),
  );
}
```

Add the import for `Session` if not already present (`import type { Session, Frame } from "./types";`).

- [ ] **Step 2: Update App.tsx list keying**

Open `frontend/src/App.tsx`. Find the `.map(...)` over sessions that renders `<SessionCard>` (or wraps it). Update the React `key`:

```tsx
{sessions.map((s) => (
  <SessionCard
    key={`${s.hostname}:${s.id}`}
    session={s}
    onOpen={() => setOpen({ hostname: s.hostname, id: s.id })}
  />
))}
```

Update the `open` state to carry both fields. Find the existing useState declaration (likely `useState<string | null>(null)`) and change to:

```tsx
const [open, setOpen] = useState<{ hostname: string; id: string } | null>(null);
```

Update wherever `open` is read (passing to `ConversationModal`) to pass both fields. Example modal mount:

```tsx
{open && (
  <ConversationModal
    sessionId={open.id}
    hostname={open.hostname}
    sessionName={sessions.find((s) => s.id === open.id && s.hostname === open.hostname)?.name ?? ""}
    project={sessions.find((s) => s.id === open.id && s.hostname === open.hostname)?.project ?? ""}
    backendHttpBase={backendHttpBase}
    onClose={() => setOpen(null)}
  />
)}
```

(If `App.tsx` currently looks up the session differently, preserve the existing pattern — only the lookup key changes.)

- [ ] **Step 3: Verify frontend builds**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/useSessionsSocket.ts frontend/src/App.tsx
git commit -m "feat(frontend): key sessions by hostname:id composite"
```

---

## Task 10: Frontend — ConversationModal uses hostname

**Files:**
- Modify: `frontend/src/components/ConversationModal.tsx`
- Modify: `frontend/src/components/ConversationModal.test.tsx`

- [ ] **Step 1: Update modal Props and fetch URL**

In `frontend/src/components/ConversationModal.tsx`:

```tsx
interface Props {
  sessionId: string;
  hostname: string;
  sessionName: string;
  project: string;
  backendHttpBase: string;
  onClose: () => void;
}

export function ConversationModal({
  sessionId,
  hostname,
  sessionName,
  project,
  backendHttpBase,
  onClose,
}: Props) {
  // ...rest unchanged except the fetch URL:
```

Change the fetch URL inside the `useEffect`:

```tsx
fetch(
  `${backendHttpBase}/sessions/${encodeURIComponent(hostname)}/${encodeURIComponent(sessionId)}/events?limit=500&_=${Date.now()}`,
  { cache: "no-store" },
)
```

And update the effect's dependency array:

```tsx
}, [sessionId, hostname, backendHttpBase]);
```

If the modal header anywhere prints `sessionName`, change it to `{sessionName}@{hostname}` to match the cards.

- [ ] **Step 2: Update the modal test**

In `frontend/src/components/ConversationModal.test.tsx`:

- Add `hostname="mac-A"` to the `<ConversationModal ... />` render.
- Update the fetch mock assertion (if any) to expect the new URL pattern. If the test doesn't currently assert the URL, add:

```ts
expect(fetch).toHaveBeenCalledWith(
  expect.stringContaining("/sessions/mac-A/abc/events"),
  expect.anything(),
);
```

- [ ] **Step 3: Run frontend tests**

Run: `cd frontend && npx vitest run`
Expected: PASS.

- [ ] **Step 4: Type-check**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/ConversationModal.tsx frontend/src/components/ConversationModal.test.tsx
git commit -m "feat(frontend): ConversationModal fetches /sessions/:hostname/:id/events"
```

---

## Task 11: End-to-end smoke test

**Files:** none modified — manual verification.

- [ ] **Step 1: Reset SQLite (old schema is incompatible)**

```bash
make down
docker volume rm supervaisor_data 2>/dev/null || true  # adjust to actual volume name if different
```

If you don't know the volume name, run `docker volume ls` to inspect. The intent is to wipe `/var/lib/supervaisor/data.db` so the new schema is created fresh.

- [ ] **Step 2: Build and start backend + frontend**

```bash
make up
```

Expected: containers come up healthy. `curl localhost:8080/healthz` returns `{"ok":true}`.

- [ ] **Step 3: Run two pollers locally (simulating two machines)**

Open two terminals:

Terminal A:
```bash
cd poller && go run ./cmd/poller -hostname mac-A
```

Terminal B:
```bash
cd poller && go run ./cmd/poller -hostname mac-B
```

Expected: both start without error, both log "poller: host=... backend=...". Neither errors with 409. (If you have only one real `~/.claude/projects` they will both tail the same files — that's fine for smoke; you'll see two cards for each session, one per hostname.)

- [ ] **Step 4: Open the frontend and verify cards**

Open http://localhost:5173. Expected: each session card title shows `name@mac-A` or `name@mac-B`. Click a card — the conversation modal opens with events from that hostname.

- [ ] **Step 5: Stop one poller and confirm the other keeps working**

Ctrl-C terminal A. Terminal B keeps shipping; cards for mac-B keep updating live. No `409 ingest already connected` anywhere.

- [ ] **Step 6: Commit the smoke checklist (optional)**

Nothing to commit unless you noticed an issue.

---

## Self-review notes

**Spec coverage:** every spec section maps to a task — envelope (T1), state (T2), schema (T3), ingest (T4), API (T5), poller flags/hostname (T6–T7), frontend type/card/dedup/modal (T8–T10), smoke (T11).

**Type consistency:** `GetSession(ctx, hostname, id)`, `AppendEvent(ctx, hostname, sessionID, ...)`, `ListEvents(ctx, hostname, sessionID, limit)` — consistent across all tasks. `Session.Hostname` everywhere. Poller `Scanner.Hostname` and `Envelope.Hostname`.

**Known followups not in scope:** the spec lists "no per-machine filtering" — confirmed absent here.
