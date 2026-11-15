// DT-UI B native pass: the sidebar toggle and the Settings button name their live shortcut in the tooltip.
import { fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { SidebarFooterActions } from "@/components/sidebar-footer";
import { SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

const keyboard = vi.hoisted(() => ({
  runAction: vi.fn(),
  bindings: [] as { action: string; effectiveCombo: string | null }[],
  isMac: false,
}));
vi.mock("@/keyboard/keyboard-provider", () => ({ useKeyboard: () => keyboard }));

beforeEach(() => {
  keyboard.bindings = [];
  keyboard.isMac = false;
});

const footer = () =>
  render(
    <SidebarProvider>
      <SidebarFooterActions providerHealth={null} agentCount={0} onAddWorkspace={() => {}} onOpenSettings={() => {}} />
    </SidebarProvider>,
  );

describe("shortcut tooltips", () => {
  it("the Settings button shows the live settings shortcut, and the bare label when none is bound", () => {
    footer();
    fireEvent.focus(screen.getByTestId("sidebar-settings-button"));
    expect(screen.getByRole("tooltip")).toHaveTextContent(/^Settings$/);
  });

  it("a bound settings shortcut is appended, formatted for the platform", () => {
    keyboard.bindings = [{ action: "settings.open", effectiveCombo: "Mod+," }];
    keyboard.isMac = true;
    footer();
    fireEvent.focus(screen.getByTestId("sidebar-settings-button"));
    expect(screen.getByRole("tooltip")).toHaveTextContent("Settings (⌘,)");
  });

  it("a customised shortcut shows as customised (the resolved binding, not the default)", () => {
    keyboard.bindings = [{ action: "settings.open", effectiveCombo: "Mod+Shift+," }];
    footer();
    fireEvent.focus(screen.getByTestId("sidebar-settings-button"));
    expect(screen.getByRole("tooltip")).toHaveTextContent("Settings (Ctrl+Shift+,)");
  });

  it("the sidebar toggle shows its shortcut", () => {
    keyboard.bindings = [{ action: "sidebar.toggle", effectiveCombo: "Mod+B" }];
    keyboard.isMac = true;
    render(
      <SidebarProvider>
        <SidebarTrigger />
      </SidebarProvider>,
    );
    fireEvent.focus(screen.getByRole("button", { name: "Toggle Sidebar" }));
    expect(screen.getByRole("tooltip")).toHaveTextContent("Toggle sidebar (⌘B)");
  });

  it("the sidebar toggle shows the bare label when the shortcut was removed", () => {
    render(
      <SidebarProvider>
        <SidebarTrigger />
      </SidebarProvider>,
    );
    fireEvent.focus(screen.getByRole("button", { name: "Toggle Sidebar" }));
    expect(screen.getByRole("tooltip")).toHaveTextContent(/^Toggle sidebar$/);
  });
});
