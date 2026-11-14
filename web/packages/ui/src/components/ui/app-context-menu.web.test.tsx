import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AppContextMenu } from "@/components/ui/app-context-menu";
import type { MenuEntry } from "@/lib/menu-model";

// The browser build: no desktop bridge exists, so the menu is the in-page Radix menu, drawn from the same model.
const desktopBridge = vi.hoisted(() => ({ contextMenuShow: vi.fn() }));
vi.mock("@/lib/platform", async (orig) => ({ ...(await orig<typeof import("@/lib/platform")>()), isDesktop: false, desktop: { contextMenuShow: desktopBridge.contextMenuShow, onContextMenuAction: () => () => {} } }));

const entries = (onA: () => void): MenuEntry[] => [
  { kind: "item", id: "a", label: "Alpha", testId: "m-a", onSelect: onA },
  { kind: "separator" },
  { kind: "item", id: "b", label: "Beta", testId: "m-b", enabled: false, onSelect: () => {} },
  { kind: "submenu", id: "s", label: "More", testId: "m-more", children: [{ kind: "item", id: "c", label: "Gamma", testId: "m-c", onSelect: () => {} }] },
];

describe("AppContextMenu in the browser build", () => {
  it("opens the in-page menu on right-click with the model's items, test ids and disabled state, and never calls the desktop bridge", async () => {
    const onA = vi.fn();
    const onOpenChange = vi.fn();
    render(
      <AppContextMenu entries={entries(onA)} onOpenChange={onOpenChange} contentProps={{ "data-testid": "the-menu", "data-path": "p" }}>
        <div data-testid="row">row</div>
      </AppContextMenu>,
    );
    fireEvent.contextMenu(screen.getByTestId("row"));
    const menu = await screen.findByTestId("the-menu");
    expect(menu).toHaveAttribute("data-path", "p");
    expect(screen.getByTestId("m-a")).toHaveTextContent("Alpha");
    expect(screen.getByTestId("m-b")).toHaveAttribute("data-disabled");
    expect(screen.getByTestId("m-more")).toBeInTheDocument();
    expect(onOpenChange).toHaveBeenCalledWith(true);
    fireEvent.click(screen.getByTestId("m-a"));
    expect(onA).toHaveBeenCalledTimes(1);
    expect(desktopBridge.contextMenuShow).not.toHaveBeenCalled();
  });

  it("a disabled item does not run", async () => {
    const run = vi.fn();
    render(
      <AppContextMenu entries={[{ kind: "item", id: "x", label: "X", testId: "m-x", enabled: false, onSelect: run }]} contentProps={{ "data-testid": "the-menu" }}>
        <div data-testid="row">row</div>
      </AppContextMenu>,
    );
    fireEvent.contextMenu(screen.getByTestId("row"));
    await screen.findByTestId("the-menu");
    fireEvent.click(screen.getByTestId("m-x"));
    expect(run).not.toHaveBeenCalled();
  });

  it("an unavailable item with a note stays reachable (aria-disabled) with its reason beside the label", async () => {
    render(
      <AppContextMenu entries={[{ kind: "item", id: "x", label: "Archive", testId: "m-x", enabled: false, note: "still running", onSelect: () => {} }]} contentProps={{ "data-testid": "the-menu" }}>
        <div data-testid="row">row</div>
      </AppContextMenu>,
    );
    fireEvent.contextMenu(screen.getByTestId("row"));
    const item = await screen.findByTestId("m-x");
    expect(item).toHaveAttribute("aria-disabled", "true");
    expect(item).toHaveAttribute("aria-describedby", "m-x-reason");
    expect(screen.getByText("still running")).toHaveAttribute("id", "m-x-reason");
  });
});
