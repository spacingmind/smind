import { fireEvent, render, screen, within } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ShortcutsOverlay } from "@/components/shortcuts-overlay";
import { KeyboardProvider, useActionHandler, useKeyboard } from "@/keyboard/keyboard-provider";
import { platformBindings, SHORTCUT_BINDINGS } from "@/keyboard/shortcuts";

/** The shell's own wiring, minimized: the action opens the overlay, a menu-style button runs the action. */
function Harness({ onCustomize }: { onCustomize: () => void }) {
  const [open, setOpen] = useState(false);
  const { runAction } = useKeyboard();
  useActionHandler("shortcuts.help", () => setOpen(true));
  return (
    <>
      <button type="button" onClick={() => runAction("shortcuts.help", null, { source: "menu" })}>
        Run shortcuts.help
      </button>
      <ShortcutsOverlay open={open} onOpenChange={setOpen} onCustomize={onCustomize} />
    </>
  );
}

function renderOverlay(onCustomize = vi.fn()) {
  render(
    <KeyboardProvider>
      <Harness onCustomize={onCustomize} />
    </KeyboardProvider>,
  );
  return onCustomize;
}

afterEach(() => {
  window.localStorage.clear();
});

describe("ShortcutsOverlay", () => {
  it("opens via the shortcuts.help action", () => {
    renderOverlay();
    expect(screen.queryByRole("dialog", { name: "Keyboard shortcuts" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Run shortcuts.help" }));
    expect(screen.getByRole("dialog", { name: "Keyboard shortcuts" })).toBeInTheDocument();
  });

  it("lists every binding read-only: no rebind/reset buttons, no Scope column", () => {
    renderOverlay();
    fireEvent.click(screen.getByRole("button", { name: "Run shortcuts.help" }));
    const dialog = screen.getByRole("dialog", { name: "Keyboard shortcuts" });
    // jsdom is not a desktop build: the web-only rows render.
    expect(within(dialog).getAllByTestId("shortcut-row")).toHaveLength(
      platformBindings(SHORTCUT_BINDINGS, false, false).length,
    );
    expect(within(dialog).queryByText("Scope")).not.toBeInTheDocument();
    expect(within(dialog).queryByText("Actions")).not.toBeInTheDocument();
    expect(within(dialog).queryByRole("button", { name: /shortcut for/ })).not.toBeInTheDocument();
  });

  it("search filters the rows", () => {
    renderOverlay();
    fireEvent.click(screen.getByRole("button", { name: "Run shortcuts.help" }));
    const dialog = screen.getByRole("dialog", { name: "Keyboard shortcuts" });
    const all = within(dialog).getAllByTestId("shortcut-row").length;
    fireEvent.change(within(dialog).getByTestId("shortcuts-overlay-search"), {
      target: { value: "palette" },
    });
    const filtered = within(dialog).getAllByTestId("shortcut-row");
    expect(filtered.length).toBeGreaterThan(0);
    expect(filtered.length).toBeLessThan(all);
  });

  it("Escape closes it", () => {
    renderOverlay();
    fireEvent.click(screen.getByRole("button", { name: "Run shortcuts.help" }));
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("dialog", { name: "Keyboard shortcuts" })).not.toBeInTheDocument();
  });

  it("'Customize in Settings' closes the overlay and hands off to the shortcuts section", () => {
    const onCustomize = renderOverlay();
    fireEvent.click(screen.getByRole("button", { name: "Run shortcuts.help" }));
    fireEvent.click(screen.getByRole("button", { name: "Customize in Settings" }));
    expect(screen.queryByRole("dialog", { name: "Keyboard shortcuts" })).not.toBeInTheDocument();
    expect(onCustomize).toHaveBeenCalledTimes(1);
  });
});
