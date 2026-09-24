import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useAttentionNotifications, type NotifiableTask } from "@/hooks/use-attention-notifications";
import type { TaskAttention } from "@/hooks/use-task-attention";

const TASKS: NotifiableTask[] = [{ ID: 1, Title: "Fix the bug" }];

/** No-op stand-in for the `onOpenTask` param in tests that don't exercise the click handler. */
const NOOP_OPEN = () => {};

/** Records every `new Notification(title, options)` call made during a test, standing in for jsdom (which has no real Notification API -- see use-notification-permission.test.ts for the same gap). Each recorded call also carries the instance, so a test can invoke its `onclick`. */
function installFakeNotification() {
  const calls: { title: string; options?: NotificationOptions; instance: FakeNotification }[] = [];
  class FakeNotification {
    onclick: (() => void) | null = null;
    constructor(
      public title: string,
      public options?: NotificationOptions,
    ) {
      calls.push({ title, options, instance: this });
    }
  }
  vi.stubGlobal("Notification", FakeNotification);
  return calls;
}

/** jsdom's document.hidden is a plain (non-configurable by default) getter -- redefine it per test so visibilitychange-adjacent behavior (here, just reading document.hidden at notify time) can be exercised both ways. */
function setDocumentHidden(hidden: boolean) {
  Object.defineProperty(document, "hidden", { value: hidden, configurable: true });
}

afterEach(() => {
  vi.unstubAllGlobals();
  setDocumentHidden(false);
});

describe("useAttentionNotifications", () => {
  it("fires once when a task's attention set gains a new reason while the tab is backgrounded", () => {
    const calls = installFakeNotification();
    setDocumentHidden(true);

    const { rerender } = renderHook(
      ({ attention }: { attention: TaskAttention }) => useAttentionNotifications(attention, TASKS, "granted", NOOP_OPEN),
      { initialProps: { attention: new Map() as TaskAttention } },
    );
    expect(calls).toHaveLength(0);

    act(() => {
      rerender({ attention: new Map([[1, new Set(["error"])]]) });
    });

    expect(calls).toHaveLength(1);
    expect(calls[0]!.title).toBe("Fix the bug");
  });

  it("does not fire while the tab is focused (document.hidden false)", () => {
    const calls = installFakeNotification();
    setDocumentHidden(false);

    const { rerender } = renderHook(
      ({ attention }: { attention: TaskAttention }) => useAttentionNotifications(attention, TASKS, "granted", NOOP_OPEN),
      { initialProps: { attention: new Map() as TaskAttention } },
    );

    act(() => {
      rerender({ attention: new Map([[1, new Set(["finished"])]]) });
    });

    expect(calls).toHaveLength(0);
  });

  it("permission denied: no notification fires, and nothing throws", () => {
    const calls = installFakeNotification();
    setDocumentHidden(true);

    const { rerender } = renderHook(
      ({ attention }: { attention: TaskAttention }) => useAttentionNotifications(attention, TASKS, "denied", NOOP_OPEN),
      { initialProps: { attention: new Map() as TaskAttention } },
    );

    expect(() =>
      act(() => {
        rerender({ attention: new Map([[1, new Set(["permission"])]]) });
      }),
    ).not.toThrow();
    expect(calls).toHaveLength(0);
  });

  it("does not re-fire for the same (task, reason) pair while it stays present -- only a genuinely new gain notifies", () => {
    const calls = installFakeNotification();
    setDocumentHidden(true);

    const { rerender } = renderHook(
      ({ attention }: { attention: TaskAttention }) => useAttentionNotifications(attention, TASKS, "granted", NOOP_OPEN),
      { initialProps: { attention: new Map() as TaskAttention } },
    );

    act(() => {
      rerender({ attention: new Map([[1, new Set(["error"])]]) });
    });
    expect(calls).toHaveLength(1);

    // Same reason, still present, plus an unrelated rerender -- must not
    // notify again.
    act(() => {
      rerender({ attention: new Map([[1, new Set(["error"])]]) });
    });
    expect(calls).toHaveLength(1);

    // A second, distinct reason on the same task does notify (it's a new
    // (task, reason) pair).
    act(() => {
      rerender({ attention: new Map([[1, new Set(["error", "finished"])]]) });
    });
    expect(calls).toHaveLength(2);
  });

  it("does not notify for attention state already present on the very first render (baseline, not a gain)", () => {
    const calls = installFakeNotification();
    setDocumentHidden(true);

    renderHook(() =>
      useAttentionNotifications(new Map([[1, new Set(["error"])]]) as TaskAttention, TASKS, "granted", NOOP_OPEN),
    );

    expect(calls).toHaveLength(0);
  });

  it("re-notifies after a reason clears and later recurs", () => {
    const calls = installFakeNotification();
    setDocumentHidden(true);

    const { rerender } = renderHook(
      ({ attention }: { attention: TaskAttention }) => useAttentionNotifications(attention, TASKS, "granted", NOOP_OPEN),
      { initialProps: { attention: new Map() as TaskAttention } },
    );

    act(() => {
      rerender({ attention: new Map([[1, new Set(["error"])]]) });
    });
    expect(calls).toHaveLength(1);

    act(() => {
      rerender({ attention: new Map() });
    });
    act(() => {
      rerender({ attention: new Map([[1, new Set(["error"])]]) });
    });
    expect(calls).toHaveLength(2);
  });

  it("a Notification constructor that throws is swallowed, not propagated", () => {
    class ThrowingNotification {
      constructor() {
        throw new Error("blocked by platform policy");
      }
    }
    vi.stubGlobal("Notification", ThrowingNotification);
    setDocumentHidden(true);

    const { rerender } = renderHook(
      ({ attention }: { attention: TaskAttention }) => useAttentionNotifications(attention, TASKS, "granted", NOOP_OPEN),
      { initialProps: { attention: new Map() as TaskAttention } },
    );

    expect(() =>
      act(() => {
        rerender({ attention: new Map([[1, new Set(["error"])]]) });
      }),
    ).not.toThrow();
  });

  it("clicking a fired notification focuses the window and opens its task (AC1)", () => {
    const calls = installFakeNotification();
    setDocumentHidden(true);
    const onOpenTask = vi.fn();
    const focusSpy = vi.spyOn(window, "focus").mockImplementation(() => {});

    const { rerender } = renderHook(
      ({ attention }: { attention: TaskAttention }) => useAttentionNotifications(attention, TASKS, "granted", onOpenTask),
      { initialProps: { attention: new Map() as TaskAttention } },
    );

    act(() => {
      rerender({ attention: new Map([[1, new Set(["error"])]]) });
    });
    expect(calls).toHaveLength(1);

    calls[0]!.instance.onclick?.();

    expect(focusSpy).toHaveBeenCalled();
    expect(onOpenTask).toHaveBeenCalledWith(1);
    focusSpy.mockRestore();
  });
});
