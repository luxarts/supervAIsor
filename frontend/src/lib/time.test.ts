import { describe, it, expect } from "vitest";
import { formatDuration } from "./time";

describe("formatDuration", () => {
  it("formats seconds", () => {
    expect(formatDuration(45_000)).toBe("00:45");
  });
  it("formats minutes:seconds", () => {
    expect(formatDuration(125_000)).toBe("02:05");
  });
  it("formats hours:minutes:seconds when >= 1h", () => {
    expect(formatDuration(3_725_000)).toBe("01:02:05");
  });
});
