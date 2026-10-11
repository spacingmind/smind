// desktop-native-feel W3: the header app menu (Windows/Linux). `@/lib/platform`
// is a module-load constant, so it is mocked per scenario and the component
// imported fresh, like keyboard/desktop-shortcuts.test.tsx.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
  vi.resetModules();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
});

function mockPlatform(opts: { desktop: boolean; os: "macos" | "windows" | "linux" | null }) {
  const menuAction = vi.fn(() => Promise.resolve());
  vi.doMock("@/lib/platform", () => ({
    isDesktop: opts.desktop,
    desktopOS: opts.os,
    desktop: { menuAction, onMenuAction: () => () => {} },
  }));
  return menuAction;
}

describe("AppMenu", () => {
  it("app-menu button renders only on desktop non-macOS", async () => {
    const cases: [boolean, "macos" | "windows" | "linux" | null, boolean][] = [
      [true, "windows", true],
      [true, "linux", true],
      [true, "macos", false],
      [false, null, false],
    ];
    for (const [desktop, os, shown] of cases) {
      vi.resetModules();
      vi.doUnmock("@/lib/platform");
      mockPlatform({ desktop, os });
      const { AppMenu } = await import("@/components/app-menu");
      const { unmount } = render(<AppMenu />);
      expect(screen.queryByTestId("app-menu-trigger") !== null, `${desktop}/${os}`).toBe(shown);
      unmount();
    }
  });

  it("app menu items dispatch the same actions as the native menu", async () => {
    const menuAction = mockPlatform({ desktop: true, os: "windows" });
    const { AppMenu, APP_MENU_SECTIONS } = await import("@/components/app-menu");
    const { KeyboardProvider, useActionHandler } = await import("@/keyboard/keyboard-provider");

    const fired: string[] = [];
    // Ids menu.rs's `menu_action_for` maps to (pinned by its own test).
    const rendererIds = [
      "tab.new",
      "tab.close",
      "palette.open",
      "pane.find",
      "settings.open",
      "sidebar.toggle",
    ] as const;
    function Claims({ findFocused }: { findFocused: boolean }) {
      useActionHandler("tab.new", () => fired.push("tab.new"));
      useActionHandler("tab.close", () => fired.push("tab.close"));
      useActionHandler("palette.open", () => fired.push("palette.open"));
      // Focus leaves the pane before the menu action arrives: only the menu fallback reaches it.
      useActionHandler("pane.find", () => fired.push("pane.find"), {
        enabled: findFocused,
        menuFallback: true,
      });
      useActionHandler("settings.open", () => fired.push("settings.open"));
      useActionHandler("sidebar.toggle", () => fired.push("sidebar.toggle"));
      return null;
    }
    const tree = (findFocused: boolean) => (
      <KeyboardProvider>
        <Claims findFocused={findFocused} />
        <AppMenu />
      </KeyboardProvider>
    );
    const { rerender } = render(tree(true));
    rerender(tree(false));

    const items = APP_MENU_SECTIONS.flatMap((s) => s.items);
    // The renderer items are exactly the native menu's renderer actions.
    expect(items.flatMap((i) => ("action" in i ? [i.action] : [])).sort()).toEqual(
      [...rendererIds].sort(),
    );

    for (const item of items) {
      fireEvent.pointerDown(screen.getByTestId("app-menu-trigger"), { button: 0 });
      fireEvent.click(await screen.findByTestId(`app-menu-item-${item.testId}`));
      // The item runs once the menu has closed (Radix's close-autofocus).
      await waitFor(() => expect(screen.queryByTestId("app-menu")).toBeNull());
      await new Promise((r) => setTimeout(r, 0));
    }
    // pane.find was enabled:false yet reached its (last focused) pane.
    expect(fired.sort()).toEqual([...rendererIds].sort());
    // Every native-only item went over the single scoped command, by allowlisted id.
    expect((menuAction.mock.calls as unknown as string[][]).map((c) => c[0]).sort()).toEqual(
      items.flatMap((i) => ("native" in i ? [i.native] : [])).sort(),
    );
    expect(menuAction).toHaveBeenCalledWith("quit");
    expect(menuAction).toHaveBeenCalledWith("hide");
  });

  it("app menu does not pull focus back to the trigger after an item runs", async () => {
    mockPlatform({ desktop: true, os: "windows" });
    const { AppMenu } = await import("@/components/app-menu");
    const { KeyboardProvider, useActionHandler } = await import("@/keyboard/keyboard-provider");

    // Stands in for the find bar: the action focuses an input.
    function Claim() {
      useActionHandler("pane.find", () => document.getElementById("find-input")?.focus());
      return <input id="find-input" aria-label="find" />;
    }
    render(
      <KeyboardProvider>
        <Claim />
        <AppMenu />
      </KeyboardProvider>,
    );

    const trigger = screen.getByTestId("app-menu-trigger");
    fireEvent.pointerDown(trigger, { button: 0 });
    fireEvent.click(await screen.findByTestId("app-menu-item-pane.find"));

    await waitFor(() => expect(screen.queryByTestId("app-menu")).toBeNull());
    // Let Radix's close-autofocus run (a microtask/timeout after unmount).
    await new Promise((r) => setTimeout(r, 50));
    expect(document.activeElement).not.toBe(trigger);
    expect(document.activeElement).toBe(document.getElementById("find-input"));
  });
});
