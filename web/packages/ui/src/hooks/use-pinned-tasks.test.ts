import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { usePinnedTasks } from "@/hooks/use-pinned-tasks";
import { writePinnedTasks } from "@/lib/sidebar-preferences";

afterEach(() => {
  window.localStorage.clear();
});

describe("usePinnedTasks", () => {
  it("starts with nothing pinned", () => {
    const { result } = renderHook(() => usePinnedTasks(null));
    expect(result.current.pinned.size).toBe(0);
  });

  it("togglePin pins, then unpins, the same task", () => {
    const { result } = renderHook(() => usePinnedTasks(null));

    act(() => result.current.togglePin(1));
    expect(result.current.pinned.has(1)).toBe(true);

    act(() => result.current.togglePin(1));
    expect(result.current.pinned.has(1)).toBe(false);
  });

  it("persists across a remount", () => {
    const { result, unmount } = renderHook(() => usePinnedTasks(null));
    act(() => result.current.togglePin(7));
    unmount();

    const { result: second } = renderHook(() => usePinnedTasks(null));
    expect(second.current.pinned.has(7)).toBe(true);
  });

  describe("pruning against the live task list", () => {
    it("does not prune before the live task list has loaded (liveTaskIds === null)", () => {
      writePinnedTasks(new Set([42]));
      const { result } = renderHook(() => usePinnedTasks(null));
      expect(result.current.pinned.has(42)).toBe(true);
    });

    it("prunes a pinned id once the live task list loads without it (e.g. archived)", () => {
      writePinnedTasks(new Set([42]));
      const { result, rerender } = renderHook(
        ({ liveTaskIds }: { liveTaskIds: ReadonlySet<number> | null }) => usePinnedTasks(liveTaskIds),
        { initialProps: { liveTaskIds: null as ReadonlySet<number> | null } },
      );
      expect(result.current.pinned.has(42)).toBe(true);

      act(() => {
        rerender({ liveTaskIds: new Set([1, 2, 3]) });
      });

      expect(result.current.pinned.has(42)).toBe(false);
    });

    it("keeps a pinned id that is still present in the live task list", () => {
      writePinnedTasks(new Set([42]));
      const { result, rerender } = renderHook(
        ({ liveTaskIds }: { liveTaskIds: ReadonlySet<number> | null }) => usePinnedTasks(liveTaskIds),
        { initialProps: { liveTaskIds: null as ReadonlySet<number> | null } },
      );

      act(() => {
        rerender({ liveTaskIds: new Set([42, 7]) });
      });

      expect(result.current.pinned.has(42)).toBe(true);
    });
  });
});
