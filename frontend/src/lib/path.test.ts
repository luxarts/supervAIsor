import { describe, it, expect } from "vitest";
import { shortProject } from "./path";

describe("shortProject", () => {
  it("returns <NONE> for empty input", () => {
    expect(shortProject("")).toBe("<NONE>");
  });

  it("returns basename of a normal path", () => {
    expect(shortProject("/Users/u/Projects/supervAIsor")).toBe("supervAIsor");
  });

  it("preserves dashed project names", () => {
    expect(shortProject("/Users/u/Projects/ai-dream-team")).toBe("ai-dream-team");
  });

  it("returns <NONE> when the path ends at the Projects dir", () => {
    expect(shortProject("/Users/u/Projects")).toBe("<NONE>");
  });

  it("ignores trailing slash", () => {
    expect(shortProject("/Users/u/Projects/foo/")).toBe("foo");
  });

  it("handles single-segment paths", () => {
    expect(shortProject("/tmp")).toBe("tmp");
  });
});
