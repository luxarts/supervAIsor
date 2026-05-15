import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useSessionsSocket } from "./useSessionsSocket";

class MockWS {
  static instances: MockWS[] = [];
  url: string;
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  constructor(url: string) {
    this.url = url;
    MockWS.instances.push(this);
  }
  send(_: string) {}
  close() {
    this.onclose?.();
  }
}

describe("useSessionsSocket", () => {
  beforeEach(() => {
    MockWS.instances = [];
    vi.stubGlobal("WebSocket", MockWS);
  });
  afterEach(() => vi.unstubAllGlobals());

  function emit(ws: MockWS, payload: unknown) {
    ws.onmessage?.({ data: JSON.stringify(payload) });
  }

  it("captures pollers map from a pollers frame", async () => {
    const { result } = renderHook(() => useSessionsSocket("ws://x"));
    const ws = MockWS.instances[0];
    act(() => {
      ws.onopen?.();
      emit(ws, { kind: "pollers", online: { "mac-A": true, "mac-B": false } });
    });
    expect(result.current.pollersOnline["mac-A"]).toBe(true);
    expect(result.current.pollersOnline["mac-B"]).toBe(false);
  });

  it("removes a session on session_removed frame", async () => {
    const { result } = renderHook(() => useSessionsSocket("ws://x"));
    const ws = MockWS.instances[0];
    act(() => {
      ws.onopen?.();
      emit(ws, {
        kind: "snapshot",
        sessions: [
          { id: "abc", hostname: "h", name: "n", project: "/p", status: "done", started_at: "", current_action: "", last_event_at: "" },
        ],
      });
    });
    expect(result.current.sessions.length).toBe(1);
    act(() => {
      emit(ws, { kind: "session_removed", hostname: "h", id: "abc" });
    });
    expect(result.current.sessions.length).toBe(0);
  });
});
