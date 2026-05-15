# CLAUDE.md

Guidance for Claude Code working in this repository.

## Project Overview

**supervAIsor** is a mobile-first, Cyberpunk 2077-themed dashboard for monitoring local Claude Code sessions. One or more host-native pollers tail Claude's JSONL session files on different machines and ship events to a single backend, which derives session state and broadcasts updates to a web UI.

It is mostly **read-only monitoring** — it does not spawn or control sessions. The one user-initiated write is **deletion**: the dashboard can request a poller to remove a specific session's `.jsonl` from disk, gated by a per-host liveness LED.

## Architecture

```
poller (mac A) ──ws──┐
poller (mac B) ──ws──┼──► backend (Docker)  ◄──ws──  frontend (Docker)
poller (mac C) ──ws──┘   └─ SQLite (volume)
```

Three processes:

- **`poller/`** — host-native Go binary. Tails `~/.claude/projects/*/*.jsonl`, persists per-file read offsets in `~/.supervaisor/state.json`, ships envelopes over `ws://backend/ws/ingest`. Reads runtime settings from `~/.supervaisor/settings.json`, which it materializes with defaults on first run. Must run on the host (not in a container) for filesystem access. Each poller stamps every envelope with its `hostname`; multiple pollers on different machines can target the same backend concurrently.
- **`backend/`** — Go (Gin + gorilla/websocket). Endpoints: `GET /healthz`, `GET /sessions`, `GET /sessions/:hostname/:id/events`, `GET /sessions/:hostname/:id/stats`, `DELETE /sessions/:hostname/:id`, `WS /ws/ingest` (multi-writer, bidirectional — accepts envelopes and dispatches `delete` commands), `WS /ws/clients` (fan-out for session updates, poller liveness, and removal events). Pure state derivation lives in `internal/state/`. Persistence via SQLite (`modernc.org/sqlite`, no CGO) with WAL + `busy_timeout` to tolerate concurrent writers. Sessions are keyed by composite `(hostname, session_id)`.
- **`frontend/`** — React 19 + Vite + TypeScript + Tailwind. Cyberpunk 2077 palette (cyan `#00f0ff`, yellow `#fcee0a`, red `#ff003c` on black). Mobile-first; cards ≥160 px; touch targets ≥44 px. Cards render `name@hostname` with the `@` and hostname color-separated for legibility. The conversation modal live-refreshes whenever the underlying session ticks forward (via the WS-driven `last_event_at` prop).

Session status values (derived in `internal/state/derive.go`):

| Status | Trigger |
|---|---|
| `working` | pending tool_use OR last event < 2s ago (debounce) |
| `done` | turn finished, last event ≤ 1h ago |
| `stale` | last event > 1h ago |

## Development Commands

### One-shot

```bash
make up              # docker compose: backend + frontend
make poller          # run the host poller (foreground)
make down            # stop containers
make test            # backend + poller + frontend tests
```

### Backend (Go)

```bash
cd backend
go test ./...                # unit + integration tests
DB_PATH=/tmp/sv.db go run ./cmd/server
```

Env vars: `PORT` (default `8080`), `DB_PATH` (default `/var/lib/supervaisor/data.db`).

### Poller (Go, host)

```bash
cd poller
go run ./cmd/poller   # or: make poller-install && supervaisor-poller
```

No flags, no env vars. The poller reads every setting from `~/.supervaisor/settings.json`. On first run it writes the defaults to disk so the operator has a file to edit:

```json
{
  "projects_dir": "/Users/you/.claude/projects",
  "state_file":   "/Users/you/.supervaisor/state.json",
  "backend_host": "localhost",
  "backend_port": 8080,
  "backend_url":  "",
  "hostname":     "",
  "interval":     "1s"
}
```

`-hostname` defaults to the OS hostname with a trailing `.local` stripped (macOS-friendly). Multiple pollers on different machines can target the same backend concurrently.

### Frontend (React/Vite)

```bash
cd frontend
npm install
npm run dev    # http://localhost:5173
npm run build
npx vitest run
```

`VITE_BACKEND_WS` env var overrides the WS URL (default `ws://localhost:8080/ws/clients`).

## Implementation Notes

- **No auth.** Single-user, local-network-only.
- **Ingest is multi-writer.** Any number of pollers can connect simultaneously; envelopes with empty `hostname` are dropped server-side.
- **Sessions are keyed by `(hostname, session_id)`.** Same Claude UUID on different machines is two distinct rows. The store API takes `hostname` as the first key arg: `GetSession(ctx, hostname, id)`, `AppendEvent(ctx, hostname, sessionID, ...)`, `ListEvents(ctx, hostname, sessionID, limit)`.
- **State derivation is pure** (`internal/state/derive.go`). The ingest handler is the only place that calls `Apply()` and `RecomputeStatus()`. `Apply` sets `Session.Hostname` from the envelope on the first event for a session and never overwrites it.
- The frontend recomputes elapsed-time counters on a 1 s tick using `started_at` / `last_prompt_at` from the backend — the backend does not push ticks.
- The frontend keys session lists by `${hostname}:${id}` so two machines with the same Claude UUID don't collide visually.
- SQLite is opened with WAL + `busy_timeout` and `SetMaxOpenConns(1)` to handle concurrent ingest writers without `SQLITE_BUSY`.
- Distroless backend container runs as root because the named volume's mount overrides UID ownership.

## Project Layout

```
backend/       — Go server, internal/{state,events,store,broadcast,ingest,api}
poller/        — Go binary, internal/{offsets,tailer,wsclient,scanner}
frontend/      — Vite + React + Tailwind
infrastructure/— backend.Dockerfile + docker-compose.yml
docs/          — superpowers/specs/ + superpowers/plans/
```

## Design and Plan Documents

- Initial spec: `docs/superpowers/specs/2026-05-14-cyberpunk-session-monitor-design.md`
- Initial plan: `docs/superpowers/plans/2026-05-14-cyberpunk-session-monitor.md`
- Frontend improvements spec: `docs/superpowers/specs/2026-05-14-frontend-improvements-design.md`
- Frontend improvements plan: `docs/superpowers/plans/2026-05-14-frontend-improvements.md`
- Multi-machine pollers spec: `docs/superpowers/specs/2026-05-14-multi-machine-pollers-design.md`
- Multi-machine pollers plan: `docs/superpowers/plans/2026-05-14-multi-machine-pollers.md`
