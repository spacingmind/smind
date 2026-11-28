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

describe("Switch on Astryx", () => {
  it("switch_toggles_and_keeps_data_state", () => {
    const onCheckedChange = vi.fn();
    const { container } = render(
      <Switch aria-label="Flag" className="my-extra" onCheckedChange={onCheckedChange} />,
    );
    const el = screen.getByRole("switch", { name: "Flag" });
    expect(el).toHaveAttribute("aria-checked", "false");
    expect(el).toHaveAttribute("data-state", "unchecked");
    expect(el).toHaveAttribute("data-slot", "switch");
    expect(container.querySelectorAll('[data-slot="switch"]')).toHaveLength(1);
    expect(container.querySelector(".my-extra")).not.toBeNull();

    fireEvent.click(el);
    expect(onCheckedChange).toHaveBeenLastCalledWith(true);
    expect(screen.getByRole("switch", { name: "Flag" })).toHaveAttribute("data-state", "checked");
    expect(screen.getByRole("switch", { name: "Flag" })).toHaveAttribute("aria-checked", "true");

    fireEvent.click(screen.getByRole("switch", { name: "Flag" }));
    expect(onCheckedChange).toHaveBeenLastCalledWith(false);
    expect(screen.getByRole("switch", { name: "Flag" })).toHaveAttribute("data-state", "unchecked");
  });

  it("disabled switch does not toggle", () => {
    const onCheckedChange = vi.fn();
    render(<Switch aria-label="Off" disabled onCheckedChange={onCheckedChange} />);
    const el = screen.getByRole("switch", { name: "Off" });
    fireEvent.click(el);
    expect(onCheckedChange).not.toHaveBeenCalled();
    expect(el).toHaveAttribute("aria-checked", "false");
  });
});
