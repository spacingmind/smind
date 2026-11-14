import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AppContextMenu } from "@/components/ui/app-context-menu";
import type { MenuEntry } from "@/lib/menu-model";
import { ARMED_TTL_MS, resetNativeContextMenuForTests } from "@/lib/native-context-menu";

// The desktop build: right-click pops a NATIVE menu from the model's descriptors; Radix content is never rendered.
const bridge = vi.hoisted(() => ({
  show: vi.fn(),
  listener: null as null | ((s: { open: number; id: string }) => void),
  unlisten: vi.fn(),
}));
vi.mock("@/lib/platform", async (orig) => ({
  ...(await orig<typeof import("@/lib/platform")>()),
  isDesktop: true,
  desktop: {
    contextMenuShow: bridge.show,
    onContextMenuAction: (cb: (s: { open: number; id: string }) => void) => {
      bridge.listener = cb;
      return bridge.unlisten;
    },
  },
}));

const deliver = (open: number, id: string) => act(() => bridge.listener?.({ open, id }));
const flush = () => act(async () => { await Promise.resolve(); await Promise.resolve(); });

function menu(onA: () => void, onB: () => void = () => {}): MenuEntry[] {
  return [
    { kind: "item", id: "a", label: "Alpha", icon: "ICON", testId: "m-a", onSelect: onA },
    { kind: "separator" },
    { kind: "item", id: "b", label: "Beta", enabled: false, onSelect: onB },
    { kind: "submenu", id: "s", label: "More", children: [{ kind: "item", id: "c", label: "Gamma", onSelect: onB }] },
  ];
}

beforeEach(() => {
  bridge.show.mockReset();
  bridge.show.mockResolvedValue(1);
  bridge.listener = null;
  bridge.unlisten.mockReset();
  resetNativeContextMenuForTests();
});
afterEach(() => vi.useRealTimers());

describe("AppContextMenu in the desktop build", () => {
  it("right-click pops the native menu with descriptors only (no icons, test ids or handlers), at the pointer, and renders no in-page menu", async () => {
    render(
      <AppContextMenu entries={menu(() => {})} contentProps={{ "data-testid": "the-menu" }}>
        <div data-testid="row">row</div>
      </AppContextMenu>,
    );
    fireEvent.contextMenu(screen.getByTestId("row"), { clientX: 40, clientY: 70 });
    await flush();
    expect(bridge.show).toHaveBeenCalledTimes(1);
    const [entries, x, y] = bridge.show.mock.calls[0];
    expect([x, y]).toEqual([40, 70]);
    expect(entries).toEqual([
      { kind: "item", id: "a", label: "Alpha", enabled: true },
      { kind: "separator" },
      { kind: "item", id: "b", label: "Beta", enabled: false },
      { kind: "submenu", label: "More", enabled: true, children: [{ kind: "item", id: "c", label: "Gamma", enabled: true }] },
    ]);
    expect(JSON.stringify(entries)).not.toMatch(/ICON|m-a|onSelect/);
    expect(screen.queryByTestId("the-menu")).toBeNull();
  });

  it("prevents the default menu and stops the event so a parent row's menu does not also open", async () => {
    const parent = vi.fn();
    render(
      <div onContextMenu={parent}>
        <AppContextMenu entries={menu(() => {})}>
          <div data-testid="row">row</div>
        </AppContextMenu>
      </div>,
    );
    const notPrevented = fireEvent.contextMenu(screen.getByTestId("row"), { clientX: 5, clientY: 5 });
    expect(notPrevented).toBe(false);
    expect(parent).not.toHaveBeenCalled();
  });

  it("a selection runs its handler once, closes the menu and a duplicate delivery does nothing", async () => {
    const onA = vi.fn();
    const onOpenChange = vi.fn();
    render(
      <AppContextMenu entries={menu(onA)} onOpenChange={onOpenChange}>
        <div data-testid="row">row</div>
      </AppContextMenu>,
    );
    fireEvent.contextMenu(screen.getByTestId("row"), { clientX: 1, clientY: 1 });
    await flush();
    expect(onOpenChange).toHaveBeenLastCalledWith(true);
    deliver(1, "a");
    deliver(1, "a");
    expect(onA).toHaveBeenCalledTimes(1);
    expect(onOpenChange).toHaveBeenLastCalledWith(false);
  });

  it("a stale open, an unknown id and a disabled item's id never run anything", async () => {
    const onA = vi.fn();
    const onB = vi.fn();
    render(
      <AppContextMenu entries={menu(onA, onB)}>
        <div data-testid="row">row</div>
      </AppContextMenu>,
    );
    fireEvent.contextMenu(screen.getByTestId("row"), { clientX: 1, clientY: 1 });
    await flush();
    deliver(99, "a"); // another menu's open id
    deliver(1, "nope"); // an id this menu does not carry
    deliver(1, "b"); // a disabled item: no handler was armed
    expect(onA).not.toHaveBeenCalled();
    expect(onB).not.toHaveBeenCalled();
    deliver(1, "c"); // a submenu item is selectable
    expect(onB).toHaveBeenCalledTimes(1);
  });

  it("opening a second menu replaces the first: the first's selection no longer fires", async () => {
    const first = vi.fn();
    const second = vi.fn();
    render(
      <>
        <AppContextMenu entries={menu(first)}>
          <div data-testid="r1">1</div>
        </AppContextMenu>
        <AppContextMenu entries={menu(second)}>
          <div data-testid="r2">2</div>
        </AppContextMenu>
      </>,
    );
    bridge.show.mockResolvedValueOnce(1).mockResolvedValueOnce(2);
    fireEvent.contextMenu(screen.getByTestId("r1"), { clientX: 1, clientY: 1 });
    await flush();
    fireEvent.contextMenu(screen.getByTestId("r2"), { clientX: 2, clientY: 2 });
    await flush();
    deliver(1, "a");
    expect(first).not.toHaveBeenCalled();
    deliver(2, "a");
    expect(second).toHaveBeenCalledTimes(1);
  });

  it("a selection that beats Rust's answer (the open id is not known yet) still reaches the armed menu", async () => {
    let resolveShow: (n: number) => void = () => {};
    bridge.show.mockReturnValue(new Promise<number>((r) => (resolveShow = r)));
    const onA = vi.fn();
    render(
      <AppContextMenu entries={menu(onA)}>
        <div data-testid="row">row</div>
      </AppContextMenu>,
    );
    fireEvent.contextMenu(screen.getByTestId("row"), { clientX: 1, clientY: 1 });
    await flush();
    deliver(7, "a");
    expect(onA).toHaveBeenCalledTimes(1);
    resolveShow(7);
    await flush();
  });

  it("a dismissed menu leaves no handler: after the time-out a late selection does nothing and the menu reports closed", async () => {
    vi.useFakeTimers();
    const onA = vi.fn();
    const onOpenChange = vi.fn();
    render(
      <AppContextMenu entries={menu(onA)} onOpenChange={onOpenChange}>
        <div data-testid="row">row</div>
      </AppContextMenu>,
    );
    fireEvent.contextMenu(screen.getByTestId("row"), { clientX: 1, clientY: 1 });
    await act(async () => { await Promise.resolve(); });
    act(() => vi.advanceTimersByTime(ARMED_TTL_MS + 1));
    expect(onOpenChange).toHaveBeenLastCalledWith(false);
    deliver(1, "a");
    expect(onA).not.toHaveBeenCalled();
  });

  it("when Rust refuses the model the menu is disarmed and reported closed, and the page keeps working", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    bridge.show.mockRejectedValue(new Error("context menu has no item"));
    const onOpenChange = vi.fn();
    render(
      <AppContextMenu entries={menu(() => {})} onOpenChange={onOpenChange}>
        <div data-testid="row">row</div>
      </AppContextMenu>,
    );
    fireEvent.contextMenu(screen.getByTestId("row"), { clientX: 1, clientY: 1 });
    await flush();
    expect(onOpenChange).toHaveBeenLastCalledWith(false);
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
  });

  it("the Menu key and Shift+F10 on the focused trigger open the native menu; other keys, and a key on a descendant, do not", async () => {
    render(
      <AppContextMenu entries={menu(() => {})}>
        <button data-testid="row" type="button">
          row <span data-testid="inner">x</span>
        </button>
      </AppContextMenu>,
    );
    const row = screen.getByTestId("row");
    row.getBoundingClientRect = () => ({ left: 10, bottom: 50, top: 20, right: 60, width: 50, height: 30, x: 10, y: 20, toJSON() {} }) as DOMRect;
    fireEvent.keyDown(row, { key: "a" });
    fireEvent.keyDown(row, { key: "F10" }); // without Shift: not the menu chord
    fireEvent.keyDown(screen.getByTestId("inner"), { key: "ContextMenu" });
    await flush();
    expect(bridge.show).not.toHaveBeenCalled();
    fireEvent.keyDown(row, { key: "ContextMenu" });
    await flush();
    expect(bridge.show).toHaveBeenCalledTimes(1);
    expect(bridge.show.mock.calls[0].slice(1)).toEqual([18, 50]);
    fireEvent.keyDown(row, { key: "F10", shiftKey: true });
    await flush();
    expect(bridge.show).toHaveBeenCalledTimes(2);
  });

  it("a keyboard-invoked menu (reported at 0,0) opens at the focused element's lower-left corner", async () => {
    render(
      <AppContextMenu entries={menu(() => {})}>
        <button data-testid="row" type="button">row</button>
      </AppContextMenu>,
    );
    const row = screen.getByTestId("row");
    row.getBoundingClientRect = () => ({ left: 30, bottom: 90, top: 60, right: 80, width: 50, height: 30, x: 30, y: 60, toJSON() {} }) as DOMRect;
    fireEvent.contextMenu(row, { clientX: 0, clientY: 0 });
    await flush();
    expect(bridge.show.mock.calls[0].slice(1)).toEqual([38, 90]);
  });
});
