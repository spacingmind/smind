import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { DEMO_BANNER_TEXT, DEMO_INSTALL_URL, DemoBanner } from "./demo-banner";
import { DEMO_LANDING_HASH, landOnLiveTask } from "./land";

afterEach(() => {
  document.documentElement.removeAttribute("data-demo");
  document.documentElement.style.removeProperty("--demo-banner-h");
});

describe("demo banner", () => {
  it("B-01 renders the exact text, role=status, the install link, and no dismiss control", () => {
    render(<DemoBanner />);
    const banner = screen.getByRole("status");
    expect(banner).toHaveTextContent(DEMO_BANNER_TEXT);
    expect(DEMO_BANNER_TEXT).toBe("Demo — not connected to a daemon. Scripted data; nothing you type leaves your browser.");
    const link = screen.getByRole("link", { name: "Install smind" });
    expect(link).toHaveAttribute("href", DEMO_INSTALL_URL);
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("marks the document so demo.css can offset the shell, and unmarks it on unmount", () => {
    const { unmount } = render(<DemoBanner />);
    expect(document.documentElement.dataset.demo).toBeDefined();
    expect(document.documentElement.style.getPropertyValue("--demo-banner-h")).toMatch(/^\d+px$/);
    unmount();
    expect(document.documentElement.dataset.demo).toBeUndefined();
  });

  it("opens the live task when the URL names nothing, and leaves a named route alone", () => {
    const calls: string[] = [];
    const history = { replaceState: (_s: unknown, _t: string, url: string) => calls.push(url) } as unknown as History;
    landOnLiveTask({ hash: "", pathname: "/", search: "" } as Location, history);
    landOnLiveTask({ hash: "#/workspace/2/task/4/chat/4", pathname: "/", search: "" } as Location, history);
    expect(calls).toEqual([`/${DEMO_LANDING_HASH}`]);
  });
});
