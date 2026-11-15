import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AppContextMenu } from "@/components/ui/app-context-menu";
import type { MenuEntry } from "@/lib/menu-model";
import { fileRowMenu, rowMenu, tabMenu, taskRowMenu, workspaceRowItems } from "@/lib/menu-models";
import type { Task } from "@/lib/types";

// The browser build: the in-page (Radix) menu is drawn from the same model the native menu gets. For each menu, in
// each state that disables something (a disabled item that stays keyboard-reachable is aria-disabled instead), the in-page items are disabled exactly where the model says they are.
vi.mock("@/lib/platform", async (orig) => ({ ...(await orig<typeof import("@/lib/platform")>()), isDesktop: false }));

const noop = () => {};
const task = (over: Partial<Task> = {}): Task => ({ ID: 5, SpaceID: null, ArchivedAt: null, ...over }) as Task;
const taskOpts = { isPinned: false, onTogglePin: noop, onMarkUnread: noop, onArchiveTask: noop, onNewSubTask: noop };
const tabBase = { kind: "chat", closable: true, hasOtherClosableTabs: true, hasClosableTabsToLeft: true, hasClosableTabsToRight: true, canSplit: false, canArchiveChat: true, onClose: noop, onCloseOthers: noop, onCloseLeft: noop, onCloseRight: noop, onRename: noop, onArchiveChat: noop, onSplit: noop };
const fileBase = { path: "a.ts", changed: true, onRevealInDiff: noop, onOpenToSide: noop, onAddToChat: noop, pathActions: null };
const local = { revealLabel: "Reveal", editors: [{ id: "zed", label: "Zed" }], reveal: noop, open: noop };
const wsBase = { path: "/w", localPathActions: local, onAddTask: noop, onAddSpace: noop, onEditScripts: noop, onDeleteWorkspace: noop };

const cases: Array<[string, MenuEntry[]]> = [
  ["task row, live", taskRowMenu(task(), taskOpts)],
  ["task row, archived", taskRowMenu(task({ ArchivedAt: "2026-01-01T00:00:00Z" } as never), taskOpts)],
  ["task row, already unread", taskRowMenu(task(), { ...taskOpts, isUnread: true })],
  ["tab, chat with no chat id and no neighbours", tabMenu({ ...tabBase, canArchiveChat: false, closable: false, hasOtherClosableTabs: false, hasClosableTabsToLeft: false, hasClosableTabsToRight: false })],
  ["tab, chat with neighbours", tabMenu(tabBase)],
  ["file, unchanged and no open-to-side", fileRowMenu({ ...fileBase, changed: false, onOpenToSide: undefined })],
  ["file, changed", fileRowMenu(fileBase)],
  ["workspace row, no folder", rowMenu(workspaceRowItems({ ...wsBase, path: "" }))],
  ["workspace row, with folder", rowMenu(workspaceRowItems(wsBase))],
];

describe("the in-page menu is disabled exactly where the model says", () => {
  it.each(cases)("%s", async (_name, entries) => {
    render(
      <AppContextMenu entries={entries} contentProps={{ "data-testid": "the-menu" }}>
        <div data-testid="row">row</div>
      </AppContextMenu>,
    );
    fireEvent.contextMenu(screen.getByTestId("row"));
    const menu = await screen.findByTestId("the-menu");
    const shown = Array.from(menu.querySelectorAll('[role="menuitem"]')).map((el) => el.hasAttribute("data-disabled") || el.getAttribute("aria-disabled") === "true");
    const want = entries.filter((e) => e.kind !== "separator").map((e) => e.kind === "item" && e.enabled === false);
    expect(shown).toEqual(want);
  });
});
