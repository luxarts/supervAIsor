# Session Deletion & Poller Liveness — Design

**Date:** 2026-05-14
**Scope:** Spec #3 of 3 in the "interactions" milestone.
**Depends on:** `status-semantics-and-stats` (the Delete button lives in the DETAILS tab; the LED dot renders on the card alongside the status badge introduced there).

## Problem

The dashboard is read-only by design (`CLAUDE.md`), but accumulating session files is a real cost: stale Claude sessions linger in `~/.claude/projects/...` and clutter the dashboard forever. The user needs a way to delete a session — both the row in the backend store **and** the underlying `.jsonl` on the host machine that produced it. Today there is no path for the backend to talk back to a poller (the WS is one-way: poller writes, backend reads).

We also have no way to know whether a given host's poller is currently online. That matters here because deletion must be refused when the poller for the session's host is offline (we cannot reach the file), and it's a useful liveness signal even outside this feature.

## Goals

1. Per-host **poller liveness** indicator: green/red LED on every card and in the DETAILS tab, derived from whether a poller for that hostname currently holds an open ingest WS connection.
2. **Delete button** in the DETAILS tab. Disabled with tooltip when the host's poller is offline. Triggers a confirmation dialog showing the exact `.jsonl` path that will be removed. Confirms with two clicks (button changes label `DELETE` → `CONFIRM DELETE`).
3. **Bidirectional ingest WS**: backend sends commands, poller reads commands, poller acks. Smallest possible change to existing code.
4. Deletion is **end-to-end atomic from the user's perspective**: the session disappears from the UI only after the file is gone on disk and the backend state is purged.

## Non-Goals

- No multi-select bulk delete.
- No undo. Filesystem deletion is irreversible.
- No retention policies, scheduled cleanups, or automatic deletion. User-initiated only.

## Architecture

```
Frontend ──HTTP DELETE──► Backend ──WS command──► Poller ──fs.Remove──► .jsonl gone
                                                  │
                          Backend ◄──WS ack──────┘
                          │
                          Backend purges store, broadcasts session-removed to clients
```

## Poller Liveness

### Backend

`internal/ingest/registry.go` (new): a small registry tracking active ingest connections.

```go
type Registry struct {
    mu    sync.RWMutex
    conns map[string]map[*websocket.Conn]struct{}   // hostname → set
}

func (r *Registry) Add(hostname string, c *websocket.Conn)
func (r *Registry) Remove(hostname string, c *websocket.Conn)
func (r *Registry) Online() map[string]bool         // snapshot
func (r *Registry) IsOnline(hostname string) bool
func (r *Registry) Subscribe() (<-chan map[string]bool, func())
```

The ingest handler calls `Add` on connect and `Remove` on disconnect. Hostname comes from the first envelope (envelopes with empty hostname are dropped — same as today).

### Broadcast to clients

`internal/broadcast/` already fans out per-session `state` messages. Add a second message kind on the same client WS:

```json
{ "type": "pollers", "online": { "mac-A": true, "mac-B": false } }
```

Sent on every transition (registry subscription) and once at client-connect. The frontend keeps a `pollersOnline: Record<string, boolean>` derived from these messages.

### Frontend

- `useSessionsSocket` parses both message kinds and exposes `pollersOnline` alongside `sessions` and `connected`.
- `SessionCard` renders a small dot before the hostname: `● mac-A` (green `#32ff7e` if online, red `#ff003c` if offline), title attribute `Poller online` / `Poller offline`.
- DETAILS tab `HOST` row shows the same dot.

## Bidirectional Ingest WS

### Wire protocol

Today envelopes are JSON objects with `hostname`, `session_id`, `project_dir`, `file_mtime`, `raw`. They have no discriminator. We add an optional `type` field — absent on existing envelopes (treated as `event`), present on new messages.

Backend → poller:

```json
{ "type": "delete", "request_id": "uuid", "session_id": "abc-123", "project_dir": "-Users-x-Projects-foo" }
```

Poller → backend ack:

```json
{ "type": "delete_ack", "request_id": "uuid", "session_id": "abc-123", "ok": true,  "error": "" }
{ "type": "delete_ack", "request_id": "uuid", "session_id": "abc-123", "ok": false, "error": "file not found" }
```

The `project_dir` is sent so the poller can find the right `~/.claude/projects/<project_dir>/<session_id>.jsonl` without scanning. We have `project_dir` in every event we ingested for that session, so the backend knows it.

### Backend — sending commands

`internal/ingest/handler.go` is upgraded from one-shot `for { read }` to a goroutine pair per connection: one reader (existing logic), one writer (drains a per-connection `chan []byte`). The registry holds the writer channel, not the raw conn.

`internal/ingest/registry.go` adds:

```go
func (r *Registry) Send(hostname string, payload []byte) error  // round-robin if multiple conns
```

If no connection is registered for `hostname`, returns an error and the API responds 409 Conflict with `{"error": "poller offline"}`. (This is a defense-in-depth check; the frontend already disables the button.)

### Poller — receiving commands

`poller/internal/wsclient/client.go` currently writes envelopes. Extend to read frames in parallel:

```go
go c.readLoop()  // dispatches to a CommandHandler
```

`CommandHandler` is an interface with `Delete(ctx, sessionID, projectDir) error` implemented in a new `poller/internal/deleter/deleter.go`:

```go
func (d *Deleter) Delete(ctx context.Context, sessionID, projectDir string) error {
    path := filepath.Join(d.projectsDir, projectDir, sessionID+".jsonl")
    // safety: refuse if path escapes projectsDir
    if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(d.projectsDir)+string(filepath.Separator)) {
        return errors.New("path outside projects dir")
    }
    if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
        return err
    }
    d.offsets.Forget(path)   // remove offset entry, persist
    return nil
}
```

The deleter writes the ack back through the existing wsclient send channel. The tailer's directory scan will naturally stop emitting events for the file on the next pass; if the tailer is mid-read of that file when delete fires, `os.Remove` succeeds on macOS (open fd keeps the inode alive); the tailer will get EOF on next read and drop the file gracefully.

### Backend — handling acks

The ingest reader recognises `delete_ack` envelopes (no `raw`) and routes them to a `DeleteCoordinator` keyed by `request_id`. The coordinator:

- On `ok: true`: calls `store.DeleteSession(ctx, hostname, id)` (new method, removes the session row + its events), then broadcasts `{ "type": "session_removed", "hostname": ..., "id": ... }` on the client WS.
- On `ok: false`: leaves state alone; resolves the pending request with the error.
- On timeout (10 s, no ack received): resolves with timeout error and clears the pending entry.

The HTTP handler waits on the coordinator (channel) and returns 204 No Content on success or 5xx with the poller's error message on failure.

### Store

`internal/store/sqlite.go` adds:

```go
func (s *Store) DeleteSession(ctx context.Context, hostname, id string) error
```

Single transaction: `DELETE FROM events WHERE hostname=? AND session_id=?` then `DELETE FROM sessions WHERE hostname=? AND id=?`. Foreign-key cascade is fine if defined; explicit two-step keeps it portable.

## API

```
DELETE /sessions/:hostname/:id
```

- 404 if no such session in store.
- 409 if no poller online for `hostname`.
- 204 on full success (file gone, store cleared).
- 504 if poller didn't ack within 10 s.
- 500 with `{"error": "..."}` if poller acked with `ok: false`.

## Frontend Delete Flow

DETAILS tab grows a "DANGER" section at the bottom:

```
DANGER ZONE
  Path on host:  /Users/x/.claude/projects/-Users-x-Projects-foo/abc-123.jsonl
  [ DELETE SESSION ]   ← red outline button
```

State machine on the button:

1. **Idle**: label `DELETE SESSION`. Disabled if `pollersOnline[hostname] !== true` (tooltip: `Poller offline — start the poller on <hostname> to enable`).
2. **Confirming**: first click swaps label to `CONFIRM DELETE` with a 5-s countdown badge; clicking outside or the countdown elapsing reverts to Idle.
3. **Deleting**: second click within the window. Sends `DELETE`. Button shows spinner.
4. **Done**: backend broadcasts `session_removed`, `useSessionsSocket` drops the session, modal auto-closes (caller component reacts to `openSession === null`).
5. **Error**: toast at top of modal with the backend's error message; button returns to Idle.

The full path is computed client-side from `${session.project_dir_encoded}` if the backend includes the encoded form in `/stats`. To keep this spec self-contained, **add `project_dir_encoded` to the `/stats` response** (this is `env.ProjectDir` from ingest, which is the encoded directory name like `-Users-x-Projects-foo`). Update spec #1's `/stats` shape implicitly via this section — the field is additive.

## File Inventory

**Modified:**
- `backend/internal/ingest/handler.go` — reader+writer goroutines, registry wiring, ack routing
- `backend/internal/store/sqlite.go` — `DeleteSession`
- `backend/internal/store/sqlite_test.go`
- `backend/internal/api/sessions.go` — `DELETE` handler
- `backend/internal/api/sessions_test.go`
- `backend/internal/state/stats.go` — include `project_dir_encoded` in response
- `backend/internal/broadcast/...` — `pollers` and `session_removed` messages
- `poller/internal/wsclient/client.go` — readLoop + command dispatch
- `poller/internal/offsets/offsets.go` — `Forget(path)` method
- `frontend/src/useSessionsSocket.ts` — parse new message kinds, expose `pollersOnline`, drop session on `session_removed`
- `frontend/src/components/SessionCard.tsx` — LED dot
- `frontend/src/components/SessionCard.test.tsx`
- `frontend/src/components/ConversationModal.tsx` — DANGER section + delete state machine
- `frontend/src/components/ConversationModal.test.tsx`

**New:**
- `backend/internal/ingest/registry.go` + test
- `backend/internal/ingest/coordinator.go` (delete coordinator) + test
- `poller/internal/deleter/deleter.go` + test
- `frontend/src/lib/deleteSession.ts` (HTTP wrapper) + test

## Testing

### Backend

- `registry_test.go`: add/remove correctness under concurrency; `IsOnline` snapshot; subscribe receives transitions.
- `coordinator_test.go`: ack `ok:true` → store `DeleteSession` called and `session_removed` broadcast; `ok:false` → no store touch, error surfaced; timeout → error after 10 s (use injected clock).
- `sessions_test.go`: 404, 409 (poller offline), 204, 504, 500 cases via fake registry + fake coordinator.

### Poller

- `deleter_test.go`: removes file, persists offset removal, refuses path-escape attempts, no-op on missing file.

### Frontend

- `useSessionsSocket.test.ts` (new): given a sequence of `state` / `pollers` / `session_removed` frames, asserts derived state.
- `ConversationModal.test.tsx`: button disabled when poller offline; two-click confirm flow; success dispatches DELETE and closes modal on `session_removed`; error path shows toast.
- `SessionCard.test.tsx`: LED dot color reflects `pollersOnline[hostname]`.

## Safety Notes

- The poller validates that the resolved path stays under `projectsDir`. Any escape attempt (`..` in `project_dir`) is rejected.
- Deletion is permanent. The two-click confirmation, the printed full path, and the disabled-when-offline rule are the user's safeguards. We do not move to trash because the requirement is explicit ("delete the files in the machine") and macOS trash semantics for non-Finder deletions are unreliable.
- The backend never deletes files itself. It only requests deletion from a poller, which means the user can revoke poller access at any time and instantly stop being able to delete (the LED goes red).

## Migration & Rollout

- Wire format additions to the ingest WS are backward-compatible: old envelopes have no `type` field and are treated as events. Old pollers that don't read commands will simply never receive them; the backend will time out and return 504, which the frontend surfaces.
- Recommend bumping poller and backend together. No DB migration.

## Open Questions

None. Decisions taken inline.
