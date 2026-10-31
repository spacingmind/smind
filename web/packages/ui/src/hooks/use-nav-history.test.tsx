import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { sameEntry, type NavEntry } from "@/lib/nav-history";
import { useNavHistory } from "@/hooks/use-nav-history";

const task = (taskId: number): NavEntry => ({ view: "workspace", taskId });

function renderNav(initial: NavEntry, apply: (entry: NavEntry) => boolean | Promise<boolean>) {
  return renderHook(({ entry }: { entry: NavEntry }) => useNavHistory(entry, apply), {
    initialProps: { entry: initial },
  });
}

describe("useNavHistory", () => {
  it("records a selection change", () => {
    const apply = vi.fn();
    const { result, rerender } = renderNav(task(1), apply);
    expect(result.current.canBack).toBe(false);
    rerender({ entry: task(2) });
    expect(result.current.canBack).toBe(true);
    expect(result.current.canForward).toBe(false);
    expect(apply).not.toHaveBeenCalled();
  });

  it("stepping does not re-record", async () => {
    const applied: NavEntry[] = [];
    const apply = vi.fn(async (entry: NavEntry) => {
      applied.push(entry);
      return true;
    });
    const { result, rerender } = renderNav(task(1), apply);
    rerender({ entry: task(2) });
    await act(async () => {
      await result.current.back();
    });
    expect(applied).toEqual([task(1)]);
    rerender({ entry: task(1) });
    expect(result.current.canBack).toBe(false);
    expect(result.current.canForward).toBe(true);
    expect(applied).toEqual([task(1)]);
  });

  it("skips a gone task", async () => {
    const apply = vi.fn(async (entry: NavEntry) => !sameEntry(entry, task(2)));
    const { result, rerender } = renderNav(task(1), apply);
    rerender({ entry: task(2) });
    rerender({ entry: task(3) });
    await act(async () => {
      await result.current.back();
    });
    expect(apply.mock.calls.map(([entry]) => entry)).toEqual([task(2), task(1)]);
    expect(result.current.canBack).toBe(false);
    expect(result.current.canForward).toBe(true);
    rerender({ entry: task(1) });
    expect(result.current.canBack).toBe(false);
  });

  it("a fresh selection after back clears forward", async () => {
    const apply = vi.fn(async () => true);
    const { result, rerender } = renderNav(task(1), apply);
    rerender({ entry: task(2) });
    await act(async () => {
      await result.current.back();
    });
    expect(result.current.canForward).toBe(true);
    rerender({ entry: task(3) });
    expect(result.current.canBack).toBe(true);
    expect(result.current.canForward).toBe(false);
  });
});
