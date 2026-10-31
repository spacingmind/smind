import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { terminalTab, type TabEntry } from "@/components/tab-registry";
import { TerminalTabReaper } from "@/hooks/use-terminal-tab-reaper";
import { bindTerminal, boundTerminalId, resetTerminalSessions } from "@/lib/terminal-sessions";
import { DEFAULT_PANE_ID, type TaskLayout } from "@/lib/split-tree";

const TASK_ID = 1;

/** A one-pane layout holding exactly the given tabs -- the minimum the reaper walks. */
function layoutWith(tabs: TabEntry[]): TaskLayout {
  return { root: { kind: "pane", pane: { id: DEFAULT_PANE_ID, tabs, activeKey: tabs[0]?.key ?? null } }, focusedPaneId: DEFAULT_PANE_ID };
}

/** A two-pane split with `tab` in `paneId` and nothing in the other pane -- same key either way, which is what the reaper must key on. */
function splitLayoutWith(tab: TabEntry, paneId: string): TaskLayout {
  const inPane = (id: string) => ({ kind: "pane", pane: { id, tabs: id === paneId ? [tab] : [], activeKey: id === paneId ? tab.key : null } }) as const;
  return {
    root: {
      kind: "group",
      group: { id: "group-1", direction: "horizontal", children: [inPane("pane-a"), inPane("pane-b")], sizes: [0.5, 0.5] },
    },
    focusedPaneId: paneId,
  };
}

function fakeClient() {
  return { call: vi.fn().mockResolvedValue({}), callStream: vi.fn().mockResolvedValue({}) };
}

beforeEach(() => {
  resetTerminalSessions();
});

describe("TerminalTabReaper", () => {
  it("closing a terminal tab forgets its session", () => {
    const key = `${TASK_ID}:terminal:2`;
    bindTerminal(key, "term-9");
    const client = fakeClient();
    const { rerender } = render(
      <TerminalTabReaper client={client} tabsByTask={new Map([[TASK_ID, layoutWith([terminalTab(TASK_ID, 2)])]])} />,
    );

    rerender(<TerminalTabReaper client={client} tabsByTask={new Map([[TASK_ID, layoutWith([])]])} />);

    expect(client.call).toHaveBeenCalledTimes(1);
    expect(client.call).toHaveBeenCalledWith("terminal.close", { terminalId: "term-9" });
    expect(boundTerminalId(key)).toBeNull();
  });

  it("keeps the session of a terminal tab that is still open", () => {
    const key = `${TASK_ID}:terminal:2`;
    bindTerminal(key, "term-9");
    const client = fakeClient();
    const { rerender } = render(
      <TerminalTabReaper client={client} tabsByTask={new Map([[TASK_ID, layoutWith([terminalTab(TASK_ID, 2)])]])} />,
    );

    rerender(
      <TerminalTabReaper client={client} tabsByTask={new Map([[TASK_ID, layoutWith([terminalTab(TASK_ID, 2)])]])} />,
    );

    expect(client.call).not.toHaveBeenCalled();
    expect(boundTerminalId(key)).toBe("term-9");
  });

  it("does not close on the first render", () => {
    // The tab is bound but was never in a layout the reaper saw -- the
    // first run must record, not close.
    bindTerminal(`${TASK_ID}:terminal:3`, "term-9");
    const client = fakeClient();
    render(<TerminalTabReaper client={client} tabsByTask={new Map([[TASK_ID, layoutWith([terminalTab(TASK_ID, 2)])]])} />);

    expect(client.call).not.toHaveBeenCalled();
    expect(boundTerminalId(`${TASK_ID}:terminal:3`)).toBe("term-9");
  });

  it("a terminal tab moving between panes is not closed", () => {
    const key = `${TASK_ID}:terminal:2`;
    bindTerminal(key, "term-9");
    const client = fakeClient();
    const tab = terminalTab(TASK_ID, 2);
    const { rerender } = render(
      <TerminalTabReaper client={client} tabsByTask={new Map([[TASK_ID, splitLayoutWith(tab, "pane-a")]])} />,
    );

    rerender(<TerminalTabReaper client={client} tabsByTask={new Map([[TASK_ID, splitLayoutWith(tab, "pane-b")]])} />);

    expect(client.call).not.toHaveBeenCalled();
    expect(boundTerminalId(key)).toBe("term-9");
  });
});
