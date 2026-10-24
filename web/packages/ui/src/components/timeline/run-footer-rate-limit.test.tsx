import { act } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import { RunFooter } from "@/components/timeline/run-footer";
import type { RunFailover } from "@/components/timeline/run-failover-actions";
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

function failoverStub(overrides: Partial<RunFailover> = {}): RunFailover {
  return {
    fallbackName: (id) => (id === 9 ? "Backup GLM" : undefined),
    retryOnFallback: vi.fn().mockResolvedValue(undefined),
    waitUntil: vi.fn().mockResolvedValue(undefined),
    isHeld: () => false,
    successor: () => undefined,
    continues: () => undefined,
    openChat: vi.fn(),
    ...overrides,
  };
}

describe("RunFooter failover actions (ADR-0041 F2)", () => {
  const NOW = new Date("2026-10-13T10:00:30Z");
  const future = { failureResetAt: "2026-10-13T12:00:00Z" };

  it("shows Retry on fallback", () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    const failover = failoverStub();
    render(<RunFooter run={limited({ ...future, fallbackProfileId: 9 })} isLatest onRerun={() => {}} failover={failover} />);
    const button = screen.getByTestId("run-retry-fallback");
    expect(button.textContent).toBe("Retry on Backup GLM");
    fireEvent.click(button);
    expect(failover.retryOnFallback).toHaveBeenCalledTimes(1);
  });

  it("hides Retry on fallback without fallback", () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    const { rerender } = render(<RunFooter run={limited(future)} isLatest onRerun={() => {}} failover={failoverStub()} />);
    expect(screen.queryByTestId("run-retry-fallback")).toBeNull();
    // A fallback id the profile list does not know is hidden too; so is a non-latest run.
    rerender(<RunFooter run={limited({ ...future, fallbackProfileId: 4 })} isLatest onRerun={() => {}} failover={failoverStub()} />);
    expect(screen.queryByTestId("run-retry-fallback")).toBeNull();
    rerender(<RunFooter run={limited({ ...future, fallbackProfileId: 9 })} onRerun={() => {}} failover={failoverStub()} />);
    expect(screen.queryByTestId("run-retry-fallback")).toBeNull();
    expect(screen.queryByTestId("run-wait")).toBeNull();
  });

  it("Wait until label and click sends notBefore", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    const failover = failoverStub();
    render(<RunFooter run={limited({ failureResetAt: "2026-10-13T12:00:18Z" })} isLatest onRerun={() => {}} failover={failover} />);
    const wait = screen.getByTestId("run-wait");
    expect(wait.textContent).toMatch(/^Wait until (\w{3} )?\d\d:\d\d$/);
    await act(async () => {
      fireEvent.click(wait);
      fireEvent.click(wait);
    });
    // Pending/queued: no second submit, the button is replaced by the queued text.
    expect(failover.waitUntil).toHaveBeenCalledTimes(1);
    expect(failover.waitUntil).toHaveBeenCalledWith(expect.objectContaining({ id: "run-1" }), "2026-10-13T12:00:48.000Z");
    expect(screen.queryByTestId("run-wait")).toBeNull();
    expect(screen.getByTestId("run-wait-queued").textContent).toMatch(/^Queued · starts after (\w{3} )?\d\d:\d\d$/);
  });

  it("Wait and Queued labels carry ≈ for an inferred reset", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    render(<RunFooter run={limited({ failureResetAt: "2026-10-13T12:00:18Z", failureResetInferred: true })} isLatest onRerun={() => {}} failover={failoverStub()} />);
    const wait = screen.getByTestId("run-wait");
    expect(wait.textContent).toMatch(/^Wait until (\w{3} )?≈\d\d:\d\d$/);
    await act(async () => {
      fireEvent.click(wait);
    });
    expect(screen.getByTestId("run-wait-queued").textContent).toMatch(/^Queued · starts after (\w{3} )?≈\d\d:\d\d$/);
  });

  it("brings the Wait button back with a toast when queuing fails", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    const failover = failoverStub({ waitUntil: vi.fn().mockRejectedValue(new Error("boom")) });
    render(<RunFooter run={limited(future)} isLatest onRerun={() => {}} failover={failover} />);
    await act(async () => {
      fireEvent.click(screen.getByTestId("run-wait"));
    });
    expect(screen.getByTestId("run-wait")).not.toBeDisabled();
    expect(screen.queryByTestId("run-wait-queued")).toBeNull();
  });

  it("shows the queued text straight away when a held retry is already in the queue", () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    render(<RunFooter run={limited(future)} isLatest onRerun={() => {}} failover={failoverStub({ isHeld: () => true })} />);
    expect(screen.queryByTestId("run-wait")).toBeNull();
    expect(screen.getByTestId("run-wait-queued")).toBeTruthy();
  });

  it("no Wait when reset unknown or past", () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    const { rerender } = render(<RunFooter run={limited()} isLatest onRerun={() => {}} failover={failoverStub()} />);
    expect(screen.queryByTestId("run-wait")).toBeNull();
    rerender(<RunFooter run={limited({ failureResetAt: "2026-10-13T09:00:00Z" })} isLatest onRerun={() => {}} failover={failoverStub()} />);
    expect(screen.queryByTestId("run-wait")).toBeNull();
    expect(screen.getByTestId("run-retry")).toBeTruthy();
  });

  it("offers neither action once a successor exists", () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    const failover = failoverStub({ successor: () => ({ runId: "run-2", chatId: 11, provider: "glm" }) });
    render(<RunFooter run={limited({ ...future, fallbackProfileId: 9 })} isLatest onRerun={() => {}} failover={failover} />);
    expect(screen.queryByTestId("run-retry-fallback")).toBeNull();
    expect(screen.queryByTestId("run-wait")).toBeNull();
    expect(screen.getByTestId("run-retry")).toBeTruthy();
  });

  it("continued-on badge links to the successor chat", () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    const failover = failoverStub({ successor: () => ({ runId: "run-2", chatId: 11, provider: "glm" }) });
    render(<RunFooter run={limited(future)} isLatest providerLabels={{ glm: "GLM" }} failover={failover} />);
    const badge = screen.getByTestId("run-continued-on");
    expect(badge.textContent).toBe("continued on GLM in chat #11");
    fireEvent.click(badge);
    expect(failover.openChat).toHaveBeenCalledWith(11);
    expect(screen.queryByTestId("run-continues")).toBeNull();
  });

  it("successor shows continues hint", () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    const failover = failoverStub({ continues: (id) => (id === "run-2" ? "abcdef0123456789" : undefined) });
    render(<RunFooter run={{ ...limited(), id: "run-2", status: "done", failureKind: undefined }} isLatest failover={failover} />);
    expect(screen.getByTestId("run-continues").textContent).toBe("continues run abcdef01");
    expect(screen.queryByTestId("run-continued-on")).toBeNull();
  });
});
