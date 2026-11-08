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

describe("HistorySourceNote confirm before starting the agent (ADR-0053)", () => {
  const acp = () => entry({ source: "skeleton", reason: "rebuild_not_requested", canLoadFull: true, loadStartsAgent: true });

  it("the button opens the confirm dialog and sends nothing", () => {
    const onLoad = vi.fn();
    render(<HistorySourceNote run={acp()} onLoad={onLoad} />);
    expect(screen.queryByTestId("history-load-confirm")).toBeNull();
    fireEvent.click(screen.getByTestId("history-load-full"));
    const dialog = screen.getByTestId("history-load-confirm");
    expect(dialog.textContent).toContain("Start the agent to load this history?");
    expect(dialog.textContent).toContain("This starts the agent to read this run's history from its own session. It can take up to 30 seconds. No prompt is sent.");
    expect(onLoad).not.toHaveBeenCalled();
  });

  it("Cancel closes the dialog, sends nothing and leaves the note as it was", () => {
    const onLoad = vi.fn();
    render(<HistorySourceNote run={acp()} onLoad={onLoad} />);
    const before = screen.getByTestId("history-source-note").textContent;
    fireEvent.click(screen.getByTestId("history-load-full"));
    fireEvent.click(screen.getByTestId("history-load-confirm-cancel"));
    expect(screen.queryByTestId("history-load-confirm")).toBeNull();
    expect(onLoad).not.toHaveBeenCalled();
    expect(screen.getByTestId("history-source-note").textContent).toBe(before);
    expect(screen.getByTestId("history-load-full")).toBeTruthy();
  });

  it("Escape cancels", () => {
    const onLoad = vi.fn();
    render(<HistorySourceNote run={acp()} onLoad={onLoad} />);
    fireEvent.click(screen.getByTestId("history-load-full"));
    fireEvent.keyDown(screen.getByTestId("history-load-confirm"), { key: "Escape" });
    expect(screen.queryByTestId("history-load-confirm")).toBeNull();
    expect(onLoad).not.toHaveBeenCalled();
  });

  it("Start agent closes the dialog and loads exactly once", () => {
    const onLoad = vi.fn();
    const run = acp();
    render(<HistorySourceNote run={run} onLoad={onLoad} />);
    fireEvent.click(screen.getByTestId("history-load-full"));
    fireEvent.click(screen.getByTestId("history-load-confirm-start"));
    expect(screen.queryByTestId("history-load-confirm")).toBeNull();
    expect(onLoad).toHaveBeenCalledTimes(1);
    expect(onLoad).toHaveBeenCalledWith(run);
  });

  it("a run without loadStartsAgent loads straight away, no dialog", () => {
    const onLoad = vi.fn();
    render(<HistorySourceNote run={entry({ source: "skeleton", reason: "rebuild_not_requested", canLoadFull: true })} onLoad={onLoad} />);
    fireEvent.click(screen.getByTestId("history-load-full"));
    expect(screen.queryByTestId("history-load-confirm")).toBeNull();
    expect(onLoad).toHaveBeenCalledTimes(1);
  });
});
