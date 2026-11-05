import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import { HistorySourceNote } from "@/components/timeline/history-source-note";
import type { RunEntry } from "@/hooks/use-run-timeline";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function entry(history: RunEntry["history"]): RunEntry {
  return { id: "r1", provider: "claude-native", prompt: "p", status: "done", startedAt: "2026-11-01T09:59:00Z", items: [], permissionMode: "", autoAccept: false, thinkingLevel: "", history };
}

const WARNING_REASONS = ["native_session_missing", "slice_not_found", "format_drift", "rebuild_failed"];

describe("HistorySourceNote", () => {
  it("a loadable skeleton shows the trimmed sentence and a Load full history button that hands the run back", () => {
    const onLoad = vi.fn();
    const run = entry({ source: "skeleton", reason: "rebuild_not_requested", canLoadFull: true });
    render(<HistorySourceNote run={run} onLoad={onLoad} />);
    expect(screen.getByTestId("history-source-note").textContent).toContain("Showing a summary: large tool output was trimmed.");
    expect(screen.getByTestId("history-source-note").getAttribute("data-tone")).toBe("info");
    fireEvent.click(screen.getByTestId("history-load-full"));
    expect(onLoad).toHaveBeenCalledWith(run);
  });

  it("while loading it says Loading full history… and offers no button", () => {
    render(<HistorySourceNote run={entry({ source: "skeleton", reason: "rebuild_not_requested", canLoadFull: true, loading: true })} onLoad={vi.fn()} />);
    expect(screen.getByTestId("history-source-note").textContent).toContain("Loading full history…");
    expect(screen.getByTestId("history-loading")).toBeTruthy();
    expect(screen.queryByTestId("history-load-full")).toBeNull();
  });

  it("a rebuilt history states it and shows no button", () => {
    render(<HistorySourceNote run={entry({ source: "rebuilt" })} onLoad={vi.fn()} />);
    expect(screen.getByTestId("history-source-note").textContent).toBe("Full history, rebuilt from the provider's session.");
    expect(screen.getByTestId("history-source-note").getAttribute("data-source")).toBe("rebuilt");
    expect(screen.queryByTestId("history-load-full")).toBeNull();
  });

  it.each(WARNING_REASONS)("a %s skeleton renders the warning tone and no button", (reason) => {
    render(<HistorySourceNote run={entry({ source: "skeleton", reason })} onLoad={vi.fn()} />);
    const note = screen.getByTestId("history-source-note");
    expect(note.getAttribute("data-tone")).toBe("warning");
    expect(note.getAttribute("data-reason")).toBe(reason);
    expect(note.className).toContain("bg-warning/10");
    expect(screen.queryByTestId("history-load-full")).toBeNull();
  });

  it("a skeleton that cannot be rebuilt shows the sentence only (never a disabled button)", () => {
    render(<HistorySourceNote run={entry({ source: "skeleton", reason: "rebuild_not_requested", canLoadFull: false })} onLoad={vi.fn()} />);
    expect(screen.getByTestId("history-source-note").textContent).toContain("Showing a summary: large tool output was trimmed.");
    expect(screen.queryByTestId("history-load-full")).toBeNull();
  });

  it("a full or unknown history renders nothing", () => {
    const { container, rerender } = render(<HistorySourceNote run={entry(undefined)} />);
    expect(container.querySelector('[data-testid="history-source-note"]')).toBeNull();
    rerender(<HistorySourceNote run={entry({ source: "full" })} />);
    expect(container.querySelector('[data-testid="history-source-note"]')).toBeNull();
  });
});
