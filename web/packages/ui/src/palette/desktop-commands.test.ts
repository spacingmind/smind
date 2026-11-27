import { describe, expect, it, vi } from "vitest";

import type { NativeMenuActionId } from "@/lib/platform";
import type { ActionId } from "@/keyboard/actions";
import { buildDesktopCommands } from "@/palette/desktop-commands";

// The pure builder with stub deps: no module mocking, no platform constants.
type Deps = Parameters<typeof buildDesktopCommands>[0];

function makeDeps(overrides: Partial<Deps> = {}): Deps {
  return {
    isDesktop: true,
    os: "windows",
    menuAction: vi.fn(() => Promise.resolve()),
    runAction: vi.fn((): boolean => true),
    ...overrides,
  };
}

const NATIVE_TITLES = [
  "Reload",
  "Zoom in",
  "Zoom out",
  "Reset zoom",
  "Toggle fullscreen",
  "Minimize",
  "Hide window",
  "About sMind",
  "Open log folder",
  "sMind on GitHub",
  "Quit sMind",
  "Check for updates",
] as const;

const RENDERER_TITLES = ["Toggle sidebar", "Find in pane", "New tab", "Close tab"] as const;

describe("buildDesktopCommands", () => {
  it("ui_cleanup_palette_lists_native_entries_on_desktop", () => {
    const commands = buildDesktopCommands(makeDeps());
    const titles = commands.map((c) => c.title);
    for (const title of [...NATIVE_TITLES, ...RENDERER_TITLES]) {
      expect(titles, title).toContain(title);
    }
  });

  it("ui_cleanup_palette_entry_runs_its_own_native_command", () => {
    const ids = [
      "reload",
      "zoom-in",
      "zoom-out",
      "zoom-reset",
      "fullscreen",
      "minimize",
      "hide",
      "about",
      "log-folder",
      "github",
      "quit",
      "check-updates",
    ] as const satisfies readonly NativeMenuActionId[];
    for (const id of ids) {
      const deps = makeDeps();
      const entry = buildDesktopCommands(deps).find((c) => c.id === `desktop-${id}`);
      expect(entry, id).toBeDefined();
      entry!.run();
      // Exactly one call, with this entry's id -- no other id, no spillover.
      expect(deps.menuAction).toHaveBeenCalledTimes(1);
      expect(deps.menuAction).toHaveBeenCalledWith(id);
    }
  });

  it("ui_cleanup_palette_entries_absent_on_web", () => {
    expect(buildDesktopCommands(makeDeps({ isDesktop: false, os: null }))).toEqual([]);
  });

  it("ui_cleanup_check_updates_only_on_windows", () => {
    const cases = [
      ["windows", true],
      ["linux", false],
      ["macos", false],
    ] as const;
    for (const [os, present] of cases) {
      const commands = buildDesktopCommands(makeDeps({ os }));
      expect(commands.some((c) => c.id === "desktop-check-updates"), os).toBe(present);
    }
  });

  it("ui_cleanup_palette_renderer_entries_run_actions", () => {
    const deps = makeDeps();
    const commands = buildDesktopCommands(deps);
    const actions = ["sidebar.toggle", "pane.find", "tab.new", "tab.close"] as const satisfies readonly ActionId[];
    for (const action of actions) {
      const entry = commands.find((c) => c.id === `desktop-${action}`);
      expect(entry, action).toBeDefined();
      entry!.run();
      expect(deps.runAction).toHaveBeenLastCalledWith(action, null, { source: "menu" });
    }
    expect(deps.runAction).toHaveBeenCalledTimes(actions.length);
  });
});
