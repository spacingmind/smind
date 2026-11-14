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
    fireEvent.click(screen.getByRole("button", { name: "Remove Terminal selection · 3 lines · 14 B" }));
    expect(onRemove).toHaveBeenCalledWith("chip-1");
    expect(onRemove).toHaveBeenCalledTimes(1);
  });

  it("Backspace on a focused chip removes it", () => {
    const onRemove = vi.fn();
    render(<ContextChipTray chips={CHIPS} onRemove={onRemove} />);
    fireEvent.keyDown(screen.getByRole("group", { name: "handler.go L1–4 · 78 B" }), { key: "Backspace" });
    expect(onRemove).toHaveBeenCalledWith("chip-2");
  });
});
