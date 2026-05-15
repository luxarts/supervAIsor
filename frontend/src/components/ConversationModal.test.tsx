import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { ConversationModal } from "./ConversationModal";

const sample = [
  {
    ts: "2026-05-14T12:00:00Z",
    type: "user",
    payload: {
      type: "user",
      message: { role: "user", content: [{ type: "text", text: "hello" }] },
    },
  },
  {
    ts: "2026-05-14T12:00:01Z",
    type: "assistant",
    payload: {
      type: "assistant",
      message: {
        role: "assistant",
        content: [
          { type: "text", text: "this is **bold**" },
          { type: "tool_use", name: "Bash", id: "x" },
        ],
      },
    },
  },
  {
    ts: "2026-05-14T12:00:02Z",
    type: "user",
    payload: {
      type: "user",
      message: {
        role: "user",
        content: [{ type: "tool_result", tool_use_id: "x", content: "should be hidden" }],
      },
    },
  },
];

describe("ConversationModal", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        status: 200,
        json: async () => sample,
      })),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders text messages with markdown and hides tool noise", async () => {
    render(
      <ConversationModal
        sessionId="abc"
        hostname="mac-A"
        sessionName="my-sess"
        project="/Users/u/Projects/foo"
        lastEventAt="2026-05-14T12:00:02Z"
        backendHttpBase="http://localhost:8080"
        onClose={() => {}}
      />,
    );

    await waitFor(() => expect(screen.getByText("hello")).toBeTruthy());

    const bold = await screen.findByText("bold");
    expect(bold.tagName).toBe("STRONG");

    // Tool calls and tool results are both hidden in v1.
    expect(screen.queryByText(/▶\s*Bash/)).toBeNull();
    expect(screen.queryByText(/should be hidden/)).toBeNull();

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining("/sessions/mac-A/abc/events"),
      expect.anything(),
    );
  });

  it("switches to DETAILS tab and fetches /stats", async () => {
    const fetchMock = vi.fn(async (url: string) => {
      if (url.includes("/stats")) {
        return {
          ok: true,
          status: 200,
          json: async () => ({
            hostname: "mac-A",
            id: "abc",
            name: "my-sess",
            project: "/Users/u/Projects/foo",
            model: "claude-opus-4-7",
            started_at: "2026-05-14T11:00:00Z",
            last_event_at: "2026-05-14T12:00:02Z",
            wall_clock_seconds: 3722,
            tokens: { input: 0, output: 0, cache_creation: 0, cache_read: 0 },
            counts: { user_prompts: 0, assistant_turns: 0, tool_calls: 0, errors: 0 },
            tool_breakdown: {},
          }),
        };
      }
      return { ok: true, status: 200, json: async () => sample };
    });
    vi.stubGlobal("fetch", fetchMock);

    render(
      <ConversationModal
        sessionId="abc"
        hostname="mac-A"
        sessionName="my-sess"
        project="/Users/u/Projects/foo"
        lastEventAt="2026-05-14T12:00:02Z"
        backendHttpBase="http://localhost:8080"
        onClose={() => {}}
      />,
    );

    await waitFor(() => expect(screen.getByText("hello")).toBeTruthy());

    fireEvent.click(screen.getByRole("tab", { name: /details/i }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("/sessions/mac-A/abc/stats"),
        expect.anything(),
      ),
    );
    await waitFor(() => expect(screen.getByText("claude-opus-4-7")).toBeTruthy());
  });
});
