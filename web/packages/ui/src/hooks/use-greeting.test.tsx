import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GREETINGS } from "@/lib/greeting";
import { useGreeting } from "./use-greeting";

const set = (h: number, m: number, s = 0) => vi.setSystemTime(new Date(2026, 9, 31, h, m, s));

describe("useGreeting", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("stable across re-render", () => {
    set(10, 0);
    const random = vi.fn(() => 0.5);
    const { result, rerender } = renderHook(() => useGreeting(random));
    const first = result.current;
    for (let i = 0; i < 5; i++) {
      random.mockReturnValue(0);
      rerender();
      act(() => {
        vi.advanceTimersByTime(60_000);
      });
    }
    expect(result.current).toBe(first);
  });

  it("updates on bucket change", () => {
    set(11, 59, 30);
    const { result } = renderHook(() => useGreeting(() => 0));
    expect(GREETINGS.morning).toContain(result.current);
    set(12, 0, 30);
    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    expect(GREETINGS.afternoon).toContain(result.current);
  });

  it("updates on visibilitychange", () => {
    set(11, 59, 30);
    const { result } = renderHook(() => useGreeting(() => 0));
    expect(GREETINGS.morning).toContain(result.current);
    set(18, 5);
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(GREETINGS.evening).toContain(result.current);
  });
});
