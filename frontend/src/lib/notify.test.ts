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
