# supervAIsor

Mobile-first Cyberpunk 2077-themed dashboard for monitoring local Claude Code sessions.

## Architecture

    host poller  ──ws──►  backend (Docker)  ◄──ws──  frontend (Docker)
                        └─ SQLite (volume)

- **`poller/`** — host-native Go binary; tails `~/.claude/projects/**/*.jsonl`.
- **`backend/`** — Go (Gin + WS), derives session state, persists to SQLite, broadcasts to clients.
- **`frontend/`** — React + Vite, Cyberpunk 2077 theme, mobile-first.
- **`infrastructure/`** — Docker Compose.

## Quickstart

```bash
make up              # starts backend + frontend
make poller-install  # installs poller into ~/.local/bin
supervaisor-poller   # run it
open http://localhost:5173
```
