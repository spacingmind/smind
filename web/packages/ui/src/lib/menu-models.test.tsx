import { describe, expect, it, vi } from "vitest";

import { enabledIds, normalizeEntries, outline, toNative, type MenuEntry, type MenuItemEntry } from "@/lib/menu-model";
import { dirRowMenu, fileRowMenu, rowMenu, tabMenu, taskRowMenu, type FileRowMenuInput, type TabMenuInput } from "@/lib/menu-models";
import type { Task } from "@/lib/types";

const noop = () => {};

// ---- helpers ---------------------------------------------------------------------------------------------

describe("menu model helpers", () => {
  const sep: MenuEntry = { kind: "separator" };
  const item = (id: string, over: Partial<MenuItemEntry> = {}): MenuItemEntry => ({ kind: "item", id, label: id, onSelect: noop, ...over });

  it("normalizeEntries drops leading, trailing and doubled separators and empty submenus", () => {
    expect(outline(normalizeEntries([sep, item("a"), sep, sep, item("b"), sep]))).toEqual(["a", "-", "b"]);
    expect(outline(normalizeEntries([item("a"), { kind: "submenu", id: "s", label: "S", children: [] }]))).toEqual(["a"]);
    expect(outline(normalizeEntries([item("a"), { kind: "submenu", id: "s", label: "S", children: [{ kind: "separator" }] }]))).toEqual(["a"]);
    expect(outline(normalizeEntries([{ kind: "submenu", id: "s", label: "S", children: [item("x")] }]))).toEqual(["s>[x]"]);
  });

  it("toNative sends labels, opaque ids and enabled flags only; a disabled item has no handler and a note joins the label", () => {
    const icon = "ICON";
    const entries: MenuEntry[] = [
      item("a", { icon, testId: "tid", data: { k: 1 } }),
      sep,
      item("b", { enabled: false, note: "still running" }),
      { kind: "submenu", id: "s", label: "More", children: [item("c")] },
    ];
    const { entries: native, handlers } = toNative(entries);
    expect(native).toEqual([
      { kind: "item", id: "a", label: "a", enabled: true },
      { kind: "separator" },
      { kind: "item", id: "b", label: "b (still running)", enabled: false },
      { kind: "submenu", label: "More", enabled: true, children: [{ kind: "item", id: "c", label: "c", enabled: true }] },
    ]);
    expect([...handlers.keys()].sort()).toEqual(["a", "c"]);
    expect(JSON.stringify(native)).not.toMatch(/ICON|tid|onSelect/);
  });

  it("native labels lose control, bidi and zero-width characters, collapse whitespace, are cut at 100 characters and never come out empty", () => {
    const { entries } = toNative([
      item("a", { label: "Fix\n the \u202Ebug\u200B\ttitle" }),
      item("b", { label: "x".repeat(300) }),
      item("c", { label: " \n\u200B " }),
      { kind: "submenu", id: "s", label: "Move\nto space", children: [item("d")] },
      item("e", { label: "a\u3164b\u2800c\uFE0Fd\u{E0041}e\u034Ff" }),
    ]);
    const labels = entries.map((e) => (e.kind === "separator" ? "-" : e.label));
    expect(labels[0]).toBe("Fix the bug title");
    expect(Array.from(labels[1]).length).toBe(100);
    expect(labels[1].endsWith("…")).toBe(true);
    expect(labels[2]).toBe("Untitled");
    expect(labels[3]).toBe("Move to space");
    expect(labels[4]).toBe("a b c d e f");
  });

  it("enabledIds lists only the enabled items, submenus included", () => {
    expect(enabledIds([item("a"), item("b", { enabled: false }), { kind: "submenu", id: "s", label: "S", children: [item("c"), item("d", { enabled: false })] }])).toEqual(["a", "c"]);
  });
});

// ---- file row ----------------------------------------------------------------------------------------------

describe("fileRowMenu", () => {
  const base: FileRowMenuInput = { path: "src/a.ts", changed: true, onRevealInDiff: noop, onOpenToSide: noop, onAddToChat: noop, pathActions: null };
  const local = (editors: { id: string; label: string }[]) => ({ root: "/work/alpha", actions: { revealLabel: "Reveal in Finder", editors, reveal: noop, open: noop } });
  const cases: Array<[string, Partial<FileRowMenuInput>, string[], string[]]> = [
    ["changed file, everything wired, no local actions", {}, ["reveal-in-diff", "open-to-side", "add-to-chat", "copy-path"], ["reveal-in-diff", "open-to-side", "add-to-chat", "copy-path"]],
    ["unchanged file: Reveal in diff is off", { changed: false }, ["reveal-in-diff(off)", "open-to-side", "add-to-chat", "copy-path"], ["open-to-side", "add-to-chat", "copy-path"]],
    ["no open-to-side handler: it is off", { onOpenToSide: undefined }, ["reveal-in-diff", "open-to-side(off)", "add-to-chat", "copy-path"], ["reveal-in-diff", "add-to-chat", "copy-path"]],
    ["no add-to-chat handler: the item is absent", { onAddToChat: undefined }, ["reveal-in-diff", "open-to-side", "copy-path"], ["reveal-in-diff", "open-to-side", "copy-path"]],
    ["local daemon, two editors", { pathActions: local([{ id: "vscode", label: "VS Code" }, { id: "zed", label: "Zed" }]) }, ["reveal-in-diff", "open-to-side", "add-to-chat", "copy-path", "-", "reveal", "open-in-editor:vscode", "open-in-editor:zed"], ["reveal-in-diff", "open-to-side", "add-to-chat", "copy-path", "reveal", "open-in-editor:vscode", "open-in-editor:zed"]],
    ["local daemon, no editors: only Reveal", { pathActions: local([]) }, ["reveal-in-diff", "open-to-side", "add-to-chat", "copy-path", "-", "reveal"], ["reveal-in-diff", "open-to-side", "add-to-chat", "copy-path", "reveal"]],
  ];
  it.each(cases)("%s", (_name, over, wantOutline, wantEnabled) => {
    const menu = fileRowMenu({ ...base, ...over });
    expect(outline(menu)).toEqual(wantOutline);
    expect(enabledIds(menu)).toEqual(wantEnabled);
  });

  it("the Reveal and Open-in-editor handlers get the absolute path under the workspace root", () => {
    const reveal = vi.fn();
    const open = vi.fn();
    const menu = fileRowMenu({ ...base, pathActions: { root: "/work/alpha", actions: { revealLabel: "Reveal", editors: [{ id: "zed", label: "Zed" }], reveal, open } } });
    for (const e of menu) if (e.kind === "item" && (e.id === "reveal" || e.id.startsWith("open-in-editor"))) e.onSelect();
    expect(reveal).toHaveBeenCalledWith("/work/alpha/src/a.ts");
    expect(open).toHaveBeenCalledWith("zed", "/work/alpha/src/a.ts");
  });
});

describe("dirRowMenu", () => {
  it("has only Add to chat, and no menu without the handler", () => {
    expect(outline(dirRowMenu({ path: "src" }))).toEqual([]);
    expect(outline(dirRowMenu({ path: "src", onAddToChat: noop }))).toEqual(["add-to-chat"]);
  });
});

// ---- tab ------------------------------------------------------------------------------------------------------

describe("tabMenu", () => {
  const base: TabMenuInput = {
    kind: "chat",
    closable: true,
    hasOtherClosableTabs: true,
    hasClosableTabsToLeft: true,
    hasClosableTabsToRight: true,
    canSplit: true,
    onClose: noop, onCloseOthers: noop, onCloseLeft: noop, onCloseRight: noop, onRename: noop, onArchiveChat: noop, onSplit: noop,
  };
  const SPLITS = ["split-left", "split-right", "split-up", "split-down"];
  const cases: Array<[string, Partial<TabMenuInput>, string[], string[]]> = [
    ["a chat tab with neighbours", {}, ["close", "close-others", "close-left", "close-right", "-", "rename", "archive-chat", "-", ...SPLITS], ["close", "close-others", "close-left", "close-right", "rename", "archive-chat", ...SPLITS]],
    ["a chat tab that is not closable and alone", { closable: false, hasOtherClosableTabs: false, hasClosableTabsToLeft: false, hasClosableTabsToRight: false, canSplit: false }, ["close(off)", "close-others(off)", "close-left(off)", "close-right(off)", "-", "rename", "archive-chat"], ["rename", "archive-chat"]],
    ["the leftmost tab: nothing to the left", { hasClosableTabsToLeft: false }, ["close", "close-others", "close-left(off)", "close-right", "-", "rename", "archive-chat", "-", ...SPLITS], ["close", "close-others", "close-right", "rename", "archive-chat", ...SPLITS]],
    ["a terminal tab: rename, no archive", { kind: "terminal" }, ["close", "close-others", "close-left", "close-right", "-", "rename", "-", ...SPLITS], ["close", "close-others", "close-left", "close-right", "rename", ...SPLITS]],
    ["a file tab with a path: copy path", { kind: "file", path: "src/a.ts" }, ["close", "close-others", "close-left", "close-right", "-", "copy-path", "-", ...SPLITS], ["close", "close-others", "close-left", "close-right", "copy-path", ...SPLITS]],
    ["a file tab with no path: no tail", { kind: "file" }, ["close", "close-others", "close-left", "close-right", "-", ...SPLITS], ["close", "close-others", "close-left", "close-right", ...SPLITS]],
    ["a diff tab: close items only", { kind: "diff", canSplit: false }, ["close", "close-others", "close-left", "close-right"], ["close", "close-others", "close-left", "close-right"]],
  ];
  it.each(cases)("%s", (_name, over, wantOutline, wantEnabled) => {
    const menu = tabMenu({ ...base, ...over });
    expect(outline(normalizeEntries(menu))).toEqual(wantOutline);
    expect(enabledIds(menu)).toEqual(wantEnabled);
  });

  it("each item calls its own handler", () => {
    const calls: string[] = [];
    const menu = tabMenu({
      ...base,
      onClose: () => calls.push("close"), onCloseOthers: () => calls.push("others"), onCloseLeft: () => calls.push("left"), onCloseRight: () => calls.push("right"),
      onRename: () => calls.push("rename"), onArchiveChat: () => calls.push("archive"), onSplit: (d) => calls.push(`split:${d}`),
    });
    for (const e of menu) if (e.kind === "item") e.onSelect();
    expect(calls).toEqual(["close", "others", "left", "right", "rename", "archive", "split:left", "split:right", "split:up", "split:down"]);
  });
});

// ---- workspace / space row -----------------------------------------------------------------------------

describe("rowMenu", () => {
  it("one item per action in order, positional ids, a dialog-opening item flagged", () => {
    const menu = rowMenu([
      { label: "Add task", icon: null, onSelect: noop },
      { label: "Reveal in Finder", icon: null, onSelect: noop, fadeOut: true },
      { label: "Worktree settings", icon: null, onSelect: noop },
      { label: "Add task", icon: null, onSelect: noop },
    ]);
    expect(outline(menu)).toEqual(["item-0", "item-1", "item-2", "item-3"]);
    expect(menu.map((e) => e.kind === "item" && e.opensDialog)).toEqual([true, false, true, true]);
  });
});

// ---- task row -------------------------------------------------------------------------------------------

const task = (over: Partial<Task> = {}): Task => ({ ID: 5, SpaceID: null, ArchivedAt: null, ...over }) as Task;
const tree = (archive: { enabled: boolean; reason: string | null }, stop: { enabled: boolean; reason: string | null }) => ({
  availability: { archive, stop } as never,
  onArchiveFinished: noop,
  onStopTree: noop,
});

describe("taskRowMenu", () => {
  const o = { isPinned: false, onTogglePin: noop, onMarkUnread: noop, onArchiveTask: noop };
  const spaces = [{ ID: 1, Title: "Alpha" }, { ID: 2, Title: "Beta" }] as never;
  const cases: Array<[string, Task, Parameters<typeof taskRowMenu>[1], string[], string[]]> = [
    ["a plain task in search results: pin, mark unread, archive", task(), { ...o }, ["pin", "mark-unread", "archive"], ["pin", "mark-unread", "archive"]],
    ["a pinned task says Unpin (same id)", task(), { ...o, isPinned: true }, ["pin", "mark-unread", "archive"], ["pin", "mark-unread", "archive"]],
    ["in the tree provider: New sub-task first", task(), { ...o, onNewSubTask: noop }, ["new-subtask", "pin", "mark-unread", "archive"], ["new-subtask", "pin", "mark-unread", "archive"]],
    ["an archived task offers no New sub-task", task({ ArchivedAt: "2026-01-01T00:00:00Z" } as never), { ...o, onNewSubTask: noop }, ["pin", "mark-unread", "archive"], ["pin", "mark-unread", "archive"]],
    ["ungrouped task with spaces: move to each space", task(), { ...o, spaces, onMoveTask: noop }, ["move>[move:1,move:2]", "pin", "mark-unread", "archive"], ["move:1", "move:2", "pin", "mark-unread", "archive"]],
    ["task in space 1: Ungrouped and the other space", task({ SpaceID: 1 } as never), { ...o, spaces, onMoveTask: noop }, ["move>[move:ungrouped,move:2]", "pin", "mark-unread", "archive"], ["move:ungrouped", "move:2", "pin", "mark-unread", "archive"]],
    ["an ungrouped task with no spaces: nowhere to move, no Move submenu", task(), { ...o, spaces: [] as never, onMoveTask: noop }, ["pin", "mark-unread", "archive"], ["pin", "mark-unread", "archive"]],
    ["the task's only space is its own: only Ungrouped", task({ SpaceID: 1 } as never), { ...o, spaces: [{ ID: 1, Title: "Alpha" }] as never, onMoveTask: noop }, ["move>[move:ungrouped]", "pin", "mark-unread", "archive"], ["move:ungrouped", "pin", "mark-unread", "archive"]],
    ["tree actions, both available", task(), { ...o, tree: tree({ enabled: true, reason: null }, { enabled: true, reason: null }) }, ["archive-finished", "stop-tree", "pin", "mark-unread", "archive"], ["archive-finished", "stop-tree", "pin", "mark-unread", "archive"]],
    ["tree actions, nothing finished and nothing running: both off", task(), { ...o, tree: tree({ enabled: false, reason: "no finished sub-tasks" }, { enabled: false, reason: "nothing running" }) }, ["archive-finished(off)", "stop-tree(off)", "pin", "mark-unread", "archive"], ["pin", "mark-unread", "archive"]],
  ];
  it.each(cases)("%s", (_name, t, opts, wantOutline, wantEnabled) => {
    const menu = taskRowMenu(t, opts);
    expect(outline(menu)).toEqual(wantOutline);
    expect(enabledIds(menu)).toEqual(wantEnabled);
  });

  it("a disabled tree action stays reachable with its reason as the note", () => {
    const menu = taskRowMenu(task(), { ...o, tree: tree({ enabled: false, reason: "no finished sub-tasks" }, { enabled: true, reason: null }) });
    const archive = menu.find((e) => e.kind === "item" && e.id === "archive-finished");
    expect(archive).toMatchObject({ enabled: false, reachable: true, note: "no finished sub-tasks", testId: "sidebar-task-archive-finished-action" });
  });

  it("the pin item flips its label with the pinned state", () => {
    const label = (pinned: boolean) => taskRowMenu(task(), { ...o, isPinned: pinned }).find((e) => e.kind === "item" && e.id === "pin");
    expect(label(false)).toMatchObject({ label: "Pin" });
    expect(label(true)).toMatchObject({ label: "Unpin" });
  });
});
