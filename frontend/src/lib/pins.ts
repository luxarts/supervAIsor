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
