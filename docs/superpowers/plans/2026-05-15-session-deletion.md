# Session Deletion & Poller Liveness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable end-to-end session deletion (UI button → backend → poller deletes the JSONL → store purged → frontend removes the card), gated by a per-host poller liveness LED.

**Architecture:** Backend `internal/ingest/registry.go` tracks active poller WS connections per hostname and exposes a writer channel for sending commands. The ingest WS becomes bidirectional via paired reader/writer goroutines per connection. A `DeleteCoordinator` keyed by `request_id` correlates outbound delete commands with `delete_ack` envelopes. The poller grows a read loop that dispatches `delete` commands to a `Deleter` that removes the JSONL and prunes its offset. Liveness changes broadcast a new `pollers` frame on `/ws/clients`. The DETAILS tab gets a DANGER section with a two-click confirm flow, gated by a per-host `pollersOnline[hostname]` flag. Wire format additions are backward-compatible (envelopes without a `type` field are still treated as events).

**Tech Stack:** Go 1.x (gorilla/websocket, modernc.org/sqlite), Gin, React 19 + Vite + TypeScript + Tailwind, Vitest.

**Spec:** `docs/superpowers/specs/2026-05-14-session-deletion-design.md`

---

## File Structure

**Modified:**
- `backend/internal/state/session.go` — add `ProjectDirEncoded string` field
- `backend/internal/state/derive.go` — set `ProjectDirEncoded` in `Apply` from `env.ProjectDir`
- `backend/internal/state/derive_test.go` — assert encoded form persists across `Apply`
- `backend/internal/state/stats.go` — include `project_dir_encoded` in `Stats` JSON
- `backend/internal/state/stats_test.go` — assert the new field
- `backend/internal/store/sqlite.go` — `DeleteSession`, persist + load `project_dir_encoded` (additive ALTER), use it in upsert/list/get
- `backend/internal/store/sqlite_test.go` — `DeleteSession`, encoded round-trip
- `backend/internal/ingest/handler.go` — split into reader+writer goroutines; register/unregister with registry; dispatch `delete_ack` to coordinator
- `backend/internal/ingest/handler_test.go`
- `backend/internal/api/sessions.go` — `DELETE /sessions/:hostname/:id`
- `backend/internal/api/sessions_test.go`
- `backend/internal/broadcast/handler.go` — accept `PollersProvider` and send a `pollers` snapshot frame at client connect; existing `frame` schema gains optional `Type`
- `backend/internal/broadcast/handler_test.go`
- `backend/cmd/server/main.go` — instantiate registry + coordinator, pass them in, subscribe to registry transitions and broadcast `pollers` frames
- `poller/internal/wsclient/client.go` — add `Run(ctx)` reader loop and `OnCommand` dispatch hook; preserve `Send` semantics
- `poller/internal/wsclient/client_test.go`
- `poller/internal/offsets/offsets.go` — `Forget(file string)`; `Save` after Forget
- `poller/internal/offsets/offsets_test.go`
- `poller/cmd/poller/main.go` — wire deleter + start `client.Run`
- `frontend/src/useSessionsSocket.ts` — parse `pollers` and `session_removed` frames; expose `pollersOnline: Record<string, boolean>`
- `frontend/src/types.ts` — `Frame` union extended with `pollers` and `session_removed`; `Session` gains optional `project_dir_encoded?: string`
- `frontend/src/components/SessionCard.tsx` — `pollerOnline?: boolean` prop, LED dot before hostname
- `frontend/src/components/SessionCard.test.tsx`
- `frontend/src/components/SessionDetails.tsx` — DANGER ZONE section with delete state machine; LED next to HOST row; props for `pollerOnline` + `onDeleted`
- `frontend/src/components/SessionDetails.test.tsx`
- `frontend/src/components/ConversationModal.tsx` — pass `pollerOnline` and `onDeleted` (auto-close) into `SessionDetails`
- `frontend/src/App.tsx` — pass `pollerOnline` to `SessionCard`/`ConversationModal`
- `CLAUDE.md` — soften "read-only monitoring" caveat with a note about user-initiated delete

**New:**
- `backend/internal/ingest/registry.go` — `Registry` with `Add`/`Remove`/`Online`/`IsOnline`/`Send`/`Subscribe`
- `backend/internal/ingest/registry_test.go`
- `backend/internal/ingest/coordinator.go` — `DeleteCoordinator` with `Begin(reqID)`/`Resolve(reqID, ok, err)`/timeout
- `backend/internal/ingest/coordinator_test.go`
- `poller/internal/deleter/deleter.go` — `Deleter.Delete(ctx, sessionID, projectDir)` with path-escape guard
- `poller/internal/deleter/deleter_test.go`
- `frontend/src/lib/deleteSession.ts` — `deleteSession(http, hostname, id)` HTTP wrapper

---

## Task 1: Backend — `Store.DeleteSession`

**Files:**
- Modify: `backend/internal/store/sqlite.go`
- Modify: `backend/internal/store/sqlite_test.go`

- [ ] **Step 1.1: Write failing test**

Append to `backend/internal/store/sqlite_test.go`:

```go
func TestDeleteSession_RemovesSessionAndEvents(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "del.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	in := &state.Session{
		ID: "abc", Hostname: "h", Name: "n", Project: "/p",
		Status:      state.StatusDone,
		StartedAt:   t0,
		LastEventAt: t0,
	}
	if err := st.UpsertSession(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendEvent(ctx, "h", "abc", t0, "user", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendEvent(ctx, "h", "abc", t0, "assistant", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteSession(ctx, "h", "abc"); err != nil {
		t.Fatal(err)
	}

	got, _ := st.GetSession(ctx, "h", "abc")
	if got != nil {
		t.Errorf("session still present after delete: %+v", got)
	}
	evs, _ := st.ListEvents(ctx, "h", "abc", -1)
	if len(evs) != 0 {
		t.Errorf("events not purged: %d remain", len(evs))
	}
}

func TestDeleteSession_NoOpOnMissing(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "del2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.DeleteSession(context.Background(), "h", "missing"); err != nil {
		t.Errorf("DeleteSession on missing should be no-op, got %v", err)
	}
}
```

- [ ] **Step 1.2: Run to verify fail**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./internal/store/ -run TestDeleteSession -v
```
Expected: FAIL — method undefined.

- [ ] **Step 1.3: Implement `DeleteSession`**

Append to `backend/internal/store/sqlite.go`:

```go
// DeleteSession removes a session row and all its events in a single
// transaction. No-op when the session does not exist.
func (s *SQLite) DeleteSession(ctx context.Context, hostname, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE hostname = ? AND session_id = ?`, hostname, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE hostname = ? AND id = ?`, hostname, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
```

- [ ] **Step 1.4: Run to verify pass**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./internal/store/ -v
```
Expected: PASS.

- [ ] **Step 1.5: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add backend/internal/store/sqlite.go backend/internal/store/sqlite_test.go
git commit -m "feat(store): DeleteSession purges session row + all events transactionally"
```

---

## Task 2: Backend — `ingest.Registry`

**Files:**
- Create: `backend/internal/ingest/registry.go`
- Create: `backend/internal/ingest/registry_test.go`

- [ ] **Step 2.1: Write failing tests**

Create `backend/internal/ingest/registry_test.go`:

```go
package ingest

import (
	"sync"
	"testing"
	"time"
)

func TestRegistry_AddRemoveOnline(t *testing.T) {
	r := NewRegistry()
	if r.IsOnline("h1") {
		t.Fatal("h1 should be offline before Add")
	}
	chA := make(chan []byte, 1)
	r.Add("h1", chA)
	if !r.IsOnline("h1") {
		t.Fatal("h1 should be online after Add")
	}
	if got := r.Online()["h1"]; !got {
		t.Fatalf("Online()['h1'] = %v, want true", got)
	}
	r.Remove("h1", chA)
	if r.IsOnline("h1") {
		t.Fatal("h1 should be offline after Remove")
	}
}

func TestRegistry_MultipleConnsForSameHost(t *testing.T) {
	r := NewRegistry()
	a := make(chan []byte, 1)
	b := make(chan []byte, 1)
	r.Add("h", a)
	r.Add("h", b)
	r.Remove("h", a)
	if !r.IsOnline("h") {
		t.Fatal("host should still be online with one remaining conn")
	}
	r.Remove("h", b)
	if r.IsOnline("h") {
		t.Fatal("host should be offline after all conns removed")
	}
}

func TestRegistry_SendDeliversAndErrorsWhenOffline(t *testing.T) {
	r := NewRegistry()
	if err := r.Send("missing", []byte("x")); err == nil {
		t.Fatal("Send to offline host should return error")
	}
	ch := make(chan []byte, 1)
	r.Add("h", ch)
	if err := r.Send("h", []byte("hello")); err != nil {
		t.Fatalf("Send to online host: %v", err)
	}
	select {
	case msg := <-ch:
		if string(msg) != "hello" {
			t.Errorf("got %q, want hello", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestRegistry_SubscribeReceivesTransitions(t *testing.T) {
	r := NewRegistry()
	sub, unsub := r.Subscribe()
	defer unsub()

	a := make(chan []byte, 1)

	var (
		mu       sync.Mutex
		snapshots []map[string]bool
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for snap := range sub {
			mu.Lock()
			snapshots = append(snapshots, snap)
			if len(snapshots) >= 2 {
				mu.Unlock()
				return
			}
			mu.Unlock()
		}
	}()

	r.Add("h1", a)
	r.Remove("h1", a)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("did not receive both transitions")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(snapshots) < 2 {
		t.Fatalf("got %d snapshots", len(snapshots))
	}
	if !snapshots[0]["h1"] {
		t.Errorf("first snapshot should show h1 online: %+v", snapshots[0])
	}
	if snapshots[1]["h1"] {
		t.Errorf("second snapshot should show h1 offline: %+v", snapshots[1])
	}
}
```

- [ ] **Step 2.2: Run to verify fail**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./internal/ingest/ -run TestRegistry -v
```
Expected: FAIL — types undefined.

- [ ] **Step 2.3: Implement Registry**

Create `backend/internal/ingest/registry.go`:

```go
package ingest

import (
	"errors"
	"sync"
)

// Registry tracks active ingest connections per hostname. Each connection
// is represented by its outbound writer channel; the ingest handler is
// responsible for owning the channel lifecycle.
type Registry struct {
	mu    sync.RWMutex
	conns map[string]map[chan []byte]struct{}
	subs  map[chan map[string]bool]struct{}
}

// ErrNoPoller is returned by Send when no connection is registered for the
// requested hostname.
var ErrNoPoller = errors.New("no poller registered for hostname")

func NewRegistry() *Registry {
	return &Registry{
		conns: map[string]map[chan []byte]struct{}{},
		subs:  map[chan map[string]bool]struct{}{},
	}
}

// Add registers a writer channel for the given hostname.
func (r *Registry) Add(hostname string, ch chan []byte) {
	r.mu.Lock()
	if r.conns[hostname] == nil {
		r.conns[hostname] = map[chan []byte]struct{}{}
	}
	r.conns[hostname][ch] = struct{}{}
	snap := r.snapshotLocked()
	r.mu.Unlock()
	r.notify(snap)
}

// Remove deregisters a writer channel; if it was the last one for the
// hostname the host is reported as offline by IsOnline.
func (r *Registry) Remove(hostname string, ch chan []byte) {
	r.mu.Lock()
	if set, ok := r.conns[hostname]; ok {
		delete(set, ch)
		if len(set) == 0 {
			delete(r.conns, hostname)
		}
	}
	snap := r.snapshotLocked()
	r.mu.Unlock()
	r.notify(snap)
}

func (r *Registry) IsOnline(hostname string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.conns[hostname]) > 0
}

// Online returns a snapshot of the current online state.
func (r *Registry) Online() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshotLocked()
}

// Send delivers payload to one of the registered connections for hostname.
// Returns ErrNoPoller when no connection exists. The pick is arbitrary
// (Go map iteration order); callers should not assume affinity.
func (r *Registry) Send(hostname string, payload []byte) error {
	r.mu.RLock()
	set := r.conns[hostname]
	if len(set) == 0 {
		r.mu.RUnlock()
		return ErrNoPoller
	}
	var ch chan []byte
	for c := range set {
		ch = c
		break
	}
	r.mu.RUnlock()
	ch <- payload
	return nil
}

// Subscribe returns a channel that emits a snapshot of the online map on
// every Add/Remove transition. The returned func unsubscribes.
func (r *Registry) Subscribe() (<-chan map[string]bool, func()) {
	ch := make(chan map[string]bool, 8)
	r.mu.Lock()
	r.subs[ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		if _, ok := r.subs[ch]; ok {
			delete(r.subs, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
}

func (r *Registry) snapshotLocked() map[string]bool {
	out := make(map[string]bool, len(r.conns))
	for h := range r.conns {
		out[h] = true
	}
	return out
}

func (r *Registry) notify(snap map[string]bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for ch := range r.subs {
		select {
		case ch <- snap:
		default: // drop if subscriber is slow
		}
	}
}
```

- [ ] **Step 2.4: Run to verify pass**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./internal/ingest/ -run TestRegistry -v
```
Expected: PASS.

- [ ] **Step 2.5: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add backend/internal/ingest/registry.go backend/internal/ingest/registry_test.go
git commit -m "feat(ingest): Registry tracks per-host poller writer channels with subscribe"
```

---

## Task 3: Backend — `DeleteCoordinator`

**Files:**
- Create: `backend/internal/ingest/coordinator.go`
- Create: `backend/internal/ingest/coordinator_test.go`

- [ ] **Step 3.1: Write failing tests**

Create `backend/internal/ingest/coordinator_test.go`:

```go
package ingest

import (
	"errors"
	"testing"
	"time"
)

func TestCoordinator_BeginAndResolveOK(t *testing.T) {
	c := NewDeleteCoordinator(time.Second)
	defer c.Close()

	id := "req-1"
	resp, err := c.Begin(id)
	if err != nil {
		t.Fatal(err)
	}
	go c.Resolve(id, true, "")
	r := <-resp
	if r.Err != nil {
		t.Errorf("Err = %v, want nil", r.Err)
	}
}

func TestCoordinator_ResolveError(t *testing.T) {
	c := NewDeleteCoordinator(time.Second)
	defer c.Close()

	id := "req-2"
	resp, _ := c.Begin(id)
	go c.Resolve(id, false, "boom")
	r := <-resp
	if r.Err == nil || r.Err.Error() != "boom" {
		t.Errorf("Err = %v, want 'boom'", r.Err)
	}
}

func TestCoordinator_Timeout(t *testing.T) {
	c := NewDeleteCoordinator(50 * time.Millisecond)
	defer c.Close()

	id := "req-3"
	resp, _ := c.Begin(id)
	r := <-resp
	if !errors.Is(r.Err, ErrTimeout) {
		t.Errorf("Err = %v, want ErrTimeout", r.Err)
	}
}

func TestCoordinator_DuplicateBeginRejected(t *testing.T) {
	c := NewDeleteCoordinator(time.Second)
	defer c.Close()
	if _, err := c.Begin("dup"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Begin("dup"); err == nil {
		t.Fatal("duplicate Begin should return error")
	}
}

func TestCoordinator_ResolveUnknownIsNoop(t *testing.T) {
	c := NewDeleteCoordinator(time.Second)
	defer c.Close()
	c.Resolve("nope", true, "") // must not panic
}
```

- [ ] **Step 3.2: Run to verify fail**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./internal/ingest/ -run TestCoordinator -v
```
Expected: FAIL — undefined.

- [ ] **Step 3.3: Implement coordinator**

Create `backend/internal/ingest/coordinator.go`:

```go
package ingest

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrTimeout is the error sent on a pending request when no ack arrives
// within the coordinator's configured timeout.
var ErrTimeout = errors.New("poller did not ack within timeout")

// DeleteResult is the outcome of a delete request awaited by an HTTP handler.
type DeleteResult struct {
	Err error
}

type pending struct {
	resp  chan DeleteResult
	timer *time.Timer
}

// DeleteCoordinator correlates outbound delete commands with delete_ack
// envelopes by request ID. Use Begin to obtain the response channel before
// sending; Resolve when an ack arrives. Pending requests time out after
// the configured duration.
type DeleteCoordinator struct {
	timeout time.Duration

	mu       sync.Mutex
	pending  map[string]*pending
	closed   bool
}

func NewDeleteCoordinator(timeout time.Duration) *DeleteCoordinator {
	return &DeleteCoordinator{
		timeout: timeout,
		pending: map[string]*pending{},
	}
}

// Begin registers a pending request. The returned channel receives exactly
// one DeleteResult, either from Resolve or from the timeout.
func (c *DeleteCoordinator) Begin(reqID string) (<-chan DeleteResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("coordinator closed")
	}
	if _, exists := c.pending[reqID]; exists {
		return nil, fmt.Errorf("request id %q already pending", reqID)
	}
	resp := make(chan DeleteResult, 1)
	p := &pending{resp: resp}
	p.timer = time.AfterFunc(c.timeout, func() {
		c.fire(reqID, DeleteResult{Err: ErrTimeout})
	})
	c.pending[reqID] = p
	return resp, nil
}

// Resolve completes a pending request with success (ok=true) or an error
// (ok=false, msg becomes the error string). Unknown reqIDs are ignored.
func (c *DeleteCoordinator) Resolve(reqID string, ok bool, msg string) {
	if ok {
		c.fire(reqID, DeleteResult{Err: nil})
		return
	}
	if msg == "" {
		msg = "unknown error"
	}
	c.fire(reqID, DeleteResult{Err: errors.New(msg)})
}

func (c *DeleteCoordinator) fire(reqID string, r DeleteResult) {
	c.mu.Lock()
	p, ok := c.pending[reqID]
	if !ok {
		c.mu.Unlock()
		return
	}
	delete(c.pending, reqID)
	c.mu.Unlock()
	p.timer.Stop()
	p.resp <- r
}

// Close stops accepting new requests. Outstanding requests still resolve
// via Resolve or timer.
func (c *DeleteCoordinator) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
}
```

- [ ] **Step 3.4: Run to verify pass**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./internal/ingest/ -run TestCoordinator -v
```
Expected: PASS for all 5 cases.

- [ ] **Step 3.5: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add backend/internal/ingest/coordinator.go backend/internal/ingest/coordinator_test.go
git commit -m "feat(ingest): DeleteCoordinator correlates delete commands with acks; 10s timeout"
```

---

## Task 4: Backend — Persist `ProjectDirEncoded`

**Files:**
- Modify: `backend/internal/state/session.go`
- Modify: `backend/internal/state/derive.go`
- Modify: `backend/internal/state/derive_test.go`
- Modify: `backend/internal/store/sqlite.go`
- Modify: `backend/internal/store/sqlite_test.go`

- [ ] **Step 4.1: Add field to `Session`**

Edit `backend/internal/state/session.go`. Add the field after `LastErrorAt`:

```go
	LastErrorAt       time.Time `json:"last_error_at,omitempty"`
	ProjectDirEncoded string    `json:"project_dir_encoded,omitempty"`
```

- [ ] **Step 4.2: Set in `Apply`**

Edit `backend/internal/state/derive.go`. After the existing `if p := DecodeProjectDir(env.ProjectDir); p != "" { next.Project = p }` block, add:

```go
	if env.ProjectDir != "" {
		next.ProjectDirEncoded = env.ProjectDir
	}
```

- [ ] **Step 4.3: Add test for Apply propagation**

Append to `backend/internal/state/derive_test.go`:

```go
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
```

- [ ] **Step 4.4: Run state tests**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./internal/state/ -v
```
Expected: PASS.

- [ ] **Step 4.5: Persist column in SQLite**

Edit `backend/internal/store/sqlite.go`. In the `schema` constant, add `project_dir_encoded TEXT,` after the `last_error_at` line:

```go
  last_event_at   TIMESTAMP NOT NULL,
  last_error_at   TIMESTAMP,
  project_dir_encoded TEXT,
  PRIMARY KEY (hostname, id)
);
```

In the `migrations` slice, append the additive `ALTER`:

```go
var migrations = []string{
	`ALTER TABLE sessions ADD COLUMN last_error_at TIMESTAMP`,
	`ALTER TABLE sessions ADD COLUMN project_dir_encoded TEXT`,
}
```

In `UpsertSession`, extend the column list, the placeholders, and the conflict-update clause; add `sess.ProjectDirEncoded` to the args. Replace the function with:

```go
func (s *SQLite) UpsertSession(ctx context.Context, sess *state.Session) error {
	var lastPrompt any
	if sess.LastPromptAt != nil {
		lastPrompt = *sess.LastPromptAt
	}
	var lastError any
	if !sess.LastErrorAt.IsZero() {
		lastError = sess.LastErrorAt
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sessions (hostname, id, name, project, status, started_at, last_prompt_at, current_action, last_event_at, last_error_at, project_dir_encoded)
VALUES (?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(hostname, id) DO UPDATE SET
  name=excluded.name,
  project=excluded.project,
  status=excluded.status,
  started_at=excluded.started_at,
  last_prompt_at=excluded.last_prompt_at,
  current_action=excluded.current_action,
  last_event_at=excluded.last_event_at,
  last_error_at=excluded.last_error_at,
  project_dir_encoded=excluded.project_dir_encoded
`,
		sess.Hostname, sess.ID, sess.Name, sess.Project, string(sess.Status),
		sess.StartedAt, lastPrompt, sess.CurrentAction, sess.LastEventAt, lastError,
		sess.ProjectDirEncoded,
	)
	return err
}
```

In `ListSessions` and `GetSession`, extend the SELECT list with `project_dir_encoded`, scan it into `sql.NullString`, and assign when valid. For `ListSessions`:

```go
func (s *SQLite) ListSessions(ctx context.Context) ([]*state.Session, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT hostname, id, name, project, status, started_at, last_prompt_at, current_action, last_event_at, last_error_at, project_dir_encoded
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
			le     sql.NullTime
			pde    sql.NullString
		)
		if err := rows.Scan(
			&sess.Hostname, &sess.ID, &sess.Name, &sess.Project, &status,
			&sess.StartedAt, &lp, &sess.CurrentAction, &sess.LastEventAt, &le, &pde,
		); err != nil {
			return nil, err
		}
		sess.Status = state.Status(status)
		if lp.Valid {
			t := lp.Time
			sess.LastPromptAt = &t
		}
		if le.Valid {
			sess.LastErrorAt = le.Time
		}
		if pde.Valid {
			sess.ProjectDirEncoded = pde.String
		}
		out = append(out, &sess)
	}
	return out, rows.Err()
}
```

For `GetSession`, mirror the same change:

```go
func (s *SQLite) GetSession(ctx context.Context, hostname, id string) (*state.Session, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT hostname, id, name, project, status, started_at, last_prompt_at, current_action, last_event_at, last_error_at, project_dir_encoded
FROM sessions WHERE hostname = ? AND id = ?`, hostname, id)
	var (
		sess   state.Session
		status string
		lp     sql.NullTime
		le     sql.NullTime
		pde    sql.NullString
	)
	err := row.Scan(&sess.Hostname, &sess.ID, &sess.Name, &sess.Project, &status,
		&sess.StartedAt, &lp, &sess.CurrentAction, &sess.LastEventAt, &le, &pde)
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
	if le.Valid {
		sess.LastErrorAt = le.Time
	}
	if pde.Valid {
		sess.ProjectDirEncoded = pde.String
	}
	return &sess, nil
}
```

- [ ] **Step 4.6: Add round-trip test**

Append to `backend/internal/store/sqlite_test.go`:

```go
func TestUpsertSession_RoundTripsProjectDirEncoded(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "pde.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	in := &state.Session{
		ID: "abc", Hostname: "h", Name: "n", Project: "/p",
		Status:            state.StatusDone,
		StartedAt:         t0,
		LastEventAt:       t0,
		ProjectDirEncoded: "-Users-x-Projects-foo",
	}
	if err := st.UpsertSession(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSession(ctx, "h", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectDirEncoded != "-Users-x-Projects-foo" {
		t.Errorf("ProjectDirEncoded = %q", got.ProjectDirEncoded)
	}
}
```

- [ ] **Step 4.7: Run all backend tests**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./...
```
Expected: PASS.

- [ ] **Step 4.8: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add backend/internal/state/session.go backend/internal/state/derive.go backend/internal/state/derive_test.go backend/internal/store/sqlite.go backend/internal/store/sqlite_test.go
git commit -m "feat(state,store): persist ProjectDirEncoded for session deletion path"
```

---

## Task 5: Backend — Expose `project_dir_encoded` in `/stats`

**Files:**
- Modify: `backend/internal/state/stats.go`
- Modify: `backend/internal/state/stats_test.go`

- [ ] **Step 5.1: Write failing test**

Append to `backend/internal/state/stats_test.go`:

```go
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
```

- [ ] **Step 5.2: Run to verify fail**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./internal/state/ -run TestComputeStats_IncludesProjectDirEncoded -v
```
Expected: FAIL — field not present.

- [ ] **Step 5.3: Add the field**

Edit `backend/internal/state/stats.go`. In the `Stats` struct, add after `Project`:

```go
	Project           string         `json:"project"`
	ProjectDirEncoded string         `json:"project_dir_encoded,omitempty"`
```

In `ComputeStats`, copy from the session:

```go
	out := Stats{
		Hostname:          sess.Hostname,
		ID:                sess.ID,
		Name:              sess.Name,
		Project:           sess.Project,
		ProjectDirEncoded: sess.ProjectDirEncoded,
		StartedAt:         sess.StartedAt,
		LastEventAt:       sess.LastEventAt,
		ToolBreakdown:     map[string]int{},
	}
```

- [ ] **Step 5.4: Run to verify pass**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./internal/state/ -v
```
Expected: PASS.

- [ ] **Step 5.5: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add backend/internal/state/stats.go backend/internal/state/stats_test.go
git commit -m "feat(stats): include project_dir_encoded in /stats response"
```

---

## Task 6: Backend — Bidirectional Ingest Handler

**Files:**
- Modify: `backend/internal/ingest/handler.go`
- Modify: `backend/internal/ingest/handler_test.go`

- [ ] **Step 6.1: Replace handler with bidirectional version**

Edit `backend/internal/ingest/handler.go` to add Registry + Coordinator dependencies and split into reader/writer goroutines. Replace the contents with:

```go
package ingest

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/luxarts/supervaisor/internal/broadcast"
	"github.com/luxarts/supervaisor/internal/events"
	"github.com/luxarts/supervaisor/internal/state"
	"github.com/luxarts/supervaisor/internal/store"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler is the HTTP handler for the /ws/ingest WebSocket endpoint.
// It processes ingest envelopes from host pollers, applying state derivation,
// persisting, and broadcasting updates. Multiple concurrent pollers are
// supported; sessions are keyed by (hostname, session_id).
type Handler struct {
	Store       *store.SQLite
	Hub         *broadcast.Hub
	Registry    *Registry
	Coordinator *DeleteCoordinator
}

type updateFrame struct {
	Kind    string         `json:"kind"`
	Session *state.Session `json:"session"`
}

// envelopeWithType is used to peek at the discriminator before full decode.
type envelopeWithType struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	OK        bool   `json:"ok,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Serve handles a single /ws/ingest WebSocket connection. Splits into a
// reader goroutine (existing event-processing path) and a writer goroutine
// (drains the registry's writer channel for this connection).
func (h *Handler) Serve(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ingest upgrade: %v", err)
		return
	}
	defer conn.Close()

	writeCh := make(chan []byte, 16)
	done := make(chan struct{})

	// Writer goroutine.
	go func() {
		for {
			select {
			case <-done:
				return
			case msg, ok := <-writeCh:
				if !ok {
					return
				}
				_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					return
				}
			}
		}
	}()
	defer close(done)

	var registeredHost string
	defer func() {
		if registeredHost != "" && h.Registry != nil {
			h.Registry.Remove(registeredHost, writeCh)
		}
	}()

	ctx := r.Context()
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		// Peek at discriminator.
		var head envelopeWithType
		_ = json.Unmarshal(data, &head)

		if head.Type == "delete_ack" {
			if h.Coordinator != nil {
				h.Coordinator.Resolve(head.RequestID, head.OK, head.Error)
			}
			continue
		}

		// Default path: treat as event envelope.
		var env events.IngestEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			log.Printf("ingest: malformed envelope: %v", err)
			continue
		}

		if env.Hostname == "" {
			log.Printf("ingest: missing hostname for session_id=%s; dropping", env.SessionID)
			continue
		}

		if registeredHost == "" && h.Registry != nil {
			h.Registry.Add(env.Hostname, writeCh)
			registeredHost = env.Hostname
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

// peekType extracts the "type" field from a raw JSONL line without full decode.
func peekType(raw json.RawMessage) string {
	var head struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &head)
	return head.Type
}
```

- [ ] **Step 6.2: Update existing handler test to inject Registry/Coordinator (nil-safe)**

The existing `handler_test.go::TestIngest_PersistsAndBroadcasts` constructs `&Handler{Store: db, Hub: hub}`. The new fields are optional; the handler must continue to work with them nil. Verify by running:

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./internal/ingest/ -run TestIngest -v
```
Expected: PASS (no changes needed because the new fields default to nil and the handler skips registry/coordinator interactions when they are).

- [ ] **Step 6.3: Add ack-routing test**

Append to `backend/internal/ingest/handler_test.go`:

```go
func TestIngest_DeleteAckRoutesToCoordinator(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "ack.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hub := broadcast.NewHub()
	go hub.Run()
	defer hub.Stop()

	reg := NewRegistry()
	coord := NewDeleteCoordinator(2 * time.Second)
	defer coord.Close()

	h := &Handler{Store: db, Hub: hub, Registry: reg, Coordinator: coord}
	srv := httptest.NewServer(http.HandlerFunc(h.Serve))
	defer srv.Close()

	c, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// Begin a pending delete request.
	resp, _ := coord.Begin("req-X")

	ack := []byte(`{"type":"delete_ack","request_id":"req-X","session_id":"abc","ok":true}`)
	if err := c.WriteMessage(websocket.TextMessage, ack); err != nil {
		t.Fatal(err)
	}

	select {
	case r := <-resp:
		if r.Err != nil {
			t.Errorf("Err = %v, want nil", r.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ack never resolved coordinator")
	}
}
```

- [ ] **Step 6.4: Run handler tests**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./internal/ingest/ -v
```
Expected: PASS for all ingest tests.

- [ ] **Step 6.5: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add backend/internal/ingest/handler.go backend/internal/ingest/handler_test.go
git commit -m "feat(ingest): bidirectional handler — register conn, dispatch delete_ack"
```

---

## Task 7: Poller — `offsets.Forget` + `Deleter`

**Files:**
- Modify: `poller/internal/offsets/offsets.go`
- Modify: `poller/internal/offsets/offsets_test.go`
- Create: `poller/internal/deleter/deleter.go`
- Create: `poller/internal/deleter/deleter_test.go`

- [ ] **Step 7.1: Write failing offsets test**

Append to `poller/internal/offsets/offsets_test.go`:

```go
func TestForget_RemovesEntry(t *testing.T) {
	s := &Store{m: map[string]fileOffset{
		"/a": {Inode: 1, Offset: 10},
		"/b": {Inode: 2, Offset: 20},
	}}
	s.Forget("/a")
	if _, _, ok := s.Get("/a"); ok {
		t.Error("/a should be forgotten")
	}
	if _, _, ok := s.Get("/b"); !ok {
		t.Error("/b should still be present")
	}
}
```

- [ ] **Step 7.2: Run to verify fail**

```
cd /Users/lucasbacelo/Projects/supervAIsor/poller && go test ./internal/offsets/ -run TestForget -v
```
Expected: FAIL — method undefined.

- [ ] **Step 7.3: Implement `Forget`**

Append to `poller/internal/offsets/offsets.go`:

```go
// Forget removes the offset entry for the given file path. Caller is
// responsible for persisting via Save.
func (s *Store) Forget(file string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, file)
}
```

- [ ] **Step 7.4: Run to verify pass**

```
cd /Users/lucasbacelo/Projects/supervAIsor/poller && go test ./internal/offsets/ -v
```
Expected: PASS.

- [ ] **Step 7.5: Write failing Deleter tests**

Create `poller/internal/deleter/deleter_test.go`:

```go
package deleter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/luxarts/supervaisor-poller/internal/offsets"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDeleter_RemovesFileAndForgetsOffset(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	off := &offsets.Store{}

	projectDir := "-Users-x-Projects-foo"
	sessionID := "abc"
	full := filepath.Join(root, projectDir, sessionID+".jsonl")
	writeFile(t, full, "{}\n")
	off.Set(full, 1, 1)

	d := New(root, statePath, off)
	if err := d.Delete(context.Background(), sessionID, projectDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Fatalf("file still exists: err=%v", err)
	}
	if _, _, ok := off.Get(full); ok {
		t.Error("offset entry should have been forgotten")
	}
}

func TestDeleter_NoOpOnMissingFile(t *testing.T) {
	root := t.TempDir()
	d := New(root, filepath.Join(root, "state.json"), &offsets.Store{})
	err := d.Delete(context.Background(), "nope", "-x")
	if err != nil {
		t.Errorf("missing file should be no-op, got %v", err)
	}
}

func TestDeleter_RefusesPathEscape(t *testing.T) {
	root := t.TempDir()
	d := New(root, filepath.Join(root, "state.json"), &offsets.Store{})
	if err := d.Delete(context.Background(), "abc", "../../etc"); err == nil {
		t.Error("path escape via project_dir should be refused")
	}
	if err := d.Delete(context.Background(), "../../etc/passwd", "x"); err == nil {
		t.Error("path escape via session_id should be refused")
	}
}
```

- [ ] **Step 7.6: Run to verify fail**

```
cd /Users/lucasbacelo/Projects/supervAIsor/poller && go test ./internal/deleter/ -v
```
Expected: FAIL — package not found.

- [ ] **Step 7.7: Implement Deleter**

Create `poller/internal/deleter/deleter.go`:

```go
package deleter

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/luxarts/supervaisor-poller/internal/offsets"
)

// Deleter removes a session's JSONL file from the host filesystem and
// prunes the offset state. Constructed once per process.
type Deleter struct {
	projectsDir string
	statePath   string
	offsets     *offsets.Store
}

func New(projectsDir, statePath string, off *offsets.Store) *Deleter {
	return &Deleter{projectsDir: projectsDir, statePath: statePath, offsets: off}
}

// Delete removes ~/.claude/projects/<projectDir>/<sessionID>.jsonl, then
// drops its entry from the offset store and persists. Refuses any path
// that resolves outside projectsDir.
func (d *Deleter) Delete(_ context.Context, sessionID, projectDir string) error {
	if sessionID == "" || projectDir == "" {
		return errors.New("empty sessionID or projectDir")
	}
	candidate := filepath.Join(d.projectsDir, projectDir, sessionID+".jsonl")
	resolved := filepath.Clean(candidate)
	root := filepath.Clean(d.projectsDir) + string(filepath.Separator)
	if !strings.HasPrefix(resolved, root) {
		return errors.New("path outside projects dir")
	}
	if err := os.Remove(resolved); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	d.offsets.Forget(resolved)
	if d.statePath != "" {
		_ = d.offsets.Save(d.statePath)
	}
	return nil
}
```

- [ ] **Step 7.8: Run to verify pass**

```
cd /Users/lucasbacelo/Projects/supervAIsor/poller && go test ./...
```
Expected: PASS.

- [ ] **Step 7.9: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add poller/internal/offsets/offsets.go poller/internal/offsets/offsets_test.go poller/internal/deleter/deleter.go poller/internal/deleter/deleter_test.go
git commit -m "feat(poller): Deleter removes JSONL + prunes offset; offsets.Forget helper"
```

---

## Task 8: Poller — `wsclient.Run` Read Loop + Command Dispatch

**Files:**
- Modify: `poller/internal/wsclient/client.go`
- Modify: `poller/internal/wsclient/client_test.go`
- Modify: `poller/cmd/poller/main.go`

- [ ] **Step 8.1: Write failing test**

Append to `poller/internal/wsclient/client_test.go`:

```go
func TestRun_DispatchesDeleteCommand(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		// Send one delete command, then keep the connection alive briefly.
		_ = c.WriteMessage(websocket.TextMessage, []byte(
			`{"type":"delete","request_id":"r1","session_id":"abc","project_dir":"-x"}`,
		))
		time.Sleep(150 * time.Millisecond)
	}))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	cli := New(url)
	if err := cli.Connect(); err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	type call struct{ id, dir string }
	got := make(chan call, 1)
	cli.OnCommand = func(cmd Command) error {
		got <- call{id: cmd.SessionID, dir: cmd.ProjectDir}
		return nil
	}

	stop := make(chan struct{})
	defer close(stop)
	go cli.Run(stop)

	select {
	case c := <-got:
		if c.id != "abc" || c.dir != "-x" {
			t.Errorf("got %+v", c)
		}
	case <-time.After(time.Second):
		t.Fatal("OnCommand was never invoked")
	}
}
```

- [ ] **Step 8.2: Run to verify fail**

```
cd /Users/lucasbacelo/Projects/supervAIsor/poller && go test ./internal/wsclient/ -run TestRun -v
```
Expected: FAIL — `Run` and `OnCommand` undefined.

- [ ] **Step 8.3: Implement Run + Command type + ack helper**

Edit `poller/internal/wsclient/client.go`. Replace the file with:

```go
package wsclient

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Command is the parsed shape of a backend→poller command frame.
type Command struct {
	Type       string `json:"type"`
	RequestID  string `json:"request_id"`
	SessionID  string `json:"session_id"`
	ProjectDir string `json:"project_dir"`
}

// Ack is the poller→backend response to a Command.
type Ack struct {
	Type      string `json:"type"`        // always "delete_ack" (or future types)
	RequestID string `json:"request_id"`
	SessionID string `json:"session_id"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
}

type Client struct {
	url  string
	mu   sync.Mutex
	conn *websocket.Conn

	// OnCommand is invoked from Run for every received command frame.
	// Implementations should be quick and either return nil (successful
	// ack will be sent automatically) or an error (sent as the ack error).
	OnCommand func(cmd Command) error
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

// Run reads frames off the WebSocket and dispatches command frames to
// OnCommand. Each command produces an automatic ack on the same socket.
// Exits when stop is closed or the connection drops.
func (c *Client) Run(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}

		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var cmd Command
		if err := json.Unmarshal(data, &cmd); err != nil {
			continue
		}
		if cmd.Type == "" {
			continue // not a command frame
		}
		ackErr := ""
		ok := true
		if c.OnCommand != nil {
			if e := c.OnCommand(cmd); e != nil {
				ok = false
				ackErr = e.Error()
			}
		}
		ackType := cmd.Type + "_ack"
		if err := c.Send(Ack{Type: ackType, RequestID: cmd.RequestID, SessionID: cmd.SessionID, OK: ok, Error: ackErr}); err != nil {
			log.Printf("wsclient: ack send: %v", err)
			return
		}
	}
}
```

- [ ] **Step 8.4: Run to verify pass**

```
cd /Users/lucasbacelo/Projects/supervAIsor/poller && go test ./internal/wsclient/ -v
```
Expected: PASS.

- [ ] **Step 8.5: Wire deleter into main**

Edit `poller/cmd/poller/main.go`. Add an import:

```go
	"github.com/luxarts/supervaisor-poller/internal/deleter"
	"github.com/luxarts/supervaisor-poller/internal/wsclient"
```

(`wsclient` is already imported; just add `deleter` near it.)

After the `cli := wsclient.New(url)` line, add:

```go
	del := deleter.New(cfg.ProjectsDir, cfg.StateFile, off)
	cli.OnCommand = func(cmd wsclient.Command) error {
		switch cmd.Type {
		case "delete":
			return del.Delete(nil, cmd.SessionID, cmd.ProjectDir)
		default:
			return nil
		}
	}
	go cli.Run(stop)
```

- [ ] **Step 8.6: Build the poller**

```
cd /Users/lucasbacelo/Projects/supervAIsor/poller && go build ./...
```
Expected: builds cleanly.

- [ ] **Step 8.7: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add poller/internal/wsclient/client.go poller/internal/wsclient/client_test.go poller/cmd/poller/main.go
git commit -m "feat(poller): wsclient.Run dispatches delete commands; main wires Deleter"
```

---

## Task 9: Backend — `DELETE /sessions/:hostname/:id`

**Files:**
- Modify: `backend/internal/api/sessions.go`
- Modify: `backend/internal/api/sessions_test.go`

- [ ] **Step 9.1: Write failing tests**

Append to `backend/internal/api/sessions_test.go`:

```go
type fakeSender struct {
	online   map[string]bool
	last     []byte
	lastHost string
}

func (f *fakeSender) IsOnline(h string) bool { return f.online[h] }
func (f *fakeSender) Send(h string, b []byte) error {
	if !f.online[h] {
		return ingest.ErrNoPoller
	}
	f.lastHost = h
	f.last = b
	return nil
}

func TestDeleteSession_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, _ := store.Open(filepath.Join(t.TempDir(), "del.db"))
	defer st.Close()
	r := gin.New()
	(&Handler{
		Store:       st,
		Sender:      &fakeSender{online: map[string]bool{}},
		Coordinator: ingest.NewDeleteCoordinator(time.Second),
	}).Register(r)

	req := httptest.NewRequest("DELETE", "/sessions/h/missing", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestDeleteSession_PollerOffline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, _ := store.Open(filepath.Join(t.TempDir(), "off.db"))
	defer st.Close()
	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	_ = st.UpsertSession(ctx, &state.Session{
		ID: "abc", Hostname: "h", Name: "n", Project: "/p",
		Status: state.StatusDone, StartedAt: t0, LastEventAt: t0,
		ProjectDirEncoded: "-x",
	})

	r := gin.New()
	(&Handler{
		Store:       st,
		Sender:      &fakeSender{online: map[string]bool{}},
		Coordinator: ingest.NewDeleteCoordinator(time.Second),
	}).Register(r)

	req := httptest.NewRequest("DELETE", "/sessions/h/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteSession_HappyPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, _ := store.Open(filepath.Join(t.TempDir(), "ok.db"))
	defer st.Close()
	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	_ = st.UpsertSession(ctx, &state.Session{
		ID: "abc", Hostname: "h", Name: "n", Project: "/p",
		Status: state.StatusDone, StartedAt: t0, LastEventAt: t0,
		ProjectDirEncoded: "-x",
	})

	coord := ingest.NewDeleteCoordinator(2 * time.Second)
	defer coord.Close()
	hub := broadcast.NewHub()
	go hub.Run()
	defer hub.Stop()

	sender := &fakeSender{online: map[string]bool{"h": true}}

	r := gin.New()
	(&Handler{Store: st, Sender: sender, Coordinator: coord, Hub: hub}).Register(r)

	// Issue the DELETE in a goroutine because the handler blocks on the ack.
	respCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest("DELETE", "/sessions/h/abc", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		respCh <- w
	}()

	// Wait until the handler has dispatched the command (sender.last is set).
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if sender.last != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if sender.last == nil {
		t.Fatal("handler never dispatched delete command")
	}

	// Pull request_id out of the dispatched command.
	var sent map[string]any
	_ = json.Unmarshal(sender.last, &sent)
	reqID, _ := sent["request_id"].(string)
	if reqID == "" {
		t.Fatal("dispatched command missing request_id")
	}
	coord.Resolve(reqID, true, "")

	w := <-respCh
	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d body=%s", w.Code, w.Body.String())
	}

	// Store is purged.
	got, _ := st.GetSession(ctx, "h", "abc")
	if got != nil {
		t.Errorf("session not purged: %+v", got)
	}
}

func TestDeleteSession_PollerErrorAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, _ := store.Open(filepath.Join(t.TempDir(), "ack.db"))
	defer st.Close()
	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	_ = st.UpsertSession(ctx, &state.Session{
		ID: "abc", Hostname: "h", Name: "n", Project: "/p",
		Status: state.StatusDone, StartedAt: t0, LastEventAt: t0,
		ProjectDirEncoded: "-x",
	})

	coord := ingest.NewDeleteCoordinator(2 * time.Second)
	defer coord.Close()
	hub := broadcast.NewHub()
	go hub.Run()
	defer hub.Stop()

	sender := &fakeSender{online: map[string]bool{"h": true}}
	r := gin.New()
	(&Handler{Store: st, Sender: sender, Coordinator: coord, Hub: hub}).Register(r)

	respCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest("DELETE", "/sessions/h/abc", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		respCh <- w
	}()
	for time.Now().Before(time.Now().Add(time.Second)) {
		if sender.last != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var sent map[string]any
	_ = json.Unmarshal(sender.last, &sent)
	reqID, _ := sent["request_id"].(string)
	coord.Resolve(reqID, false, "boom")
	w := <-respCh
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
	// Store NOT purged on error.
	got, _ := st.GetSession(ctx, "h", "abc")
	if got == nil {
		t.Fatal("session should still exist after error ack")
	}
}
```

The test imports `ingest`, `broadcast`, `json`, `time` — make sure those are present in the import block.

- [ ] **Step 9.2: Run to verify fail**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./internal/api/ -run TestDeleteSession -v
```
Expected: FAIL — `Sender`/`Coordinator`/`Hub` fields missing on `Handler`; `DELETE` not registered.

- [ ] **Step 9.3: Extend `Handler` and add the route**

Edit `backend/internal/api/sessions.go`.

(a) Replace the imports block to add `crypto/rand`, `encoding/hex`, `time`, `github.com/luxarts/supervaisor/internal/broadcast`, and `github.com/luxarts/supervaisor/internal/ingest`:

```go
import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/luxarts/supervaisor/internal/broadcast"
	"github.com/luxarts/supervaisor/internal/events"
	"github.com/luxarts/supervaisor/internal/ingest"
	"github.com/luxarts/supervaisor/internal/state"
	"github.com/luxarts/supervaisor/internal/store"
)
```

(b) Replace the `Handler` struct and add a `PollerSender` interface:

```go
// PollerSender is the subset of *ingest.Registry the API needs. Fakeable
// in tests.
type PollerSender interface {
	IsOnline(hostname string) bool
	Send(hostname string, payload []byte) error
}

// Handler holds dependencies for the REST API handlers.
type Handler struct {
	Store       *store.SQLite
	Sender      PollerSender
	Coordinator *ingest.DeleteCoordinator
	Hub         *broadcast.Hub
}
```

(c) Register the new route in `Register`:

```go
	r.GET("/sessions/:hostname/:id/stats", h.getSessionStats)
	r.DELETE("/sessions/:hostname/:id", h.deleteSession)
```

(d) Add the handler at the end of the file:

```go
type deleteCommand struct {
	Type       string `json:"type"`
	RequestID  string `json:"request_id"`
	SessionID  string `json:"session_id"`
	ProjectDir string `json:"project_dir"`
}

type sessionRemovedFrame struct {
	Kind     string `json:"kind"`
	Hostname string `json:"hostname"`
	ID       string `json:"id"`
}

func newRequestID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (h *Handler) deleteSession(c *gin.Context) {
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
	if h.Sender == nil || !h.Sender.IsOnline(hostname) {
		c.JSON(http.StatusConflict, gin.H{"error": "poller offline"})
		return
	}

	reqID := newRequestID()
	respCh, err := h.Coordinator.Begin(reqID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	cmd := deleteCommand{
		Type:       "delete",
		RequestID:  reqID,
		SessionID:  id,
		ProjectDir: sess.ProjectDirEncoded,
	}
	body, _ := json.Marshal(cmd)
	if err := h.Sender.Send(hostname, body); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	select {
	case r := <-respCh:
		if r.Err != nil {
			if errors.Is(r.Err, ingest.ErrTimeout) {
				c.JSON(http.StatusGatewayTimeout, gin.H{"error": r.Err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": r.Err.Error()})
			return
		}
	case <-ctx.Done():
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "client cancelled"})
		return
	}

	if err := h.Store.DeleteSession(ctx, hostname, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.Hub != nil {
		removed := sessionRemovedFrame{Kind: "session_removed", Hostname: hostname, ID: id}
		if b, err := json.Marshal(removed); err == nil {
			h.Hub.Broadcast(b)
		}
	}
	c.Status(http.StatusNoContent)
	_ = events.IngestEnvelope{} // keep events import used
	_ = time.Second              // keep time import used
}
```

(Note: `events` and `time` may already be used elsewhere in the file. The trailing `_ = ...` lines are insurance against unused-import errors during incremental compilation. Remove them once you confirm both packages are referenced.)

- [ ] **Step 9.4: Run tests to verify pass**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./internal/api/ -v
```
Expected: PASS for the four new `TestDeleteSession_*` cases plus all existing.

- [ ] **Step 9.5: Run all backend tests**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./...
```
Expected: PASS.

- [ ] **Step 9.6: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add backend/internal/api/sessions.go backend/internal/api/sessions_test.go
git commit -m "feat(api): DELETE /sessions/:hostname/:id with poller-online gate and ack wait"
```

---

## Task 10: Backend — Wire Registry + Coordinator + Pollers Frame

**Files:**
- Modify: `backend/internal/broadcast/handler.go`
- Modify: `backend/internal/broadcast/handler_test.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 10.1: Extend `SnapshotProvider` and emit `pollers` snapshot at connect**

Edit `backend/internal/broadcast/handler.go`. Extend the `SnapshotProvider` interface and the `frame` struct:

```go
// SnapshotProvider supplies the current session list and per-host poller
// liveness for initial hydration.
type SnapshotProvider interface {
	Snapshot() []*state.Session
	PollersOnline() map[string]bool
}

type frame struct {
	Kind     string           `json:"kind"`
	Sessions []*state.Session `json:"sessions,omitempty"`
	Session  *state.Session   `json:"session,omitempty"`
	Online   map[string]bool  `json:"online,omitempty"`
}
```

In `Serve`, after sending the `snapshot` frame, also send a `pollers` frame:

```go
	pollersFrame := frame{Kind: "pollers", Online: h.Snapshot.PollersOnline()}
	if b, err := json.Marshal(pollersFrame); err == nil {
		_ = conn.WriteMessage(websocket.TextMessage, b)
	}
```

- [ ] **Step 10.2: Update broadcast test fixture**

In `backend/internal/broadcast/handler_test.go`, the existing `staticSnapshot` now needs to satisfy the new interface. Add the method:

```go
func (s *staticSnapshot) PollersOnline() map[string]bool { return map[string]bool{} }
```

- [ ] **Step 10.3: Add a connect-emits-pollers test**

Append to `backend/internal/broadcast/handler_test.go`:

```go
func TestHandler_PollersFrameOnConnect(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Stop)

	provider := &staticSnapshotWithPollers{
		sessions: nil,
		pollers:  map[string]bool{"mac-A": true},
	}
	h := &Handler{Hub: hub, Snapshot: provider}
	srv := httptest.NewServer(http.HandlerFunc(h.Serve))
	t.Cleanup(srv.Close)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// First frame is snapshot, second is pollers.
	for i := 0; i < 2; i++ {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var f map[string]any
		_ = json.Unmarshal(data, &f)
		if f["kind"] == "pollers" {
			online, _ := f["online"].(map[string]any)
			if online["mac-A"] != true {
				t.Errorf("expected mac-A online, got %+v", online)
			}
			return
		}
	}
	t.Fatal("never received pollers frame")
}

type staticSnapshotWithPollers struct {
	sessions []*state.Session
	pollers  map[string]bool
}

func (s *staticSnapshotWithPollers) Snapshot() []*state.Session    { return s.sessions }
func (s *staticSnapshotWithPollers) PollersOnline() map[string]bool { return s.pollers }
```

- [ ] **Step 10.4: Run broadcast tests**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./internal/broadcast/ -v
```
Expected: PASS.

- [ ] **Step 10.5: Wire registry, coordinator, and pollers broadcaster in main**

Edit `backend/cmd/server/main.go`. Replace the file with:

```go
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
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

	registry := ingest.NewRegistry()
	coord := ingest.NewDeleteCoordinator(10 * time.Second)
	defer coord.Close()

	ingestH := &ingest.Handler{Store: db, Hub: hub, Registry: registry, Coordinator: coord}
	clientsH := &broadcast.Handler{Hub: hub, Snapshot: snapshotProvider{db: db, registry: registry}}
	apiH := &api.Handler{Store: db, Sender: registry, Coordinator: coord, Hub: hub}

	r := gin.Default()
	r.Use(corsMiddleware())
	apiH.Register(r)
	r.GET("/ws/ingest", gin.WrapF(ingestH.Serve))
	r.GET("/ws/clients", gin.WrapF(clientsH.Serve))

	go runStatusTicker(db, hub)
	go runPollersBroadcaster(registry, hub)

	log.Printf("supervAIsor backend on :%s, db=%s", port, dbPath)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}

type snapshotProvider struct {
	db       *store.SQLite
	registry *ingest.Registry
}

func (s snapshotProvider) Snapshot() []*state.Session {
	sess, _ := s.db.ListSessions(context.Background())
	if sess == nil {
		return []*state.Session{}
	}
	return sess
}

func (s snapshotProvider) PollersOnline() map[string]bool {
	if s.registry == nil {
		return map[string]bool{}
	}
	return s.registry.Online()
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
				if err := db.UpsertSession(context.Background(), s); err != nil {
					log.Printf("status tick upsert: %v", err)
					continue
				}
				if b, err := json.Marshal(map[string]any{"kind": "update", "session": s}); err == nil {
					hub.Broadcast(b)
				}
			}
		}
	}
}

// runPollersBroadcaster fans poller liveness transitions out to all
// connected dashboard clients.
func runPollersBroadcaster(registry *ingest.Registry, hub *broadcast.Hub) {
	sub, _ := registry.Subscribe()
	for snap := range sub {
		if b, err := json.Marshal(map[string]any{"kind": "pollers", "online": snap}); err == nil {
			hub.Broadcast(b)
		}
	}
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
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
```

- [ ] **Step 10.6: Build the backend**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go build ./...
```
Expected: builds cleanly.

- [ ] **Step 10.7: Run all backend tests**

```
cd /Users/lucasbacelo/Projects/supervAIsor/backend && go test ./...
```
Expected: PASS.

- [ ] **Step 10.8: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add backend/internal/broadcast/handler.go backend/internal/broadcast/handler_test.go backend/cmd/server/main.go
git commit -m "feat(server): instantiate registry+coordinator; broadcast pollers liveness"
```

---

## Task 11: Frontend — Parse `pollers` and `session_removed` Frames

**Files:**
- Modify: `frontend/src/types.ts`
- Modify: `frontend/src/useSessionsSocket.ts`
- Create: `frontend/src/useSessionsSocket.test.ts`

- [ ] **Step 11.1: Extend types**

Edit `frontend/src/types.ts`. Add `project_dir_encoded?` to `Session` and extend `Frame`:

```ts
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
  last_error_at?: string;
  project_dir_encoded?: string;
}

export type Frame =
  | { kind: "snapshot"; sessions: Session[] }
  | { kind: "update"; session: Session }
  | { kind: "delete"; session_id: string; hostname: string }
  | { kind: "pollers"; online: Record<string, boolean> }
  | { kind: "session_removed"; hostname: string; id: string };
```

- [ ] **Step 11.2: Write failing hook test**

Create `frontend/src/useSessionsSocket.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useSessionsSocket } from "./useSessionsSocket";

class MockWS {
  static instances: MockWS[] = [];
  url: string;
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  constructor(url: string) {
    this.url = url;
    MockWS.instances.push(this);
  }
  send(_: string) {}
  close() {
    this.onclose?.();
  }
}

describe("useSessionsSocket", () => {
  beforeEach(() => {
    MockWS.instances = [];
    vi.stubGlobal("WebSocket", MockWS);
  });
  afterEach(() => vi.unstubAllGlobals());

  function emit(ws: MockWS, payload: unknown) {
    ws.onmessage?.({ data: JSON.stringify(payload) });
  }

  it("captures pollers map from a pollers frame", async () => {
    const { result } = renderHook(() => useSessionsSocket("ws://x"));
    const ws = MockWS.instances[0];
    act(() => {
      ws.onopen?.();
      emit(ws, { kind: "pollers", online: { "mac-A": true, "mac-B": false } });
    });
    expect(result.current.pollersOnline["mac-A"]).toBe(true);
    expect(result.current.pollersOnline["mac-B"]).toBe(false);
  });

  it("removes a session on session_removed frame", async () => {
    const { result } = renderHook(() => useSessionsSocket("ws://x"));
    const ws = MockWS.instances[0];
    act(() => {
      ws.onopen?.();
      emit(ws, {
        kind: "snapshot",
        sessions: [
          { id: "abc", hostname: "h", name: "n", project: "/p", status: "done", started_at: "", current_action: "", last_event_at: "" },
        ],
      });
    });
    expect(result.current.sessions.length).toBe(1);
    act(() => {
      emit(ws, { kind: "session_removed", hostname: "h", id: "abc" });
    });
    expect(result.current.sessions.length).toBe(0);
  });
});
```

- [ ] **Step 11.3: Run to verify fail**

```
cd /Users/lucasbacelo/Projects/supervAIsor/frontend && npx vitest run src/useSessionsSocket.test.ts
```
Expected: FAIL — `pollersOnline` undefined.

- [ ] **Step 11.4: Update the hook**

Edit `frontend/src/useSessionsSocket.ts`. Replace with:

```ts
import { useEffect, useRef, useState } from "react";
import type { Session, Frame } from "./types";

export interface SocketState {
  sessions: Session[];
  connected: boolean;
  pollersOnline: Record<string, boolean>;
}

export function useSessionsSocket(url: string): SocketState {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [connected, setConnected] = useState(false);
  const [pollersOnline, setPollersOnline] = useState<Record<string, boolean>>({});
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
          } else if (frame.kind === "pollers") {
            setPollersOnline(frame.online);
          } else if (frame.kind === "session_removed") {
            setSessions((prev) =>
              prev.filter((s) => !(s.id === frame.id && s.hostname === frame.hostname)),
            );
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

  return { sessions, connected, pollersOnline };
}
```

- [ ] **Step 11.5: Run to verify pass**

```
cd /Users/lucasbacelo/Projects/supervAIsor/frontend && npx vitest run src/useSessionsSocket.test.ts
```
Expected: PASS.

- [ ] **Step 11.6: Type-check (App still expects only `sessions`/`connected`; this is fine since we add a non-breaking field)**

```
cd /Users/lucasbacelo/Projects/supervAIsor/frontend && npx tsc --noEmit
```
Expected: PASS.

- [ ] **Step 11.7: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add frontend/src/types.ts frontend/src/useSessionsSocket.ts frontend/src/useSessionsSocket.test.ts
git commit -m "feat(ui): useSessionsSocket parses pollers + session_removed frames"
```

---

## Task 12: Frontend — Poller LED on `SessionCard`

**Files:**
- Modify: `frontend/src/components/SessionCard.tsx`
- Modify: `frontend/src/components/SessionCard.test.tsx`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 12.1: Write failing test**

Append to `frontend/src/components/SessionCard.test.tsx`:

```tsx
it("renders a green LED when pollerOnline is true", () => {
  const { container } = render(
    <SessionCard session={sess} onOpen={() => {}} pollerOnline />,
  );
  const led = container.querySelector('[data-testid="poller-led"]');
  expect(led).toBeTruthy();
  expect(led?.getAttribute("data-online")).toBe("true");
});

it("renders a red LED when pollerOnline is false", () => {
  const { container } = render(
    <SessionCard session={sess} onOpen={() => {}} pollerOnline={false} />,
  );
  const led = container.querySelector('[data-testid="poller-led"]');
  expect(led?.getAttribute("data-online")).toBe("false");
});
```

- [ ] **Step 12.2: Run to verify fail**

```
cd /Users/lucasbacelo/Projects/supervAIsor/frontend && npx vitest run src/components/SessionCard.test.tsx
```
Expected: FAIL on the two new cases.

- [ ] **Step 12.3: Add the LED**

Edit `frontend/src/components/SessionCard.tsx`. Add `pollerOnline?: boolean` to the `Props` interface:

```ts
interface Props {
  session: Session;
  onOpen: () => void;
  pinned?: boolean;
  notify?: boolean;
  errorActive?: boolean;
  flash?: "complete" | "error" | null;
  pollerOnline?: boolean;
}
```

Default it in the destructure:

```ts
export function SessionCard({
  session,
  onOpen,
  pinned = false,
  notify = false,
  errorActive = false,
  flash = null,
  pollerOnline,
}: Props) {
```

In the `<h2>` heading where the hostname is rendered, prepend the LED:

```tsx
      <h2 className="mt-3 font-hud text-xl uppercase tracking-wider text-txt truncate">
        {session.name}
        <span className="text-yl mx-0.5">@</span>
        {pollerOnline !== undefined && (
          <span
            data-testid="poller-led"
            data-online={pollerOnline ? "true" : "false"}
            title={pollerOnline ? "Poller online" : "Poller offline"}
            className={`mx-1 inline-block h-2 w-2 rounded-full align-middle ${
              pollerOnline ? "bg-[#32ff7e]" : "bg-rd"
            }`}
            aria-label={pollerOnline ? "Poller online" : "Poller offline"}
          />
        )}
        <span className="text-cy">{session.hostname}</span>
      </h2>
```

- [ ] **Step 12.4: Pass `pollerOnline` from App**

Edit `frontend/src/App.tsx`. Read `pollersOnline` from the hook:

```ts
const { sessions, connected, pollersOnline } = useSessionsSocket(WS_URL);
```

Add `pollerOnline={pollersOnline[s.hostname] ?? false}` to the `<SessionCard>` mapping (around the existing `flash={flashes.get(key) ?? null}` line):

```tsx
              <SessionCard
                key={key}
                session={s}
                onOpen={() => setOpen({ hostname: s.hostname, id: s.id })}
                pinned={pinned.has(key)}
                notify={notify.has(key)}
                errorActive={errorActive.has(key)}
                flash={flashes.get(key) ?? null}
                pollerOnline={pollersOnline[s.hostname] ?? false}
              />
```

- [ ] **Step 12.5: Run tests**

```
cd /Users/lucasbacelo/Projects/supervAIsor/frontend && npx vitest run && npx tsc --noEmit
```
Expected: PASS, clean.

- [ ] **Step 12.6: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add frontend/src/components/SessionCard.tsx frontend/src/components/SessionCard.test.tsx frontend/src/App.tsx
git commit -m "feat(ui): poller-online LED on SessionCard + App wiring"
```

---

## Task 13: Frontend — DANGER Zone in `SessionDetails`

**Files:**
- Create: `frontend/src/lib/deleteSession.ts`
- Create: `frontend/src/lib/deleteSession.test.ts`
- Modify: `frontend/src/components/SessionDetails.tsx`
- Modify: `frontend/src/components/SessionDetails.test.tsx`
- Modify: `frontend/src/components/ConversationModal.tsx`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 13.1: Write the HTTP wrapper test**

Create `frontend/src/lib/deleteSession.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { deleteSession } from "./deleteSession";

describe("deleteSession", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({ ok: true, status: 204, text: async () => "" })),
    );
  });
  afterEach(() => vi.unstubAllGlobals());

  it("issues DELETE against the right URL on success", async () => {
    await deleteSession("http://localhost:8080", "mac-A", "abc");
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/sessions/mac-A/abc",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("throws with backend error message on non-204 responses", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: false,
        status: 409,
        text: async () => `{"error":"poller offline"}`,
      })),
    );
    await expect(deleteSession("http://x", "h", "id")).rejects.toThrow(/poller offline/);
  });
});
```

- [ ] **Step 13.2: Run to verify fail**

```
cd /Users/lucasbacelo/Projects/supervAIsor/frontend && npx vitest run src/lib/deleteSession.test.ts
```
Expected: FAIL — module not found.

- [ ] **Step 13.3: Implement the wrapper**

Create `frontend/src/lib/deleteSession.ts`:

```ts
export async function deleteSession(
  backendHttpBase: string,
  hostname: string,
  id: string,
): Promise<void> {
  const r = await fetch(
    `${backendHttpBase}/sessions/${encodeURIComponent(hostname)}/${encodeURIComponent(id)}`,
    { method: "DELETE" },
  );
  if (r.ok) return;
  let msg = `HTTP ${r.status}`;
  try {
    const body = await r.text();
    if (body) {
      const parsed = JSON.parse(body) as { error?: string };
      if (parsed.error) msg = parsed.error;
    }
  } catch {
    // body wasn't JSON; fall through with the HTTP status
  }
  throw new Error(msg);
}
```

- [ ] **Step 13.4: Run wrapper tests**

```
cd /Users/lucasbacelo/Projects/supervAIsor/frontend && npx vitest run src/lib/deleteSession.test.ts
```
Expected: PASS.

- [ ] **Step 13.5: Write failing UI tests**

Append to `frontend/src/components/SessionDetails.test.tsx`:

```tsx
it("disables DELETE when pollerOnline is false", async () => {
  render(
    <SessionDetails
      sessionId="abc"
      hostname="mac-A"
      lastEventAt="2026-05-14T10:00:00Z"
      backendHttpBase="http://localhost:8080"
      pollerOnline={false}
    />,
  );
  await waitFor(() => expect(screen.getByText("claude-opus-4-7")).toBeTruthy());
  const btn = screen.getByRole("button", { name: /delete session/i });
  expect((btn as HTMLButtonElement).disabled).toBe(true);
});

it("requires two clicks to delete and calls onDeleted on 204", async () => {
  const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "DELETE") {
      return { ok: true, status: 204, text: async () => "" };
    }
    return { ok: true, status: 200, json: async () => stats };
  });
  vi.stubGlobal("fetch", fetchMock);

  const onDeleted = vi.fn();
  render(
    <SessionDetails
      sessionId="abc"
      hostname="mac-A"
      lastEventAt="2026-05-14T10:00:00Z"
      backendHttpBase="http://localhost:8080"
      pollerOnline
      onDeleted={onDeleted}
    />,
  );
  await waitFor(() => expect(screen.getByText("claude-opus-4-7")).toBeTruthy());
  const btn = screen.getByRole("button", { name: /delete session/i });
  fireEvent.click(btn); // → confirm state
  expect(btn.textContent).toMatch(/confirm delete/i);
  fireEvent.click(btn); // → actually deletes
  await waitFor(() => expect(onDeleted).toHaveBeenCalled());
  expect(fetchMock).toHaveBeenCalledWith(
    expect.stringContaining("/sessions/mac-A/abc"),
    expect.objectContaining({ method: "DELETE" }),
  );
});
```

- [ ] **Step 13.6: Run to verify fail**

```
cd /Users/lucasbacelo/Projects/supervAIsor/frontend && npx vitest run src/components/SessionDetails.test.tsx
```
Expected: FAIL — DELETE button not present.

- [ ] **Step 13.7: Add the DANGER section**

Edit `frontend/src/components/SessionDetails.tsx`.

(a) Add new optional props to the `Props` interface:

```ts
interface Props {
  sessionId: string;
  hostname: string;
  lastEventAt: string;
  backendHttpBase: string;
  pollerOnline?: boolean;
  onDeleted?: () => void;
}
```

(b) Destructure them:

```ts
export function SessionDetails({
  sessionId,
  hostname,
  lastEventAt,
  backendHttpBase,
  pollerOnline,
  onDeleted,
}: Props) {
```

(c) Add an import at the top:

```ts
import { deleteSession } from "../lib/deleteSession";
```

(d) Inside the function body, alongside the other state, add a delete state machine:

```ts
  const [deleteState, setDeleteState] = useState<
    | { phase: "idle" }
    | { phase: "confirming" }
    | { phase: "deleting" }
    | { phase: "error"; message: string }
  >({ phase: "idle" });

  // Auto-revert confirmation after 5s.
  useEffect(() => {
    if (deleteState.phase !== "confirming") return;
    const t = setTimeout(() => setDeleteState({ phase: "idle" }), 5000);
    return () => clearTimeout(t);
  }, [deleteState]);
```

(e) Add a helper inside the function:

```ts
  const onDeleteClick = async () => {
    if (deleteState.phase === "idle") {
      setDeleteState({ phase: "confirming" });
      return;
    }
    if (deleteState.phase === "confirming") {
      setDeleteState({ phase: "deleting" });
      try {
        await deleteSession(backendHttpBase, hostname, sessionId);
        onDeleted?.();
      } catch (e) {
        setDeleteState({ phase: "error", message: e instanceof Error ? e.message : String(e) });
      }
    }
  };

  const buttonLabel =
    deleteState.phase === "deleting"
      ? "DELETING…"
      : deleteState.phase === "confirming"
      ? "CONFIRM DELETE"
      : "DELETE SESSION";
  const deleteDisabled =
    pollerOnline !== true || deleteState.phase === "deleting";
  const fullPath = stats?.project_dir_encoded
    ? `~/.claude/projects/${stats.project_dir_encoded}/${sessionId}.jsonl`
    : "(path unavailable)";
```

(f) Add a fifth `<Section>` after `SETTINGS`:

```tsx
      <Section title="DANGER ZONE">
        <Row k="Path on host" v={fullPath} />
        {deleteState.phase === "error" && (
          <div className="mt-2 border border-rd bg-rd/10 p-2 text-rd text-xs">
            // {deleteState.message}
          </div>
        )}
        <button
          type="button"
          onClick={onDeleteClick}
          disabled={deleteDisabled}
          aria-label="Delete session"
          title={pollerOnline ? "" : `Poller offline — start the poller on ${hostname} to enable`}
          className={`mt-2 w-full border px-3 py-2 font-hud text-xs uppercase tracking-widest
                      touch-manipulation transition-colors
                      ${deleteDisabled ? "border-dim text-dim cursor-not-allowed" : "border-rd text-rd hover:bg-rd/10"}`}
        >
          {buttonLabel}
        </button>
      </Section>
```

(g) Update the `Stats` interface in the same file to include the new field:

```ts
interface Stats {
  hostname: string;
  id: string;
  name: string;
  project: string;
  project_dir_encoded?: string;
  model?: string;
  started_at: string;
  last_event_at: string;
  wall_clock_seconds: number;
  tokens: Tokens;
  counts: Counts;
  tool_breakdown: Record<string, number>;
}
```

- [ ] **Step 13.8: Wire `pollerOnline` and `onDeleted` from `ConversationModal`**

Edit `frontend/src/components/ConversationModal.tsx`. Add to the `Props` interface:

```ts
interface Props {
  sessionId: string;
  hostname: string;
  sessionName: string;
  project: string;
  lastEventAt: string;
  backendHttpBase: string;
  onClose: () => void;
  pollerOnline?: boolean;
}
```

Destructure `pollerOnline` and pass it (plus `onDeleted={onClose}`) into `SessionDetails`:

```tsx
            <SessionDetails
              sessionId={sessionId}
              hostname={hostname}
              lastEventAt={lastEventAt}
              backendHttpBase={backendHttpBase}
              pollerOnline={pollerOnline}
              onDeleted={onClose}
            />
```

- [ ] **Step 13.9: Pass `pollerOnline` from App into `ConversationModal`**

Edit `frontend/src/App.tsx`. Around the existing `<ConversationModal>` element, add the prop:

```tsx
        <ConversationModal
          sessionId={openSession.id}
          hostname={openSession.hostname}
          sessionName={openSession.name}
          project={openSession.project}
          lastEventAt={openSession.last_event_at}
          backendHttpBase={HTTP_BASE}
          onClose={() => setOpen(null)}
          pollerOnline={pollersOnline[openSession.hostname] ?? false}
        />
```

- [ ] **Step 13.10: Run all frontend tests**

```
cd /Users/lucasbacelo/Projects/supervAIsor/frontend && npx tsc --noEmit && npx vitest run
```
Expected: PASS.

- [ ] **Step 13.11: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add frontend/src/lib/deleteSession.ts frontend/src/lib/deleteSession.test.ts frontend/src/components/SessionDetails.tsx frontend/src/components/SessionDetails.test.tsx frontend/src/components/ConversationModal.tsx frontend/src/App.tsx
git commit -m "feat(ui): DANGER ZONE in SessionDetails — two-click delete gated by poller LED"
```

---

## Task 14: Docs — Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 14.1: Soften the read-only line and add deletion note**

Replace the line `It is **read-only monitoring** — it does not spawn, kill, or control sessions.` with:

```md
It is mostly **read-only monitoring** — it does not spawn or control sessions. The one user-initiated write is **deletion**: the dashboard can request a poller to remove a specific session's `.jsonl` from disk, gated by a per-host liveness LED.
```

Update the bullet describing the backend's endpoints to include `DELETE /sessions/:hostname/:id` and to mention `/sessions/:hostname/:id/stats` (already added in spec #1 but the doc may not list it).

- [ ] **Step 14.2: Commit**

```bash
cd /Users/lucasbacelo/Projects/supervAIsor
git add CLAUDE.md
git commit -m "docs: note user-initiated deletion in CLAUDE.md overview"
```

---

## Task 15: End-to-End Smoke

**Files:** none — manual verification.

- [ ] **Step 15.1: Build everything**

```
cd /Users/lucasbacelo/Projects/supervAIsor && make test
```
Expected: backend, poller, and frontend tests all PASS.

- [ ] **Step 15.2: Bring up the stack**

```
make up
```
Wait for containers to be healthy. In another terminal, start a host poller:
```
make poller
```

- [ ] **Step 15.3: Browser checks at http://localhost:5173**

- (a) Each card shows a small LED next to the hostname; green when the poller for that host is connected.
- (b) Stop the poller (`Ctrl-C`). LED for that host turns red within ~1 s.
- (c) Open any card → DETAILS → DANGER ZONE shows the full `~/.claude/projects/.../<id>.jsonl` path.
- (d) With poller offline: DELETE SESSION button is disabled; tooltip says "Poller offline".
- (e) Restart the poller. LED returns to green; DELETE button enables.
- (f) Click DELETE SESSION → label flips to CONFIRM DELETE for 5 s. If you don't click again, it reverts to idle.
- (g) Click again within 5 s → modal closes, card disappears from the grid, the `.jsonl` is gone from disk on the host.
- (h) Refresh the dashboard — the deleted session does not return.

- [ ] **Step 15.4: Tear down**

```
make down
```

- [ ] **Step 15.5: No commit needed.**

---

## Self-Review Notes

1. **Spec coverage**:
   - Poller liveness (T2 registry, T10 broadcaster, T11 frontend frame, T12 LED on card, T13 LED in DANGER button gating).
   - Bidirectional ingest WS (T6 handler split, T8 wsclient.Run + Command/Ack types).
   - DeleteCoordinator (T3) + DELETE endpoint (T9) + store.DeleteSession (T1).
   - End-to-end atomic deletion: T9 only purges store and broadcasts `session_removed` after the ack returns ok; T11 drops the session on receipt; T13 closes modal via `onDeleted`.
   - Path safety in poller: T7 `Deleter` rejects path-escape; tests cover both `..` in projectDir and sessionID.
   - Status codes: 404 (T9.1), 409 (T9.1), 204 (T9.1), 504 surfaced by `ErrTimeout` from coordinator (T3 covers timeout; T9 maps it to 504), 500 on poller error ack (T9.1).
   - `project_dir_encoded` plumbed end-to-end (T4 persist, T5 stats, T11 type, T13 path display).
2. **Backwards compat**: ingest envelopes without `type` still treated as events (T6); old pollers won't read commands and the backend will return 504 — surfaced in UI as an error toast (T13 error state).
3. **Type consistency**: `PollerSender` interface in T9 is satisfied by `*ingest.Registry` (Online + Send/IsOnline) — verified in T10 wiring. `Coordinator` field on api.Handler matches `*ingest.DeleteCoordinator` returned by `NewDeleteCoordinator`. Frame `kind` strings are stable across T6, T9, T10, T11.
4. **No placeholders.** Every code step shows actual code; every command has expected output.

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-05-15-session-deletion.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**
