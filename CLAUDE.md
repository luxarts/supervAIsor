# CLAUDE.md

Guidance for Claude Code working in this repository.

## Project Overview

**supervAIsor** is a mobile-first, Cyberpunk 2077-themed dashboard for monitoring local Claude Code sessions. A host-native poller tails Claude's JSONL session files, ships events to a backend, which derives session state and broadcasts updates to a web UI.

It is **read-only monitoring** — it does not spawn, kill, or control sessions.

## Architecture

```
host poller  ──ws──►  backend (Docker)  ◄──ws──  frontend (Docker)
                    └─ SQLite (volume)
```

Three processes:

- **`poller/`** — host-native Go binary. Tails `~/.claude/projects/*/*.jsonl`, persists per-file read offsets in `~/.supervAIsor/poller-state.json`, ships envelopes over `ws://backend/ws/ingest`. Must run on the host (not in a container) for filesystem access.
- **`backend/`** — Go (Gin + gorilla/websocket). Endpoints: `GET /healthz`, `GET /sessions`, `WS /ws/ingest` (single-writer), `WS /ws/clients` (fan-out). Pure state derivation lives in `internal/state/`. Persistence via SQLite (`modernc.org/sqlite`, no CGO).
- **`frontend/`** — React 19 + Vite + TypeScript + Tailwind. Cyberpunk 2077 palette (cyan `#00f0ff`, yellow `#fcee0a`, red `#ff003c` on black). Mobile-first; cards ≥160 px; touch targets ≥44 px.

Session status values (derived in `internal/state/derive.go`):

| Status | Trigger |
|---|---|
| `working` | unmatched `tool_use` is pending |
| `waiting_input` | last event < 30 s ago, no pending tool |
| `idle` | last event between 30 s and 1 h ago |
| `stale` | last event > 1 h ago |

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

Flags: `-projects-dir`, `-state-file`, `-backend-host`, `-backend-port`, `-backend` (full WS URL override), `-hostname`, `-interval`.

Env vars (override defaults; flags still take precedence over env): `SUPERVAISOR_PROJECTS_DIR`, `SUPERVAISOR_STATE_FILE`, `SUPERVAISOR_BACKEND_HOST`, `SUPERVAISOR_BACKEND_PORT`, `SUPERVAISOR_BACKEND_URL`, `SUPERVAISOR_HOSTNAME`, `SUPERVAISOR_INTERVAL`. Example: `SUPERVAISOR_BACKEND_HOST=10.0.0.5 SUPERVAISOR_HOSTNAME=mac-A make poller`.

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

- **No auth.** Single-user, local-only.
- **Ingest is single-writer.** The backend rejects a second poller with HTTP 409.
- **State derivation is pure** (`internal/state/derive.go`). The ingest handler is the only place that calls `Apply()` and `RecomputeStatus()`.
- The frontend recomputes elapsed-time counters on a 1 s tick using `started_at` / `last_prompt_at` from the backend — the backend does not push ticks.
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

- Spec: `docs/superpowers/specs/2026-05-14-cyberpunk-session-monitor-design.md`
- Plan: `docs/superpowers/plans/2026-05-14-cyberpunk-session-monitor.md`
