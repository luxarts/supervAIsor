# supervAIsor — Backend

Go server: SQLite-backed session state, WS ingest from the host poller, WS broadcast to frontends.

## Endpoints

- `GET  /healthz`          — liveness
- `GET  /sessions`         — snapshot of derived sessions
- `WS   /ws/ingest`        — single-writer; receives raw JSONL events from the poller
- `WS   /ws/clients`       — fan-out; sends snapshot + update + delete frames to clients

## Env vars

| Var       | Default                          |
|-----------|----------------------------------|
| `PORT`    | `8080`                           |
| `DB_PATH` | `/var/lib/supervaisor/data.db`   |

## Tests

```bash
go test ./...
```
