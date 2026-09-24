import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { useUnreadTasks } from "@/hooks/use-unread-tasks";
import type { TaskAttention } from "@/hooks/use-task-attention";
import { writeUnreadTasks } from "@/lib/sidebar-preferences";

/** jsdom's document.hidden is a plain (non-configurable by default) getter -- redefine it per test, same helper use-attention-notifications.test.ts uses. */
function setDocumentHidden(hidden: boolean) {
  Object.defineProperty(document, "hidden", { value: hidden, configurable: true });
}

afterEach(() => {
  window.localStorage.clear();
  setDocumentHidden(false);
});

describe("useUnreadTasks", () => {
  it("does not mark anything unread for attention already present on first render (baseline, not a gain)", () => {
    const { result } = renderHook(() =>
      useUnreadTasks(new Map([[1, new Set(["error"])]]) as TaskAttention, null, null),
    );
    expect(result.current.unread.has(1)).toBe(false);
  });

  it("marks a task unread when its attention set gains a new reason", () => {
    const { result, rerender } = renderHook(
      ({ attention }: { attention: TaskAttention }) => useUnreadTasks(attention, null, null),
      { initialProps: { attention: new Map() as TaskAttention } },
    );

    act(() => {
      rerender({ attention: new Map([[1, new Set(["finished"])]]) });
    });

    expect(result.current.unread.has(1)).toBe(true);
  });

  it("does not mark the currently-selected task unread while the document is visible", () => {
    setDocumentHidden(false);
    const { result, rerender } = renderHook(
      ({ attention }: { attention: TaskAttention }) => useUnreadTasks(attention, 1, null),
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
        useUnreadTasks(attention, selectedTaskId, null),
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
    const { result } = renderHook(() => useUnreadTasks(new Map() as TaskAttention, null, null));

    act(() => {
      result.current.markUnread(9);
    });

    expect(result.current.unread.has(9)).toBe(true);
  });

  it("persists unread state across a remount", () => {
    const { result, rerender, unmount } = renderHook(
      ({ attention }: { attention: TaskAttention }) => useUnreadTasks(attention, null, null),
      { initialProps: { attention: new Map() as TaskAttention } },
    );
    act(() => {
      rerender({ attention: new Map([[7, new Set(["error"])]]) });
    });
    expect(result.current.unread.has(7)).toBe(true);
    unmount();

    const { result: second } = renderHook(() => useUnreadTasks(new Map() as TaskAttention, null, null));
    expect(second.current.unread.has(7)).toBe(true);
  });

  it("does not re-add a task to unread once it's been read, even if the same reason is still present", () => {
    const { result, rerender } = renderHook(
      ({ attention, selectedTaskId }: { attention: TaskAttention; selectedTaskId: number | null }) =>
        useUnreadTasks(attention, selectedTaskId, null),
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

  describe("hidden-window gap (review fix)", () => {
    it("marks the currently-selected task unread when a new reason arrives while the document is hidden", () => {
      setDocumentHidden(true);
      const { result, rerender } = renderHook(
        ({ attention }: { attention: TaskAttention }) => useUnreadTasks(attention, 1, null),
        { initialProps: { attention: new Map() as TaskAttention } },
      );

      act(() => {
        rerender({ attention: new Map([[1, new Set(["error"])]]) });
      });

      expect(result.current.unread.has(1)).toBe(true);
    });

    it("becoming visible again clears the unread flag for the still-selected task", () => {
      setDocumentHidden(true);
      const { result, rerender } = renderHook(
        ({ attention }: { attention: TaskAttention }) => useUnreadTasks(attention, 1, null),
        { initialProps: { attention: new Map() as TaskAttention } },
      );
      act(() => {
        rerender({ attention: new Map([[1, new Set(["error"])]]) });
      });
      expect(result.current.unread.has(1)).toBe(true);

      act(() => {
        setDocumentHidden(false);
        document.dispatchEvent(new Event("visibilitychange"));
      });

      expect(result.current.unread.has(1)).toBe(false);
    });

    it("becoming visible again does not clear unread for a task that is not selected", () => {
      setDocumentHidden(true);
      const { result, rerender } = renderHook(
        ({ attention }: { attention: TaskAttention }) => useUnreadTasks(attention, null, null),
        { initialProps: { attention: new Map() as TaskAttention } },
      );
      act(() => {
        rerender({ attention: new Map([[1, new Set(["error"])]]) });
      });
      expect(result.current.unread.has(1)).toBe(true);

      act(() => {
        setDocumentHidden(false);
        document.dispatchEvent(new Event("visibilitychange"));
      });

      expect(result.current.unread.has(1)).toBe(true);
    });
  });

  describe("pruning against the live task list (review fix)", () => {
    it("the persisted set survives the initial empty-list render (liveTaskIds === null)", () => {
      writeUnreadTasks(new Set([42]));
      const { result } = renderHook(() => useUnreadTasks(new Map() as TaskAttention, null, null));
      expect(result.current.unread.has(42)).toBe(true);
    });

    it("an archived unread task drops out of the count once the live task list loads without it", () => {
      writeUnreadTasks(new Set([42]));
      const { result, rerender } = renderHook(
        ({ liveTaskIds }: { liveTaskIds: ReadonlySet<number> | null }) =>
          useUnreadTasks(new Map() as TaskAttention, null, liveTaskIds),
        { initialProps: { liveTaskIds: null as ReadonlySet<number> | null } },
      );
      expect(result.current.unread.has(42)).toBe(true);

      act(() => {
        rerender({ liveTaskIds: new Set([1, 2, 3]) });
      });

      expect(result.current.unread.has(42)).toBe(false);
    });

    it("keeps an unread id that is still present in the live task list", () => {
      writeUnreadTasks(new Set([42]));
      const { result, rerender } = renderHook(
        ({ liveTaskIds }: { liveTaskIds: ReadonlySet<number> | null }) =>
          useUnreadTasks(new Map() as TaskAttention, null, liveTaskIds),
        { initialProps: { liveTaskIds: null as ReadonlySet<number> | null } },
      );

      act(() => {
        rerender({ liveTaskIds: new Set([42, 7]) });
      });

      expect(result.current.unread.has(42)).toBe(true);
    });
  });
});
