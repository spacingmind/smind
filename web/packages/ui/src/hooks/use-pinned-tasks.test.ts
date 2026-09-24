import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { usePinnedTasks } from "@/hooks/use-pinned-tasks";

afterEach(() => {
  window.localStorage.clear();
});

describe("usePinnedTasks", () => {
  it("starts with nothing pinned", () => {
    const { result } = renderHook(() => usePinnedTasks());
    expect(result.current.pinned.size).toBe(0);
  });

  it("togglePin pins, then unpins, the same task", () => {
    const { result } = renderHook(() => usePinnedTasks());

    act(() => result.current.togglePin(1));
    expect(result.current.pinned.has(1)).toBe(true);

    act(() => result.current.togglePin(1));
    expect(result.current.pinned.has(1)).toBe(false);
  });

  it("persists across a remount", () => {
    const { result, unmount } = renderHook(() => usePinnedTasks());
    act(() => result.current.togglePin(7));
    unmount();

    const { result: second } = renderHook(() => usePinnedTasks());
    expect(second.current.pinned.has(7)).toBe(true);
  });
});
