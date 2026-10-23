import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { chatTab, prTabKey, TAB_KINDS } from "@/components/tab-registry";
import { useAutoOpenPRTab } from "@/hooks/use-auto-open-pr-tab";
import { useTaskTabs } from "@/hooks/use-task-tabs";
import { readPRTabState } from "@/lib/pr-tab-state";
import { collectAllTabs, findPaneContainingTab } from "@/lib/split-tree";

afterEach(() => window.localStorage.clear());

const CHATS = [{ ID: 1, Title: "Chat" }];

/** The tab strip of task 1 with the auto-open hook wired exactly as App.tsx does. */
function harness(prNumber: number | null) {
  return renderHook(
    ({ pr }: { pr: number | null }) => {
      const tabs = useTaskTabs();
      const layout = tabs.tabsByTask.get(1);
      useAutoOpenPRTab({ taskId: 1, prNumber: pr, tabs: layout ? collectAllTabs(layout.root) : null, openTab: tabs.openTab });
      return { ...tabs, strip: layout ? collectAllTabs(layout.root).map((t) => t.key) : null, layout };
    },
    { initialProps: { pr: prNumber } },
  );
}

describe("PR tab", () => {
  it("auto-opens once when a PR exists, without taking focus from the chat", () => {
    const h = harness(7);
    // No strip yet: opening first would stop the default chat from being seeded.
    expect(h.result.current.strip).toBeNull();
    act(() => h.result.current.ensureTaskChats(1, CHATS));
    expect(h.result.current.strip).toEqual([chatTab(1, CHATS[0]!).key, prTabKey(1)]);
    const pane = findPaneContainingTab(h.result.current.layout!.root, prTabKey(1))!;
    expect(pane.activeKey).toBe(chatTab(1, CHATS[0]!).key);
    expect(h.result.current.strip!.filter((k) => k === prTabKey(1))).toHaveLength(1);
    expect(collectAllTabs(h.result.current.layout!.root).find((t) => t.kind === "pr")?.title).toBe("PR #7");
    expect(readPRTabState(1)).toBe("auto-opened");
    h.rerender({ pr: 7 });
    expect(h.result.current.strip).toHaveLength(2);
  });

  it("closing opts the task out", () => {
    const h = harness(7);
    act(() => h.result.current.ensureTaskChats(1, CHATS));
    act(() => h.result.current.closeTab(1, prTabKey(1)));
    expect(h.result.current.strip).toEqual([chatTab(1, CHATS[0]!).key]);
    expect(readPRTabState(1)).toBe("opted-out");
    h.rerender({ pr: 7 });
    expect(h.result.current.strip).toEqual([chatTab(1, CHATS[0]!).key]);
    // A reload: a fresh hook over the same storage does not bring it back either.
    const again = harness(7);
    expect(again.result.current.strip).toEqual([chatTab(1, CHATS[0]!).key]);
  });

  it("closing it with the other-tabs actions also opts out", () => {
    const h = harness(7);
    act(() => h.result.current.ensureTaskChats(1, CHATS));
    act(() => h.result.current.closeOtherTabs(1, chatTab(1, CHATS[0]!).key));
    expect(h.result.current.strip).toEqual([chatTab(1, CHATS[0]!).key]);
    expect(readPRTabState(1)).toBe("opted-out");
  });

  it("no auto-open without a PR, and it opens when the PR appears", () => {
    const h = harness(null);
    act(() => h.result.current.ensureTaskChats(1, CHATS));
    expect(h.result.current.strip).toEqual([chatTab(1, CHATS[0]!).key]);
    expect(readPRTabState(1)).toBeUndefined();
    h.rerender({ pr: 9 });
    expect(h.result.current.strip).toEqual([chatTab(1, CHATS[0]!).key, prTabKey(1)]);
  });

  it("is a closable, task-scoped kind", () => {
    expect(TAB_KINDS.pr.closable).toBe(true);
    expect(prTabKey(5)).toBe("5:pr");
  });
});
