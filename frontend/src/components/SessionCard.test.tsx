import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { SessionCard } from "./SessionCard";
import type { Session } from "../types";

const sess: Session = {
  id: "abc",
  hostname: "mac-A",
  name: "feature-x",
  project: "/Users/u/Projects/foo",
  status: "working",
  started_at: new Date().toISOString(),
  current_action: "Edit: x.ts",
  last_event_at: new Date().toISOString(),
};

describe("SessionCard", () => {
  it("renders name@hostname with @ and hostname styled differently", () => {
    const { container } = render(<SessionCard session={sess} onOpen={() => {}} />);
    const h2 = container.querySelector("h2");
    expect(h2?.textContent).toBe("feature-x@mac-A");
    expect(h2?.querySelector(".text-yl")?.textContent).toBe("@");
    expect(h2?.querySelector(".text-cy")?.textContent).toBe("mac-A");
  });

  it("renders status badge with relative-age subtitle", () => {
    const sess2 = {
      ...sess,
      status: "done" as const,
      last_event_at: new Date(Date.now() - 12_000).toISOString(),
    };
    const { container } = render(<SessionCard session={sess2} onOpen={() => {}} />);
    expect(container.textContent).toContain("DONE");
    expect(container.textContent).toMatch(/·\s*1[12]s/);
  });

  it("shows the pin icon when pinned prop is true", () => {
    const { container, rerender } = render(<SessionCard session={sess} onOpen={() => {}} />);
    expect(container.querySelector('[aria-label="Pinned"]')).toBeNull();
    rerender(<SessionCard session={sess} onOpen={() => {}} pinned />);
    expect(container.querySelector('[aria-label="Pinned"]')).toBeTruthy();
  });

  it("shows the notify icon when notify prop is true", () => {
    const { container, rerender } = render(<SessionCard session={sess} onOpen={() => {}} />);
    expect(container.querySelector('[aria-label="Notify enabled"]')).toBeNull();
    rerender(<SessionCard session={sess} onOpen={() => {}} notify />);
    expect(container.querySelector('[aria-label="Notify enabled"]')).toBeTruthy();
  });

  it("applies the flash-complete class when flash='complete'", () => {
    const { container } = render(
      <SessionCard session={sess} onOpen={() => {}} flash="complete" />,
    );
    expect(container.querySelector(".flash-complete")).toBeTruthy();
  });

  it("applies persistent red border + flash-error when errorActive", () => {
    const { container } = render(
      <SessionCard session={sess} onOpen={() => {}} errorActive flash="error" />,
    );
    const btn = container.querySelector("button");
    expect(btn?.className).toContain("border-rd");
    expect(container.querySelector(".flash-error")).toBeTruthy();
  });

  it("dims the card when pollerOnline is false", () => {
    const { container } = render(
      <SessionCard session={sess} onOpen={() => {}} pollerOnline={false} />,
    );
    const btn = container.querySelector("button");
    expect(btn?.getAttribute("data-poller-online")).toBe("false");
    expect(btn?.className).toContain("opacity-50");
    expect(btn?.className).toContain("grayscale");
  });

  it("renders normally when pollerOnline is true", () => {
    const { container } = render(
      <SessionCard session={sess} onOpen={() => {}} pollerOnline />,
    );
    const btn = container.querySelector("button");
    expect(btn?.getAttribute("data-poller-online")).toBe("true");
    expect(btn?.className).not.toContain("opacity-50");
    expect(btn?.className).not.toContain("grayscale");
  });
});
