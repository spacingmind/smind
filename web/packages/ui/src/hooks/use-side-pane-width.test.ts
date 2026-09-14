import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import {
  SIDE_PANE_DEFAULT_WIDTH,
  SIDE_PANE_MAX_WIDTH,
  SIDE_PANE_MIN_WIDTH,
  useSidePaneWidth,
} from "@/hooks/use-side-pane-width";

afterEach(() => {
  window.localStorage.clear();
});

describe("useSidePaneWidth", () => {
  it("defaults to SIDE_PANE_DEFAULT_WIDTH with nothing persisted yet", () => {
    const { result } = renderHook(() => useSidePaneWidth(1));
    expect(result.current[0]).toBe(SIDE_PANE_DEFAULT_WIDTH);
  });

  it("returns the default when taskId is null, and setting it is a no-op", () => {
    const { result } = renderHook(() => useSidePaneWidth(null));
    expect(result.current[0]).toBe(SIDE_PANE_DEFAULT_WIDTH);
    act(() => result.current[1](600));
    expect(result.current[0]).toBe(SIDE_PANE_DEFAULT_WIDTH);
  });

  it("clamps outside [MIN, MAX]", () => {
    const { result } = renderHook(() => useSidePaneWidth(1));
    act(() => result.current[1](10_000));
    expect(result.current[0]).toBe(SIDE_PANE_MAX_WIDTH);
    act(() => result.current[1](1));
    expect(result.current[0]).toBe(SIDE_PANE_MIN_WIDTH);
  });

  it("persists per task id, independently", () => {
    const first = renderHook(() => useSidePaneWidth(1));
    act(() => first.result.current[1](500));

    const second = renderHook(() => useSidePaneWidth(2));
    expect(second.result.current[0]).toBe(SIDE_PANE_DEFAULT_WIDTH);

    // A fresh mount for task 1, as a reload produces, reads back 500.
    const reload = renderHook(() => useSidePaneWidth(1));
    expect(reload.result.current[0]).toBe(500);
  });
});
