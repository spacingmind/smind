import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { useSidebarGroupMode } from "@/hooks/use-sidebar-group-mode";
import { readSidebarGroupMode } from "@/lib/sidebar-preferences";

afterEach(() => {
  window.localStorage.clear();
});

describe("useSidebarGroupMode", () => {
  it("defaults to the tree view", () => {
    const { result } = renderHook(() => useSidebarGroupMode());
    expect(result.current.groupMode).toBe("tree");
  });

  it("setGroupMode updates state and persists", () => {
    const { result } = renderHook(() => useSidebarGroupMode());

    act(() => result.current.setGroupMode("status"));

    expect(result.current.groupMode).toBe("status");
    expect(readSidebarGroupMode()).toBe("status");
  });

  it("persists across a remount", () => {
    const { result, unmount } = renderHook(() => useSidebarGroupMode());
    act(() => result.current.setGroupMode("status"));
    unmount();

    const { result: second } = renderHook(() => useSidebarGroupMode());
    expect(second.current.groupMode).toBe("status");
  });
});
