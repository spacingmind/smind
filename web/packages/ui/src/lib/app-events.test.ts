import { describe, expect, it, vi } from "vitest";

import { NEW_WORKSPACE_EVENT, requestNewWorkspace } from "@/lib/app-events";

describe("app events", () => {
  it("requestNewWorkspace dispatches the window event", () => {
    const listener = vi.fn();
    window.addEventListener(NEW_WORKSPACE_EVENT, listener);
    requestNewWorkspace();
    window.removeEventListener(NEW_WORKSPACE_EVENT, listener);
    expect(listener).toHaveBeenCalledTimes(1);
  });
});
