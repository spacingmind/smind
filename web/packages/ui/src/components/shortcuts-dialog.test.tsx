import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ShortcutRows } from "@/components/shortcuts-dialog";
import { KeyboardProvider } from "@/keyboard/keyboard-provider";
import { platformBindings, SECTION_TITLES, SHORTCUT_BINDINGS } from "@/keyboard/shortcuts";

function renderRows(query?: string) {
  return render(
    <KeyboardProvider>
      <ShortcutRows query={query} />
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

describe("ShortcutRows", () => {
  it("lists every registered binding", () => {
    renderRows();
    const rows = screen.getAllByTestId("shortcut-row");
    // jsdom is not a desktop build: the web-only rows render, the
    // desktop-gated Mod+T / Mod+Digit ones do not.
    const webBindings = platformBindings(SHORTCUT_BINDINGS, false, false);
    expect(rows).toHaveLength(webBindings.length);
    expect(rows.map((r) => r.dataset.bindingId).sort()).toEqual(
      webBindings.map((b) => b.id).sort(),
    );
    for (const binding of webBindings) {
      expect(screen.getAllByText(binding.label).length).toBeGreaterThan(0);
    }
  });

  it("groups rows under their section headings", () => {
    renderRows();
    for (const binding of SHORTCUT_BINDINGS) {
      const section = screen.getByTestId(`shortcut-section-${binding.section}`);
      expect(within(section).getByText(SECTION_TITLES[binding.section])).toBeInTheDocument();
      expect(within(section).getAllByText(binding.label).length).toBeGreaterThan(0);
    }
  });

  it("renders each binding's keys", () => {
    renderRows();
    // Non-mac in jsdom, so Ctrl+K renders as three caps.
    const row = screen.getByTestId("shortcut-sections").querySelector<HTMLElement>(
      '[data-binding-id="palette-open"]',
    )!;
    expect(within(row).getByText("Ctrl")).toBeInTheDocument();
    expect(within(row).getByText("K")).toBeInTheDocument();
  });

  it("narrows to rows matching a search query", () => {
    renderRows("close current tab");
    const rowIds = screen.getAllByTestId("shortcut-row").map((r) => r.dataset.bindingId);
    expect(rowIds).toEqual(["tab-close", "tab-close-terminal"]);
  });

  it("keeps every row of a section whose own title matches the query", () => {
    renderRows("tabs");
    // The "Tabs & panes" section title itself matches "tabs" -- every row
    // in it stays, not just ones individually matching the query.
    const tabsSection = screen.getByTestId("shortcut-section-tabs");
    const tabsRowIds = within(tabsSection)
      .getAllByTestId("shortcut-row")
      .map((r) => r.dataset.bindingId);
    expect(tabsRowIds).toEqual(
      platformBindings(SHORTCUT_BINDINGS, false, false)
        .filter((b) => b.section === "tabs")
        .map((b) => b.id),
    );
  });

  it("shows an empty state for a query matching nothing", () => {
    renderRows("this matches no binding at all");
    expect(screen.queryByTestId("shortcut-sections")).not.toBeInTheDocument();
    expect(screen.getByTestId("shortcut-sections-empty")).toBeInTheDocument();
  });

  it("rebinds a shortcut from the captured key press", () => {
    renderRows();
    fireEvent.click(screen.getAllByLabelText("Change shortcut for Open command palette")[0]!);
    expect(screen.getByTestId("shortcut-capturing")).toBeInTheDocument();

    fireEvent.keyDown(window, { key: "j", code: "KeyJ", ctrlKey: true, shiftKey: true });

    const row = screen
      .getByTestId("shortcut-sections")
      .querySelector<HTMLElement>('[data-binding-id="palette-open"]')!;
    expect(within(row).getByText("J")).toBeInTheDocument();
    expect(within(row).getByText("Shift")).toBeInTheDocument();
    expect(within(row).queryByText("K")).not.toBeInTheDocument();
  });

  it("stays in capture for a bare modifier press and cancels on Escape", () => {
    renderRows();
    fireEvent.click(screen.getAllByLabelText("Change shortcut for Open command palette")[0]!);

    fireEvent.keyDown(window, { key: "Shift", shiftKey: true });
    expect(screen.getByTestId("shortcut-capturing")).toBeInTheDocument();

    fireEvent.keyDown(window, { key: "Escape", code: "Escape" });
    expect(screen.queryByTestId("shortcut-capturing")).not.toBeInTheDocument();
    const row = screen
      .getByTestId("shortcut-sections")
      .querySelector<HTMLElement>('[data-binding-id="palette-open"]')!;
    expect(within(row).getByText("K")).toBeInTheDocument();
  });

  it("flags a rebind that collides with another binding", () => {
    renderRows();
    // Put "Close current tab" onto Ctrl+K, which "Open command palette" has.
    fireEvent.click(screen.getAllByLabelText("Change shortcut for Close current tab")[0]!);
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
    renderRows();
    expect(
      screen.queryByLabelText("Reset shortcut for Open command palette"),
    ).not.toBeInTheDocument();

    fireEvent.click(screen.getAllByLabelText("Change shortcut for Open command palette")[0]!);
    fireEvent.keyDown(window, { key: "j", code: "KeyJ", ctrlKey: true });
    fireEvent.click(screen.getAllByLabelText("Reset shortcut for Open command palette")[0]!);

    const row = screen
      .getByTestId("shortcut-sections")
      .querySelector<HTMLElement>('[data-binding-id="palette-open"]')!;
    expect(within(row).getByText("K")).toBeInTheDocument();
    expect(
      screen.queryByLabelText("Reset shortcut for Open command palette"),
    ).not.toBeInTheDocument();
  });

  it("shows Reset all only once something is overridden", () => {
    renderRows();
    expect(screen.queryByText("Reset all to defaults")).not.toBeInTheDocument();

    fireEvent.click(screen.getAllByLabelText("Change shortcut for Open command palette")[0]!);
    fireEvent.keyDown(window, { key: "j", code: "KeyJ", ctrlKey: true });
    fireEvent.click(screen.getByText("Reset all to defaults"));

    expect(screen.queryByText("Reset all to defaults")).not.toBeInTheDocument();
    const row = screen
      .getByTestId("shortcut-sections")
      .querySelector<HTMLElement>('[data-binding-id="palette-open"]')!;
    expect(within(row).getByText("K")).toBeInTheDocument();
  });

  it("records a chord across two key presses and commits on Enter", () => {
    renderRows();
    fireEvent.click(screen.getAllByLabelText("Record a chord for Open command palette")[0]!);
    expect(screen.getByTestId("shortcut-capturing")).toHaveTextContent("Recording a chord");

    fireEvent.keyDown(window, { key: "k", code: "KeyK", ctrlKey: true });
    // Still capturing -- a chord isn't committed by its first step.
    expect(screen.getByTestId("shortcut-capturing")).toBeInTheDocument();

    fireEvent.keyDown(window, { key: "s", code: "KeyS" });
    fireEvent.keyDown(window, { key: "Enter", code: "Enter" });

    const row = screen
      .getByTestId("shortcut-sections")
      .querySelector<HTMLElement>('[data-binding-id="palette-open"]')!;
    // formatChord renders "Ctrl+K then S" off mac; KeyCaps splits display
    // text on "+" only (mac's own separator-less glyphs), so the chord's
    // "then" separator stays inside its own cap rather than being split
    // further: two caps, "Ctrl" and "K then S".
    expect(within(row).getByText("Ctrl")).toBeInTheDocument();
    expect(within(row).getByText("K then S")).toBeInTheDocument();
  });

  it("cancels a chord recording via the Cancel button without committing anything captured so far", () => {
    // Escape itself is a legitimate chord step once recording is underway
    // (it has to be, for a binding whose combo genuinely is Escape) -- the
    // Cancel button, not Escape, is how a chord recording in progress is
    // aborted after its first step.
    renderRows();
    fireEvent.click(screen.getAllByLabelText("Record a chord for Open command palette")[0]!);
    fireEvent.keyDown(window, { key: "k", code: "KeyK", ctrlKey: true });
    fireEvent.click(screen.getAllByLabelText("Cancel rebinding Open command palette")[0]!);

    expect(screen.queryByTestId("shortcut-capturing")).not.toBeInTheDocument();
    const row = screen
      .getByTestId("shortcut-sections")
      .querySelector<HTMLElement>('[data-binding-id="palette-open"]')!;
    expect(within(row).getByText("K")).toBeInTheDocument();
  });

  it("Escape cancels a chord recording that hasn't captured a first step yet", () => {
    renderRows();
    fireEvent.click(screen.getAllByLabelText("Record a chord for Open command palette")[0]!);
    fireEvent.keyDown(window, { key: "Escape", code: "Escape" });

    expect(screen.queryByTestId("shortcut-capturing")).not.toBeInTheDocument();
    const row = screen
      .getByTestId("shortcut-sections")
      .querySelector<HTMLElement>('[data-binding-id="palette-open"]')!;
    expect(within(row).getByText("K")).toBeInTheDocument();
  });
});

describe("terminal hatch rows", () => {
  it("shortcuts-dialog-lists-terminal-hatch-rows", () => {
    renderRows();
    const row = screen.getByTestId("shortcut-sections").querySelector<HTMLElement>(
      '[data-binding-id="palette-open-terminal"]',
    )!;
    expect(within(row).getByText("Shift")).toBeInTheDocument();
    expect(within(row).getByText("P")).toBeInTheDocument();
    expect(within(row).getByText(/while a terminal has focus/)).toBeInTheDocument();
  });

  it("omits them on Mac", () => {
    vi.spyOn(navigator, "platform", "get").mockReturnValue("MacIntel");
    renderRows();
    expect(
      screen.getByTestId("shortcut-sections").querySelector('[data-binding-id="palette-open-terminal"]'),
    ).toBeNull();
  });
});
