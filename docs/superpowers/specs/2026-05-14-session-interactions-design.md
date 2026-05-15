# Session Interactions — Design

**Date:** 2026-05-14
**Scope:** Spec #2 of 3 in the "interactions" milestone.
**Depends on:** `status-semantics-and-stats` (the DETAILS tab is where per-session toggles live, the 3-state model is what notifications observe).

## Problem

The current dashboard treats every session identically and gives the user no levers to surface the ones that matter, no way to know the moment a session finishes a turn without watching the screen, and no warning when a session has hit an error. With multiple machines and dozens of sessions, the user has to scan continuously.

## Goals

1. **Pin** sessions so they always sort to the top.
2. **Sort** the rest by Name, Host, Last update, or Status.
3. **Notifications** when a session transitions `WORKING → DONE`: short cyberpunk beep + 3-pulse yellow→cyan flash on the card. Toggle is per-session, lives in the DETAILS tab.
4. **Header counters** for at-a-glance situational awareness: `▶ N · ✓ N · 💀 N · 🔔 N new`.
5. **Error highlight**: when a session's most recent `tool_result.is_error == true`, the card pulses red once and keeps a persistent red border until the next non-error event.

## Non-Goals

- No backend changes other than what's necessary to surface `last_error_at` (see below). Pin and notify state stays client-side.
- No keyboard shortcuts, no last-prompt-on-card preview, no group-by-host, no sparklines, no cost estimates, no full-text search, no export. Explicitly cut by the user.

## Pin

### State

`localStorage["sv:pinned"]` = JSON-serialized `string[]` of `"hostname:id"` keys.

A typed wrapper module `frontend/src/lib/pins.ts`:

```ts
export function isPinned(key: string): boolean
export function togglePin(key: string): void
export function listPinned(): Set<string>
export function subscribe(fn: () => void): () => void   // emits on toggle
```

`subscribe` lets `App.tsx` rerender when a pin changes from inside the modal.

### UI

- **Card**: small 📌 icon top-right when pinned. Tapping it does **not** toggle (avoid accidental on touch); the only way to toggle is the DETAILS tab control.
- **DETAILS tab**: `📌 Pin to top` row with a switch.

### Sort behavior

Pinned sessions render first, in the order produced by the active sort. Unpinned follow.

## Sort

### Options

`Last update` (default), `Name`, `Host`, `Status` (`WORKING` first → `DONE` → `STALE`).

### State

`localStorage["sv:sort"]` = `'last_update' | 'name' | 'host' | 'status'`.

### UI

`HeaderControls` adds a compact dropdown to the right of the search field, label `SORT ▾`. Single-select chip-style menu.

### Implementation

In `App.tsx`'s `useMemo` filter, after filtering, partition `[pinned, rest]`, sort each by the active key, concat. Tie-break for Name and Host by `last_event_at` desc.

## Notifications

### Per-session toggle

`localStorage["sv:notify"]` = JSON-serialized `string[]` of `"hostname:id"` keys.
Wrapper module `frontend/src/lib/notify.ts` mirroring `pins.ts`.

### UI

- **DETAILS tab**: `🔔 Notify on turn complete` row with a switch. Disabled with a tooltip if the browser denies the Notification permission *and* WebAudio is unavailable; enabling the switch first time triggers the permission prompt.
- **Card**: small 🔔 icon top-right when notify is enabled (next to 📌 if both).

### Trigger

A new hook `useTurnCompletionNotifier(sessions)` in `frontend/src/lib/notify.ts`:

- Keeps a ref `prevStatuses: Map<key, Status>`.
- On every render with a new `sessions` array, for each session whose key is in the notify set:
  - If `prev === 'working'` and `next === 'done'`, fire.
- Updates the ref at end of effect.

### Fire effect

1. **Sound**: WebAudio API, lazy-initialized `AudioContext`. Plays a 150 ms square wave at 880 Hz with a fast attack/decay envelope. No asset files.
2. **Card flash**: add a CSS class `flash-complete` to the card for 1.2 s (`useState` keyed by `${key}-${tick}` cleared via `setTimeout`). Class animates a 3-pulse box-shadow yellow→cyan.
3. **Browser notification** (if permission granted *and* document is hidden): `new Notification(\`${session.name}\`, { body: 'Turn complete', tag: key })`.
4. **Increment "new completions" counter** (see Header Counters).

## Header Counters

### Display

In `App.tsx` header row, between the title and the link status:

```
▶ 3   ✓ 12   💀 4   🔔 2 NEW
```

- `▶` count of `working`
- `✓` count of `done`
- `💀` count of `stale` (shown even when "hide stale" filter is on)
- `🔔 N NEW` count of completions since the user last interacted with the page; clears on any click anywhere in the dashboard. Hidden when zero.

### State

`useMemo` over `sessions` for the three status counts. `newCompletions` is a `useState` driven by `useTurnCompletionNotifier`'s callback. A single `onClick` listener on the root `<div>` resets it.

## Error Highlighting

### Backend signal

The session derivation already drains `tool_result` blocks but doesn't track failures. Extend `state.Session`:

```go
type Session struct {
    // existing fields…
    LastErrorAt time.Time `json:"last_error_at,omitempty"`
}
```

In `applyUser` (state/derive.go), when iterating `tool_result` blocks, if `b.IsError` (field added by spec #1), set `s.LastErrorAt = ts`. When a subsequent successful event arrives we leave `LastErrorAt` alone — it represents "most recent error timestamp", not "currently in error". The frontend decides display from the relationship `LastErrorAt > some-threshold-before-LastEventAt`.

Frontend rule: error highlight is **active** when `last_error_at` exists and `last_event_at - last_error_at <= 0` (i.e., the most recent event was the error itself). The next non-error event will move `last_event_at` past `last_error_at`, clearing the highlight automatically.

### UI

- One-time **red flash** when a card transitions into the error-active state (same mechanism as completion flash, different keyframes — pulses red).
- **Persistent red border** (`border-rd`) on the card while error-active.
- The error flash takes precedence over the completion flash if both fire on the same event.

### Detection

Reuse `useTurnCompletionNotifier`'s status-diff pattern: a parallel `useErrorFlash(sessions)` hook tracking the boolean "error active" per key.

## File Inventory

**Modified:**
- `backend/internal/state/types.go` — `LastErrorAt` field
- `backend/internal/state/derive.go` — set `LastErrorAt` from `tool_result.is_error`
- `backend/internal/state/derive_test.go` — error timestamp coverage
- `frontend/src/types.ts` — `last_error_at?: string` on Session
- `frontend/src/App.tsx` — header counters, sort wiring, mount the two notifier hooks, click-to-clear new counter
- `frontend/src/components/HeaderControls.tsx` — sort dropdown
- `frontend/src/components/SessionCard.tsx` — pin icon, notify icon, completion flash class, error flash + persistent red border
- `frontend/src/components/SessionCard.test.tsx` — pin/notify icons, error border state, flash class lifecycle
- `frontend/src/components/ConversationModal.tsx` — DETAILS tab gains the two switches
- `frontend/src/components/ConversationModal.test.tsx`
- `frontend/src/index.css` — `flash-complete` and `flash-error` keyframes

**New:**
- `frontend/src/lib/pins.ts`
- `frontend/src/lib/notify.ts` — `notify` storage helpers + `useTurnCompletionNotifier` + `useErrorFlash` hooks + WebAudio beep
- `frontend/src/lib/sort.ts` — pure sort + partition function (`pinned` first, then by key)
- `frontend/src/lib/notify.test.ts`
- `frontend/src/lib/sort.test.ts`

## Testing

### Backend

- `derive_test.go`: assert `LastErrorAt` is set when `tool_result.is_error: true`, unchanged on subsequent non-error events.

### Frontend

- `sort.test.ts`: pinned-first invariant, sort-by-name stable tie-break, status ordering `working < done < stale`.
- `notify.test.ts`: `useTurnCompletionNotifier` fires exactly once on `working→done` for keys in the notify set, never for keys outside it, never on other transitions; storage round-trips.
- `SessionCard.test.tsx`: red border appears when `last_error_at >= last_event_at`, disappears when a newer non-error event arrives; pin and notify icons render based on storage; flash class is added then removed.
- `ConversationModal.test.tsx`: toggling pin/notify switches the storage state and re-renders the card.

## Open Questions

None. Decisions taken inline.
