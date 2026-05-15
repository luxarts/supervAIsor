# Cyberpunk Session Monitor — Design

**Date:** 2026-05-14
**Status:** Approved for planning
**Author:** Lucas Bacelo (with Claude)

## Goal

A mobile-first web dashboard that shows the live status of every Claude Code session running on the user's machine, styled like a Cyberpunk 2077 terminal. The user opens it on their phone/tablet and immediately sees which sessions are working, which are waiting for input, how long each has been running, and what each is doing right now.

## Non-goals

- Authentication / multi-user (single-user local instance only).
- Spawning, killing, or otherwise controlling Claude Code sessions. Read-only monitoring.
- Remote / cross-machine monitoring. The poller and the user's browser are on the same LAN.
- Historical analytics (charts over days/weeks). Only "now" plus light recent history.

## Architecture

Three independent processes:

```
   ┌───────────────────────────┐
   │  poller (host, Go)        │
   │  watches ~/.claude/       │
   │  projects/**/*.jsonl      │
   └───────────┬───────────────┘
               │ WS /ws/ingest
               ▼
   ┌───────────────────────────┐    ┌──────────────────┐
   │  backend (Docker, Go)     │◀───│ SQLite (volume)  │
   │  - ingest WS              │    └──────────────────┘
   │  - state derivation       │
   │  - broadcast WS           │
   │  - REST snapshot          │
   └───────────┬───────────────┘
               │ WS /ws/clients  +  GET /sessions
               ▼
   ┌───────────────────────────┐
   │  frontend (Docker, React) │
   │  Cyberpunk 2077 UI        │
   └───────────────────────────┘
```

### Why split the poller from the backend

The Claude Code session files live on the host at `~/.claude/projects/`. Mounting that path into a container is doable but couples the deploy to host paths and OS quirks (macOS file events through bind mounts are unreliable). A native host binary watching the filesystem and pushing structured events over WS is more robust and matches the user's stated requirement.

## Component 1 — Poller

**Process:** Native Go binary, runs on the host (not containerized).

**Responsibilities:**
1. Discover session files under `~/.claude/projects/*/*.jsonl`.
2. For each file, tail-read new lines as they are appended.
3. Persist per-file read offset (inode + byte offset) to `~/.supervAIsor/poller-state.json` so a restart resumes without re-shipping the whole history.
4. Open a WebSocket to the backend (`ws://localhost:8080/ws/ingest`) and stream each new line as a structured event.
5. Reconnect with exponential backoff if the WS drops.
6. On startup, after loading state, scan for files that grew while we were down and ship the delta.

**Detection of new files / new lines:**
- Use `fsnotify` for low-latency notification.
- Fallback to a 1s polling tick (every directory `Readdir` + each open file `Stat`) for robustness — fsnotify on macOS over network volumes and edge cases is flaky.

**Wire format** (each WS message, one JSON object per line):

```json
{
  "session_id": "54f9076d-0048-4b52-aa47-7f4c55832c28",
  "project_dir": "-Users-you-Projects-supervAIsor",
  "file_mtime": "2026-05-14T01:13:22Z",
  "line_index": 57,
  "raw": { /* the original JSONL line, parsed */ }
}
```

The poller does NOT derive status. It ships raw events. Derivation lives in the backend so the rule logic is in one place.

**Why a state file:** at session JSONL files easily reach thousands of lines; re-sending the full history on every poller restart would be wasteful and would cause noisy "ghost" status flips on the frontend.

## Component 2 — Backend

**Process:** Go HTTP + WS server, runs in a Docker container.

**Refactor scope:** The existing `agent.Manager` and `hooks.Handler` are removed entirely. Per Lucas's direction, no legacy spawn/hook flow is preserved.

**Modules:**

- `internal/ingest/` — WS endpoint `/ws/ingest`, accepts events from the poller. One connection at a time (single user). Validates schema. Writes raw events to SQLite. Triggers `state.Apply` to update derived session state.
- `internal/state/` — pure derivation. Takes a raw JSONL line, the previous session state, and returns the new session state. No I/O. Unit-tested with table-driven tests covering each status transition.
- `internal/store/` — SQLite repository. `modernc.org/sqlite` (pure Go, no CGO).
- `internal/broadcast/` — WS hub for frontends. Reuses the current `internal/ws/hub.go` pattern. On every state change, fans out to all connected frontend clients.
- `internal/api/` — REST handlers. Slimmed to just `GET /sessions` (snapshot for hydration) and `GET /healthz`.
- `cmd/server/main.go` — wires it all up.

**Removed files:** `internal/agent/`, `internal/hooks/`, the spawn-related code in `internal/api/handler.go`, the agent-related hook config writing.

### State derivation rules

The backend keeps a `Session` in-memory and persists every change.

| Field | Derivation |
|---|---|
| `session_id` | from filename |
| `project` | decoded directory name; the dir name format is path with `/` replaced by `-`. Decoding restores `<repo>` |
| `name` | latest `custom-title.customTitle` else latest `agent-name.agentName` else first 8 chars of `session_id` |
| `started_at` | timestamp of the first event in the file, or file ctime if no timestamp |
| `last_event_at` | timestamp of the most recent event |
| `last_prompt_at` | timestamp of the most recent `user` entry that is a real prompt (not a tool_result) |
| `current_action` | derived as described below |
| `status` | derived as described below |

**`current_action`:**
- If last event is an `assistant` message containing a `tool_use`, action = `"<ToolName>: <short summary>"`. Summary built from common arguments (Write/Edit → file path; Bash → command first 60 chars; Read → file path; Task → description; WebFetch → URL host; etc.).
- Else if last event is an `assistant` message containing only text, action = first 80 chars of that text.
- Else if last event is a `user` message that is a real prompt (not a `tool_result`), action = first 80 chars of the user text.
- Else (e.g. last event is a `tool_result`), action = previous non-tool-result action retained.

**`status`** — one of:
- `working` — most recent event is an `assistant` with at least one `tool_use` that has not yet been answered by a matching `tool_result` in a later `user` entry.
- `waiting_input` — most recent event is an `assistant` end-of-turn (no pending tool_use) AND `last_event_at` is within the last 30 seconds. This is the state right after Claude finished a turn and is waiting for the user.
- `idle` — `last_event_at` is older than 30 seconds and status is not `working`. The session is sitting there with nothing happening.
- `stale` — `last_event_at` is older than 1 hour. Visually de-emphasized.

**Durations the frontend shows:**
- `time_running_total = now - started_at`
- `time_running_last_prompt = now - last_prompt_at` (only meaningful while `working`)

These are computed on the frontend from the timestamps the backend sends — the backend does not push tick events.

### Persistence schema (SQLite)

```sql
CREATE TABLE sessions (
  id              TEXT PRIMARY KEY,
  name            TEXT NOT NULL,
  project         TEXT NOT NULL,
  status          TEXT NOT NULL,
  started_at      DATETIME NOT NULL,
  last_prompt_at  DATETIME,
  current_action  TEXT,
  last_event_at   DATETIME NOT NULL,
  raw_meta        TEXT  -- JSON blob, free-form
);

CREATE TABLE events (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id  TEXT NOT NULL REFERENCES sessions(id),
  ts          DATETIME NOT NULL,
  type        TEXT NOT NULL,
  payload     TEXT NOT NULL  -- raw JSON line
);
CREATE INDEX idx_events_session_ts ON events(session_id, ts);
```

`events` is append-only and primarily exists so the dashboard can show a recent-history strip (last N events per session) without re-reading the JSONL files.

### WS protocol — frontend bound

Frontend connects to `/ws/clients`. On connect, the backend sends a `snapshot` frame, then `update` frames as state changes:

```json
{ "kind": "snapshot", "sessions": [ Session, Session, ... ] }
{ "kind": "update", "session": Session }
{ "kind": "delete", "session_id": "..." }  // when a JSONL file is removed
```

`Session` is the row from the `sessions` table, JSON-encoded.

## Component 3 — Frontend

**Stack:** React 19 + Vite + TypeScript + Tailwind CSS. No state library — `useState` + a small WS hook. Mobile-first responsive layout.

**Routes:**
- `/` — grid of session cards.
- `/s/:id` — single session detail drawer (or full page on small screens), with recent events.

**Cyberpunk 2077 theme tokens:**

```
--bg-base:    #0a0a0f
--bg-panel:   #11111a
--accent-cy:  #00f0ff   (cyan — primary)
--accent-yl:  #fcee0a   (yellow — warnings / waiting_input)
--accent-rd:  #ff003c   (red — alerts / stale)
--text:       #e6f9ff
--text-dim:   #6b7280
--grid-line:  rgba(0,240,255,0.08)
```

**Typography:** `Share Tech Mono` for HUD-y elements, `JetBrains Mono` for body text.

**Card layout (per session):**
- Min height 160 px so it's easily tappable.
- Top-left: status badge (color-coded, with a pulsing dot when `working`).
- Top-right: kebab / details affordance (44 px hit target).
- Title: session `name` in big mono caps.
- Subtitle: project (truncated, ellipsis).
- Body: `current_action` (truncated to 2 lines).
- Footer row: `total` time and (when working) `prompt` time, both with HUD-tick animation on update.

**Animations:**
- Subtle CRT scanlines overlay (single fixed element, low opacity).
- 1-frame glitch on status transitions.
- Continuous "data tick" on the time counters (per-second re-render is fine).
- Status-color border pulse on `working` cards.

**Touch interactions:**
- Tap card → opens the detail view.
- Long-press card → quick-copy session id to clipboard with a toast.
- Pull-to-refresh forces a REST `GET /sessions` re-hydrate (in case the WS missed an update).

**Accessibility:** All interactive elements meet 44 × 44 px. Color is never the only signal — status badges include a text label (`WORKING`, `WAIT`, `IDLE`, `STALE`).

## Infrastructure

`infrastructure/docker-compose.yml`:

```yaml
services:
  backend:
    build: ../backend
    ports: ["8080:8080"]
    volumes:
      - supervaisor-data:/var/lib/supervaisor   # SQLite lives here

  frontend:
    build: ../frontend
    ports: ["5173:5173"]
    environment:
      VITE_BACKEND_WS: "ws://localhost:8080/ws/clients"
      VITE_BACKEND_HTTP: "http://localhost:8080"

volumes:
  supervaisor-data:
```

The poller is installed on the host with `make install-poller` (builds the binary and prints instructions for starting it). A `launchd` plist for macOS comes in v1.1 — out of scope here.

## Failure modes and how we handle them

| Failure | Behavior |
|---|---|
| Poller WS drops | Reconnect with exponential backoff capped at 30 s. On reconnect, replay deltas since last persisted offset. |
| Backend restart | Frontend reconnects to `/ws/clients` and gets a fresh `snapshot`. SQLite preserves session state across restarts. |
| Frontend WS drops | Visible "DISCONNECTED" banner in cyan/red. Auto-reconnect every 3 s. |
| JSONL line malformed | Logged at WARN, dropped. Session state is not mutated. Counter exposed via `/healthz`. |
| Two pollers connect to ingest | Reject the second one. Single-writer assumption. |
| Session file deleted | Backend emits `delete` frame; frontend removes the card. |

## Testing strategy

- **`internal/state/`** — table-driven unit tests, one row per (previous state, incoming event) → expected new state. This is where bugs would live, so coverage here matters.
- **`internal/ingest/`** — integration test that opens a real WS to a test server and asserts DB rows.
- **Poller** — feed a synthetic JSONL file in a temp dir and assert WS messages.
- **Frontend** — Vitest for the WS hook and the time-formatting helpers. No full e2e in this iteration.

## What we deliberately don't do

- No auth, no TLS. Local only.
- No agent control. Read-only.
- No long-term analytics. SQLite holds current + recent.
- No multi-host. Single poller, single backend, single user.
