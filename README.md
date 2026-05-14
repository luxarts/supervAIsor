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

    poller (mac A) ──ws──┐
    poller (mac B) ──ws──┼──► backend (Docker)  ◄──ws──  frontend (Docker)
    poller (mac C) ──ws──┘   └─ SQLite (volume)

Backend and frontend run on a single machine. One or more pollers can run on different machines and feed the same backend concurrently. Each poller stamps every event with its `hostname`; sessions are keyed by `(hostname, session_id)` so the same Claude UUID can coexist across machines. Cards show the session as `name@hostname`.

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

**Backend env vars:** `PORT` (default `8080`), `DB_PATH` (default `/var/lib/supervaisor/data.db`, persisted via Docker volume).

**Frontend env:** `VITE_BACKEND_WS` (default `ws://localhost:8080/ws/clients`).

**Poller** — both flags and env vars are supported. Flags override env, env overrides defaults.

| Flag             | Env var                       | Default                              | Description                                  |
|------------------|-------------------------------|--------------------------------------|----------------------------------------------|
| `-backend-host`  | `SUPERVAISOR_BACKEND_HOST`    | `localhost`                          | Backend host                                 |
| `-backend-port`  | `SUPERVAISOR_BACKEND_PORT`    | `8080`                               | Backend port                                 |
| `-backend`       | `SUPERVAISOR_BACKEND_URL`     | (derived from host+port)             | Full WS URL override                         |
| `-hostname`      | `SUPERVAISOR_HOSTNAME`        | OS hostname (with `.local` stripped) | Machine tag shown after `@` in each card     |
| `-projects-dir`  | `SUPERVAISOR_PROJECTS_DIR`    | `~/.claude/projects`                 | Claude projects dir                          |
| `-state-file`    | `SUPERVAISOR_STATE_FILE`      | `~/.supervAIsor/poller-state.json`   | File offsets state                           |
| `-interval`      | `SUPERVAISOR_INTERVAL`        | `1s`                                 | Poll interval                                |

Example — point a remote poller at a backend on another machine:

```bash
SUPERVAISOR_BACKEND_HOST=10.0.0.5 SUPERVAISOR_HOSTNAME=mac-A make poller
```

No auth — this is a single-user, local-network-only instance.

## Design + plans

- Initial spec: [`docs/superpowers/specs/2026-05-14-cyberpunk-session-monitor-design.md`](docs/superpowers/specs/2026-05-14-cyberpunk-session-monitor-design.md)
- Initial plan: [`docs/superpowers/plans/2026-05-14-cyberpunk-session-monitor.md`](docs/superpowers/plans/2026-05-14-cyberpunk-session-monitor.md)
- Multi-machine pollers spec: [`docs/superpowers/specs/2026-05-14-multi-machine-pollers-design.md`](docs/superpowers/specs/2026-05-14-multi-machine-pollers-design.md)
- Multi-machine pollers plan: [`docs/superpowers/plans/2026-05-14-multi-machine-pollers.md`](docs/superpowers/plans/2026-05-14-multi-machine-pollers.md)
