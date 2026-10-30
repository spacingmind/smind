import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { SettingsScreen } from "@/components/settings/settings-screen";
import { KeyboardProvider } from "@/keyboard/keyboard-provider";
import { SHORTCUT_OVERRIDES_STORAGE_KEY } from "@/keyboard/overrides";
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
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveAttribute("data-binding-id", "tab-close");
  });

  it("renders Command, Keybinding, Scope and Actions column headers", () => {
    openShortcuts();

    expect(screen.getByRole("columnheader", { name: "Command" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Keybinding" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Scope" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Actions" })).toBeInTheDocument();
  });

  it("Reset all appears only with overrides", () => {
    const ui = (
      <KeyboardProvider>
        <SettingsScreen client={null} onNavigateBack={() => {}} initialSectionId="shortcuts" />
      </KeyboardProvider>
    );
    render(ui);
    expect(screen.queryByText("Reset all to defaults")).toBeNull();

    // `KeyboardProvider` (owned by App) is what reads overrides, and a key
    // jsdom can synthesize isn't in SHORTCUT_KEY_NAMES, so seed its storage.
    window.localStorage.setItem(
      SHORTCUT_OVERRIDES_STORAGE_KEY,
      JSON.stringify({ "tab-close": "F9" }),
    );
    cleanup();
    render(ui);

    expect(screen.getByText("Reset all to defaults")).toBeInTheDocument();
  });

  it("clearing the search box restores the full list", () => {
    openShortcuts();
    const search = screen.getByTestId("settings-shortcuts-search");

    fireEvent.change(search, { target: { value: "close current tab" } });
    expect(screen.getAllByTestId("shortcut-row")).toHaveLength(2);

    fireEvent.change(search, { target: { value: "" } });
    expect(screen.getAllByTestId("shortcut-row")).toHaveLength(SHORTCUT_BINDINGS.length);
  });
});
