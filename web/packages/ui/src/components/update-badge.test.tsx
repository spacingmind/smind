// AH-AC11: the header's update badge. `isDesktop` is a module-load constant,
// so the platform module is mocked per scenario and the component imported fresh.
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { AppUpdateState } from "@/lib/app-update";

const INFO = { version: "0.11.0", currentVersion: "0.10.0", notes: null, date: null };

async function mountBadge(opts: { desktop: boolean; state: AppUpdateState; onOpen?: () => void }) {
  vi.resetModules();
  vi.doMock("@/lib/platform", () => ({ isDesktop: opts.desktop }));
  const { UpdateBadge } = await import("@/components/update-badge");
  return render(<UpdateBadge state={opts.state} onOpen={opts.onOpen ?? (() => {})} />);
}

afterEach(() => {
  cleanup();
  vi.doUnmock("@/lib/platform");
});

describe("UpdateBadge", () => {
  it("AH-AC11 shows for the open prompt and for a dismissed update, naming the version", async () => {
    await mountBadge({ desktop: true, state: { stage: "prompt", info: INFO, error: null } });
    expect(screen.getByTestId("update-badge")).toHaveAttribute("aria-label", "Update available: sMind v0.11.0");
    cleanup();
    await mountBadge({ desktop: true, state: { stage: "idle", available: INFO } });
    expect(screen.getByTestId("update-badge")).toBeInTheDocument();
  });

  it("AH-AC11 click asks to re-open the prompt", async () => {
    const onOpen = vi.fn();
    await mountBadge({ desktop: true, state: { stage: "idle", available: INFO }, onOpen });
    fireEvent.click(screen.getByTestId("update-badge"));
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it("AH-AC11 is hidden when idle, while installing, and in a browser build", async () => {
    const { container } = await mountBadge({ desktop: true, state: { stage: "idle" } });
    expect(container).toBeEmptyDOMElement();
    cleanup();
    const installing = await mountBadge({ desktop: true, state: { stage: "installing", info: INFO } });
    expect(installing.container).toBeEmptyDOMElement();
    cleanup();
    const web = await mountBadge({ desktop: false, state: { stage: "prompt", info: INFO, error: null } });
    expect(web.container).toBeEmptyDOMElement();
  });
});
