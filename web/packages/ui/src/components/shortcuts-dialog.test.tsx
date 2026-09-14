import { useState } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ShortcutsDialog } from "@/components/shortcuts-dialog";
import { KeyboardProvider, useActionHandler } from "@/keyboard/keyboard-provider";
import { SECTION_TITLES, SHORTCUT_BINDINGS } from "@/keyboard/shortcuts";

/** The dialog wired to `shortcuts.help` exactly as App.tsx wires it, so `Shift+?` is exercised end to end. */
function Host() {
  const [open, setOpen] = useState(false);
  useActionHandler("shortcuts.help", () => setOpen(true));
  return <ShortcutsDialog open={open} onOpenChange={setOpen} />;
}

function renderHost() {
  return render(
    <KeyboardProvider>
      <Host />
    </KeyboardProvider>,
  );
}

beforeEach(() => {
  window.localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
});

describe("ShortcutsDialog", () => {
  it("opens on Shift+? and lists every registered binding", () => {
    renderHost();
    expect(screen.queryByTestId("shortcuts-dialog")).not.toBeInTheDocument();

    fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });

    const dialog = screen.getByTestId("shortcuts-dialog");
    const rows = within(dialog).getAllByTestId("shortcut-row");
    expect(rows).toHaveLength(SHORTCUT_BINDINGS.length);
    expect(rows.map((r) => r.dataset.bindingId).sort()).toEqual(
      SHORTCUT_BINDINGS.map((b) => b.id).sort(),
    );
    for (const binding of SHORTCUT_BINDINGS) {
      expect(within(dialog).getByText(binding.label)).toBeInTheDocument();
    }
  });

  it("groups rows under their section headings", () => {
    renderHost();
    fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });

    for (const binding of SHORTCUT_BINDINGS) {
      const section = screen.getByTestId(`shortcut-section-${binding.section}`);
      expect(within(section).getByText(SECTION_TITLES[binding.section])).toBeInTheDocument();
      expect(within(section).getByText(binding.label)).toBeInTheDocument();
    }
  });

  it("renders each binding's keys", () => {
    renderHost();
    fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });

    // Non-mac in jsdom, so Ctrl+K renders as three caps.
    const row = screen.getByTestId("shortcuts-dialog").querySelector<HTMLElement>(
      '[data-binding-id="palette-open"]',
    )!;
    expect(within(row).getByText("Ctrl")).toBeInTheDocument();
    expect(within(row).getByText("K")).toBeInTheDocument();
  });

  it("rebinds a shortcut from the captured key press", () => {
    renderHost();
    fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });

    fireEvent.click(screen.getByLabelText("Change shortcut for Open command palette"));
    expect(screen.getByTestId("shortcut-capturing")).toBeInTheDocument();

    fireEvent.keyDown(window, { key: "j", code: "KeyJ", ctrlKey: true, shiftKey: true });

    const row = screen
      .getByTestId("shortcuts-dialog")
      .querySelector<HTMLElement>('[data-binding-id="palette-open"]')!;
    expect(within(row).getByText("J")).toBeInTheDocument();
    expect(within(row).getByText("Shift")).toBeInTheDocument();
    expect(within(row).queryByText("K")).not.toBeInTheDocument();
  });

  it("stays in capture for a bare modifier press and cancels on Escape", () => {
    renderHost();
    fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });
    fireEvent.click(screen.getByLabelText("Change shortcut for Open command palette"));

    fireEvent.keyDown(window, { key: "Shift", shiftKey: true });
    expect(screen.getByTestId("shortcut-capturing")).toBeInTheDocument();

    fireEvent.keyDown(window, { key: "Escape", code: "Escape" });
    expect(screen.queryByTestId("shortcut-capturing")).not.toBeInTheDocument();
    const row = screen
      .getByTestId("shortcuts-dialog")
      .querySelector<HTMLElement>('[data-binding-id="palette-open"]')!;
    expect(within(row).getByText("K")).toBeInTheDocument();
  });

  it("flags a rebind that collides with another binding", () => {
    renderHost();
    fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });

    // Put "Close current tab" onto Ctrl+K, which "Open command palette" has.
    fireEvent.click(screen.getByLabelText("Change shortcut for Close current tab"));
    fireEvent.keyDown(window, { key: "k", code: "KeyK", ctrlKey: true });

    const conflicts = screen.getAllByTestId("shortcut-conflict");
    expect(conflicts.length).toBe(2);
    expect(conflicts.map((c) => c.textContent)).toEqual(
      expect.arrayContaining([
        "Also used by Close current tab",
        "Also used by Open command palette",
      ]),
    );
  });

  it("offers Reset only on an overridden row, and restores the default", () => {
    renderHost();
    fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });
    expect(
      screen.queryByLabelText("Reset shortcut for Open command palette"),
    ).not.toBeInTheDocument();

    fireEvent.click(screen.getByLabelText("Change shortcut for Open command palette"));
    fireEvent.keyDown(window, { key: "j", code: "KeyJ", ctrlKey: true });
    fireEvent.click(screen.getByLabelText("Reset shortcut for Open command palette"));

    const row = screen
      .getByTestId("shortcuts-dialog")
      .querySelector<HTMLElement>('[data-binding-id="palette-open"]')!;
    expect(within(row).getByText("K")).toBeInTheDocument();
    expect(
      screen.queryByLabelText("Reset shortcut for Open command palette"),
    ).not.toBeInTheDocument();
  });

  it("shows Reset all only once something is overridden", () => {
    renderHost();
    fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });
    expect(screen.queryByText("Reset all to defaults")).not.toBeInTheDocument();

    fireEvent.click(screen.getByLabelText("Change shortcut for Open command palette"));
    fireEvent.keyDown(window, { key: "j", code: "KeyJ", ctrlKey: true });
    fireEvent.click(screen.getByText("Reset all to defaults"));

    expect(screen.queryByText("Reset all to defaults")).not.toBeInTheDocument();
    const row = screen
      .getByTestId("shortcuts-dialog")
      .querySelector<HTMLElement>('[data-binding-id="palette-open"]')!;
    expect(within(row).getByText("K")).toBeInTheDocument();
  });

  it("holds the modal keyboard lock while open, so no shortcut fires underneath it", () => {
    const palette = vi.fn();
    function Underneath() {
      useActionHandler("palette.open", palette);
      return null;
    }
    render(
      <KeyboardProvider>
        <Underneath />
        <Host />
      </KeyboardProvider>,
    );

    fireEvent.keyDown(document, { key: "k", code: "KeyK", ctrlKey: true });
    expect(palette).toHaveBeenCalledTimes(1);

    fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });
    expect(screen.getByTestId("shortcuts-dialog")).toBeInTheDocument();

    fireEvent.keyDown(document, { key: "k", code: "KeyK", ctrlKey: true });
    expect(palette).toHaveBeenCalledTimes(1);
  });
});
