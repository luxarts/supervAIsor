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
