import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { useUnreadTasks } from "@/hooks/use-unread-tasks";
import type { TaskAttention } from "@/hooks/use-task-attention";

afterEach(() => {
  window.localStorage.clear();
});

describe("useUnreadTasks", () => {
  it("does not mark anything unread for attention already present on first render (baseline, not a gain)", () => {
    const { result } = renderHook(() => useUnreadTasks(new Map([[1, new Set(["error"])]]) as TaskAttention, null));
    expect(result.current.unread.has(1)).toBe(false);
  });

  it("marks a task unread when its attention set gains a new reason", () => {
    const { result, rerender } = renderHook(
      ({ attention }: { attention: TaskAttention }) => useUnreadTasks(attention, null),
      { initialProps: { attention: new Map() as TaskAttention } },
    );

    act(() => {
      rerender({ attention: new Map([[1, new Set(["finished"])]]) });
    });

    expect(result.current.unread.has(1)).toBe(true);
  });

  it("does not mark the currently-selected task unread", () => {
    const { result, rerender } = renderHook(
      ({ attention }: { attention: TaskAttention }) => useUnreadTasks(attention, 1),
      { initialProps: { attention: new Map() as TaskAttention } },
    );

    act(() => {
      rerender({ attention: new Map([[1, new Set(["finished"])]]) });
    });

    expect(result.current.unread.has(1)).toBe(false);
  });

  it("opening a task (selecting it) marks it read", () => {
    const { result, rerender } = renderHook(
      ({ attention, selectedTaskId }: { attention: TaskAttention; selectedTaskId: number | null }) =>
        useUnreadTasks(attention, selectedTaskId),
      { initialProps: { attention: new Map() as TaskAttention, selectedTaskId: null as number | null } },
    );

    act(() => {
      rerender({ attention: new Map([[1, new Set(["error"])]]), selectedTaskId: null });
    });
    expect(result.current.unread.has(1)).toBe(true);

    act(() => {
      rerender({ attention: new Map([[1, new Set(["error"])]]), selectedTaskId: 1 });
    });
    expect(result.current.unread.has(1)).toBe(false);
  });

  it("markUnread sets a task unread even with no attention reason", () => {
    const { result } = renderHook(() => useUnreadTasks(new Map() as TaskAttention, null));

    act(() => {
      result.current.markUnread(9);
    });

    expect(result.current.unread.has(9)).toBe(true);
  });

  it("persists unread state across a remount", () => {
    const { result, rerender, unmount } = renderHook(
      ({ attention }: { attention: TaskAttention }) => useUnreadTasks(attention, null),
      { initialProps: { attention: new Map() as TaskAttention } },
    );
    act(() => {
      rerender({ attention: new Map([[7, new Set(["error"])]]) });
    });
    expect(result.current.unread.has(7)).toBe(true);
    unmount();

    const { result: second } = renderHook(() => useUnreadTasks(new Map() as TaskAttention, null));
    expect(second.current.unread.has(7)).toBe(true);
  });

  it("does not re-add a task to unread once it's been read, even if the same reason is still present", () => {
    const { result, rerender } = renderHook(
      ({ attention, selectedTaskId }: { attention: TaskAttention; selectedTaskId: number | null }) =>
        useUnreadTasks(attention, selectedTaskId),
      { initialProps: { attention: new Map() as TaskAttention, selectedTaskId: null as number | null } },
    );

    act(() => {
      rerender({ attention: new Map([[1, new Set(["error"])]]), selectedTaskId: null });
    });
    expect(result.current.unread.has(1)).toBe(true);

    act(() => {
      rerender({ attention: new Map([[1, new Set(["error"])]]), selectedTaskId: 1 });
    });
    expect(result.current.unread.has(1)).toBe(false);

    act(() => {
      rerender({ attention: new Map([[1, new Set(["error"])]]), selectedTaskId: null });
    });
    expect(result.current.unread.has(1)).toBe(false);
  });
});
