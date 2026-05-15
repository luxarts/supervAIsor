import { describe, it, expect } from "vitest";
import { formatRelative } from "./relativeTime";

describe("formatRelative", () => {
  it("returns 'just now' under 5 seconds", () => {
    expect(formatRelative(0)).toBe("just now");
    expect(formatRelative(4_999)).toBe("just now");
  });
  it("returns seconds when under a minute", () => {
    expect(formatRelative(12_000)).toBe("12s");
    expect(formatRelative(59_999)).toBe("59s");
  });
  it("returns minutes when under an hour", () => {
    expect(formatRelative(60_000)).toBe("1m");
    expect(formatRelative(4 * 60_000 + 30_000)).toBe("4m");
    expect(formatRelative(59 * 60_000)).toBe("59m");
  });
  it("returns hours when over an hour", () => {
    expect(formatRelative(60 * 60_000)).toBe("1h");
    expect(formatRelative(2 * 60 * 60_000 + 12 * 60_000)).toBe("2h");
  });
  it("clamps negative inputs to 'just now'", () => {
    expect(formatRelative(-50_000)).toBe("just now");
  });
});
