import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { Switch } from "@/components/ui/switch";

describe("switch", () => {
  it("Switch toggles and reports checked state", () => {
    const onCheckedChange = vi.fn();
    const { unmount } = render(
      <Switch aria-label="Public" onCheckedChange={onCheckedChange} />
    );

    fireEvent.click(screen.getByRole("switch", { name: "Public" }));
    expect(screen.getByRole("switch", { name: "Public" })).toHaveAttribute("aria-checked", "true");
    expect(onCheckedChange).toHaveBeenCalledWith(true);

    unmount();

    render(
      <Switch
        aria-label="Public"
        checked={true}
        onCheckedChange={() => {}}
      />
    );
    expect(screen.getByRole("switch", { name: "Public" })).toHaveAttribute("aria-checked", "true");
  });

  it("Switch is keyboard operable and disabled when asked", () => {
    const onCheckedChange = vi.fn();
    const { unmount } = render(
      <Switch aria-label="Autoplay" onCheckedChange={onCheckedChange} />
    );

    const toggle = screen.getByRole("switch", { name: "Autoplay" });
    toggle.focus();
    expect(toggle).toHaveFocus();
    // jsdom does not synthesize the click a real browser fires when Space
    // activates a <button>, so the keydown and the activation it produces
    // are both dispatched here.
    fireEvent.keyDown(toggle, { key: " " });
    fireEvent.click(toggle);
    expect(screen.getByRole("switch", { name: "Autoplay" })).toHaveAttribute("aria-checked", "true");
    expect(onCheckedChange).toHaveBeenCalledWith(true);

    unmount();

    const disabledChange = vi.fn();
    render(
      <Switch aria-label="Autoplay" disabled onCheckedChange={disabledChange} />
    );
    fireEvent.click(screen.getByRole("switch", { name: "Autoplay" }));
    expect(disabledChange).not.toHaveBeenCalled();
  });
});
