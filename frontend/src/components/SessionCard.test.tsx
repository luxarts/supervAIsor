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
});
