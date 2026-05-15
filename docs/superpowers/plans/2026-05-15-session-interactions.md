# Session Interactions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add pin, sort, per-session notifications (sound + flash), header counters, and error highlight (red flash + persistent red border) — turning the dashboard from a passive viewer into a controllable signal stream.

**Architecture:** Pin/notify state lives in `localStorage` behind small typed wrapper modules with subscribe semantics. Sort and pin partitioning is a pure function. Two React hooks (`useTurnCompletionNotifier`, `useErrorFlash`) diff successive `sessions` arrays to detect transitions and drive WebAudio beep + per-card CSS animation classes + browser Notification API. Backend is touched only to surface a `last_error_at` timestamp on `Session`, derived from `tool_result.is_error`.

**Tech Stack:** Go (state derivation), React 19 + TypeScript + Tailwind, Vitest + React Testing Library, browser WebAudio API, browser Notification API, `localStorage`.

**Spec:** `docs/superpowers/specs/2026-05-14-session-interactions-design.md`

---

## File Structure

**Modified:**
- `backend/internal/state/session.go` — add `LastErrorAt time.Time` field
- `backend/internal/state/derive.go` — set `LastErrorAt` in `applyUser` when a `tool_result.is_error == true` is seen
- `backend/internal/state/derive_test.go` — coverage for `LastErrorAt` set/preserved
- `backend/internal/store/sqlite.go` — persist + load `last_error_at` (the field must round-trip; `LastEventAt` already does)
- `backend/internal/store/sqlite_test.go` — round-trip test for the new column
- `frontend/src/types.ts` — add optional `last_error_at?: string` to `Session`
- `frontend/src/components/HeaderControls.tsx` — sort dropdown + new `sort` / `onSortChange` props
- `frontend/src/components/SessionCard.tsx` — pin/notify icons (top-right), flash class wiring, persistent red border when error-active, props for `pinned`/`notify`/`flash`/`errorActive`
- `frontend/src/components/SessionCard.test.tsx` — pin/notify icon visibility, error border state, completion flash class lifecycle
- `frontend/src/components/ConversationModal.tsx` — accept `hostnameId` key; nothing else (pin/notify switches live in `SessionDetails`)
- `frontend/src/components/SessionDetails.tsx` — append a `SETTINGS` section with pin + notify switches
- `frontend/src/components/SessionDetails.test.tsx` — switches read/write storage and re-render
- `frontend/src/App.tsx` — header counters, sort/pin wiring, mount `useTurnCompletionNotifier` and `useErrorFlash`, click-to-clear new counter
- `frontend/src/index.css` — `@keyframes flash-complete` (yellow→cyan 3-pulse) and `@keyframes flash-error` (red 1-pulse) + utility classes

**New:**
- `frontend/src/lib/pins.ts` — pin storage + `subscribe`
- `frontend/src/lib/pins.test.ts`
- `frontend/src/lib/notify.ts` — notify storage + `useTurnCompletionNotifier` + `useErrorFlash` + WebAudio beep + Notification permission helper
- `frontend/src/lib/notify.test.ts`
- `frontend/src/lib/sort.ts` — pure `partitionAndSort(sessions, pinned, sortKey)`
- `frontend/src/lib/sort.test.ts`

**Schema migration:** none — SQLite is loose-typed; the new `last_error_at` column on the `sessions` table is added via `ALTER TABLE … ADD COLUMN IF NOT EXISTS` in the existing `schema` constant. Existing rows get `NULL`. `Apply`/`UpsertSession` always re-derive on next ingest, so old rows self-heal as soon as a tool_result with is_error arrives.

---

## Task 1: Backend — `LastErrorAt` on Session

**Files:**
- Modify: `backend/internal/state/session.go`
- Modify: `backend/internal/state/derive.go`
- Modify: `backend/internal/state/derive_test.go`

- [ ] **Step 1.1: Write failing tests**

Append to `backend/internal/state/derive_test.go`:

```go
func TestApply_ToolResultError_SetsLastErrorAt(t *testing.T) {
	now := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	prev := &Session{
		ID:                "abc",
		StartedAt:         now.Add(-1 * time.Minute),
		LastEventAt:       now.Add(-10 * time.Second),
		Status:            StatusWorking,
		PendingToolUseIDs: map[string]struct{}{"tool_1": {}},
	}
	env := events.IngestEnvelope{
		SessionID: "abc", ProjectDir: "-tmp",
		FileMTime: now, LineIndex: 2,
		Raw: mustRaw(t, map[string]any{
			"type":      "user",
			"timestamp": now,
			"message": map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "tool_1",
						"is_error":    true,
					},
				},
			},
		}),
	}
	got, err := Apply(prev, env)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastErrorAt.Equal(now) {
		t.Errorf("LastErrorAt = %v, want %v", got.LastErrorAt, now)
	}
}

func TestApply_ToolResultSuccess_PreservesLastErrorAt(t *testing.T) {
	earlier := time.Date(2026, 5, 15, 9, 59, 0, 0, time.UTC)
	now := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	prev := &Session{
		ID:                "abc",
		StartedAt:         earlier.Add(-1 * time.Minute),
		LastEventAt:       earlier,
		LastErrorAt:       earlier,
		Status:            StatusWorking,
		PendingToolUseIDs: map[string]struct{}{"tool_2": {}},
	}
	env := events.IngestEnvelope{
		SessionID: "abc", ProjectDir: "-tmp",
		FileMTime: now, LineIndex: 3,
		Raw: mustRaw(t, map[string]any{
			"type":      "user",
			"timestamp": now,
			"message": map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "tool_2",
					},
				},
			},
		}),
	}
	got, err := Apply(prev, env)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastErrorAt.Equal(earlier) {
		t.Errorf("LastErrorAt = %v, want %v (success result must not clear)", got.LastErrorAt, earlier)
	}
}
```

- [ ] **Step 1.2: Run tests to verify they fail**

```
cd <repo>/backend && go test ./internal/state/ -run 'TestApply_ToolResult(Error|Success)' -v
```
Expected: compile error — `LastErrorAt` undefined on `Session`.

- [ ] **Step 1.3: Add the field**

Edit `backend/internal/state/session.go`. Insert this field in the `Session` struct, after `LastEventAt`:

```go
	LastErrorAt   time.Time  `json:"last_error_at,omitempty"`
```

Final struct should look like:

```go
type Session struct {
	Hostname      string     `json:"hostname"`
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Project       string     `json:"project"`
	Status        Status     `json:"status"`
	StartedAt     time.Time  `json:"started_at"`
	LastPromptAt  *time.Time `json:"last_prompt_at,omitempty"`
	CurrentAction string     `json:"current_action"`
	LastEventAt   time.Time  `json:"last_event_at"`
	LastErrorAt   time.Time  `json:"last_error_at,omitempty"`

	PendingToolUseIDs map[string]struct{} `json:"-"`
}
```

- [ ] **Step 1.4: Run again to confirm only the assertion fails**

Same command. Expected: tests compile but fail because `LastErrorAt` is never set.

- [ ] **Step 1.5: Set `LastErrorAt` in `applyUser`**

Edit `backend/internal/state/derive.go`. Replace the `applyUser` function with:

```go
func applyUser(s *Session, line events.RawLine, ts time.Time) {
	if line.Message == nil {
		return
	}
	sawToolResult := false
	for _, b := range line.Message.Content {
		if b.Type == "tool_result" {
			sawToolResult = true
			delete(s.PendingToolUseIDs, b.ToolUseID)
			if b.IsError {
				s.LastErrorAt = ts
			}
		}
	}
	if !sawToolResult {
		// Real user prompt.
		for _, b := range line.Message.Content {
			if b.Type == "text" {
				s.CurrentAction = truncate("user: "+strings.TrimSpace(b.Text), 80)
				break
			}
		}
	}
}
```

Then update its single caller in `Apply`. Find:

```go
	case "user":
		applyUser(&next, line)
```

and replace with:

```go
	case "user":
		applyUser(&next, line, ts)
```

- [ ] **Step 1.6: Run tests to verify pass**

```
cd <repo>/backend && go test ./internal/state/ -v
```
Expected: PASS for everything in the package.

- [ ] **Step 1.7: Run the whole backend suite**

```
cd <repo>/backend && go test ./...
```
Expected: PASS.

- [ ] **Step 1.8: Commit**

```bash
cd <repo>
git add backend/internal/state/session.go backend/internal/state/derive.go backend/internal/state/derive_test.go
git commit -m "feat(state): track LastErrorAt from tool_result.is_error"
```

---

## Task 2: Backend — Persist `last_error_at` Through SQLite

**Files:**
- Modify: `backend/internal/store/sqlite.go`
- Modify: `backend/internal/store/sqlite_test.go`

- [ ] **Step 2.1: Write failing test**

Append to `backend/internal/store/sqlite_test.go`:

```go
func TestUpsertSession_RoundTripsLastErrorAt(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "err.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	in := &state.Session{
		ID: "abc", Hostname: "h", Name: "n", Project: "/p",
		Status:      state.StatusDone,
		StartedAt:   t0,
		LastEventAt: t0.Add(time.Minute),
		LastErrorAt: t0.Add(30 * time.Second),
	}
	if err := st.UpsertSession(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSession(ctx, "h", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("got nil session")
	}
	if !got.LastErrorAt.Equal(in.LastErrorAt) {
		t.Errorf("LastErrorAt = %v, want %v", got.LastErrorAt, in.LastErrorAt)
	}
}

func TestUpsertSession_ZeroLastErrorAtRoundTrips(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "noerr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	in := &state.Session{
		ID: "abc", Hostname: "h", Name: "n", Project: "/p",
		Status:      state.StatusDone,
		StartedAt:   t0,
		LastEventAt: t0,
		// LastErrorAt left zero.
	}
	if err := st.UpsertSession(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSession(ctx, "h", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastErrorAt.IsZero() {
		t.Errorf("LastErrorAt = %v, want zero", got.LastErrorAt)
	}
}
```

If `time` and `context` aren't already imported, add them.

- [ ] **Step 2.2: Run tests to verify they fail**

```
cd <repo>/backend && go test ./internal/store/ -run TestUpsertSession_.*LastErrorAt -v
```
Expected: FAIL — column doesn't exist or is not stored/loaded.

- [ ] **Step 2.3: Add the column to the schema**

Edit `backend/internal/store/sqlite.go`. Replace the `sessions` CREATE TABLE in the `schema` constant with the version that includes `last_error_at`, and add an explicit migration line for existing DBs:

```go
const schema = `
CREATE TABLE IF NOT EXISTS sessions (
  hostname        TEXT NOT NULL,
  id              TEXT NOT NULL,
  name            TEXT NOT NULL,
  project         TEXT NOT NULL,
  status          TEXT NOT NULL,
  started_at      TIMESTAMP NOT NULL,
  last_prompt_at  TIMESTAMP,
  current_action  TEXT,
  last_event_at   TIMESTAMP NOT NULL,
  last_error_at   TIMESTAMP,
  PRIMARY KEY (hostname, id)
);
CREATE TABLE IF NOT EXISTS events (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  hostname    TEXT NOT NULL DEFAULT '',
  session_id  TEXT NOT NULL,
  ts          TIMESTAMP NOT NULL,
  type        TEXT NOT NULL,
  payload     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_session_ts ON events(hostname, session_id, ts);
`

// migrations contains idempotent ALTER TABLE statements that bring older
// databases up to the current schema. SQLite errors on duplicate columns,
// so we tolerate the failure for the additive case.
var migrations = []string{
	`ALTER TABLE sessions ADD COLUMN last_error_at TIMESTAMP`,
}
```

Then in `Open`, after `db.Exec(schema)`, add the migration loop:

```go
	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			// duplicate column on already-migrated DBs is fine
			if !strings.Contains(err.Error(), "duplicate column name") {
				return nil, fmt.Errorf("migrate: %w", err)
			}
		}
	}
```

You will need to add `"strings"` to the import block if it isn't already there.

- [ ] **Step 2.4: Persist the field in `UpsertSession`**

Replace the `UpsertSession` function with a version that includes `last_error_at`:

```go
func (s *SQLite) UpsertSession(ctx context.Context, sess *state.Session) error {
	var lastPrompt any
	if sess.LastPromptAt != nil {
		lastPrompt = *sess.LastPromptAt
	}
	var lastError any
	if !sess.LastErrorAt.IsZero() {
		lastError = sess.LastErrorAt
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sessions (hostname, id, name, project, status, started_at, last_prompt_at, current_action, last_event_at, last_error_at)
VALUES (?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(hostname, id) DO UPDATE SET
  name=excluded.name,
  project=excluded.project,
  status=excluded.status,
  started_at=excluded.started_at,
  last_prompt_at=excluded.last_prompt_at,
  current_action=excluded.current_action,
  last_event_at=excluded.last_event_at,
  last_error_at=excluded.last_error_at
`,
		sess.Hostname, sess.ID, sess.Name, sess.Project, string(sess.Status),
		sess.StartedAt, lastPrompt, sess.CurrentAction, sess.LastEventAt, lastError,
	)
	return err
}
```

- [ ] **Step 2.5: Load the field in `ListSessions` and `GetSession`**

Replace `ListSessions`:

```go
func (s *SQLite) ListSessions(ctx context.Context) ([]*state.Session, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT hostname, id, name, project, status, started_at, last_prompt_at, current_action, last_event_at, last_error_at
FROM sessions ORDER BY last_event_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*state.Session
	for rows.Next() {
		var (
			sess   state.Session
			status string
			lp     sql.NullTime
			le     sql.NullTime
		)
		if err := rows.Scan(
			&sess.Hostname, &sess.ID, &sess.Name, &sess.Project, &status,
			&sess.StartedAt, &lp, &sess.CurrentAction, &sess.LastEventAt, &le,
		); err != nil {
			return nil, err
		}
		sess.Status = state.Status(status)
		if lp.Valid {
			t := lp.Time
			sess.LastPromptAt = &t
		}
		if le.Valid {
			sess.LastErrorAt = le.Time
		}
		out = append(out, &sess)
	}
	return out, rows.Err()
}
```

Replace `GetSession`:

```go
func (s *SQLite) GetSession(ctx context.Context, hostname, id string) (*state.Session, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT hostname, id, name, project, status, started_at, last_prompt_at, current_action, last_event_at, last_error_at
FROM sessions WHERE hostname = ? AND id = ?`, hostname, id)
	var (
		sess   state.Session
		status string
		lp     sql.NullTime
		le     sql.NullTime
	)
	err := row.Scan(&sess.Hostname, &sess.ID, &sess.Name, &sess.Project, &status,
		&sess.StartedAt, &lp, &sess.CurrentAction, &sess.LastEventAt, &le)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sess.Status = state.Status(status)
	if lp.Valid {
		t := lp.Time
		sess.LastPromptAt = &t
	}
	if le.Valid {
		sess.LastErrorAt = le.Time
	}
	return &sess, nil
}
```

- [ ] **Step 2.6: Run tests to verify pass**

```
cd <repo>/backend && go test ./...
```
Expected: PASS, including the two new round-trip tests.

- [ ] **Step 2.7: Commit**

```bash
cd <repo>
git add backend/internal/store/sqlite.go backend/internal/store/sqlite_test.go
git commit -m "feat(store): persist last_error_at on sessions; migrate older DBs additively"
```

---

## Task 3: Frontend — `Session.last_error_at` Type

**Files:**
- Modify: `frontend/src/types.ts`

- [ ] **Step 3.1: Add the field**

Replace the `Session` interface with:

```ts
export interface Session {
  id: string;
  hostname: string;
  name: string;
  project: string;
  status: Status;
  started_at: string;
  last_prompt_at?: string;
  current_action: string;
  last_event_at: string;
  last_error_at?: string;
}
```

- [ ] **Step 3.2: Type-check**

```
cd <repo>/frontend && npx tsc --noEmit
```
Expected: PASS (additive optional field).

- [ ] **Step 3.3: Commit**

```bash
cd <repo>
git add frontend/src/types.ts
git commit -m "feat(types): expose last_error_at on Session"
```

---

## Task 4: Frontend — `lib/pins.ts`

**Files:**
- Create: `frontend/src/lib/pins.ts`
- Create: `frontend/src/lib/pins.test.ts`

- [ ] **Step 4.1: Write the failing tests**

Create `frontend/src/lib/pins.test.ts`:

```ts
import { describe, it, expect, beforeEach } from "vitest";
import { isPinned, togglePin, listPinned, subscribe } from "./pins";

describe("pins storage", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("starts empty", () => {
    expect(listPinned().size).toBe(0);
    expect(isPinned("h:abc")).toBe(false);
  });

  it("togglePin adds and removes a key", () => {
    togglePin("h:abc");
    expect(isPinned("h:abc")).toBe(true);
    expect(listPinned().size).toBe(1);
    togglePin("h:abc");
    expect(isPinned("h:abc")).toBe(false);
    expect(listPinned().size).toBe(0);
  });

  it("survives a reload via localStorage", () => {
    togglePin("h:abc");
    togglePin("h:def");
    // Simulate fresh module read by re-listing.
    const set = listPinned();
    expect(set.has("h:abc")).toBe(true);
    expect(set.has("h:def")).toBe(true);
  });

  it("subscribers fire on toggle and are removable", () => {
    let count = 0;
    const off = subscribe(() => {
      count++;
    });
    togglePin("h:abc");
    togglePin("h:abc");
    expect(count).toBe(2);
    off();
    togglePin("h:abc");
    expect(count).toBe(2);
  });

  it("ignores malformed JSON in storage", () => {
    localStorage.setItem("sv:pinned", "not json");
    expect(listPinned().size).toBe(0);
  });
});
```

- [ ] **Step 4.2: Run to verify fail**

```
cd <repo>/frontend && npx vitest run src/lib/pins.test.ts
```
Expected: FAIL — module not found.

- [ ] **Step 4.3: Implement**

Create `frontend/src/lib/pins.ts`:

```ts
const KEY = "sv:pinned";

type Listener = () => void;
const listeners = new Set<Listener>();

function read(): Set<string> {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return new Set();
    const arr = JSON.parse(raw);
    if (!Array.isArray(arr)) return new Set();
    return new Set(arr.filter((v): v is string => typeof v === "string"));
  } catch {
    return new Set();
  }
}

function write(set: Set<string>): void {
  localStorage.setItem(KEY, JSON.stringify([...set]));
  for (const fn of listeners) fn();
}

export function listPinned(): Set<string> {
  return read();
}

export function isPinned(key: string): boolean {
  return read().has(key);
}

export function togglePin(key: string): void {
  const set = read();
  if (set.has(key)) set.delete(key);
  else set.add(key);
  write(set);
}

export function subscribe(fn: Listener): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
```

- [ ] **Step 4.4: Run to verify pass**

```
cd <repo>/frontend && npx vitest run src/lib/pins.test.ts
```
Expected: PASS for all five cases.

- [ ] **Step 4.5: Commit**

```bash
cd <repo>
git add frontend/src/lib/pins.ts frontend/src/lib/pins.test.ts
git commit -m "feat(lib): pins storage with subscribe helper"
```

---

## Task 5: Frontend — `lib/sort.ts`

**Files:**
- Create: `frontend/src/lib/sort.ts`
- Create: `frontend/src/lib/sort.test.ts`

- [ ] **Step 5.1: Write the failing tests**

Create `frontend/src/lib/sort.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { partitionAndSort, type SortKey } from "./sort";
import type { Session } from "../types";

const mk = (over: Partial<Session>): Session => ({
  id: "x",
  hostname: "h",
  name: "n",
  project: "/p",
  status: "done",
  started_at: "2026-05-15T10:00:00Z",
  current_action: "",
  last_event_at: "2026-05-15T10:00:00Z",
  ...over,
});

const A = mk({ id: "A", hostname: "ha", name: "alpha", status: "done",    last_event_at: "2026-05-15T10:00:01Z" });
const B = mk({ id: "B", hostname: "hb", name: "bravo", status: "working", last_event_at: "2026-05-15T10:00:02Z" });
const C = mk({ id: "C", hostname: "hc", name: "charlie", status: "stale", last_event_at: "2026-05-15T10:00:00Z" });

const key = (s: Session) => `${s.hostname}:${s.id}`;

describe("partitionAndSort", () => {
  const cases: { sort: SortKey; expected: Session[] }[] = [
    { sort: "last_update", expected: [B, A, C] }, // newest first
    { sort: "name",        expected: [A, B, C] },
    { sort: "host",        expected: [A, B, C] },
    { sort: "status",      expected: [B, A, C] }, // working < done < stale
  ];

  for (const c of cases) {
    it(`sorts by ${c.sort}`, () => {
      expect(partitionAndSort([C, A, B], new Set(), c.sort)).toEqual(c.expected);
    });
  }

  it("places pinned entries first, sorted by the same key", () => {
    const pinned = new Set([key(A), key(C)]);
    // Sort by name → pinned [A, C] first; rest [B].
    expect(partitionAndSort([C, A, B], pinned, "name")).toEqual([A, C, B]);
  });

  it("breaks name ties by last_event_at desc", () => {
    const A1 = mk({ id: "A1", name: "dup", last_event_at: "2026-05-15T10:00:01Z" });
    const A2 = mk({ id: "A2", name: "dup", last_event_at: "2026-05-15T10:00:09Z" });
    expect(partitionAndSort([A1, A2], new Set(), "name")).toEqual([A2, A1]);
  });
});
```

- [ ] **Step 5.2: Run to verify fail**

```
cd <repo>/frontend && npx vitest run src/lib/sort.test.ts
```
Expected: FAIL — module not found.

- [ ] **Step 5.3: Implement**

Create `frontend/src/lib/sort.ts`:

```ts
import type { Session, Status } from "../types";

export type SortKey = "last_update" | "name" | "host" | "status";

const STATUS_ORDER: Record<Status, number> = {
  working: 0,
  done: 1,
  stale: 2,
};

function tsDesc(a: Session, b: Session): number {
  return new Date(b.last_event_at).getTime() - new Date(a.last_event_at).getTime();
}

function compare(a: Session, b: Session, key: SortKey): number {
  switch (key) {
    case "last_update":
      return tsDesc(a, b);
    case "name": {
      const c = a.name.localeCompare(b.name);
      return c !== 0 ? c : tsDesc(a, b);
    }
    case "host": {
      const c = a.hostname.localeCompare(b.hostname);
      return c !== 0 ? c : tsDesc(a, b);
    }
    case "status": {
      const c = STATUS_ORDER[a.status] - STATUS_ORDER[b.status];
      return c !== 0 ? c : tsDesc(a, b);
    }
  }
}

const key = (s: Session) => `${s.hostname}:${s.id}`;

export function partitionAndSort(
  sessions: readonly Session[],
  pinned: ReadonlySet<string>,
  sortKey: SortKey,
): Session[] {
  const pin: Session[] = [];
  const rest: Session[] = [];
  for (const s of sessions) {
    if (pinned.has(key(s))) pin.push(s);
    else rest.push(s);
  }
  pin.sort((a, b) => compare(a, b, sortKey));
  rest.sort((a, b) => compare(a, b, sortKey));
  return [...pin, ...rest];
}
```

- [ ] **Step 5.4: Run to verify pass**

```
cd <repo>/frontend && npx vitest run src/lib/sort.test.ts
```
Expected: PASS for all six cases.

- [ ] **Step 5.5: Commit**

```bash
cd <repo>
git add frontend/src/lib/sort.ts frontend/src/lib/sort.test.ts
git commit -m "feat(lib): partitionAndSort for pinned-first session ordering"
```

---

## Task 6: Frontend — `lib/notify.ts` Storage + Hooks

**Files:**
- Create: `frontend/src/lib/notify.ts`
- Create: `frontend/src/lib/notify.test.ts`

- [ ] **Step 6.1: Write the failing tests**

Create `frontend/src/lib/notify.test.ts`:

```ts
import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";
import { renderHook } from "@testing-library/react";
import {
  isNotifyEnabled,
  toggleNotify,
  listNotify,
  useTurnCompletionNotifier,
  useErrorFlash,
  __setBeepForTests,
} from "./notify";
import type { Session } from "../types";

const mk = (over: Partial<Session>): Session => ({
  id: "x",
  hostname: "h",
  name: "n",
  project: "/p",
  status: "done",
  started_at: "2026-05-15T10:00:00Z",
  current_action: "",
  last_event_at: "2026-05-15T10:00:00Z",
  ...over,
});

describe("notify storage", () => {
  beforeEach(() => localStorage.clear());

  it("toggle round-trips", () => {
    expect(isNotifyEnabled("h:abc")).toBe(false);
    toggleNotify("h:abc");
    expect(isNotifyEnabled("h:abc")).toBe(true);
    expect(listNotify().has("h:abc")).toBe(true);
    toggleNotify("h:abc");
    expect(isNotifyEnabled("h:abc")).toBe(false);
  });

  it("ignores malformed JSON", () => {
    localStorage.setItem("sv:notify", "not json");
    expect(listNotify().size).toBe(0);
  });
});

describe("useTurnCompletionNotifier", () => {
  let beep: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    localStorage.clear();
    beep = vi.fn();
    __setBeepForTests(beep);
  });
  afterEach(() => __setBeepForTests(null));

  it("fires once on working→done for keys in the notify set", () => {
    const onComplete = vi.fn();
    toggleNotify("h:abc");
    const a1 = mk({ id: "abc", hostname: "h", status: "working" });
    const a2 = mk({ id: "abc", hostname: "h", status: "done" });

    const { rerender } = renderHook(
      ({ s }: { s: Session[] }) => useTurnCompletionNotifier(s, onComplete),
      { initialProps: { s: [a1] } },
    );
    rerender({ s: [a2] });
    expect(onComplete).toHaveBeenCalledTimes(1);
    expect(onComplete).toHaveBeenCalledWith("h:abc");
    expect(beep).toHaveBeenCalledTimes(1);
  });

  it("does not fire for keys not in the notify set", () => {
    const onComplete = vi.fn();
    const a1 = mk({ id: "abc", hostname: "h", status: "working" });
    const a2 = mk({ id: "abc", hostname: "h", status: "done" });

    const { rerender } = renderHook(
      ({ s }: { s: Session[] }) => useTurnCompletionNotifier(s, onComplete),
      { initialProps: { s: [a1] } },
    );
    rerender({ s: [a2] });
    expect(onComplete).not.toHaveBeenCalled();
    expect(beep).not.toHaveBeenCalled();
  });

  it("does not fire on done→done", () => {
    const onComplete = vi.fn();
    toggleNotify("h:abc");
    const a = mk({ id: "abc", hostname: "h", status: "done" });
    const { rerender } = renderHook(
      ({ s }: { s: Session[] }) => useTurnCompletionNotifier(s, onComplete),
      { initialProps: { s: [a] } },
    );
    rerender({ s: [a] });
    expect(onComplete).not.toHaveBeenCalled();
  });
});

describe("useErrorFlash", () => {
  it("reports error-active for sessions where last_error_at >= last_event_at", () => {
    const a = mk({
      id: "abc", hostname: "h",
      last_event_at: "2026-05-15T10:00:00Z",
      last_error_at: "2026-05-15T10:00:00Z",
    });
    const b = mk({
      id: "def", hostname: "h",
      last_event_at: "2026-05-15T10:01:00Z",
      last_error_at: "2026-05-15T10:00:00Z",
    });
    const { result } = renderHook(() => useErrorFlash([a, b]));
    expect(result.current.errorActive.has("h:abc")).toBe(true);
    expect(result.current.errorActive.has("h:def")).toBe(false);
  });

  it("emits a fresh-flash key when a session newly enters error-active state", () => {
    const ok = mk({ id: "abc", hostname: "h", last_event_at: "2026-05-15T10:00:00Z" });
    const err = mk({
      id: "abc", hostname: "h",
      last_event_at: "2026-05-15T10:00:01Z",
      last_error_at: "2026-05-15T10:00:01Z",
    });
    const { result, rerender } = renderHook(({ s }: { s: Session[] }) => useErrorFlash(s), {
      initialProps: { s: [ok] },
    });
    expect(result.current.freshErrors.size).toBe(0);
    rerender({ s: [err] });
    expect(result.current.freshErrors.has("h:abc")).toBe(true);
  });
});
```

- [ ] **Step 6.2: Run to verify fail**

```
cd <repo>/frontend && npx vitest run src/lib/notify.test.ts
```
Expected: FAIL — module not found.

- [ ] **Step 6.3: Implement**

Create `frontend/src/lib/notify.ts`:

```ts
import { useEffect, useMemo, useRef, useState } from "react";
import type { Session } from "../types";

const KEY = "sv:notify";

type Listener = () => void;
const listeners = new Set<Listener>();

function read(): Set<string> {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return new Set();
    const arr = JSON.parse(raw);
    if (!Array.isArray(arr)) return new Set();
    return new Set(arr.filter((v): v is string => typeof v === "string"));
  } catch {
    return new Set();
  }
}

function write(set: Set<string>): void {
  localStorage.setItem(KEY, JSON.stringify([...set]));
  for (const fn of listeners) fn();
}

export function listNotify(): Set<string> {
  return read();
}

export function isNotifyEnabled(key: string): boolean {
  return read().has(key);
}

export function toggleNotify(key: string): void {
  const set = read();
  if (set.has(key)) set.delete(key);
  else set.add(key);
  write(set);
}

export function subscribeNotify(fn: Listener): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

// ---------- WebAudio beep ----------

// Lazy AudioContext: created on first user-driven beep so the browser
// allows it (some browsers reject AudioContext without a gesture).
let ctx: AudioContext | null = null;
type BeepFn = () => void;
let beepImpl: BeepFn | null = null;

function defaultBeep(): void {
  try {
    if (typeof window === "undefined") return;
    const AC = window.AudioContext || (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!AC) return;
    if (!ctx) ctx = new AC();
    const t0 = ctx.currentTime;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = "square";
    osc.frequency.value = 880;
    gain.gain.setValueAtTime(0.0001, t0);
    gain.gain.exponentialRampToValueAtTime(0.18, t0 + 0.01);
    gain.gain.exponentialRampToValueAtTime(0.0001, t0 + 0.15);
    osc.connect(gain).connect(ctx.destination);
    osc.start(t0);
    osc.stop(t0 + 0.16);
  } catch {
    // Audio failure is non-fatal — the visual flash still fires.
  }
}

/** Test-only injection seam. Pass null to restore the real beep. */
export function __setBeepForTests(fn: BeepFn | null): void {
  beepImpl = fn;
}

function beep(): void {
  (beepImpl ?? defaultBeep)();
}

// ---------- Hooks ----------

const sessionKey = (s: Session) => `${s.hostname}:${s.id}`;

/**
 * Calls `onComplete(key)` once per session whose status transitions
 * `working → done`, but only for sessions whose key is in the notify set.
 * Also plays the cyberpunk beep on each fire.
 */
export function useTurnCompletionNotifier(
  sessions: readonly Session[],
  onComplete: (key: string) => void,
): void {
  const prev = useRef<Map<string, Session["status"]>>(new Map());
  useEffect(() => {
    const next = new Map<string, Session["status"]>();
    const enabled = read();
    for (const s of sessions) {
      const k = sessionKey(s);
      next.set(k, s.status);
      const wasWorking = prev.current.get(k) === "working";
      if (wasWorking && s.status === "done" && enabled.has(k)) {
        beep();
        onComplete(k);
      }
    }
    prev.current = next;
  }, [sessions, onComplete]);
}

export interface ErrorFlashState {
  errorActive: Set<string>;
  freshErrors: Set<string>;
}

/**
 * Returns the set of session keys currently in the error-active state
 * (where `last_error_at >= last_event_at`) and the set of keys that just
 * entered that state on this render — for one-time red flash animations.
 */
export function useErrorFlash(sessions: readonly Session[]): ErrorFlashState {
  const errorActive = useMemo(() => {
    const set = new Set<string>();
    for (const s of sessions) {
      if (!s.last_error_at) continue;
      if (new Date(s.last_error_at).getTime() >= new Date(s.last_event_at).getTime()) {
        set.add(sessionKey(s));
      }
    }
    return set;
  }, [sessions]);

  const prev = useRef<Set<string>>(new Set());
  const [fresh, setFresh] = useState<Set<string>>(() => new Set());
  useEffect(() => {
    const next = new Set<string>();
    for (const k of errorActive) {
      if (!prev.current.has(k)) next.add(k);
    }
    prev.current = errorActive;
    setFresh(next);
  }, [errorActive]);

  return { errorActive, freshErrors: fresh };
}
```

- [ ] **Step 6.4: Run to verify pass**

```
cd <repo>/frontend && npx vitest run src/lib/notify.test.ts
```
Expected: PASS.

- [ ] **Step 6.5: Commit**

```bash
cd <repo>
git add frontend/src/lib/notify.ts frontend/src/lib/notify.test.ts
git commit -m "feat(lib): notify storage, turn-completion + error-flash hooks, WebAudio beep"
```

---

## Task 7: Frontend — Flash Keyframes in `index.css`

**Files:**
- Modify: `frontend/src/index.css`

- [ ] **Step 7.1: Append keyframes**

Add at the end of `frontend/src/index.css`:

```css
@keyframes flash-complete-kf {
  0%   { box-shadow: 0 0 0 0 rgba(252, 238, 10, 0.0); }
  10%  { box-shadow: 0 0 28px 4px rgba(252, 238, 10, 0.85); }
  35%  { box-shadow: 0 0 0 0 rgba(252, 238, 10, 0.0); }
  45%  { box-shadow: 0 0 28px 4px rgba(0, 240, 255, 0.85); }
  70%  { box-shadow: 0 0 0 0 rgba(0, 240, 255, 0.0); }
  80%  { box-shadow: 0 0 28px 4px rgba(252, 238, 10, 0.85); }
  100% { box-shadow: 0 0 0 0 rgba(252, 238, 10, 0.0); }
}
.flash-complete { animation: flash-complete-kf 1.2s ease-out 1; }

@keyframes flash-error-kf {
  0%   { box-shadow: 0 0 0 0 rgba(255, 0, 60, 0.0); }
  20%  { box-shadow: 0 0 30px 6px rgba(255, 0, 60, 0.95); }
  100% { box-shadow: 0 0 0 0 rgba(255, 0, 60, 0.0); }
}
.flash-error { animation: flash-error-kf 0.9s ease-out 1; }
```

- [ ] **Step 7.2: Commit**

```bash
cd <repo>
git add frontend/src/index.css
git commit -m "feat(ui): keyframes for flash-complete (yellow→cyan) and flash-error (red)"
```

---

## Task 8: Frontend — Sort Dropdown in `HeaderControls`

**Files:**
- Modify: `frontend/src/components/HeaderControls.tsx`

- [ ] **Step 8.1: Replace the component**

Replace the entire contents of `frontend/src/components/HeaderControls.tsx` with:

```tsx
import type { SortKey } from "../lib/sort";

interface Props {
  hideStale: boolean;
  onToggleHideStale: () => void;
  query: string;
  onQueryChange: (q: string) => void;
  sort: SortKey;
  onSortChange: (key: SortKey) => void;
}

const SORT_LABEL: Record<SortKey, string> = {
  last_update: "LAST UPDATE",
  name: "NAME",
  host: "HOST",
  status: "STATUS",
};

const SORT_OPTIONS: SortKey[] = ["last_update", "name", "host", "status"];

export function HeaderControls({
  hideStale,
  onToggleHideStale,
  query,
  onQueryChange,
  sort,
  onSortChange,
}: Props) {
  return (
    <div className="mt-3 flex flex-col gap-2 sm:flex-row sm:items-center">
      <div className="relative flex-1">
        <input
          type="search"
          inputMode="search"
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
          placeholder="SEARCH SESSIONS…"
          className="h-12 w-full bg-bg-panel pl-3 pr-12 font-hud text-base text-txt
                     border border-cy/30 focus:border-cy focus:outline-none
                     placeholder:text-dim"
        />
        {query.length > 0 && (
          <button
            type="button"
            aria-label="Clear search"
            onClick={() => onQueryChange("")}
            className="absolute right-0 top-0 grid h-12 w-11 place-items-center
                       font-hud text-lg text-dim hover:text-cy touch-manipulation"
          >
            ×
          </button>
        )}
      </div>

      <label className="flex items-center gap-2 font-hud text-xs uppercase tracking-widest text-dim">
        <span className="hidden sm:inline">SORT</span>
        <select
          aria-label="Sort sessions"
          value={sort}
          onChange={(e) => onSortChange(e.target.value as SortKey)}
          className="h-11 bg-bg-panel border border-cy/30 px-2 font-hud text-xs uppercase
                     tracking-widest text-cy focus:border-cy focus:outline-none touch-manipulation"
        >
          {SORT_OPTIONS.map((k) => (
            <option key={k} value={k}>
              {SORT_LABEL[k]}
            </option>
          ))}
        </select>
      </label>

      <button
        type="button"
        onClick={onToggleHideStale}
        aria-pressed={hideStale}
        className={`h-11 px-4 font-hud text-xs uppercase tracking-widest border
                    touch-manipulation
                    ${
                      hideStale
                        ? "border-cy text-cy bg-cy/10"
                        : "border-cy/30 text-dim hover:border-cy/60"
                    }`}
      >
        {hideStale ? "HIDE STALE" : "SHOW ALL"}
      </button>
    </div>
  );
}
```

- [ ] **Step 8.2: Type-check**

```
cd <repo>/frontend && npx tsc --noEmit
```
Expected: FAIL — `App.tsx` doesn't supply `sort`/`onSortChange` yet. That gets fixed in Task 11. Continue.

- [ ] **Step 8.3: Commit**

```bash
cd <repo>
git add frontend/src/components/HeaderControls.tsx
git commit -m "feat(ui): SORT dropdown in HeaderControls"
```

---

## Task 9: Frontend — `SessionCard` Pin/Notify Icons + Flash + Error Border

**Files:**
- Modify: `frontend/src/components/SessionCard.tsx`
- Modify: `frontend/src/components/SessionCard.test.tsx`

- [ ] **Step 9.1: Write the failing tests**

Replace the contents of `frontend/src/components/SessionCard.test.tsx` with:

```tsx
import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { SessionCard } from "./SessionCard";
import type { Session } from "../types";

const sess: Session = {
  id: "abc",
  hostname: "mac-A",
  name: "feature-x",
  project: "/Users/u/Projects/foo",
  status: "working",
  started_at: new Date().toISOString(),
  current_action: "Edit: x.ts",
  last_event_at: new Date().toISOString(),
};

describe("SessionCard", () => {
  it("renders name@hostname with @ and hostname styled differently", () => {
    const { container } = render(<SessionCard session={sess} onOpen={() => {}} />);
    const h2 = container.querySelector("h2");
    expect(h2?.textContent).toBe("feature-x@mac-A");
    expect(h2?.querySelector(".text-yl")?.textContent).toBe("@");
    expect(h2?.querySelector(".text-cy")?.textContent).toBe("mac-A");
  });

  it("renders status badge with relative-age subtitle", () => {
    const sess2 = {
      ...sess,
      status: "done" as const,
      last_event_at: new Date(Date.now() - 12_000).toISOString(),
    };
    const { container } = render(<SessionCard session={sess2} onOpen={() => {}} />);
    expect(container.textContent).toContain("DONE");
    expect(container.textContent).toMatch(/·\s*1[12]s/);
  });

  it("shows the pin icon when pinned prop is true", () => {
    const { container, rerender } = render(<SessionCard session={sess} onOpen={() => {}} />);
    expect(container.textContent).not.toContain("📌");
    rerender(<SessionCard session={sess} onOpen={() => {}} pinned />);
    expect(container.textContent).toContain("📌");
  });

  it("shows the notify icon when notify prop is true", () => {
    const { container, rerender } = render(<SessionCard session={sess} onOpen={() => {}} />);
    expect(container.textContent).not.toContain("🔔");
    rerender(<SessionCard session={sess} onOpen={() => {}} notify />);
    expect(container.textContent).toContain("🔔");
  });

  it("applies the flash-complete class when flash='complete'", () => {
    const { container } = render(
      <SessionCard session={sess} onOpen={() => {}} flash="complete" />,
    );
    expect(container.querySelector(".flash-complete")).toBeTruthy();
  });

  it("applies persistent red border + flash-error when errorActive", () => {
    const { container } = render(
      <SessionCard session={sess} onOpen={() => {}} errorActive flash="error" />,
    );
    const btn = container.querySelector("button");
    expect(btn?.className).toContain("border-rd");
    expect(container.querySelector(".flash-error")).toBeTruthy();
  });
});
```

- [ ] **Step 9.2: Run to verify fail**

```
cd <repo>/frontend && npx vitest run src/components/SessionCard.test.tsx
```
Expected: FAIL on the four new cases.

- [ ] **Step 9.3: Update the component**

Replace `frontend/src/components/SessionCard.tsx` with:

```tsx
import { useEffect, useState } from "react";
import type { Session } from "../types";
import { StatusBadge } from "./StatusBadge";
import { formatDuration } from "../lib/time";
import { shortProject } from "../lib/path";
import { formatRelative } from "../lib/relativeTime";

interface Props {
  session: Session;
  onOpen: () => void;
  pinned?: boolean;
  notify?: boolean;
  errorActive?: boolean;
  flash?: "complete" | "error" | null;
}

export function SessionCard({
  session,
  onOpen,
  pinned = false,
  notify = false,
  errorActive = false,
  flash = null,
}: Props) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  const total = now - new Date(session.started_at).getTime();
  const prompt = session.last_prompt_at
    ? now - new Date(session.last_prompt_at).getTime()
    : null;
  const age = now - new Date(session.last_event_at).getTime();

  const flashClass =
    flash === "error" ? "flash-error" : flash === "complete" ? "flash-complete" : "";
  const borderColor = errorActive
    ? "border-rd"
    : session.status === "working"
    ? "border-cy/30 hover:border-cy"
    : "border-cy/30 hover:border-cy";
  const workingGlow =
    session.status === "working" && !errorActive
      ? "shadow-[0_0_18px_rgba(0,240,255,0.25)]"
      : "";

  return (
    <button
      type="button"
      onClick={onOpen}
      className={`relative min-h-[160px] w-full border bg-bg-panel p-4 text-left transition-colors
                  active:scale-[0.99] touch-manipulation
                  focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cy
                  ${borderColor} ${workingGlow} ${flashClass}`}
    >
      {(pinned || notify) && (
        <div className="absolute right-2 top-2 flex items-center gap-1 text-xs">
          {pinned && <span aria-label="Pinned" title="Pinned">📌</span>}
          {notify && <span aria-label="Notify enabled" title="Notify enabled">🔔</span>}
        </div>
      )}

      <header className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2">
          <StatusBadge status={session.status} />
          <span className="font-hud text-[10px] text-dim">
            · {formatRelative(age)}
          </span>
        </div>
        <div className="font-hud text-[10px] text-dim truncate max-w-[55%]">
          {shortProject(session.project)}
        </div>
      </header>

      <h2 className="mt-3 font-hud text-xl uppercase tracking-wider text-txt truncate">
        {session.name}
        <span className="text-yl mx-0.5">@</span>
        <span className="text-cy">{session.hostname}</span>
      </h2>

      <p className="mt-2 line-clamp-2 font-mono text-xs text-cy/80">
        {session.current_action || "—"}
      </p>

      <footer className="mt-auto flex items-end justify-between pt-4 font-hud text-xs">
        <div>
          <div className="text-dim">TOTAL</div>
          <div className="text-cy text-base">{formatDuration(total)}</div>
        </div>
        {prompt !== null && session.status === "working" && (
          <div className="text-right">
            <div className="text-dim">PROMPT</div>
            <div className="text-yl text-base">{formatDuration(prompt)}</div>
          </div>
        )}
      </footer>
    </button>
  );
}
```

- [ ] **Step 9.4: Run to verify pass**

```
cd <repo>/frontend && npx vitest run src/components/SessionCard.test.tsx
```
Expected: PASS for all six cases.

- [ ] **Step 9.5: Commit**

```bash
cd <repo>
git add frontend/src/components/SessionCard.tsx frontend/src/components/SessionCard.test.tsx
git commit -m "feat(ui): pin/notify icons, flash classes, and error border on SessionCard"
```

---

## Task 10: Frontend — Pin/Notify Switches in `SessionDetails`

**Files:**
- Modify: `frontend/src/components/SessionDetails.tsx`
- Modify: `frontend/src/components/SessionDetails.test.tsx`

- [ ] **Step 10.1: Write the failing test**

Append to `frontend/src/components/SessionDetails.test.tsx`:

```tsx
import { fireEvent } from "@testing-library/react";
import { isPinned } from "../lib/pins";
import { isNotifyEnabled } from "../lib/notify";

it("toggles pin via the SETTINGS switch and persists to storage", async () => {
  localStorage.clear();
  render(
    <SessionDetails
      sessionId="abc"
      hostname="mac-A"
      lastEventAt="2026-05-14T10:00:00Z"
      backendHttpBase="http://localhost:8080"
    />,
  );
  await waitFor(() => expect(screen.getByText("claude-opus-4-7")).toBeTruthy());

  const pinSwitch = screen.getByRole("switch", { name: /pin to top/i });
  expect(pinSwitch.getAttribute("aria-checked")).toBe("false");
  fireEvent.click(pinSwitch);
  expect(pinSwitch.getAttribute("aria-checked")).toBe("true");
  expect(isPinned("mac-A:abc")).toBe(true);
});

it("toggles notify via the SETTINGS switch and persists to storage", async () => {
  localStorage.clear();
  render(
    <SessionDetails
      sessionId="abc"
      hostname="mac-A"
      lastEventAt="2026-05-14T10:00:00Z"
      backendHttpBase="http://localhost:8080"
    />,
  );
  await waitFor(() => expect(screen.getByText("claude-opus-4-7")).toBeTruthy());

  const notifySwitch = screen.getByRole("switch", { name: /notify on turn complete/i });
  fireEvent.click(notifySwitch);
  expect(isNotifyEnabled("mac-A:abc")).toBe(true);
});
```

- [ ] **Step 10.2: Run to verify fail**

```
cd <repo>/frontend && npx vitest run src/components/SessionDetails.test.tsx
```
Expected: FAIL — no `switch` role found.

- [ ] **Step 10.3: Add the SETTINGS section**

Edit `frontend/src/components/SessionDetails.tsx`. Add these imports at the top, alongside the existing imports:

```ts
import { useSyncExternalStore } from "react";
import { isPinned, togglePin, subscribe as subscribePins } from "../lib/pins";
import { isNotifyEnabled, toggleNotify, subscribeNotify } from "../lib/notify";
```

Inside the `SessionDetails` function body, before the early returns, add:

```ts
  const key = `${hostname}:${sessionId}`;
  const pinned = useSyncExternalStore(subscribePins, () => isPinned(key));
  const notify = useSyncExternalStore(subscribeNotify, () => isNotifyEnabled(key));
```

Then, in the returned JSX, add a fourth `<Section>` after `TOOL BREAKDOWN`:

```tsx
      <Section title="SETTINGS">
        <SwitchRow
          label="📌 Pin to top"
          checked={pinned}
          onChange={() => togglePin(key)}
        />
        <SwitchRow
          label="🔔 Notify on turn complete"
          checked={notify}
          onChange={() => {
            // First-time enable triggers Notification permission prompt
            // (best-effort; failure is silently swallowed).
            if (!notify && typeof window !== "undefined" && "Notification" in window) {
              if (Notification.permission === "default") {
                Notification.requestPermission().catch(() => {});
              }
            }
            toggleNotify(key);
          }}
        />
      </Section>
```

Append the `SwitchRow` helper at the end of the file, after `Row`:

```tsx
function SwitchRow({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: () => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-txt">{label}</span>
      <button
        type="button"
        role="switch"
        aria-label={label}
        aria-checked={checked}
        onClick={onChange}
        className={`h-6 w-12 border touch-manipulation transition-colors
                    ${checked ? "border-cy bg-cy/30" : "border-cy/30 bg-bg-panel"}`}
      >
        <span
          aria-hidden
          className={`block h-full w-1/2 transition-transform
                      ${checked ? "translate-x-full bg-cy" : "translate-x-0 bg-dim"}`}
        />
      </button>
    </div>
  );
}
```

- [ ] **Step 10.4: Run to verify pass**

```
cd <repo>/frontend && npx vitest run src/components/SessionDetails.test.tsx
```
Expected: PASS for the new pin + notify cases (and existing two cases).

- [ ] **Step 10.5: Commit**

```bash
cd <repo>
git add frontend/src/components/SessionDetails.tsx frontend/src/components/SessionDetails.test.tsx
git commit -m "feat(ui): SETTINGS section in SessionDetails with pin and notify switches"
```

---

## Task 11: Frontend — Wire Sort, Pin, Notify, and Counters in `App.tsx`

**Files:**
- Modify: `frontend/src/App.tsx`

- [ ] **Step 11.1: Replace the component**

Replace `frontend/src/App.tsx` with:

```tsx
import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { useSessionsSocket } from "./useSessionsSocket";
import { SessionCard } from "./components/SessionCard";
import { ScanlineOverlay } from "./components/ScanlineOverlay";
import { HeaderControls } from "./components/HeaderControls";
import { ConversationModal } from "./components/ConversationModal";
import { shortProject } from "./lib/path";
import { backendHttpBase } from "./lib/backendUrl";
import { partitionAndSort, type SortKey } from "./lib/sort";
import { listPinned, subscribe as subscribePins } from "./lib/pins";
import {
  listNotify,
  subscribeNotify,
  useTurnCompletionNotifier,
  useErrorFlash,
} from "./lib/notify";

function defaultWsUrl(): string {
  if (typeof window === "undefined") return "ws://localhost:8080/ws/clients";
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${window.location.hostname}:8080/ws/clients`;
}

const WS_URL =
  (import.meta.env.VITE_BACKEND_WS as string | undefined) ?? defaultWsUrl();

const HTTP_BASE = backendHttpBase(WS_URL);
const HIDE_STALE_KEY = "sv:hideStale";
const SORT_KEY = "sv:sort";

const isSortKey = (v: string | null): v is SortKey =>
  v === "last_update" || v === "name" || v === "host" || v === "status";

const FLASH_MS = 1200;

export default function App() {
  const { sessions, connected } = useSessionsSocket(WS_URL);

  const [hideStale, setHideStale] = useState<boolean>(() => {
    const v = localStorage.getItem(HIDE_STALE_KEY);
    return v === null ? true : v === "1";
  });
  const [sort, setSort] = useState<SortKey>(() => {
    const v = localStorage.getItem(SORT_KEY);
    return isSortKey(v) ? v : "last_update";
  });
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState<{ hostname: string; id: string } | null>(null);
  const [newCompletions, setNewCompletions] = useState(0);
  const [flashes, setFlashes] = useState<Map<string, "complete" | "error">>(
    () => new Map(),
  );

  useEffect(() => {
    localStorage.setItem(HIDE_STALE_KEY, hideStale ? "1" : "0");
  }, [hideStale]);
  useEffect(() => {
    localStorage.setItem(SORT_KEY, sort);
  }, [sort]);

  const pinned = useSyncExternalStore(subscribePins, () => listPinned());
  const notify = useSyncExternalStore(subscribeNotify, () => listNotify());

  const fireFlash = (key: string, kind: "complete" | "error") => {
    setFlashes((prev) => {
      const next = new Map(prev);
      next.set(key, kind);
      return next;
    });
    setTimeout(() => {
      setFlashes((prev) => {
        const next = new Map(prev);
        if (next.get(key) === kind) next.delete(key);
        return next;
      });
    }, FLASH_MS);
  };

  // Track turn completions: beep + completion flash + browser notification + counter bump.
  useTurnCompletionNotifier(sessions, (key) => {
    fireFlash(key, "complete");
    setNewCompletions((n) => n + 1);
    if (
      typeof window !== "undefined" &&
      "Notification" in window &&
      Notification.permission === "granted" &&
      document.hidden
    ) {
      const sess = sessions.find((s) => `${s.hostname}:${s.id}` === key);
      try {
        new Notification(sess?.name ?? "Claude session", {
          body: "Turn complete",
          tag: key,
        });
      } catch {
        // ignore
      }
    }
  });

  // Track error transitions: error flash overrides any in-flight completion flash.
  const { errorActive, freshErrors } = useErrorFlash(sessions);
  const lastFreshKeysRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    for (const key of freshErrors) {
      if (!lastFreshKeysRef.current.has(key)) {
        fireFlash(key, "error");
      }
    }
    lastFreshKeysRef.current = new Set(freshErrors);
  }, [freshErrors]);

  // Counts visible to the user (computed from raw sessions, not the filtered view).
  const counts = useMemo(() => {
    let working = 0;
    let done = 0;
    let stale = 0;
    for (const s of sessions) {
      if (s.status === "working") working++;
      else if (s.status === "done") done++;
      else if (s.status === "stale") stale++;
    }
    return { working, done, stale };
  }, [sessions]);

  const sorted = useMemo(() => {
    const q = query.trim().toLowerCase();
    const filtered = sessions.filter((s) => {
      if (hideStale && s.status === "stale") return false;
      if (!q) return true;
      const project = shortProject(s.project).toLowerCase();
      return s.name.toLowerCase().includes(q) || project.includes(q);
    });
    return partitionAndSort(filtered, pinned, sort);
  }, [sessions, hideStale, query, sort, pinned]);

  const openSession = open
    ? sessions.find((s) => s.id === open.id && s.hostname === open.hostname) ?? null
    : null;

  return (
    <div className="min-h-full p-4" onClick={() => setNewCompletions(0)}>
      <ScanlineOverlay />
      <header className="mb-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h1 className="font-hud text-2xl tracking-widest text-cy">
            SUPERV<span className="text-yl">AI</span>SOR
          </h1>
          <div className="flex items-center gap-3 font-hud text-xs">
            <span className="text-cy">▶ {counts.working}</span>
            <span className="text-yl">✓ {counts.done}</span>
            <span className="text-rd">💀 {counts.stale}</span>
            {newCompletions > 0 && (
              <span className="text-yl animate-pulseDot">🔔 {newCompletions} NEW</span>
            )}
            <span
              className={`${connected ? "text-cy" : "text-rd animate-glitch"}`}
            >
              {connected ? "// LINK OK" : "// DISCONNECTED"}
            </span>
          </div>
        </div>
        <HeaderControls
          hideStale={hideStale}
          onToggleHideStale={() => setHideStale((v) => !v)}
          query={query}
          onQueryChange={setQuery}
          sort={sort}
          onSortChange={setSort}
        />
      </header>

      {sorted.length === 0 ? (
        <div className="mt-12 text-center font-hud text-dim">
          {sessions.length === 0 ? "NO SESSIONS DETECTED" : "NO MATCHES"}
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {sorted.map((s) => {
            const key = `${s.hostname}:${s.id}`;
            return (
              <SessionCard
                key={key}
                session={s}
                onOpen={() => setOpen({ hostname: s.hostname, id: s.id })}
                pinned={pinned.has(key)}
                notify={notify.has(key)}
                errorActive={errorActive.has(key)}
                flash={flashes.get(key) ?? null}
              />
            );
          })}
        </div>
      )}

      {openSession && (
        <ConversationModal
          sessionId={openSession.id}
          hostname={openSession.hostname}
          sessionName={openSession.name}
          project={openSession.project}
          lastEventAt={openSession.last_event_at}
          backendHttpBase={HTTP_BASE}
          onClose={() => setOpen(null)}
        />
      )}
    </div>
  );
}
```

- [ ] **Step 11.2: Type-check**

```
cd <repo>/frontend && npx tsc --noEmit
```
Expected: PASS.

- [ ] **Step 11.3: Run the full frontend suite**

```
cd <repo>/frontend && npx vitest run
```
Expected: PASS — all suites including the new pin/notify/sort/flash coverage.

- [ ] **Step 11.4: Commit**

```bash
cd <repo>
git add frontend/src/App.tsx
git commit -m "feat(ui): wire sort/pin/notify/counters and turn-complete + error flashes"
```

---

## Task 12: End-to-End Smoke

**Files:** none — manual verification.

- [ ] **Step 12.1: Build everything**

```
cd <repo> && make test
```
Expected: backend, poller, and frontend tests all PASS.

- [ ] **Step 12.2: Bring up the stack**

```
make up
```
Then start a host poller in a second terminal:
```
make poller
```

- [ ] **Step 12.3: Browser checks at http://localhost:5173**

Verify:

- (a) Header shows three counters and they update live as sessions transition (`▶ N`, `✓ N`, `💀 N`).
- (b) `SORT` dropdown switches order between Last update / Name / Host / Status. Setting persists across reload.
- (c) Open any card → DETAILS tab shows the new SETTINGS section with two switches.
- (d) Toggle 📌 Pin to top → close modal → that card moves to the top of the grid and shows the 📌 icon.
- (e) Toggle 🔔 Notify on turn complete (grant browser permission when asked) → wait for Claude to finish a turn → audible beep + yellow→cyan flash on the card + `🔔 1 NEW` appears in the header.
- (f) Click anywhere on the dashboard → the `🔔 N NEW` counter clears.
- (g) When a tool fails (e.g. an invalid `Bash` command), the card flashes red briefly and shows a persistent red border. The next non-error event clears the border.

- [ ] **Step 12.4: Tear down**

```
make down
```

- [ ] **Step 12.5: No commit needed.**

---

## Self-Review Notes

1. **Spec coverage:** Pin (T4 storage + T9 card icon + T10 settings switch + T11 wiring); Sort (T5 + T8 dropdown + T11 wiring); Notifications (T6 hook + T7 keyframes + T9 flash class + T10 settings switch + T11 wiring including browser Notification + counter); Header counters (T11); Error highlight (T1+T2 backend `last_error_at`, T3 type, T6 `useErrorFlash`, T9 border + flash class, T11 wiring).
2. **Type consistency:** `SortKey` is the single source of truth (`lib/sort.ts`); `HeaderControls` and `App` import it. `SessionCard` props match what `App` passes (`pinned`, `notify`, `errorActive`, `flash`). `ErrorFlashState.errorActive` and `freshErrors` field names match between `useErrorFlash` and `App`.
3. **Backwards compat:** new `last_error_at` column is added via additive migration; older rows tolerate `NULL`. Pin/notify storage is fresh; missing keys default to "off". `App.tsx` defaults `sort` to `"last_update"` if storage is empty or invalid.
4. **No placeholders.** Every step shows actual code; every command has expected output.

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-05-15-session-interactions.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**
