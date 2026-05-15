# Status Semantics & Session Stats — Design

**Date:** 2026-05-14
**Scope:** Spec #1 of 3 in the "interactions" milestone.
**Sibling specs:** `session-interactions` (pin/sort/notifications/header counters/error highlight), `session-deletion` (delete + poller LED).

## Problem

Today the backend derives four session statuses (`working`, `waiting_input`, `idle`, `stale`) from time-since-last-event. Three of them mean the same thing in practice — "Claude finished its turn" — distinguished only by age. Users can't quickly tell whether a session is **doing something** or **done**, which defeats the dashboard's purpose.

In addition, the conversation modal exposes only the message stream. There is no place to see useful metadata that already exists in the JSONL (token usage, model, durations, tool-call breakdown, project full path, started_at), so users have to read the raw logs to understand a session's footprint.

## Goals

1. Status model collapses to **three** semantically distinct values: `WORKING`, `DONE`, `STALE`.
2. The conversation modal grows a **DETAILS** tab in-place (no new route, no separate page) showing project, host, timeline, tokens, model, and tool breakdown.
3. A new backend endpoint computes per-session aggregates on demand from stored events (no schema migration for derived data).

## Non-Goals

- No URL routing changes. The app remains a single-page SPA without React Router.
- No new persisted columns for derived stats — recomputed from events on each request. Cheap because events are already in SQLite and a session's event count is small (hundreds, not millions).
- No pin/sort/notifications/delete here — those live in the sibling specs.

## Status Model

### New states

| Status | Trigger | Card color (cyberpunk palette) |
|---|---|---|
| `WORKING` | `len(PendingToolUseIDs) > 0` **OR** time since last event < 2s | cyan `#00f0ff` |
| `DONE` | not `WORKING`, and time since last event ≤ 1h | yellow `#fcee0a` |
| `STALE` | time since last event > 1h | dim/red `#ff003c` border, muted fill |

The 2-s window for `WORKING` debounces the gap between an assistant text chunk and the next `tool_use` so the card doesn't flicker `DONE→WORKING` mid-turn.

### Removed states

`waiting_input` and `idle` are gone. The age of a `DONE` session is shown as a subtitle on the card (`DONE · 12s`, `DONE · 4m`, `DONE · 47m`) computed on the frontend's 1-s tick from `last_event_at`.

### Backend changes (`internal/state/`)

- `Status` constants: keep `StatusWorking` and `StatusStale`, rename `StatusWaitingInput` → `StatusDone`, delete `StatusIdle`.
- `RecomputeStatus(s, now)`:
  ```
  if len(PendingToolUseIDs) > 0 || now.Sub(LastEventAt) < 2s → WORKING
  else if now.Sub(LastEventAt) > 1h                          → STALE
  else                                                       → DONE
  ```
- `Apply()` no longer sets `WaitingInput` directly; after applying an event it falls through to `RecomputeStatus`.
- All existing tests in `internal/state/` updated to the new vocabulary. Add tests covering the 2-s `WORKING` debounce and the `DONE→STALE` boundary.

### Frontend changes

- `frontend/src/types.ts`: union `"working" | "done" | "stale"`.
- `StatusBadge.tsx`: three variants. Subtitle text rendered by the card, not the badge.
- `SessionCard.tsx`: shows `STATUS · <age>` where `<age>` is `'just now' | '<N>s' | '<N>m' | '<N>h'` derived from `last_event_at` on the existing 1-s tick.
- "Hide stale" toggle keeps working as today.

## Stats Endpoint

### Route

```
GET /sessions/:hostname/:id/stats
```

### Response shape

```json
{
  "hostname": "mac-A",
  "id": "abc-123",
  "name": "fix login bug",
  "project": "/Users/x/Projects/foo",
  "model": "claude-opus-4-7",
  "started_at": "2026-05-14T09:12:03Z",
  "last_event_at": "2026-05-14T10:47:55Z",
  "wall_clock_seconds": 5752,
  "tokens": {
    "input": 12450,
    "output": 8732,
    "cache_creation": 4200,
    "cache_read": 91020
  },
  "counts": {
    "user_prompts": 7,
    "assistant_turns": 14,
    "tool_calls": 63,
    "errors": 2
  },
  "tool_breakdown": {
    "Bash": 12,
    "Read": 28,
    "Edit": 9,
    "Grep": 6,
    "Task": 2,
    "Write": 4,
    "Glob": 2
  }
}
```

### Computation (backend, `internal/state/stats.go`)

Pure function `ComputeStats(events []RawLine, session Session) Stats`:

- Iterate stored events from `store.ListEvents(ctx, hostname, id, 0)` (0 = no limit).
- `tokens`: sum `message.usage.{input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens}` across all `assistant` messages.
- `model`: take from the most recent `assistant` message's `message.model`.
- `user_prompts`: count `user` events whose content has at least one non-`tool_result` text block.
- `assistant_turns`: count `assistant` events.
- `tool_calls`: sum of `tool_use` blocks across all `assistant` events.
- `errors`: count `tool_result` blocks where `is_error == true` (live in `user` events).
- `tool_breakdown`: map of tool `name → count` from `tool_use` blocks.
- `wall_clock_seconds`: `last_event_at - started_at`.

### Event type extensions (`internal/events/types.go`)

The `RawLine` struct currently parses what derive needs. Extend `Message` and `ContentBlock` to surface:

```go
type Message struct {
    Role    string         `json:"role"`
    Model   string         `json:"model,omitempty"`
    Content []ContentBlock `json:"content"`
    Usage   *Usage         `json:"usage,omitempty"`
}

type Usage struct {
    InputTokens              int `json:"input_tokens"`
    OutputTokens             int `json:"output_tokens"`
    CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
    CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type ContentBlock struct {
    // existing fields…
    IsError bool `json:"is_error,omitempty"`
}
```

These are additive; existing decoding paths are unaffected.

### API wiring (`internal/api/sessions.go`)

Register the route, parameter-validate `hostname` and `id`, return 404 if `GetSession` returns `nil`. Body computed inline; no caching (one request per modal open is cheap).

## Frontend: DETAILS Tab in Modal

`ConversationModal.tsx` grows a tab strip at the top:

```
┌────────────────────────────────────────────┐
│  CONVERSATION  │  DETAILS    [×]           │
├────────────────────────────────────────────┤
│  (existing event stream)  /  (stats view)  │
└────────────────────────────────────────────┘
```

State:
- `const [tab, setTab] = useState<'conversation' | 'details'>('conversation')`.
- When `tab === 'details'`, fetch `${HTTP_BASE}/sessions/${hostname}/${id}/stats` once on tab activation and on every change of the `last_event_at` prop (so the live tick refreshes the numbers — same pattern the conversation tab already uses).

Layout (mobile-first, single column, ≥ 44 px touch targets):

```
PROJECT      /Users/x/Projects/foo
HOST         mac-A
MODEL        claude-opus-4-7
STARTED      2026-05-14 09:12:03  (1h 35m ago)
LAST EVENT   2026-05-14 10:47:55  (12s ago)
DURATION     1h 35m 49s

TOKENS
  Input            12,450
  Output            8,732
  Cache create      4,200
  Cache read       91,020

COUNTS
  User prompts        7
  Assistant turns    14
  Tool calls         63
  Errors              2

TOOL BREAKDOWN
  Read   ████████████████ 28
  Bash   ███████          12
  Edit   █████             9
  Grep   ███               6
  Write  ██                4
  Task   █                 2
  Glob   █                 2
```

Bars are pure CSS using percentage of the max count; no chart library.

Pin toggle and notify toggle (specified in `session-interactions`) and Delete button (specified in `session-deletion`) will be added to this DETAILS view by their respective specs. This spec leaves room for them — a `settings` and `danger` section is reserved at the bottom of the layout but renders empty until those specs ship.

## Testing

### Backend

- `internal/state/derive_test.go`: rename existing assertions; add cases for the new 2-s `WORKING` debounce and exact `DONE/STALE` boundary semantics at 1h ± 1s.
- `internal/state/stats_test.go` (new): table-driven test feeding hand-crafted `[]RawLine` slices and asserting the computed `Stats` struct, including: empty events, tokens with missing usage, mixed user/assistant/tool_use/tool_result, error counting, model picked from latest assistant.
- `internal/api/sessions_test.go`: add `/stats` 200 + 404 cases.

### Frontend

- `ConversationModal.test.tsx`: add coverage for tab switching and that `DETAILS` fetches `/stats` once and re-fetches when `lastEventAt` changes.
- `SessionCard.test.tsx`: assert the `DONE · <age>` subtitle renders and ticks.

## Migration & Rollout

- Status enum change is a breaking wire-format change for the WS clients channel. Both backend and frontend ship together. No DB migration needed (status is recomputed on every request and never persisted in long-lived form — see `RecomputeStatus`).
- Stored events keep their JSON; new fields are read additively.
- No poller change.

## File Inventory

**Modified:**
- `backend/internal/state/types.go` — status enum
- `backend/internal/state/derive.go` — 3-state derivation
- `backend/internal/state/derive_test.go`
- `backend/internal/events/types.go` — `Usage`, `Model`, `IsError` fields
- `backend/internal/api/sessions.go` — register `/stats`
- `backend/internal/api/sessions_test.go`
- `frontend/src/types.ts` — status union
- `frontend/src/components/StatusBadge.tsx`
- `frontend/src/components/SessionCard.tsx` — age subtitle + 1-s tick
- `frontend/src/components/SessionCard.test.tsx`
- `frontend/src/components/ConversationModal.tsx` — tab strip + details view
- `frontend/src/components/ConversationModal.test.tsx`

**New:**
- `backend/internal/state/stats.go`
- `backend/internal/state/stats_test.go`

## Open Questions

None. Decisions taken inline.
