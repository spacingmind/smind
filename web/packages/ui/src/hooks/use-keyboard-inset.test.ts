import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useKeyboardInset } from "@/hooks/use-keyboard-inset";

const originalHeight = window.innerHeight;

function installViewport(height: number, offsetTop = 0) {
  const listeners: Record<string, () => void> = {};
  const vv = {
    height,
    offsetTop,
    addEventListener: vi.fn((type: string, fn: () => void) => {
      listeners[type] = fn;
    }),
    removeEventListener: vi.fn(),
  };
  Object.defineProperty(window, "visualViewport", { configurable: true, value: vv });
  window.innerHeight = 800;
  return { vv, listeners };
}

afterEach(() => {
  Object.defineProperty(window, "visualViewport", { configurable: true, value: undefined });
  window.innerHeight = originalHeight;
});

describe("useKeyboardInset", () => {
  it("returns 0 without visualViewport", () => {
    const { result } = renderHook(() => useKeyboardInset());
    expect(result.current).toBe(0);
  });

  it("returns innerHeight - height - offsetTop when set", () => {
    installViewport(500, 50);
    const { result } = renderHook(() => useKeyboardInset());
    expect(result.current).toBe(250);
  });

  it("updates after the resize listener fires", () => {
    const { vv, listeners } = installViewport(500);
    const { result } = renderHook(() => useKeyboardInset());
    expect(result.current).toBe(300);
    vv.height = 800;
    act(() => listeners.resize());
    expect(result.current).toBe(0);
  });

  it("clamps negatives to 0", () => {
    installViewport(900);
    const { result } = renderHook(() => useKeyboardInset());
    expect(result.current).toBe(0);
  });

  it("removes the listeners on unmount", () => {
    const { vv } = installViewport(500);
    const { unmount } = renderHook(() => useKeyboardInset());
    unmount();
    expect(vv.removeEventListener).toHaveBeenCalledWith("resize", expect.any(Function));
    expect(vv.removeEventListener).toHaveBeenCalledWith("scroll", expect.any(Function));
  });
});
