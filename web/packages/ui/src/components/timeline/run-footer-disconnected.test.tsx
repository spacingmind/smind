import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";

import { RunFooter } from "@/components/timeline/run-footer";
import type { RunFooterRun } from "@/components/timeline/run-footer-model";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

function running(): RunFooterRun {
  return { id: "run-1", provider: "glm", status: "running", startedAt: "2026-10-23T09:59:00Z" };
}

describe("RunFooter while the daemon is disconnected (U14)", () => {
  it("shows Reconnecting… with no timer, and takes precedence over waiting", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-10-23T10:00:00Z"));
    render(<RunFooter run={running()} waiting disconnected />);
    expect(screen.getByRole("status").textContent).toBe("Reconnecting…");
    expect(screen.getByRole("group", { name: "Reconnecting" })).toBeTruthy();
    expect(screen.queryByTestId("run-working-elapsed")).toBeNull();
    expect(screen.queryByText("Working…")).toBeNull();
  });

  it("does not tick while disconnected", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-10-23T10:00:00Z"));
    render(<RunFooter run={running()} disconnected />);
    expect(vi.getTimerCount()).toBe(0);
    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(screen.getByRole("status").textContent).toBe("Reconnecting…");
  });

  it("shows the normal Working… with a timer when connected", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-10-23T10:00:00Z"));
    render(<RunFooter run={running()} />);
    expect(screen.getByRole("status").textContent).toBe("Working…");
    expect(screen.getByTestId("run-working-elapsed")).toBeTruthy();
    expect(vi.getTimerCount()).toBeGreaterThan(0);
  });
});
