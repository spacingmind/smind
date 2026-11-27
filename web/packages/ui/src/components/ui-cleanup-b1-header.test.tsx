// UI cleanup B1 (docs/plans/active/ui-cleanup-b1.md H2a): the header keyboard
// icon is gone, but the `?` keystroke still opens the shortcuts overlay and
// the terminal key bar keeps its keyboard toggle on its own registry key.
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { TerminalKeyBar } from "@/components/terminal-key-bar";
import { KeyboardProvider, useActionHandler } from "@/keyboard/keyboard-provider";

function Claim({ onFire }: { onFire: (payload: unknown) => void }) {
  useActionHandler("shortcuts.help", onFire);
  return null;
}

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

describe("UI cleanup B1 header", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("ui_cleanup_shortcuts_help_keystroke_still_opens_overlay", () => {
    const spy = vi.fn();
    render(
      <KeyboardProvider>
        <Claim onFire={spy} />
      </KeyboardProvider>,
    );

    fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it("ui_cleanup_terminal_key_bar_keeps_its_keyboard_toggle", () => {
    renderBar();
    const toggle = screen.getByTestId("terminal-keyboard-toggle");
    expect(toggle).toBeInTheDocument();
    const svg = toggle.querySelector("svg");
    expect(svg?.getAttribute("class")).toContain("lucide-keyboard");
  });
});
