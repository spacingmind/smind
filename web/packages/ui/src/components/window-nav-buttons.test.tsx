import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { WindowNavButtons } from "@/components/window-nav-buttons";

// `nav.back` / `nav.forward` are not in the binding table on this branch yet
// (another task adds the desktop-only rows), so the chord test supplies them.
const keyboard = vi.hoisted(() => ({
  bindings: [] as { action: string; effectiveCombo: string | null }[],
  isMac: false,
}));

vi.mock("@/keyboard/keyboard-provider", () => ({
  useKeyboard: () => keyboard,
}));

function mount(overrides: Partial<{ canBack: boolean; canForward: boolean }> = {}) {
  const onBack = vi.fn();
  const onForward = vi.fn();
  const view = render(
    <WindowNavButtons
      canBack={overrides.canBack ?? true}
      canForward={overrides.canForward ?? true}
      onBack={onBack}
      onForward={onForward}
    />,
  );
  return { view, onBack, onForward };
}

beforeEach(() => {
  keyboard.bindings = [];
  keyboard.isMac = false;
});

describe("WindowNavButtons", () => {
  it("renders back and forward with labels", () => {
    mount();
    expect(screen.getByRole("button", { name: "Back" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Forward" })).toBeInTheDocument();
  });

  it("both disabled when history is empty", () => {
    mount({ canBack: false, canForward: false });
    expect(screen.getByTestId("window-nav-back")).toBeDisabled();
    expect(screen.getByTestId("window-nav-forward")).toBeDisabled();
  });

  it("click calls the handlers", () => {
    const enabled = mount();
    fireEvent.click(screen.getByTestId("window-nav-back"));
    expect(enabled.onBack).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByTestId("window-nav-forward"));
    expect(enabled.onForward).toHaveBeenCalledTimes(1);

    // A disabled button swallows the click rather than firing the handler.
    enabled.view.unmount();
    const empty = mount({ canBack: false, canForward: false });
    fireEvent.click(screen.getByTestId("window-nav-back"));
    fireEvent.click(screen.getByTestId("window-nav-forward"));
    expect(empty.onBack).not.toHaveBeenCalled();
    expect(empty.onForward).not.toHaveBeenCalled();
  });

  it("tooltip shows the chord", () => {
    keyboard.bindings = [
      { action: "nav.back", effectiveCombo: "Alt+ArrowLeft" },
      { action: "nav.forward", effectiveCombo: "Alt+ArrowRight" },
    ];
    mount();
    fireEvent.focus(screen.getByTestId("window-nav-back"));
    expect(screen.getByRole("tooltip")).toHaveTextContent("Back (Alt+←)");
    fireEvent.focus(screen.getByTestId("window-nav-forward"));
    expect(screen.getByRole("tooltip")).toHaveTextContent("Forward (Alt+→)");
  });
});
