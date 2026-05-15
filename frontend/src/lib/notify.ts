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
  // Keep onComplete stable in a ref so it never appears in the effect's
  // dependency array — prevents infinite re-render if callers pass inline fns.
  const onCompleteRef = useRef(onComplete);
  useEffect(() => { onCompleteRef.current = onComplete; });

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
        onCompleteRef.current(k);
      }
    }
    prev.current = next;
  }, [sessions]);
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
  // Compute the active error keys as a stable comma-joined string to avoid
  // spurious Set identity changes that would re-trigger the fresh-flash effect.
  const errorKeysStr = useMemo(() => {
    const keys: string[] = [];
    for (const s of sessions) {
      if (!s.last_error_at) continue;
      if (new Date(s.last_error_at).getTime() >= new Date(s.last_event_at).getTime()) {
        keys.push(sessionKey(s));
      }
    }
    return keys.sort().join(",");
  }, [sessions]);

  const errorActive = useMemo(
    () => new Set(errorKeysStr ? errorKeysStr.split(",") : []),
    [errorKeysStr],
  );

  const prevStr = useRef<string>("");
  const [fresh, setFresh] = useState<Set<string>>(() => new Set());
  useEffect(() => {
    const prevSet = new Set(prevStr.current ? prevStr.current.split(",") : []);
    const next = new Set<string>();
    for (const k of errorActive) {
      if (!prevSet.has(k)) next.add(k);
    }
    prevStr.current = errorKeysStr;
    setFresh(next);
  }, [errorKeysStr, errorActive]);

  return { errorActive, freshErrors: fresh };
}
