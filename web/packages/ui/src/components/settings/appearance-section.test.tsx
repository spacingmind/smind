import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { AppearanceSection } from "@/components/settings/appearance-section";

/** Extracts the one `text-ui-*` font-size utility present in a className string, if any. */
function textUiSize(className: string): string | undefined {
  return className.match(/\btext-ui-(?:xl|lg|base|caption|sm|xs)\b/)?.[0];
}

afterEach(() => {
  window.localStorage.clear();
  document.documentElement.className = "";
  document.documentElement.style.cssText = "";
});

/**
 * Regression coverage for the tailwind-merge bug lib/utils.ts's cn() comment
 * describes: before `text-ui-*` was registered under tailwind-merge's
 * `font-size` group, appending a bare `text-{color}` utility after a
 * `text-ui-*` size class in the same cn() call silently dropped the size
 * (mis-resolved as a `text-color` conflict). The StepGroup segmented
 * control's selected button is exactly this shape -- its Button base
 * classes include `text-ui-base`, and the selected variant appends
 * `bg-selected` (no color class, so it's safe today), but the *un*selected
 * siblings and the selected one must still land on the identical
 * `text-ui-*` token, not one inherited from ambient context.
 */
describe("AppearanceSection StepGroup", () => {
  it("gives the selected and unselected theme step buttons the same text-ui-* size", () => {
    render(<AppearanceSection />);

    const selected = screen.getByTestId("settings-theme-system"); // default preference is "system"
    const unselected = screen.getByTestId("settings-theme-light");

    expect(selected).toHaveAttribute("aria-pressed", "true");
    expect(unselected).toHaveAttribute("aria-pressed", "false");

    const selectedSize = textUiSize(selected.className);
    const unselectedSize = textUiSize(unselected.className);

    expect(selectedSize).toBeDefined();
    expect(selectedSize).toBe(unselectedSize);
  });

  it("gives every font-size axis's step buttons the same text-ui-* size regardless of which is selected", () => {
    render(<AppearanceSection />);

    for (const axis of ["interface", "content", "code"]) {
      const small = screen.getByTestId(`settings-font-size-${axis}-small`);
      const medium = screen.getByTestId(`settings-font-size-${axis}-medium`);
      const large = screen.getByTestId(`settings-font-size-${axis}-large`);
      const sizes = [small, medium, large].map((el) => textUiSize(el.className));

      expect(sizes.every((s) => s !== undefined)).toBe(true);
      expect(new Set(sizes).size).toBe(1);
    }
  });
});
