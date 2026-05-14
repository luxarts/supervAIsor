# supervAIsor — Backend

Go server: SQLite-backed session state, WS ingest from the host poller, WS broadcast to frontends.

## Endpoints

- `GET  /healthz`                              — liveness
- `GET  /sessions`                             — snapshot of derived sessions (from all hosts)
- `GET  /sessions/:hostname/:id/events`        — recent raw JSONL events for a session
- `WS   /ws/ingest`                            — receives envelopes from one or more pollers (one connection per poller)
- `WS   /ws/clients`                           — fan-out; sends snapshot + update + delete frames to clients

Each ingest envelope carries `hostname`, `session_id`, `project_dir`, `file_mtime`, `line_index`, and `raw`. Sessions are keyed by `(hostname, session_id)` so the same Claude UUID can coexist across machines. Envelopes with an empty `hostname` are dropped server-side.

## Env vars

| Var       | Default                          |
|-----------|----------------------------------|
| `PORT`    | `8080`                           |
| `DB_PATH` | `/var/lib/supervaisor/data.db`   |

## Tests

```bash
go test ./...
```
