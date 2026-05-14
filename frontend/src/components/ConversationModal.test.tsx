import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
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
});
