import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { deleteSession } from "./deleteSession";

describe("deleteSession", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({ ok: true, status: 204, text: async () => "" })),
    );
  });
  afterEach(() => vi.unstubAllGlobals());

  it("issues DELETE against the right URL on success", async () => {
    await deleteSession("http://localhost:8080", "mac-A", "abc");
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/sessions/mac-A/abc",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("throws with backend error message on non-204 responses", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: false,
        status: 409,
        text: async () => `{"error":"poller offline"}`,
      })),
    );
    await expect(deleteSession("http://x", "h", "id")).rejects.toThrow(/poller offline/);
  });
});
