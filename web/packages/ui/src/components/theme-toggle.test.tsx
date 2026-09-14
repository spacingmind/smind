import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { ThemeToggle } from "@/components/theme-toggle";
import { ThemeProvider } from "@/hooks/use-theme";
import { THEME_STORAGE_KEY } from "@/lib/theme";

afterEach(() => {
  window.localStorage.clear();
  document.documentElement.classList.remove("dark");
});

describe("ThemeToggle", () => {
  it("opens a menu listing Light/Dark/System, and clicking one sets the preference", () => {
    render(
      <ThemeProvider>
        <ThemeToggle />
      </ThemeProvider>,
    );

    // Radix's DropdownMenuTrigger opens on pointerdown (so touch
    // press-and-hold works), not click -- see
    // @radix-ui/react-dropdown-menu's Trigger, which composes
    // onOpenToggle into onPointerDown.
    fireEvent.pointerDown(screen.getByTestId("theme-toggle-trigger"), { button: 0 });
    fireEvent.click(screen.getByTestId("theme-option-dark"));

    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("marks the active preference with aria-checked", () => {
    render(
      <ThemeProvider>
        <ThemeToggle />
      </ThemeProvider>,
    );

    // Radix's DropdownMenuTrigger opens on pointerdown (so touch
    // press-and-hold works), not click -- see
    // @radix-ui/react-dropdown-menu's Trigger, which composes
    // onOpenToggle into onPointerDown.
    fireEvent.pointerDown(screen.getByTestId("theme-toggle-trigger"), { button: 0 });
    expect(screen.getByTestId("theme-option-system")).toHaveAttribute("aria-checked", "true");
    expect(screen.getByTestId("theme-option-light")).toHaveAttribute("aria-checked", "false");
  });

  it("renders standalone (no ThemeProvider ancestor) without throwing, matching this codebase's isolated-component-test convention", () => {
    expect(() => render(<ThemeToggle />)).not.toThrow();
  });
});
