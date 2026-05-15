import { describe, it, expect, beforeEach } from "vitest";
import { isPinned, togglePin, listPinned, subscribe } from "./pins";

describe("pins storage", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("starts empty", () => {
    expect(listPinned().size).toBe(0);
    expect(isPinned("h:abc")).toBe(false);
  });

  it("togglePin adds and removes a key", () => {
    togglePin("h:abc");
    expect(isPinned("h:abc")).toBe(true);
    expect(listPinned().size).toBe(1);
    togglePin("h:abc");
    expect(isPinned("h:abc")).toBe(false);
    expect(listPinned().size).toBe(0);
  });

  it("survives a reload via localStorage", () => {
    togglePin("h:abc");
    togglePin("h:def");
    const set = listPinned();
    expect(set.has("h:abc")).toBe(true);
    expect(set.has("h:def")).toBe(true);
  });

  it("subscribers fire on toggle and are removable", () => {
    let count = 0;
    const off = subscribe(() => {
      count++;
    });
    togglePin("h:abc");
    togglePin("h:abc");
    expect(count).toBe(2);
    off();
    togglePin("h:abc");
    expect(count).toBe(2);
  });

  it("ignores malformed JSON in storage", () => {
    localStorage.setItem("sv:pinned", "not json");
    expect(listPinned().size).toBe(0);
  });
});
