import { useMemo, useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { CommandPalette } from "@/components/command-palette";
import { KeyboardProvider, useActionHandler } from "@/keyboard/keyboard-provider";
import { PaletteProvider, useCommands, usePalette } from "@/palette/palette-provider";
import type { Command } from "@/palette/commands";

/** Claims `palette.open` exactly as App.tsx does, so Ctrl+K is exercised end to end. */
function PaletteShortcut() {
  const { open, setOpen } = usePalette();
  useActionHandler("palette.open", () => setOpen(!open));
  return null;
}

/** A source registering a fixed command list -- the "register one in the test" the plan asks for. */
function Source({
  sourceId,
  groupRank,
  commands,
}: {
  sourceId: string;
  groupRank: number;
  commands: Command[];
}) {
  const memo = useMemo(() => commands, [commands]);
  useCommands(sourceId, groupRank, memo);
  return null;
}

function open(): void {
  fireEvent.keyDown(document, { key: "k", code: "KeyK", ctrlKey: true });
}

function rowTitles(): string[] {
  return screen
    .queryAllByTestId("command-palette-row")
    .map((r) => r.querySelector("span span")?.textContent ?? "");
}

function highlighted(): string | null {
  const row = screen
    .queryAllByTestId("command-palette-row")
    .find((r) => r.getAttribute("aria-selected") === "true");
  return row?.querySelector("span span")?.textContent ?? null;
}

const TASKS: Command[] = [
  { id: "t1", group: "Tasks", title: "Fix the bug", subtitle: "fix-bug", run: () => {} },
  { id: "t2", group: "Tasks", title: "Add telemetry", subtitle: "telemetry", run: () => {} },
];

function renderPalette(extra?: { commands: Command[]; sourceId: string; groupRank: number }) {
  return render(
    <KeyboardProvider>
      <PaletteProvider>
        <PaletteShortcut />
        <Source sourceId="tasks" groupRank={0} commands={TASKS} />
        {extra && (
          <Source sourceId={extra.sourceId} groupRank={extra.groupRank} commands={extra.commands} />
        )}
        <CommandPalette />
      </PaletteProvider>
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

describe("CommandPalette", () => {
  it("opens on Ctrl+K and closes on a second press", () => {
    renderPalette();
    expect(screen.queryByTestId("command-palette")).not.toBeInTheDocument();

    open();
    expect(screen.getByTestId("command-palette")).toBeInTheDocument();

    // The palette holds the modal keyboard lock, so this one is handled by
    // the input itself -- still resolved through the binding table.
    fireEvent.keyDown(screen.getByTestId("command-palette-input"), {
      key: "k",
      code: "KeyK",
      ctrlKey: true,
    });
    expect(screen.queryByTestId("command-palette")).not.toBeInTheDocument();
  });

  it("typing filters across every registered source", () => {
    renderPalette({
      sourceId: "actions",
      groupRank: 1,
      commands: [{ id: "a1", group: "Actions", title: "Cycle theme", run: () => {} }],
    });
    open();
    expect(rowTitles()).toEqual(["Fix the bug", "Add telemetry", "Cycle theme"]);

    fireEvent.change(screen.getByTestId("command-palette-input"), { target: { value: "the" } });
    const titles = rowTitles();
    expect(titles).toContain("Fix the bug");
    expect(titles).toContain("Cycle theme");
    expect(titles).not.toContain("Add telemetry");

    fireEvent.change(screen.getByTestId("command-palette-input"), { target: { value: "zzzz" } });
    expect(rowTitles()).toEqual([]);
    expect(screen.getByTestId("command-palette-empty")).toBeInTheDocument();
  });

  it("Enter runs the highlighted entry and closes", () => {
    const run = vi.fn();
    renderPalette({
      sourceId: "actions",
      groupRank: 1,
      commands: [{ id: "a1", group: "Actions", title: "Cycle theme", run }],
    });
    open();

    const input = screen.getByTestId("command-palette-input");
    fireEvent.change(input, { target: { value: "cycle" } });
    expect(highlighted()).toBe("Cycle theme");

    fireEvent.keyDown(input, { key: "Enter" });
    expect(run).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("command-palette")).not.toBeInTheDocument();
  });

  it("clicking a row runs it and closes", () => {
    const run = vi.fn();
    renderPalette({
      sourceId: "actions",
      groupRank: 1,
      commands: [{ id: "a1", group: "Actions", title: "Cycle theme", run }],
    });
    open();
    fireEvent.click(screen.getAllByTestId("command-palette-row")[2]!);
    expect(run).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("command-palette")).not.toBeInTheDocument();
  });

  it("Escape closes and returns focus to the previously focused element", () => {
    render(
      <KeyboardProvider>
        <PaletteProvider>
          <PaletteShortcut />
          <Source sourceId="tasks" groupRank={0} commands={TASKS} />
          <CommandPalette />
          <button data-testid="before">before</button>
        </PaletteProvider>
      </KeyboardProvider>,
    );

    const before = screen.getByTestId("before");
    before.focus();
    expect(document.activeElement).toBe(before);

    open();
    expect(screen.getByTestId("command-palette")).toBeInTheDocument();

    fireEvent.keyDown(screen.getByTestId("command-palette-input"), { key: "Escape" });
    expect(screen.queryByTestId("command-palette")).not.toBeInTheDocument();
    expect(document.activeElement).toBe(before);
  });

  it("arrow navigation moves one command at a time and wraps at both ends", () => {
    renderPalette({
      sourceId: "actions",
      groupRank: 1,
      commands: [{ id: "a1", group: "Actions", title: "Cycle theme", run: () => {} }],
    });
    open();
    const input = screen.getByTestId("command-palette-input");

    expect(highlighted()).toBe("Fix the bug");
    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(highlighted()).toBe("Add telemetry");
    // Crossing into the next group lands on its first *command*, never on
    // the group heading.
    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(highlighted()).toBe("Cycle theme");
    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(highlighted()).toBe("Fix the bug");

    fireEvent.keyDown(input, { key: "ArrowUp" });
    expect(highlighted()).toBe("Cycle theme");
  });

  it("renders group headings, and they are not selectable rows", () => {
    renderPalette({
      sourceId: "actions",
      groupRank: 1,
      commands: [{ id: "a1", group: "Actions", title: "Cycle theme", run: () => {} }],
    });
    open();

    expect(screen.getAllByTestId("command-palette-group").map((g) => g.textContent)).toEqual([
      "Tasks",
      "Actions",
    ]);
    // Three commands, three rows -- headings add no navigable positions.
    expect(screen.getAllByTestId("command-palette-row")).toHaveLength(3);
  });

  it("a contribution registered by another component appears without the palette being modified", () => {
    renderPalette({
      sourceId: "some-other-track",
      groupRank: 9,
      commands: [
        { id: "x", group: "Elsewhere", title: "A command from another surface", run: () => {} },
      ],
    });
    open();
    expect(rowTitles()).toContain("A command from another surface");
    expect(screen.getAllByTestId("command-palette-group").map((g) => g.textContent)).toContain(
      "Elsewhere",
    );
  });

  it("a source that unmounts stops contributing", () => {
    function Host() {
      const [mounted, setMounted] = useState(true);
      const commands = useMemo<Command[]>(
        () => [{ id: "x", group: "Elsewhere", title: "Transient", run: () => {} }],
        [],
      );
      return (
        <>
          {mounted && <Source sourceId="transient" groupRank={9} commands={commands} />}
          <button onClick={() => setMounted(false)}>unmount</button>
        </>
      );
    }
    render(
      <KeyboardProvider>
        <PaletteProvider>
          <PaletteShortcut />
          <Source sourceId="tasks" groupRank={0} commands={TASKS} />
          <Host />
          <CommandPalette />
        </PaletteProvider>
      </KeyboardProvider>,
    );

    open();
    expect(rowTitles()).toContain("Transient");
    fireEvent.keyDown(screen.getByTestId("command-palette-input"), { key: "Escape" });

    fireEvent.click(screen.getByText("unmount"));
    open();
    expect(rowTitles()).not.toContain("Transient");
  });

  it("renders the current shortcut for a command that maps to a keyboard action", () => {
    renderPalette({
      sourceId: "actions",
      groupRank: 1,
      commands: [
        { id: "a1", group: "Actions", title: "Cycle theme", action: "theme.cycle", run: () => {} },
      ],
    });
    open();
    // Non-mac in jsdom.
    expect(screen.getByText("Ctrl+Alt+T")).toBeInTheDocument();
  });

  it("resets its query each time it opens", () => {
    renderPalette();
    open();
    const input = screen.getByTestId("command-palette-input");
    fireEvent.change(input, { target: { value: "telemetry" } });
    expect(rowTitles()).toEqual(["Add telemetry"]);

    fireEvent.keyDown(input, { key: "Escape" });
    open();
    expect(screen.getByTestId("command-palette-input")).toHaveValue("");
    expect(rowTitles()).toEqual(["Fix the bug", "Add telemetry"]);
  });

  it("holds the modal keyboard lock, so no other shortcut fires while it is open", () => {
    const help = vi.fn();
    function Underneath() {
      useActionHandler("shortcuts.help", help);
      return null;
    }
    render(
      <KeyboardProvider>
        <PaletteProvider>
          <PaletteShortcut />
          <Underneath />
          <Source sourceId="tasks" groupRank={0} commands={TASKS} />
          <CommandPalette />
        </PaletteProvider>
      </KeyboardProvider>,
    );

    open();
    fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });
    expect(help).not.toHaveBeenCalled();
  });
});
