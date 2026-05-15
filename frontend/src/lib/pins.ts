const KEY = "sv:pinned";

type Listener = () => void;
const listeners = new Set<Listener>();

// Cached snapshot for useSyncExternalStore — must be referentially stable
// when the underlying serialized value hasn't changed, otherwise React 19
// throws an infinite-loop detection error.
let cachedRaw: string | null = null;
let cachedSet: Set<string> = new Set();

function read(): Set<string> {
  const raw = localStorage.getItem(KEY);
  if (raw === cachedRaw) return cachedSet;
  cachedRaw = raw;
  try {
    if (!raw) {
      cachedSet = new Set();
      return cachedSet;
    }
    const arr = JSON.parse(raw);
    if (!Array.isArray(arr)) {
      cachedSet = new Set();
      return cachedSet;
    }
    cachedSet = new Set(arr.filter((v): v is string => typeof v === "string"));
  } catch {
    cachedSet = new Set();
  }
  return cachedSet;
}

function write(set: Set<string>): void {
  localStorage.setItem(KEY, JSON.stringify([...set]));
  // Invalidate cache so the next read() rebuilds from the new serialized form.
  cachedRaw = null;
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
