import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { SettingsScreen } from "@/components/settings/settings-screen";
import { SHORTCUT_BINDINGS } from "@/keyboard/shortcuts";

afterEach(() => {
  window.localStorage.clear();
});

function openShortcuts() {
  render(<SettingsScreen client={null} onNavigateBack={() => {}} initialSectionId="shortcuts" />);
}

describe("Settings Shortcuts section", () => {
  it("is registered and reachable from the section nav", () => {
    render(<SettingsScreen client={null} onNavigateBack={() => {}} />);
    expect(screen.getByTestId("settings-nav-shortcuts")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("settings-nav-shortcuts"));
    expect(screen.getByTestId("settings-section-shortcuts")).toBeInTheDocument();
  });

  it("lists every binding by default", () => {
    openShortcuts();
    expect(screen.getAllByTestId("shortcut-row")).toHaveLength(SHORTCUT_BINDINGS.length);
  });

  it("typing in the search box narrows the list", () => {
    openShortcuts();

    fireEvent.change(screen.getByTestId("settings-shortcuts-search"), {
      target: { value: "close current tab" },
    });

    const rows = screen.getAllByTestId("shortcut-row");
    expect(rows).toHaveLength(1);
    expect(rows[0]).toHaveAttribute("data-binding-id", "tab-close");
  });

  it("clearing the search box restores the full list", () => {
    openShortcuts();
    const search = screen.getByTestId("settings-shortcuts-search");

    fireEvent.change(search, { target: { value: "close current tab" } });
    expect(screen.getAllByTestId("shortcut-row")).toHaveLength(1);

    fireEvent.change(search, { target: { value: "" } });
    expect(screen.getAllByTestId("shortcut-row")).toHaveLength(SHORTCUT_BINDINGS.length);
  });
});
