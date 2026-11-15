import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// `@/lib/platform` is a module-load constant, so it is mocked per scenario
// and the hook imported fresh (see use-theme-desktop.test.tsx).
function mockPlatform(desktopBuild: boolean) {
  const attentionFocusReport = vi.fn(() => Promise.resolve());
  vi.doMock("@/lib/platform", () => ({
    isDesktop: desktopBuild,
    desktop: { attentionFocusReport },
  }));
  return { attentionFocusReport };
}

function composerTextarea(): HTMLTextAreaElement {
  const el = document.createElement("textarea");
  el.setAttribute("data-composer-input", "");
  document.body.appendChild(el);
  return el;
}

beforeEach(() => {
  vi.useFakeTimers();
  // jsdom's window is never focused; a desktop app mount starts focused.
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.resetModules();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
  document.body.replaceChildren();
});

async function mountHook(taskId: number | null) {
  const { useAttentionReport } = await import("@/hooks/use-attention-report");
  return renderHook((id: number | null) => useAttentionReport(id), { initialProps: taskId });
}

const flush = () => act(() => vi.advanceTimersByTime(100));

describe("useAttentionReport", () => {
  it("reports once on mount with the initial state", async () => {
    const { attentionFocusReport } = mockPlatform(true);
    await mountHook(7);
    expect(attentionFocusReport).toHaveBeenCalledTimes(1);
    expect(attentionFocusReport).toHaveBeenCalledWith({ taskId: 7, windowFocused: true, composerFocused: false });
    flush();
    expect(attentionFocusReport).toHaveBeenCalledTimes(1);
  });

  it("reports on a task change", async () => {
    const { attentionFocusReport } = mockPlatform(true);
    const { rerender } = await mountHook(7);
    rerender(8);
    flush();
    expect(attentionFocusReport).toHaveBeenCalledTimes(2);
    expect(attentionFocusReport).toHaveBeenLastCalledWith({ taskId: 8, windowFocused: true, composerFocused: false });
  });

  it("reports on window blur and focus", async () => {
    const { attentionFocusReport } = mockPlatform(true);
    await mountHook(7);
    act(() => window.dispatchEvent(new Event("blur")));
    flush();
    expect(attentionFocusReport).toHaveBeenLastCalledWith({ taskId: 7, windowFocused: false, composerFocused: false });
    act(() => window.dispatchEvent(new Event("focus")));
    flush();
    expect(attentionFocusReport).toHaveBeenLastCalledWith({ taskId: 7, windowFocused: true, composerFocused: false });
  });

  it("reports the window unfocused when the page becomes hidden", async () => {
    const { attentionFocusReport } = mockPlatform(true);
    await mountHook(7);
    vi.spyOn(document, "hidden", "get").mockReturnValue(true);
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    flush();
    expect(attentionFocusReport).toHaveBeenLastCalledWith({ taskId: 7, windowFocused: false, composerFocused: false });
  });

  it("reports on composer focus and blur", async () => {
    const { attentionFocusReport } = mockPlatform(true);
    await mountHook(7);
    const el = composerTextarea();
    act(() => el.dispatchEvent(new FocusEvent("focusin", { bubbles: true })));
    flush();
    expect(attentionFocusReport).toHaveBeenLastCalledWith({ taskId: 7, windowFocused: true, composerFocused: true });
    act(() => el.dispatchEvent(new FocusEvent("focusout", { bubbles: true })));
    flush();
    expect(attentionFocusReport).toHaveBeenLastCalledWith({ taskId: 7, windowFocused: true, composerFocused: false });
  });

  it("a plain (non-composer) textarea focus does not set composerFocused", async () => {
    const { attentionFocusReport } = mockPlatform(true);
    await mountHook(7);
    const plain = document.createElement("textarea");
    document.body.appendChild(plain);
    act(() => plain.dispatchEvent(new FocusEvent("focusin", { bubbles: true })));
    flush();
    expect(attentionFocusReport).toHaveBeenCalledTimes(1);
    expect(attentionFocusReport).toHaveBeenLastCalledWith({ taskId: 7, windowFocused: true, composerFocused: false });
  });

  it("an unchanged state is not re-sent", async () => {
    const { attentionFocusReport } = mockPlatform(true);
    await mountHook(7);
    act(() => window.dispatchEvent(new Event("focus")));
    flush();
    expect(attentionFocusReport).toHaveBeenCalledTimes(1);
  });

  it("rapid changes within 100 ms collapse to one trailing report with the final state", async () => {
    const { attentionFocusReport } = mockPlatform(true);
    const { rerender } = await mountHook(7);
    rerender(8);
    act(() => window.dispatchEvent(new Event("blur")));
    rerender(9);
    flush();
    expect(attentionFocusReport).toHaveBeenCalledTimes(2);
    expect(attentionFocusReport).toHaveBeenLastCalledWith({ taskId: 9, windowFocused: false, composerFocused: false });
  });

  it("pagehide sends window unfocused at once, without the debounce", async () => {
    const { attentionFocusReport } = mockPlatform(true);
    const { rerender } = await mountHook(7);
    rerender(8); // a pending debounced report that must not need the timer
    act(() => window.dispatchEvent(new Event("pagehide")));
    expect(attentionFocusReport).toHaveBeenCalledTimes(2);
    expect(attentionFocusReport).toHaveBeenLastCalledWith({ taskId: 8, windowFocused: false, composerFocused: false });
    flush();
    expect(attentionFocusReport).toHaveBeenCalledTimes(2);
  });

  it("in the browser build nothing is called", async () => {
    const { attentionFocusReport } = mockPlatform(false);
    const { rerender } = await mountHook(7);
    rerender(8);
    act(() => window.dispatchEvent(new Event("blur")));
    flush();
    act(() => window.dispatchEvent(new Event("pagehide")));
    expect(attentionFocusReport).not.toHaveBeenCalled();
  });

  it("unmount removes the listeners: a later blur sends nothing", async () => {
    const { attentionFocusReport } = mockPlatform(true);
    const { unmount } = await mountHook(7);
    unmount();
    act(() => window.dispatchEvent(new Event("blur")));
    flush();
    expect(attentionFocusReport).toHaveBeenCalledTimes(1);
  });

  it("a focused window re-sends its state every 8 s even when unchanged", async () => {
    const { attentionFocusReport } = mockPlatform(true);
    await mountHook(7);
    act(() => vi.advanceTimersByTime(8000));
    expect(attentionFocusReport).toHaveBeenCalledTimes(2);
    act(() => vi.advanceTimersByTime(8000));
    expect(attentionFocusReport).toHaveBeenCalledTimes(3);
    expect(attentionFocusReport).toHaveBeenLastCalledWith({ taskId: 7, windowFocused: true, composerFocused: false });
  });

  it("an unfocused window sends no heartbeat", async () => {
    const { attentionFocusReport } = mockPlatform(true);
    await mountHook(7);
    act(() => window.dispatchEvent(new Event("blur")));
    flush();
    expect(attentionFocusReport).toHaveBeenCalledTimes(2);
    act(() => vi.advanceTimersByTime(30000));
    expect(attentionFocusReport).toHaveBeenCalledTimes(2);
  });

  it("unmount stops the heartbeat", async () => {
    const { attentionFocusReport } = mockPlatform(true);
    const { unmount } = await mountHook(7);
    unmount();
    act(() => vi.advanceTimersByTime(30000));
    expect(attentionFocusReport).toHaveBeenCalledTimes(1);
  });
});
