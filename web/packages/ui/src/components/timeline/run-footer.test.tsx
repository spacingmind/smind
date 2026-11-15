import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";

import { RunFooter } from "@/components/timeline/run-footer";
import type { RunFooterRun } from "@/components/timeline/run-footer-model";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

function base(): RunFooterRun {
  return { id: "run-1", provider: "glm", status: "running", startedAt: "2026-10-23T09:59:00Z" };
}

describe("RunFooter (the transcript footer only states an ended run)", () => {
  it("transcript-footer-only-states-an-ended-run", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-10-23T10:00:00Z"));

    // A live run renders nothing here -- its state lives under the composer.
    const { container, rerender } = render(<RunFooter run={base()} />);
    expect(container).toBeEmptyDOMElement();

    // A finished run still renders the ended-run sentence and the duration.
    rerender(<RunFooter run={{ ...base(), status: "done", finishedAt: "2026-10-23T09:59:54Z", stopReason: "end_turn" }} />);
    expect(screen.getByTestId("run-footer")).toBeInTheDocument();
    expect(screen.getByTestId("run-elapsed")).toHaveTextContent("54s");
    expect(screen.getByTestId("run-meta")).toHaveAttribute("data-status", "done");

    rerender(<RunFooter run={{ ...base(), status: "stopped", finishedAt: "2026-10-23T09:59:54Z", stopReason: "cancelled" }} />);
    expect(screen.getByTestId("run-stop-reason")).toHaveTextContent("Stopped by you");

    rerender(<RunFooter run={{ ...base(), status: "error", finishedAt: "2026-10-23T09:59:54Z", err: "boom" }} />);
    expect(screen.getByTestId("run-stop-reason")).toHaveTextContent("Failed after 54s");
  });

  it("a running run mounts no ticker (the elapsed timer lives under the composer)", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-10-23T10:00:00Z"));
    const { container } = render(<RunFooter run={base()} />);
    expect(container).toBeEmptyDOMElement();
    expect(vi.getTimerCount()).toBe(0);
    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(container).toBeEmptyDOMElement();
  });
});
