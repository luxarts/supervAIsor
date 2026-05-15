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
    { sort: "last_update", expected: [B, A, C] },
    { sort: "name",        expected: [A, B, C] },
    { sort: "host",        expected: [A, B, C] },
    { sort: "status",      expected: [B, A, C] },
  ];

  for (const c of cases) {
    it(`sorts by ${c.sort}`, () => {
      expect(partitionAndSort([C, A, B], new Set(), c.sort)).toEqual(c.expected);
    });
  }

  it("places pinned entries first, sorted by the same key", () => {
    const pinned = new Set([key(A), key(C)]);
    expect(partitionAndSort([C, A, B], pinned, "name")).toEqual([A, C, B]);
  });

  it("breaks name ties by last_event_at desc", () => {
    const A1 = mk({ id: "A1", name: "dup", last_event_at: "2026-05-15T10:00:01Z" });
    const A2 = mk({ id: "A2", name: "dup", last_event_at: "2026-05-15T10:00:09Z" });
    expect(partitionAndSort([A1, A2], new Set(), "name")).toEqual([A2, A1]);
  });
});
