import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { resolveKeyBarRows, TerminalKeyBar } from "@/components/terminal-key-bar";

function renderBar(overrides: Partial<React.ComponentProps<typeof TerminalKeyBar>> = {}) {
  const props = {
    modifiers: { ctrl: false, shift: false, alt: false },
    onToggleModifier: vi.fn(),
    onKey: vi.fn(),
    onCopy: vi.fn(),
    onPaste: vi.fn(),
    onToggleKeyboard: vi.fn(),
    ...overrides,
  };
  render(<TerminalKeyBar {...props} />);
  return props;
}

describe("TerminalKeyBar", () => {
  it("resolveKeyBarRows picks the layout by width", () => {
    expect(resolveKeyBarRows(299)).toHaveLength(4);
    expect(resolveKeyBarRows(400)).toHaveLength(2);
    expect(resolveKeyBarRows(660)).toHaveLength(1);
    expect(resolveKeyBarRows(0)).toHaveLength(2);
  });

  it("reports DOM keys for the plain keys", () => {
    const props = renderBar();
    for (const [id, key] of [
      ["esc", "Escape"],
      ["tab", "Tab"],
      ["enter", "Enter"],
      ["up", "ArrowUp"],
      ["down", "ArrowDown"],
      ["left", "ArrowLeft"],
      ["right", "ArrowRight"],
    ]) {
      fireEvent.click(screen.getByTestId(`terminal-key-${id}`));
      expect(props.onKey).toHaveBeenLastCalledWith(key);
    }
  });

  it("shows an armed modifier as pressed and toggles it", () => {
    const props = renderBar({ modifiers: { ctrl: true, shift: false, alt: false } });
    expect(screen.getByTestId("terminal-key-ctrl")).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByTestId("terminal-key-alt")).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(screen.getByTestId("terminal-key-alt"));
    expect(props.onToggleModifier).toHaveBeenCalledWith("alt");
  });

  it("keeps focus on the terminal when a key is pressed", () => {
    renderBar();
    expect(fireEvent.mouseDown(screen.getByTestId("terminal-key-esc"))).toBe(false);
  });

  it("the bar's Copy and Paste buttons still work", () => {
    const props = renderBar({ copyDisabled: true });
    expect(screen.getByTestId("terminal-copy")).toBeDisabled();
    fireEvent.click(screen.getByTestId("terminal-paste"));
    expect(props.onPaste).toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("terminal-keyboard-toggle"));
    expect(props.onToggleKeyboard).toHaveBeenCalled();
  });
});
