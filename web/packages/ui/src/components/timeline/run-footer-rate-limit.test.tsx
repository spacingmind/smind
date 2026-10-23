import { act } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import { RunFooter } from "@/components/timeline/run-footer";
import type { RunFooterRun } from "@/components/timeline/run-footer-model";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

function limited(overrides: Partial<RunFooterRun> = {}): RunFooterRun {
  return {
    id: "run-1",
    provider: "glm",
    status: "error",
    startedAt: "2026-10-13T09:59:00Z",
    finishedAt: "2026-10-13T10:00:00Z",
    failureKind: "rate_limited",
    failureWindow: "5h",
    ...overrides,
  };
}

describe("RunFooter rate limit (ADR-0041 R3)", () => {
  it("renders the warning sentence with Retry on the latest run", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-10-13T10:00:30Z"));
    render(<RunFooter run={limited({ failureResetAt: "2026-10-13T12:00:00Z" })} isLatest onRerun={() => {}} />);
    const sentence = screen.getByTestId("run-stop-reason");
    expect(sentence.textContent).toMatch(/^Rate limited \(5h window\) · resets \d\d:\d\d$|^Rate limited \(5h window\) · resets \w{3} \d\d:\d\d$/);
    expect(sentence.dataset.tone).toBe("warning");
    expect(screen.getByTestId("run-retry")).toBeTruthy();
  });

  it("re-evaluates once a minute: the sentence flips when the reset passes", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-10-13T10:00:30Z"));
    render(<RunFooter run={limited({ failureResetAt: "2026-10-13T10:02:00Z" })} />);
    expect(screen.getByTestId("run-stop-reason").textContent).toContain("resets");
    act(() => {
      vi.advanceTimersByTime(3 * 60_000);
    });
    expect(screen.getByTestId("run-stop-reason").textContent).toBe("Rate limited (5h window) · limit has reset");
  });
});
