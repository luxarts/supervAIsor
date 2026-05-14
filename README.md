# supervAIsor

Mobile-first Cyberpunk 2077-themed dashboard for monitoring local Claude Code sessions.

## What it does

Watches every Claude Code session running on your machine and shows them as live cards on a phone-friendly web UI. For each session you see:

- name + project
- status: `working` / `waiting_input` / `idle` / `stale`
- current action (e.g., `Write: src/foo.go`)
- total elapsed time and current-prompt elapsed time

Read-only — supervAIsor does not spawn, kill, or interact with sessions.

## Architecture

    host poller  ──ws──►  backend (Docker)  ◄──ws──  frontend (Docker)
                        └─ SQLite (volume)

- **`poller/`** — host-native Go binary; tails `~/.claude/projects/*/*.jsonl` and ships events over WebSocket.
- **`backend/`** — Go (Gin + gorilla/websocket). Derives session state, persists to SQLite, broadcasts updates.
- **`frontend/`** — React + Vite + Tailwind. Cyberpunk 2077 palette, mobile-first, touch-friendly.
- **`infrastructure/`** — Dockerfiles + `docker-compose.yml`.

## Quickstart

```bash
make up              # starts backend + frontend in Docker
make poller-install  # installs poller into ~/.local/bin
supervaisor-poller   # run it (foreground)
open http://localhost:5173
```

To use it from your phone, point your browser at `http://<your-mac-lan-ip>:5173`.

## Make targets

| Command | Action |
|---|---|
| `make up` | docker compose up backend + frontend |
| `make down` | stop containers |
| `make poller` | run the host poller in the foreground |
| `make poller-install` | build and install poller to `~/.local/bin` |
| `make test` | run all tests (backend, poller, frontend) |
| `make logs` | tail docker compose logs |

## Status semantics

| Status | Meaning |
|---|---|
| `working` | Claude is currently running a tool (unmatched `tool_use`) |
| `waiting_input` | Claude finished a turn within the last 30 s, waiting on you |
| `idle` | No activity for 30 s – 1 h |
| `stale` | No activity for over 1 h |

## Configuration

Backend env vars: `PORT` (default `8080`), `DB_PATH` (default `/var/lib/supervaisor/data.db`, persisted via Docker volume).
Frontend env: `VITE_BACKEND_WS` (default `ws://localhost:8080/ws/clients`).
Poller flags: `-projects-dir`, `-state-file`, `-backend`, `-interval`.

No auth — this is a single-user, local-only instance.

## Design + plan

- Spec: [`docs/superpowers/specs/2026-05-14-cyberpunk-session-monitor-design.md`](docs/superpowers/specs/2026-05-14-cyberpunk-session-monitor-design.md)
- Plan: [`docs/superpowers/plans/2026-05-14-cyberpunk-session-monitor.md`](docs/superpowers/plans/2026-05-14-cyberpunk-session-monitor.md)
