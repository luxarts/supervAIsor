# supervAIsor — Backend

Go HTTP server that manages Claude Code agents, receives real-time events via hooks, and streams state to the frontend over WebSockets.

## Stack

- **Go 1.23+**
- **Gin Gonic** — HTTP router and middleware
- **gorilla/websocket** — WebSocket hub
- In-memory state (no database in v1)

## Getting Started

```bash
cd backend
go mod tidy
go run ./cmd/server
# Server starts on :8080
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Port the server listens on |
| `HOOK_SECRET` | *(empty)* | Optional shared secret to validate incoming hook payloads |

## API Endpoints

### Agents

#### `GET /agents`
Returns the list of all registered agents and their current state.

**Response**
```json
[
  {
    "id": "uuid",
    "name": "backend-dev",
    "type": "developer",
    "status": "working",
    "current_tool": "Write",
    "work_dir": "/path/to/project",
    "pid": 12345,
    "created_at": "2026-03-04T10:00:00Z"
  }
]
```

---

#### `POST /agents`
Spawns a new Claude Code agent subprocess.

**Request body**
```json
{
  "name": "backend-dev",
  "type": "developer",
  "work_dir": "/path/to/project",
  "prompt": "Implement the user authentication module"
}
```

**Response** — the created agent object (same shape as GET /agents item).

The backend:
1. Writes a hook configuration so Claude Code calls `POST /hooks` on every event.
2. Spawns `claude` as a subprocess in `work_dir`.
3. Registers the agent in the in-memory store.

---

#### `DELETE /agents/:id`
Kills the agent subprocess and removes it from the store.

**Response** `204 No Content`

---

### Hooks (internal — called by Claude Code)

#### `POST /hooks`
Receives lifecycle events from Claude Code hook callbacks. Not meant to be called manually.

**Payload shape** (set by Claude Code):
```json
{
  "agent_id": "uuid",
  "event": "PostToolUse",
  "tool": "Write",
  "input": { ... },
  "output": { ... }
}
```

Supported `event` values:
- `PreToolUse` — agent is about to call a tool
- `PostToolUse` — tool returned a result
- `Notification` — informational message from the agent
- `Stop` — agent session ended

Each event updates the in-memory store and is broadcast to all connected WebSocket clients.

---

### WebSocket

#### `GET /ws`
Upgrades the connection to WebSocket. The frontend connects here to receive real-time agent events.

**Server → Client messages**
```json
{
  "type": "AGENT_UPDATED",
  "payload": { /* agent object */ }
}
```

Event types:
- `AGENT_CREATED` — new agent spawned
- `AGENT_UPDATED` — status or tool changed
- `AGENT_STOPPED` — agent subprocess exited

**Client → Server messages**

The frontend can also send commands over the same socket:
```json
{ "type": "CREATE_AGENT", "payload": { ... } }
{ "type": "STOP_AGENT",   "payload": { "id": "uuid" } }
```

These are forwarded to the same handlers as the REST API.

## Claude Code Hook Configuration

When the backend spawns an agent it automatically injects hook settings so Claude Code reports back. The generated config (written to the agent's Claude config dir) looks like:

```json
{
  "hooks": {
    "PreToolUse":  [{ "type": "http", "url": "http://localhost:8080/hooks" }],
    "PostToolUse": [{ "type": "http", "url": "http://localhost:8080/hooks" }],
    "Notification":[{ "type": "http", "url": "http://localhost:8080/hooks" }],
    "Stop":        [{ "type": "http", "url": "http://localhost:8080/hooks" }]
  }
}
```

If you change `PORT`, update the hook URL accordingly (or set `HOOK_BASE_URL` env var — to be implemented).

## Project Structure

```
backend/
├── cmd/server/main.go          # Entry point, wires up Gin + hub
├── internal/
│   ├── agent/
│   │   ├── model.go            # Agent struct, Status/Type enums
│   │   └── manager.go          # Spawn/kill claude subprocesses
│   ├── state/
│   │   └── store.go            # Thread-safe in-memory agent store
│   ├── hooks/
│   │   └── handler.go          # Gin handler for POST /hooks
│   ├── ws/
│   │   ├── hub.go              # Broadcast goroutine
│   │   └── client.go           # One goroutine pair per WS connection
│   └── api/
│       └── handler.go          # Gin handlers for /agents routes
├── go.mod
└── go.sum
```
