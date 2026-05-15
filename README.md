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

There are two ways to run supervAIsor: **local dev** (fastest, runs everything on one machine without Docker) and **Docker** (production-style, what you'd deploy on a home server).

### Option A — Local dev (recommended to try it out)

Prereqs: Go ≥ 1.22, Node ≥ 20, npm.

In three terminals, from the repo root:

```bash
# 1) backend on :8080
cd backend && DB_PATH=/tmp/supervaisor.db go run ./cmd/server

# 2) frontend on :5173 (proxies WS to :8080)
cd frontend && npm install && npm run dev

# 3) poller on the host (same machine here)
make poller
```

Open <http://localhost:5173>. The poller will auto-write `~/.supervaisor/settings.json` with `backend: localhost:8080` on first run.

### Option B — Docker (backend + frontend in containers)

Prereqs: Docker, and an external Docker network named `traefik_default` with a Traefik instance routing `/supervaisor` to the stack. The compose file does **not** publish host ports — it's designed to sit behind Traefik.

```bash
make up      # docker compose up backend + frontend
make logs    # tail logs
make down    # stop
```

Then visit `http://<your-traefik-host>/supervaisor`. If you don't have Traefik, use Option A or edit `infrastructure/docker-compose.yml` to add `ports:` mappings (`8080:8080` for backend, `80:80` for frontend) and remove the Traefik labels.

### Installing the poller on each monitored machine

On every machine where Claude Code runs, install the poller as a background service:

```bash
curl -fsSL https://raw.githubusercontent.com/luxarts/supervAIsor/main/install.sh | bash
```

The installer:
1. Downloads the latest poller binary to `~/.local/bin/supervaisor`.
2. Prompts for the backend (e.g. `192.168.1.10:8080` or `dashboard.local/supervaisor`).
3. Writes `~/.supervaisor/settings.json`.
4. Calls `supervaisor install` — registers a native service (launchd on macOS, systemd `--user` on Linux) that auto-starts at login and restarts on crash.

Skip the prompt non-interactively:

```bash
SUPERVAISOR_BACKEND=192.168.1.10:8080 bash install.sh
```

Make sure `~/.local/bin` is on your `PATH`. Then:

```bash
supervaisor status      # report service state
supervaisor start       # start the service
supervaisor stop        # stop the service
supervaisor restart     # restart the service (use this after editing settings.json)
supervaisor install     # (re)register the unit file and start
supervaisor uninstall   # stop, remove unit + binary + ~/.supervaisor/ (asks for confirmation)
supervaisor             # foreground mode (for debugging)
```

Logs go to `~/.supervaisor/poller.log`. The OS service manager owns process supervision — there is no PID file.

To use the dashboard from your phone, point its browser at `http://<server-lan-ip>:5173` (dev) or `http://<server-lan-ip>/supervaisor` (Docker + Traefik).

### Building the poller from source

If you'd rather not curl the install script:

```bash
make poller-build    # produces poller/supervaisor
./poller/supervaisor # run in foreground
```

The binary materializes `~/.supervaisor/settings.json` with sensible defaults on first run.

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
| `dashboard.local/supervaisor`   | `ws://dashboard.local/supervaisor/ws/ingest`       |

After editing settings, restart the poller with `supervaisor restart` (or re-run the binary if you're using the foreground/manual path).

No auth — this is a single-user, local-network-only instance.

## Design + plans

- Initial spec: [`docs/superpowers/specs/2026-05-14-cyberpunk-session-monitor-design.md`](docs/superpowers/specs/2026-05-14-cyberpunk-session-monitor-design.md)
- Initial plan: [`docs/superpowers/plans/2026-05-14-cyberpunk-session-monitor.md`](docs/superpowers/plans/2026-05-14-cyberpunk-session-monitor.md)
- Multi-machine pollers spec: [`docs/superpowers/specs/2026-05-14-multi-machine-pollers-design.md`](docs/superpowers/specs/2026-05-14-multi-machine-pollers-design.md)
- Multi-machine pollers plan: [`docs/superpowers/plans/2026-05-14-multi-machine-pollers.md`](docs/superpowers/plans/2026-05-14-multi-machine-pollers.md)
