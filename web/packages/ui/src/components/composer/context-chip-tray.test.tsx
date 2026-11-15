import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ContextChipTray } from "@/components/composer/context-chip-tray";
import type { ContextChip } from "@/lib/send-set";

afterEach(cleanup);

const CHIPS: ContextChip[] = [
  { localId: "chip-1", kind: "terminal", body: "one\ntwo\nthree", blockLabel: "selection", label: "Terminal selection · 3 lines · 14 B" },
  { localId: "chip-2", kind: "file_ref", ref: { path: "src/handler.go", line: 1, endLine: 4 }, label: "handler.go L1–4 · 78 B" },
];

describe("ContextChipTray (DT3-D)", () => {
  it("renders each chip's label with its kind", () => {
    render(<ContextChipTray chips={CHIPS} onRemove={() => {}} />);
    expect(screen.getByTestId("context-chip-tray")).toBeInTheDocument();
    const chips = screen.getAllByTestId("context-chip");
    expect(chips.map((c) => c.dataset.kind)).toEqual(["terminal", "file_ref"]);
    expect(chips[0]).toHaveTextContent("Terminal selection · 3 lines · 14 B");
    expect(chips[1]).toHaveTextContent("handler.go L1–4 · 78 B");
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
  });

  it("renders nothing for an empty list", () => {
    const { container } = render(<ContextChipTray chips={[]} onRemove={() => {}} />);
    expect(screen.queryByTestId("context-chip-tray")).not.toBeInTheDocument();
    expect(container.firstChild).toBeNull();
  });

  it("the close button removes its chip by id", () => {
    const onRemove = vi.fn();
    render(<ContextChipTray chips={CHIPS} onRemove={onRemove} />);
    fireEvent.click(screen.getByRole("button", { name: "Remove terminal selection" }));
    expect(onRemove).toHaveBeenCalledWith("chip-1");
    expect(onRemove).toHaveBeenCalledTimes(1);
  });

  it("the remove control is always visible, keyboard reachable and 24px", () => {
    render(<ContextChipTray chips={CHIPS} onRemove={() => {}} />);
    for (const close of screen.getAllByTestId("context-chip-close")) {
      // No hover-only styling: nothing hides it, so touch and keyboard users always see it.
      expect(close.className).not.toMatch(/opacity-0|hidden|invisible/);
      expect(close.getAttribute("tabindex")).not.toBe("-1");
      expect(close.className).toMatch(/\bsize-6\b/);
    }
  });

  it("the remove control names what it removes, per kind", () => {
    const chips: ContextChip[] = [
      CHIPS[0]!,
      { localId: "chip-3", kind: "terminal", body: "x", blockLabel: "last 100 lines", label: "Terminal output · 1 line · 1 B" },
      CHIPS[1]!,
    ];
    render(<ContextChipTray chips={chips} onRemove={() => {}} />);
    expect(screen.getAllByTestId("context-chip-close").map((b) => b.getAttribute("aria-label"))).toEqual([
      "Remove terminal selection",
      "Remove terminal output",
      "Remove file reference",
    ]);
  });

  it("Backspace on a focused chip removes it", () => {
    const onRemove = vi.fn();
    render(<ContextChipTray chips={CHIPS} onRemove={onRemove} />);
    fireEvent.keyDown(screen.getByRole("group", { name: "handler.go L1–4 · 78 B" }), { key: "Backspace" });
    expect(onRemove).toHaveBeenCalledWith("chip-2");
  });
});
