import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { fireEvent } from "@testing-library/react";
import { SessionDetails } from "./SessionDetails";
import { isPinned } from "../lib/pins";
import { isNotifyEnabled } from "../lib/notify";

const stats = {
  hostname: "mac-A",
  id: "abc",
  name: "feature-x",
  project: "/Users/u/Projects/foo",
  model: "claude-opus-4-7",
  started_at: "2026-05-14T09:00:00Z",
  last_event_at: "2026-05-14T10:00:00Z",
  wall_clock_seconds: 3600,
  tokens: { input: 100, output: 50, cache_creation: 10, cache_read: 200 },
  counts: { user_prompts: 3, assistant_turns: 7, tool_calls: 12, errors: 1 },
  tool_breakdown: { Read: 8, Bash: 4 },
};

describe("SessionDetails", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({ ok: true, status: 200, json: async () => stats })),
    );
  });
  afterEach(() => vi.unstubAllGlobals());

  it("fetches and renders the stats payload", async () => {
    render(
      <SessionDetails
        sessionId="abc"
        hostname="mac-A"
        lastEventAt="2026-05-14T10:00:00Z"
        backendHttpBase="http://localhost:8080"
      />,
    );
    await waitFor(() => expect(screen.getByText("claude-opus-4-7")).toBeTruthy());
    expect(screen.getByText("/Users/u/Projects/foo")).toBeTruthy();
    expect(screen.getByText("mac-A")).toBeTruthy();
    expect(screen.getByText("100")).toBeTruthy();
    expect(screen.getByText("Read")).toBeTruthy();
    expect(screen.getByText("Bash")).toBeTruthy();

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining("/sessions/mac-A/abc/stats"),
      expect.anything(),
    );
  });

  it("refetches when lastEventAt changes", async () => {
    const { rerender } = render(
      <SessionDetails
        sessionId="abc"
        hostname="mac-A"
        lastEventAt="2026-05-14T10:00:00Z"
        backendHttpBase="http://localhost:8080"
      />,
    );
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    rerender(
      <SessionDetails
        sessionId="abc"
        hostname="mac-A"
        lastEventAt="2026-05-14T10:00:05Z"
        backendHttpBase="http://localhost:8080"
      />,
    );
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
  });

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
});
