# supervAIsor

Mobile-first Cyberpunk 2077-themed dashboard for monitoring local Claude Code sessions.

## What it does

Watches every Claude Code session running on your machine and shows them as live cards on a phone-friendly web UI. For each session you see:

- name + project
- status: `working` / `done` / `stale`
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

On the **server** machine (runs backend + frontend):

```bash
make up              # starts backend + frontend in Docker
open http://localhost:5173
```

On every **monitored** machine (where Claude Code runs):

```bash
curl -fsSL https://raw.githubusercontent.com/luxarts/supervAIsor/main/install.sh | bash
```

The installer downloads the latest poller binary, prompts you for the backend (e.g. `localhost:8080` or `mmm4p.local/supervaisor`), writes `~/.supervaisor/settings.json`, and launches the poller in the background. To skip the prompt, set `SUPERVAISOR_BACKEND=...` before piping.

To use it from your phone, point your browser at `http://<your-server-lan-ip>:5173`.

## Installing the poller manually

If you'd rather build from source on the same machine as the backend:

```bash
make poller-build    # produces poller/supervaisor
./poller/supervaisor # foreground
```

Both paths read settings from `~/.supervaisor/settings.json`, which the binary materializes with defaults on first run if it's missing.

## Make targets

| Command | Action |
|---|---|
| `make up` | docker compose up backend + frontend |
| `make down` | stop containers |
| `make poller` | run the host poller in the foreground |
| `make poller-build` | build the poller binary at `poller/supervaisor` |
| `make poller-install` | build and install poller to `~/.local/bin` |
| `make test` | run all tests (backend, poller, frontend) |
| `make logs` | tail docker compose logs |

## Status semantics

| Status | Meaning |
|---|---|
| `working` | Claude is actively running a tool, or a new event arrived in the last 2 s (debounce) |
| `done` | Turn finished, no activity for up to 1 h |
| `stale` | No activity for over 1 h |

## Configuration

**Backend env vars:** `PORT` (default `8080`), `DB_PATH` (default `/var/lib/supervaisor/data.db`, persisted via Docker volume).

**Frontend env:** `VITE_BACKEND_WS` (default `ws://localhost:8080/ws/clients`).

**Poller** — runs as a native per-user service. No flags or env vars; everything is read from `~/.supervaisor/settings.json`, which the binary creates on first run.

```json
{
  "projects_dir": "/Users/you/.claude/projects",
  "state_file":   "/Users/you/.supervaisor/state.json",
  "backend":      "localhost:8080",
  "hostname":     "",
  "interval":     "1s"
}
```

| Field          | Default                              | Description                                                                 |
|----------------|--------------------------------------|-----------------------------------------------------------------------------|
| `projects_dir` | `~/.claude/projects`                 | Where to find Claude session JSONL files                                    |
| `state_file`   | `~/.supervaisor/state.json`          | Per-file read offsets (so restarts don't replay history)                    |
| `backend`      | `localhost:8080`                     | `host[:port][/path]` — poller prepends `ws://` and appends `/ws/ingest`     |
| `hostname`     | OS hostname (with `.local` stripped) | Machine tag shown after `@` in each card                                    |
| `interval`     | `1s`                                 | Poll interval                                                               |

Examples of `backend`:

| Value                       | Resolved WS URL                                |
|-----------------------------|------------------------------------------------|
| `localhost:8080`            | `ws://localhost:8080/ws/ingest`                |
| `10.0.0.5:8080`             | `ws://10.0.0.5:8080/ws/ingest`                 |
| `mmm4p.local/supervaisor`   | `ws://mmm4p.local/supervaisor/ws/ingest`       |

After editing settings, restart the poller (`kill $(cat ~/.supervaisor/poller.pid) && bash <(curl -fsSL https://raw.githubusercontent.com/luxarts/supervAIsor/main/install.sh)` re-launches with the new values; or run the binary manually).

No auth — this is a single-user, local-network-only instance.

## Design + plans

- Initial spec: [`docs/superpowers/specs/2026-05-14-cyberpunk-session-monitor-design.md`](docs/superpowers/specs/2026-05-14-cyberpunk-session-monitor-design.md)
- Initial plan: [`docs/superpowers/plans/2026-05-14-cyberpunk-session-monitor.md`](docs/superpowers/plans/2026-05-14-cyberpunk-session-monitor.md)
- Multi-machine pollers spec: [`docs/superpowers/specs/2026-05-14-multi-machine-pollers-design.md`](docs/superpowers/specs/2026-05-14-multi-machine-pollers-design.md)
- Multi-machine pollers plan: [`docs/superpowers/plans/2026-05-14-multi-machine-pollers.md`](docs/superpowers/plans/2026-05-14-multi-machine-pollers.md)
