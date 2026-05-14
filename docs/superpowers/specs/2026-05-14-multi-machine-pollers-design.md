# Multi-machine pollers — design spec

**Date:** 2026-05-14
**Status:** Approved for planning

## Goal

Allow multiple poller instances running on different machines to ship Claude session events to a single backend. The frontend identifies each session card with the originating machine's hostname using the format `name@hostname`.

The backend and frontend continue to run on a single machine; only the poller is distributed.

## Non-goals

- Authentication, authorization, or TLS. The system remains local-network, single-user (per `CLAUDE.md`).
- Per-machine filtering or grouping in the UI (YAGNI for a handful of machines).
- Spawning, killing, or controlling sessions remotely. Read-only monitoring only.
- Cross-platform hostname handling beyond macOS for v1.

## Architecture summary

```
poller (mac A) ──ws──┐
poller (mac B) ──ws──┼──► backend ──ws──► frontend
poller (mac C) ──ws──┘     SQLite
```

Each poller tags every envelope with its `hostname`. The backend keys session state by `(hostname, session_id)`, allowing the same Claude UUID to coexist across machines without collision. The frontend renders cards as `name@hostname`.

## Data model changes

### Ingest envelope (`backend/internal/events/types.go`, mirrored in poller)

Add a required `hostname` field:

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

Envelopes with empty `hostname` are logged and dropped (the connection is not closed — the poller is expected to always send it).

### Session state (`backend/internal/state/types.go`)

Add `Hostname` to `Session`. It is set on first event from the envelope and never changes afterwards.

### SQLite schema (`backend/internal/store/sqlite.go`)

```sql
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
  hostname    TEXT NOT NULL,
  session_id  TEXT NOT NULL,
  ts          TIMESTAMP NOT NULL,
  type        TEXT NOT NULL,
  payload     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_session_ts ON events(hostname, session_id, ts);
```

**Migration of an existing DB:** add `hostname` column with default `''` to both tables; recreate `sessions` with the new composite PK (SQLite requires copy-rename for PK changes). Old rows keep `hostname=''` and will be re-upserted with the correct hostname when new events arrive. No protocol versioning needed (no deployed users).

## Backend changes

### Ingest handler (`backend/internal/ingest/handler.go`)

- **Remove the `connected.CompareAndSwap` single-writer guard.** Multiple pollers connect concurrently.
- Reject envelopes with empty `hostname`: log a warning and continue reading.
- Pass `hostname` through to `state.Apply` so it can set `Session.Hostname` on first event.
- Use `(hostname, session_id)` as the lookup key for `GetSession`, `UpsertSession`, and `AppendEvent`.

### Store API

Signature changes:

- `GetSession(ctx, hostname, id)`
- `UpsertSession(ctx, *Session)` — derives the composite key from `Session.Hostname` and `Session.ID`.
- `AppendEvent(ctx, hostname, sessionID, ts, type, payload)`
- `ListSessions(ctx)` — unchanged shape; returns sessions from all hosts ordered by `last_event_at DESC`.
- `ListEvents(ctx, hostname, sessionID, limit)`

### HTTP API

`GET /sessions/:id/events` is replaced by `GET /sessions/:hostname/:id/events`. The frontend always knows both fields, so this is straightforward. (Alternative: keep one path segment and encode `hostname:id` — rejected as harder to read in logs.)

## Poller changes

### Config flags (`poller/cmd/poller/main.go`)

Replace the single `-backend` URL with host+port pieces, keeping `-backend` as an explicit override:

| Flag             | Default                  | Description                                      |
|------------------|--------------------------|--------------------------------------------------|
| `-backend-host`  | `localhost`              | Backend host                                     |
| `-backend-port`  | `8080`                   | Backend port                                     |
| `-hostname`      | `os.Hostname()` (cleaned) | Hostname to attach to envelopes                  |
| `-backend`       | (derived)                | Optional full WS URL override                    |
| `-projects-dir`  | `~/.claude/projects`     | unchanged                                        |
| `-state-file`    | `~/.supervAIsor/poller-state.json` | unchanged                              |
| `-interval`      | `1s`                     | unchanged                                        |

If `-backend` is empty, the poller builds `ws://{backend-host}:{backend-port}/ws/ingest`.

**Hostname resolution (macOS):** call `os.Hostname()`; strip a trailing `.local` suffix if present so cards read `lucas-mbp` instead of `lucas-mbp.local`. If the call fails or returns empty, abort startup with a clear error (hostname is required).

### Envelope construction

The poller stamps `hostname` on every envelope before sending. `wsclient` stays generic — it just marshals whatever it receives.

## Frontend changes

### Type (`frontend/src/types.ts`)

```ts
export interface Session {
  id: string;
  hostname: string;
  name: string;
  // ...rest unchanged
}
```

### Card display (`frontend/src/components/SessionCard.tsx`)

Render the title as `{name}@{hostname}`. Same typography; no extra row. The `@` keeps the visual rhythm of the cyberpunk theme.

### Identity in the UI

React list keys become `${hostname}:${id}` to prevent collisions when two machines have the same Claude UUID.

### Conversation modal

`ConversationModal` accepts `hostname` and fetches `GET /sessions/:hostname/:id/events`.

## Error handling

- **Two pollers on the same machine (same hostname):** allowed by the backend; the user is expected not to do this. If they do, they may see thrashing on session rows since both pollers will tail the same files. We do not detect or warn about this in v1.
- **Empty hostname envelope:** logged and dropped on the backend (defense in depth; the new poller will never send one).
- **Old SQLite rows with `hostname=''`:** show as `name@` until the next event re-upserts them with the real hostname. Acceptable.

## Testing

- **Backend unit tests:** update `store` tests for composite key; add a test that two sessions with the same UUID but different hostnames coexist; assert the ingest handler accepts two concurrent connections.
- **Poller unit tests:** envelope construction stamps the configured hostname; URL derivation from `-backend-host`/`-backend-port` falls back when `-backend` is empty; `.local` stripping.
- **Frontend tests:** card renders `name@hostname`; modal builds the correct events URL.

## Open items

None — all in scope is decided.
