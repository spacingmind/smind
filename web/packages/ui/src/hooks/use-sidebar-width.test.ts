import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import {
  SIDEBAR_DEFAULT_WIDTH,
  SIDEBAR_MAX_WIDTH,
  SIDEBAR_MIN_WIDTH,
  useSidebarWidth,
} from "@/hooks/use-sidebar-width";

afterEach(() => {
  window.localStorage.clear();
});

describe("useSidebarWidth", () => {
  it("defaults to SIDEBAR_DEFAULT_WIDTH with nothing persisted yet", () => {
    const { result } = renderHook(() => useSidebarWidth());
    expect(result.current[0]).toBe(SIDEBAR_DEFAULT_WIDTH);
  });

  it("clamps a width above SIDEBAR_MAX_WIDTH down to the max -- dragging past the bound settles at the bound, doesn't grow off-screen", () => {
    const { result } = renderHook(() => useSidebarWidth());

    act(() => {
      result.current[1](10_000);
    });

    expect(result.current[0]).toBe(SIDEBAR_MAX_WIDTH);
  });

  it("clamps a width below SIDEBAR_MIN_WIDTH up to the min -- dragging past the bound settles at the bound, doesn't collapse to 0", () => {
    const { result } = renderHook(() => useSidebarWidth());

    act(() => {
      result.current[1](1);
    });

    expect(result.current[0]).toBe(SIDEBAR_MIN_WIDTH);
  });

  it("accepts a value within bounds unchanged", () => {
    const { result } = renderHook(() => useSidebarWidth());

    act(() => {
      result.current[1](300);
    });

    expect(result.current[0]).toBe(300);
  });

  it("persists the resized width across a fresh mount (simulating a reload)", () => {
    const first = renderHook(() => useSidebarWidth());
    act(() => {
      first.result.current[1](400);
    });
    expect(first.result.current[0]).toBe(400);
    first.unmount();

    // A brand-new hook instance, as a reload would produce -- reads back
    // whatever was last persisted instead of the default.
    const second = renderHook(() => useSidebarWidth());
    expect(second.result.current[0]).toBe(400);
  });

  it("a corrupt/non-numeric persisted value falls back to the default instead of throwing", () => {
    window.localStorage.setItem("smind:sidebar-width", "not-a-number");
    const { result } = renderHook(() => useSidebarWidth());
    expect(result.current[0]).toBe(SIDEBAR_DEFAULT_WIDTH);
  });
});
