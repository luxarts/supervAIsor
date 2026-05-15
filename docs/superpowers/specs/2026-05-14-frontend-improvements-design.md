# Frontend Improvements — Design Spec

**Date:** 2026-05-14
**Scope:** Dashboard UX improvements: hide-stale toggle, project-name display fix, conversation modal, search bar. Mobile-first with touchscreen ergonomics.

## Goals

1. Hide `stale` sessions by default with a toggle to show them.
2. Display only the project's basename, correctly handling project names that contain `-`. Show `<NONE>` when there is no project context.
3. Click a session card → modal showing the full conversation with scroll and rendered markdown.
4. Search bar to filter sessions by name or project.

All four must work comfortably on a touchscreen (≥44 px touch targets, swipe gestures where natural, no iOS zoom on focus).

## Non-goals (YAGNI)

- Pagination / virtual scrolling in the modal.
- Full-text search inside message contents.
- Filtering by status beyond hide-stale.
- Persisting search query in the URL.
- Rendering `tool_result` blocks (too noisy for v1).

## Architecture changes

### Project path resolution (root-cause fix)

Today: `poller` sends Claude's encoded project dir (e.g. `-Users-you-Projects-ai-dream-team`); `backend/internal/state/projectdir.go` naively replaces every `-` with `/`, producing the wrong path `/Users/you/Projects/ai/dream/team` whenever the project name contains a dash.

The encoded form is ambiguous in isolation. Disambiguation requires checking which path actually exists on disk. The backend runs in a Docker container without host filesystem access, so resolution must happen in the poller (host-native).

**New `poller/internal/scanner/projectpath.go`:**

```
ResolveProjectPath(encoded string) string
```

- Split encoded string by `-`. The first element is empty (encoded paths start with `-`).
- Greedy left-to-right walk starting from `/`:
  - At each step, find the longest contiguous slice `parts[i..j]` such that `filepath.Join(currentPath, strings.Join(parts[i..j+1], "-"))` exists on disk.
  - Append the longest match, advance `i = j + 1`.
  - If no match exists at any step → fall back to the naive decode (`ReplaceAll(s, "-", "/")`) so absent/test paths still produce something usable.
- Tests use `t.TempDir()` to create real dirs (including names with dashes) and assert resolution.

**`poller/internal/scanner/scanner.go`:** call `ResolveProjectPath(encodedDir)` once when discovering a project, send the resolved real path in `IngestEnvelope.ProjectDir`.

**`backend/internal/state/projectdir.go`:** `DecodeProjectDir` becomes passthrough when input starts with `/` (real path already), keeps existing behavior for legacy encoded inputs (preserves existing tests).

### Conversation events endpoint

Backend already persists every raw JSONL line in the `events` table. Expose it.

**`backend/internal/store/sqlite.go`:**

```
ListEvents(ctx context.Context, sessionID string, limit int) ([]Event, error)
```
Returns `{ts time.Time, type string, payload json.RawMessage}` sorted by `ts ASC`, capped at `limit` (default 500).

**`backend/internal/api/sessions.go`:**

`GET /sessions/:id/events?limit=500` → JSON array of events. 404 if the session does not exist.

Single-user, local-only — no auth, no rate limit.

## Frontend changes

### Header (`App.tsx`)

Layout (mobile-first, single column; inline on `sm+`):

```
SUPERVAISOR                                  // LINK OK
[search input — full width, h-12]
[HIDE STALE pill]                            (right-aligned on sm+)
```

State (`App.tsx`):
- `hideStale: boolean` — default `true`, persisted in `localStorage` under `sv:hideStale`.
- `query: string` — local only.
- `openSessionId: string | null` — modal target.

Filtering pipeline:
```
sessions
  .filter(s => !hideStale || s.status !== "stale")
  .filter(s => matchesQuery(s, query))
```

`matchesQuery(s, q)` lowercases both sides and tests against `s.name` and `shortProject(s.project)`.

### Hide-stale toggle

`<button>` styled as a pill, not a checkbox:
- Size: `h-11 px-4` (≥44 px tap target).
- Active: `border-cy text-cy bg-cy/10`.
- Inactive: `border-cy/30 text-dim`.
- Label flips between `HIDE STALE` and `SHOW ALL`.
- `aria-pressed` for accessibility.

### Search input

`<input type="search" inputMode="search">`:
- `h-12 text-base` (16 px prevents iOS zoom).
- Background `bg-bg-panel`, border `border-cy/30`, focus border `border-cy`.
- Placeholder: `SEARCH SESSIONS…`.
- Clear button (`×`) `w-11 h-11` appears when `query.length > 0`.

### `shortProject(path)` helper (`frontend/src/lib/path.ts`)

Rules:
- `path === ""` → `<NONE>`.
- `path === "/Users/<user>/Projects"` heuristically (basename equals `Projects` *and* path is exactly that segment under home) → `<NONE>`. Simpler implementation: if the resolved basename is `Projects` and there is no further nesting after it, return `<NONE>`. We accept the edge case of a literal project named "Projects" mapping to `<NONE>`; cheap to revisit later.
- Otherwise: return `basename(path)`.

Tests cover: dashed project names, exact `/Users/x/Projects`, empty, single-segment paths, trailing slash.

Used in `SessionCard` and `ConversationModal` headers.

### `SessionCard` becomes clickable

- Wrap article body in `<button type="button">` with `min-h-[160px] w-full text-left touch-manipulation`.
- `onClick={() => onOpen(session.id)}` (prop drilled from `App`).
- Keep current visual styling. Add `focus-visible:ring-2 focus-visible:ring-cy` for keyboard.

### `ConversationModal.tsx` (new)

Behavior:
- Opens when `openSessionId !== null`.
- On open: `fetch(\`${BACKEND_HTTP}/sessions/${id}/events?limit=500\`)`. `BACKEND_HTTP` is derived from `VITE_BACKEND_WS` (replace `ws://`→`http://`, `wss://`→`https://`, strip `/ws/clients`).
- Close paths: `Esc`, click on backdrop, close button, swipe-down past threshold.

Layout:
- Mobile: `inset-0` fullscreen, `bg-bg-panel`.
- Desktop (`sm+`): centered, `max-w-3xl w-full max-h-[90vh]`, backdrop `bg-black/70`.
- Top bar (sticky): drag handle `▬▬▬` centered (visible only on mobile), session name + `shortProject` on the left, close `<button>` `w-12 h-12` on the right.
- Body: `overflow-y-auto overscroll-contain` with rendered messages.
- Auto-scroll to bottom on initial load.

Message rendering (iterate over events ordered by `ts`):
- `type === "user"` (from `RawLine.Type` or `message.role`): left-aligned block, border `border-cy/40`, text `text-cy/90`.
- `type === "assistant"` with text content blocks: right-aligned block, border `border-yl/40`, text rendered with `react-markdown` + `remark-gfm` so `**bold**`, lists, fenced code, links work.
- `tool_use` content blocks: compact one-liner `▶ <tool_name>` in `text-dim font-mono`.
- `tool_result`: omitted.
- Anything else: skipped silently.

Swipe-to-close (mobile only — gated by `window.matchMedia("(pointer: coarse)")`):
- Listen `touchstart` on the modal root, capture initial `clientY`.
- On `touchmove`, if scrollTop === 0 and delta > 0, apply `transform: translateY(delta)` and dim backdrop proportionally.
- On `touchend`: if delta > 80 px → close. Otherwise animate back to 0.
- Cancel if multi-touch.

Markdown safety: content is from local Claude sessions on the same machine, single user. `react-markdown` does not execute HTML by default; do not enable `rehype-raw`. No additional sanitizer needed.

### Dependencies

Add to `frontend/package.json`:
- `react-markdown`
- `remark-gfm`

Both are small and have no native deps.

## Data flow

```
poller --(real path)--> backend events table
                              |
              GET /sessions/:id/events?limit=500
                              |
                              v
                  ConversationModal renders
```

Session list flow is unchanged except `project` is now a real path on every envelope; `shortProject` derives the display name in the frontend.

## Error handling

- Backend `GET /sessions/:id/events`: 404 if session absent, 500 on store error.
- Modal fetch failure: show `// EVENT FEED LOST` placeholder with retry button.
- Empty events: show `// NO MESSAGES`.
- WS frames continue to update the underlying card list while the modal is open; the modal does not live-stream new events in v1 (refresh on close-and-reopen).

## Testing

**Go:**
- `poller/internal/scanner/projectpath_test.go`: `t.TempDir()` with subdirs like `Projects/ai-dream-team`, asserts resolution; falls back when nothing exists.
- `backend/internal/store/sqlite_test.go`: `ListEvents` returns rows ordered by ts, respects limit.
- `backend/internal/api/sessions_test.go`: `GET /sessions/:id/events` happy path + 404.

**Frontend (vitest):**
- `lib/path.test.ts`: `shortProject` cases above.
- Modal: render with mocked events fixture, assert markdown bold renders as `<strong>`, tool_use renders as `▶ name`, tool_result is omitted.

Manual touchscreen check (real device or DevTools mobile emulation):
- Toggle is comfortable to tap.
- Search input does not trigger iOS zoom.
- Card click opens modal.
- Swipe-down closes modal.

## Out of scope / future

- Live-streaming new events into an open modal.
- Search inside message text.
- Multi-session selection / batch actions.
- Server-side filtering or pagination.
