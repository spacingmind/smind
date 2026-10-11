import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { useCollapsedTasks } from "@/hooks/use-collapsed-tasks";
import { writeCollapsedTasks } from "@/lib/sidebar-preferences";

afterEach(() => {
  window.localStorage.clear();
});

describe("useCollapsedTasks", () => {
  it("starts with nothing collapsed (everything expanded)", () => {
    const { result } = renderHook(() => useCollapsedTasks(null));
    expect(result.current.collapsed.size).toBe(0);
  });

  it("toggle collapses, then expands, the same parent", () => {
    const { result } = renderHook(() => useCollapsedTasks(null));

    act(() => result.current.toggle(1));
    expect(result.current.collapsed.has(1)).toBe(true);

    act(() => result.current.toggle(1));
    expect(result.current.collapsed.has(1)).toBe(false);
  });

  it("expand removes several collapsed ids at once and persists across a remount", () => {
    const { result, unmount } = renderHook(() => useCollapsedTasks(null));
    act(() => {
      result.current.toggle(1);
      result.current.toggle(2);
    });

    act(() => result.current.expand([1, 2, 99]));
    expect(result.current.collapsed.size).toBe(0);
    unmount();

    const { result: second } = renderHook(() => useCollapsedTasks(null));
    expect(second.current.collapsed.size).toBe(0);
  });

  it("prunes a collapsed id once the live task list loads without it (e.g. archived)", () => {
    writeCollapsedTasks(new Set([42]));
    const { result, rerender } = renderHook(
      ({ liveTaskIds }: { liveTaskIds: ReadonlySet<number> | null }) => useCollapsedTasks(liveTaskIds),
      { initialProps: { liveTaskIds: null as ReadonlySet<number> | null } },
    );
    expect(result.current.collapsed.has(42)).toBe(true);

    act(() => {
      rerender({ liveTaskIds: new Set([1, 2]) });
    });

    expect(result.current.collapsed.has(42)).toBe(false);
  });
});
