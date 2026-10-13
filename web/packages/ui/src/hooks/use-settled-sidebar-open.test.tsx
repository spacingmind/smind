import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useSettledSidebarOpen } from "@/hooks/use-settled-sidebar-open";

function panel() {
  const el = document.createElement("div");
  document.body.appendChild(el);
  return { current: el };
}

function withDuration(value: string, property = "width") {
  const real = window.getComputedStyle.bind(window);
  vi.spyOn(window, "getComputedStyle").mockImplementation((el: Element) => {
    const style = real(el);
    return new Proxy(style, {
      get: (t, p) => (p === "transitionDuration" ? value : p === "transitionProperty" ? property : Reflect.get(t, p)),
    });
  });
}

describe("useSettledSidebarOpen", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("follows `open` at once when nothing animates (no transition, tests, reduced motion)", () => {
    const ref = panel();
    const { result, rerender } = renderHook(({ open }) => useSettledSidebarOpen(open, ref, true), { initialProps: { open: true } });
    expect(result.current).toBe(true);
    rerender({ open: false });
    expect(result.current).toBe(false);
  });

  it("follows `open` at once under reduced motion (duration kept, property none) -- no hidden gap", () => {
    withDuration("0.2s", "none");
    const ref = panel();
    const { result, rerender } = renderHook(({ open }) => useSettledSidebarOpen(open, ref, true), { initialProps: { open: true } });
    rerender({ open: false });
    expect(result.current).toBe(false);
  });

  it("follows `open` at once when disabled (compact sheet), even with a transition", () => {
    withDuration("0.2s");
    const ref = panel();
    const { result, rerender } = renderHook(({ open }) => useSettledSidebarOpen(open, ref, false), { initialProps: { open: true } });
    rerender({ open: false });
    expect(result.current).toBe(false);
  });

  it("trails `open` until the width transition ends", () => {
    withDuration("0.2s");
    const ref = panel();
    const { result, rerender } = renderHook(({ open }) => useSettledSidebarOpen(open, ref, true), { initialProps: { open: true } });
    rerender({ open: false });
    expect(result.current).toBe(true);
    // Another property finishing does not settle it.
    act(() => {
      const e = new Event("transitionend") as TransitionEvent & { propertyName: string };
      Object.assign(e, { propertyName: "opacity" });
      ref.current.dispatchEvent(e);
    });
    expect(result.current).toBe(true);
    act(() => {
      const e = new Event("transitionend") as TransitionEvent & { propertyName: string };
      Object.assign(e, { propertyName: "width" });
      ref.current.dispatchEvent(e);
    });
    expect(result.current).toBe(false);
  });

  it("falls back to a timer when transitionend never fires", () => {
    withDuration("200ms");
    const ref = panel();
    const { result, rerender } = renderHook(({ open }) => useSettledSidebarOpen(open, ref, true), { initialProps: { open: false } });
    rerender({ open: true });
    expect(result.current).toBe(false);
    act(() => void vi.advanceTimersByTime(349));
    expect(result.current).toBe(false);
    act(() => void vi.advanceTimersByTime(2));
    expect(result.current).toBe(true);
  });

  it("a toggle back mid-flight settles on the original state without flipping the layout", () => {
    withDuration("0.2s");
    const ref = panel();
    const { result, rerender } = renderHook(({ open }) => useSettledSidebarOpen(open, ref, true), { initialProps: { open: true } });
    rerender({ open: false });
    rerender({ open: true });
    act(() => void vi.advanceTimersByTime(1000));
    expect(result.current).toBe(true);
  });
});
